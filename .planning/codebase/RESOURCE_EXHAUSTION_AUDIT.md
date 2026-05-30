# Resource Exhaustion Audit: Multi-User Load

**Analysis Date:** Fri May 29 2026

**Scope:** `cmd/server/main.go`, `internal/service/room_manager.go`, `internal/api/websocket.go`, `internal/api/routes.go`, `internal/warwick/`, `internal/db/db.go`, `internal/db/repository.go`, `internal/domain/room.go`

**Method:** Production-First v2 framework — each resource analyzed for consumption pattern, production threshold, failure mode.

---

## 1. Goroutine Leak Potential

### Consumption Pattern

| Source | Goroutines per unit | Scaling |
|--------|-------------------|---------|
| Room worker (`runRoomWorker`) | 1 per running room | Rooms × 1 |
| WS write pump | 1 per WS conn | Clients × 1 |
| WS read pump | 1 per WS conn | Clients × 1 |
| WS event forwarder | 1 per WS conn (leaked) | Clients × 1 |
| `Subscribe()` fan-out goroutine | 1 per WS conn (leaked) | Clients × 1 |
| Async DB writes | 0–2 per fetch/stop | ~req rate × 2 |

### Measured: 100 users + 50 rooms

If 50 rooms running: 50 workers. If 100 WS clients: 100 write pumps + 100 read pumps + 100 event forwarders + 100 subscriber goroutines = 450 goroutines total. But the leak compounds.

### Leak Path Confirmation

**Leak 1 — `Subscribe()` goroutine (`room_manager.go:44-53`):**
```go
func (rm *RoomManager) Subscribe() <-chan RoomManagerEvent {
    ch := make(chan RoomManagerEvent, 256)
    go func() {
        for event := range rm.eventCh {  // rm.eventCh NEVER closed
            select {
            case ch <- event:
            default:
                slog.Warn("dropping event for slow subscriber")
            }
        }
        close(ch)  // NEVER reached
    }()
    return ch
}
```
`rm.eventCh` is allocated in `NewRoomManager` (`make(chan RoomManagerEvent, 100)`) and never closed anywhere in the codebase. The goroutine runs **forever**.

**Leak 2 — WS event forwarder (`websocket.go:72-82`):**
```go
go func() {
    for event := range events {  // 'events' channel NEVER closed
        data := marshalEvent(event)
        select {
        case client.send <- data:
        default:
            slog.Warn("dropping event for slow ws client")
        }
    }
    close(client.send)
}()
```
The `events` channel (from `Subscribe()`) is never closed because the `Subscribe()` goroutine never exits. This goroutine also runs **forever**.

**Leak 3 — Write pump stuck (`websocket.go:47-58`):**
```go
for msg := range client.send {  // client.send closed by event forwarder — which never exits
```
Since event forwarder never closes `client.send`, the write pump cannot exit via channel closure. It can only exit if `client.conn.Write()` returns an error (client disconnects). But even after write pump exits, the error-threshold depends on the websocket library's write deadline behavior.

**Leak 4 — Async DB writes (`room_manager.go:256-260, 284-288`):**
```go
go func() {
    if _, err := rm.repository.UpdateRoom(roomCopy); err != nil {
```
Fire-and-forget goroutines on every QR fetch and status change. No WaitGroup, no tracking. They use `context.Background()` so they won't be cancelled on shutdown. A burst of 1000 room operations = 1000 transient goroutines living for query duration.

### Rapid Create/Delete Cycle

```go
func (rm *RoomManager) DeleteRoom(roomID string) error {
    // ...
    if state.cancel != nil {
        state.cancel()  // Marks ctx done
    }
    delete(rm.rooms, roomID)
```

On `DeleteRoom`, `state.cancel()` is called. The worker goroutine will exit on the **next** iteration's `case <-state.ctx.Done()`. BUT: if the worker is currently blocked on `rm.qrClient.FetchQR(classID)` (HTTP call with 30s timeout), the context cancellation is **not propagated** — `FetchQR` uses its own `http.Client` with no context parameter. The worker stays alive up to 30s after room deletion.

**Same issue in `StopRoom`** (`room_manager.go:163-190`): cancel called, but in-flight HTTP fetch is not interrupted.

### Severity

**Critical.** Each WS connection that opens and closes leaks 2 goroutines (`Subscribe` fan-out + event forwarder) permanently. Under repeated reconnect (e.g., mobile clients, network flaky), goroutine count grows without bound. Async DB write goroutines are transient but unmanaged.

### Fix

1. Close `rm.eventCh` on `RoomManager` shutdown (attach to server lifecycle).
2. Use `context.WithCancel` on the subscriber: pass cancel to WS handler, call cancel on disconnect.
3. Make `FetchQR` take a `context.Context` parameter so in-flight HTTP can be cancelled on room delete/stop.
4. Track async DB writes with a `sync.WaitGroup` and await on graceful shutdown.

---

## 2. Memory: Unbounded Room Workers

### Consumption Pattern

`RoomManager.rooms` is a `map[string]*RoomState` with **no limit**. Any authenticated user can create rooms via `POST /api/rooms` and `POST /api/rooms/from-session`. Room IDs are generated via `uuid.New().String()` so there is no practical collision/backpressure.

**Per-room memory cost (running):**

| Component | Size |
|-----------|------|
| `RoomState` struct + `domain.Room` | ~400 bytes |
| Map entry overhead | ~100 bytes |
| `context.Context` + `CancelFunc` | ~200 bytes |
| **Total per room** | **~700 bytes** |
| 10,000 rooms (idle) | ~7 MB |
| 10,000 running rooms | +10,000 goroutine stacks (~8 KB each = ~80 MB) |

### Production Threshold

At 10,000 rooms, memory is ~87 MB — not alarming for a server with 512 MB+. The bigger issue is the goroutine count (10k workers) and QR fetch load (see #5).

### Failure Mode at 100,000 Rooms

- Map grows to ~70 MB
- 100k goroutine stacks (8 KB each) = ~800 MB
- `GetAllRooms()` copies all rooms: O(n) memory allocation each call

### Severity

**Medium.** Memory itself is manageable but the unbounded resource creation enables a trivial DoS: call `POST /api/rooms` in a loop. Each room entry is a persistent DB row + in-memory state. Even idle rooms consume map entries.

### Fix

- Add `maxRooms` config (default 1000).
- Enforce per-user rate limit on room creation.
- Reject creation when `len(rm.rooms) >= maxRooms`.

---

## 3. Memory: WebSocket Client Buffers

### Per-Client Buffer Chain

```
WS client
├── send chan []byte  (buffer: 256)   ← event forwarder writes to this
└── events chan       (buffer: 256)   ← Subscribe() returns this
```

Each buffer slot holds a JSON-marshalled `RoomManagerEvent`. Typical event size: ~500–1500 bytes. Assume ~1 KB average.

**Per-client memory:**
- `send channel`: 256 slots × 1 KB = 256 KB
- `events channel` (in subscriber): 256 slots × 1 KB = 256 KB
- **Total per connection: ~512 KB** (just channel buffers)

### Scaling Table

| Connections | Channel buffer memory |
|-------------|----------------------|
| 100 | ~50 MB |
| 1,000 | ~500 MB |
| 5,000 | ~2.5 GB |
| 10,000 | ~5 GB |

### Blocking Behavior

When a client is slow, both channels fill to capacity. After buffer is full:
- Event forwarder hits `default` branch and **drops events** (log warning)
- Subscriber goroutine hits `default` branch and **drops events** (log warning)
- No goroutine blocks indefinitely (select-with-default prevents this)

So the backpressure mechanism works *per-channel*. But at 10k connections, every `rm.emit()` triggers 10k subscriber goroutines, each trying to write to their channel. When many channels are full, the subscriber goroutines do quick `default` drops — high CPU but no blocking.

### Severity

**High.** At 1,000+ concurrent WS connections, memory for channel buffers alone hits ~500 MB. At 10,000, it exceeds typical container memory limits (1–2 GB). The subscriber channels are allocated per-connection and never freed (goroutine leak #1 means the subscriber goroutine + its channel live forever even after disconnect).

### Fix

- Reduce buffer sizes from 256 to 64 or 32 (tunable).
- Fix goroutine leak to free channels on disconnect.
- Add max WS connections limit (configurable, default 1000).

---

## 4. HTTP Connection Pool to Warwick

### Current Configuration

Three separate `http.Client` instances, each with **default Go transport**:

| Client | File | Connection pool |
|--------|------|----------------|
| `WarwickAuth.client` | `internal/warwick/auth.go:37` | Default Transport: MaxIdleConns=100, MaxIdleConnsPerHost=2, MaxConnsPerHost=0 (unlimited) |
| `WarwickQrClient.client` | `internal/warwick/client.go:26` | Same defaults |
| `ClassroomClient.client` | `internal/warwick/classroom_client.go:30` | Same defaults |

**Key defaults (Go 1.22 `http.DefaultTransport`):**
- `MaxIdleConns`: 100 (total across all hosts)
- `MaxIdleConnsPerHost`: 2
- `MaxConnsPerHost`: 0 (**unlimited**)
- `IdleConnTimeout`: 90s
- `DisableKeepAlives`: false

### Load Calculation (100 rooms)

Each room worker calls `FetchQR` every ~45 seconds (75% of 60s TTL). 100 rooms = ~2.2 fetches/second.

Each fetch: POST to `https://warwick.humantix.cloud/admin/ClassAttendance/GetQRCode`.

With 500ms average response latency: ~1–2 concurrent connections to Warwick at steady state. With `MaxIdleConnsPerHost=2`, this fits within idle pool. No connection exhaustion.

### At 1,000 rooms

~22 fetches/second. With 500ms latency: ~11 concurrent connections. `MaxConnsPerHost=0` allows this. `MaxIdleConnsPerHost=2` means only 2 connections will be kept alive; the other ~9 connections per burst will be **created fresh** each cycle (TCP + TLS handshake). Handshake adds latency and server load.

### At 10,000 rooms

~222 fetches/second. With 500ms latency: ~111 concurrent connections to Warwick. **Unlimited `MaxConnsPerHost` means no upper bound.** If Warwick slows under load (1s latency): ~222 concurrent connections. This could exhaust Warwick's connection pool.

### Classroom Client User Traffic

Separate from room workers: user-initiated API calls (`GET /api/teacher/courses`, `GET /api/teacher/courses/{id}/sessions/{id}`, etc.) use `ClassroomClient`. Under 100 concurrent users browsing courses, each request creates a new connection pool entry. With `MaxIdleConns=100` total, this is sufficient.

### Severity

**Medium.** At moderate scale (1,000 rooms), the unlimited `MaxConnsPerHost` to Warwick is a risk. If Warwick becomes slow, in-flight connections pile up without bound. Three separate clients mean three separate connection pools — none of which cap concurrency to the same origin.

### Fix

- **Share one `http.Client`** across all three Warwick consumers (or use a shared transport).
- Set `MaxConnsPerHost` to a reasonable value (e.g., 20–50) to prevent overwhelming Warwick.
- Set `MaxIdleConnsPerHost` to match expected concurrency.
- Add a circuit breaker / retry with backoff for Warwick API calls.

---

## 5. CPU: QR Refetch Loop

### Mechanics

```go
// room_manager.go:217-218
defaultTTL := uint64(60)
shouldFetch := expiresAt == nil || now.After(expiresAt.Add(-time.Duration(45)*time.Second))
```

`CalculateNextFetchDelay(60)` = 45 seconds. Each room worker loops every 1 second, checks if refetch is due, and fires a synchronous HTTP call if so.

### Fetch Rate

| Rooms | Fetches/second | CPU profile per fetch |
|-------|---------------|----------------------|
| 100 | ~2.2 | 1 POST + JSON parse + 1 DB write |
| 1,000 | ~22 | 22 POSTs + 22 parses + 22 DB writes |
| 5,000 | ~111 | 111 POSTs + 111 parses + 111 DB writes |
| 10,000 | ~222 | 222 POSTs + 222 parses + 222 DB writes |

### Per-Fetch Work

1. `GetValidSession()` — RLock check (fast, no network if session valid)
2. `doFetch()` — HTTP POST to Warwick with 30s timeout
3. JSON decode `QrResponse` (~200 bytes)
4. Validate `qrUrl` prefix and non-empty
5. Lock/unlock room mutex (×3: RLock for check, Lock for update, Lock for emit)
6. Async DB write (`go func { UpdateRoom }`)
7. JSON marshal event + send to event channel

### CPU Hotspots

- **Mutex contention:** `rm.mu.RLock()` / `Lock()` called 3+ times per fetch. At 222 fetches/second with 10k rooms, lock contention on the shared `rm.mu` becomes significant. The lock protects the entire rooms map — not individual rooms.
- **Event broadcast:** Each `rm.emit()` sends to `rm.eventCh`. Then each subscriber goroutine receives and writes to its channel. At 10k subscribers, each event is written 10k times (in `select{default:...}` which is a fast path, but 10k iterations per event × ~2 events/second = 20k goroutine wakeups/second).
- **GC pressure:** Async goroutines and JSON marshal/unmarshal on every fetch create allocation churn.

### Severity

**High at scale.** At 1,000+ rooms, CPU is dominated by the polling loop. At 5,000+ rooms, the `mu.Lock()` thrashing and broadcast overhead become significant even before HTTP latency. The 1-second tick loop itself is wasteful — most ticks do nothing.

### Fix

- Replace 1-second tick with a calculated timer that fires exactly when the next fetch is due (`time.NewTimer(calculateNextFetch(...))`). This eliminates 44/45 wasted wakeups per worker.
- Replace global `rm.mu` with per-room `sync.Mutex` to eliminate lock contention.
- Batch room saves instead of per-fetch DB writes.
- Add fetch jitter (±10%) to prevent thundering herd on Warwick.

---

## 6. Database: Concurrent Worker Writes

### Write Sources

| Source | Rate | Pattern |
|--------|------|---------|
| QR fetch success → `UpdateRoom` | 1 per fetch | async goroutine, `context.Background()` |
| QR fetch error → `UpdateRoom` | on error | async goroutine |
| `StopRoom` persistence | on stop | async goroutine |
| `CreateRoom` | on create | synchronous |
| `DeleteRoom` | on delete | synchronous |

### Write TPS at 1,000 rooms

~22 writes/second (one per fetch cycle). All are `UPDATE rooms SET ... WHERE room_id = $1` — single-row updates by primary key. This is extremely lightweight.

PostgreSQL handles thousands of TPS without breaking a sweat. Even pgxpool default of 4 connections (or whatever Supabase pooler provides) is sufficient for 22 TPS.

### Configuration Risk

```go
// db.go:19-27
func NewPool(databaseURL string) (*pgxpool.Pool, error) {
    config, err := pgxpool.ParseConfig(databaseURL)
    config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
    return pgxpool.NewWithConfig(context.Background(), config)
}
```

`pgxpool.ParseConfig` uses defaults:
- **`MaxConns`: 4** (pgxpool default) — may be too low for 10k rooms.
- **`MinConns`: 0** — no warm connections.

For Supabase pooler (PgBouncer transaction mode), 4 max conns is correct but the **`UpdateRoom` calls use `context.Background()`**, which means they bypass request-scoped cancellation. During shutdown, these background queries race with `pool.Close()` in `defer pool.Close()` (`main.go:58`). The shutdown sequence is:

1. Signal received → `srv.Shutdown()` (10s timeout)
2. HTTP server stops accepting connections
3. Current requests drain
4. Room workers are NOT explicitly stopped
5. `pool.Close()` in defer fires → closes all connections
6. Background async writes to pool **panic** with "pool closed"

### Severity

**Low** for TPS load. **Medium** for shutdown safety. The pool config itself is fine for moderate scale. The risk is goroutines writing to a closed pool during shutdown.

### Fix

- Replace `context.Background()` in room worker DB writes with the room's `state.ctx` (or a manager-level shutdown context).
- Add `rm.Shutdown(ctx)` that cancels all room contexts, then waits for pending writes.
- Remove `defer pool.Close()` and make it part of the orderly shutdown sequence in `main.go`.

---

## 7. No Maximum WebSocket Connections

### Current State

`routes.go:50` — `r.Get("/ws", wsHandler(rm))` — no middleware, no rate limiter, no max connections guard. Any number of clients can connect.

### Bottleneck Analysis at Scale

| Bottleneck | 1,000 clients | 10,000 clients |
|-----------|--------------|----------------|
| **FD count** | ~1,000 TCP + 10 server = ~1,010 | ~10,010 (ulimit -n default 1024 on Linux: **breaks first**) |
| **Goroutines** | ~4,000 (leaked) | ~40,000 (leaked) |
| **Channel buffer memory** | ~500 MB | ~5 GB |
| **Event broadcast cost** | 1 event → 1k goroutine wakeups | 1 event → 10k goroutine wakeups |
| **Read pump contention** | 1k goroutines blocking on `conn.Read()` | 10k goroutines blocking — Go runtime, fine |
| **Write pump contention** | 1k goroutines blocking on `conn.Write()` | 10k goroutines — network I/O dominates |

### First Failure Point

**File descriptors** (Linux default `ulimit -n` = 1024). At ~1,020 FDs, `net.Listen` or `websocket.Accept` fails with "too many open files". This is the hard cap before any memory/CPU limits.

Second failure: **memory**. At ~2,000 clients with default ulimit raised, the ~1 GB channel buffer memory approaches container limits.

### Severity

**Critical.** The WS endpoint is completely unbounded. An attacker can open 10,000 connections and exhaust server resources (FDs → crash, memory → OOM). The FD limit on Linux provides incidental protection at default ulimit, but this is not a security boundary.

### Fix

1. **Add `ws.MaxConns` guard** — a counting semaphore or atomic counter. Reject connections above limit with 503.
   ```go
   var wsConns atomic.Int64
   if wsConns.Load() >= maxWSConns { http.Error(w, "too many connections", 503); return }
   wsConns.Add(1)
   defer wsConns.Add(-1)
   ```
2. Configure `ReadTimeout` and `WriteTimeout` at the websocket accept level (currently nil options in `websocket.Accept(w, r, nil)`).
3. Fix goroutine leak first — otherwise even with max connections guard, disconnected clients leak goroutines.

---

## 8. Startup Validation: Hard Dependency on Warwick

### Code Path

```go
// main.go:31-40
auth, err := warwick.FromEnv()
if err != nil {
    slog.Error("...")
    os.Exit(1)
}
_, err = auth.GetValidSession()
if err != nil {
    slog.Error("...")
    os.Exit(1)
}
```

`GetValidSession()` calls `performLogin()` which makes an HTTP POST to `https://warwick.humantix.cloud/admin/` with a 30s timeout. If Warwick is unreachable, the server exits.

### Failure Modes

| Scenario | Result | Impact |
|----------|--------|--------|
| Warwick down for maintenance | Server fails to start | Deploy rollback, outage |
| Network ACL blocks server → Warwick | Server fails to start | Hard failure, no graceful degradation |
| DNS resolution failure for warwick.humantix.cloud | Server fails to start after DNS timeout | Delayed deploy failure |
| Warwick returns login page (credential changed) | Server fails to start | Blocks until credentials updated |
| Warwick session cookie format changes | Server fails to start | Blocks until code updated |

### Design Tradeoff

This makes the server's availability **equal to** the intersection of server availability AND Warwick availability. If Warwick has 99.5% uptime, the server has effectively 99.5% uptime for deploys — even though the server's core function (serving cached QR codes and UI) doesn't strictly need a live Warwick connection at boot.

### Severity

**Medium.** For a tool that shows QR codes from Warwick, failing fast at startup is an acceptable pattern in many production setups. However, it creates unnecessary deploy risk for what is fundamentally a monitoring/display tool that could work with cached data or retry on first user request.

### Fix

- **Defer Warwick auth check to first use** (lazy validation). Remove the startup check. Let the first room fetch validate credentials.
- Or: make startup validation a **warning** not a fatal error. Log the failure but continue serving.
- Or: add a `--validate` flag that does the check and exits (for deploy scripts to use separately).

---

## Summary: Priority Order for Fixes

| # | Issue | Severity | Effort | Quick win? |
|---|-------|----------|--------|-----------|
| 1 | **Goroutine leak** (Subscribe + event forwarder) | Critical | Low | ✅ Close channels on lifecycle |
| 7 | **No max WS connections** | Critical | Low | ✅ Atomic counter guard |
| 4 | **Unlimited HTTP conns to Warwick** | Medium | Low | ✅ Share client + cap MaxConnsPerHost |
| 3 | **Large WS channel buffers** | High | Low | ✅ Reduce 256 → 64 |
| 2 | **Unbounded room creation** | Medium | Low | ✅ Add maxRooms guard |
| 5 | **1s tick polling inefficiency** | High | Medium | Replace with calculated timer |
| 8 | **Hard startup dependency on Warwick** | Medium | Medium | Defer or warn-only |
| 6 | **Race on pool.Close during shutdown** | Medium | Medium | Replace Background context with room ctx |

## Go Runtime Configuration Recommendations

For production deployment at >100 concurrent users:

```bash
# Increase FD limit (Docker default is often 1024)
ulimit -n 65536

# Go runtime
export GOMAXPROCS=4            # match CPU quota
export GOGC=100                # default, fine for this pattern
```

Add to `main.go`:
```go
import "runtime/debug"
func init() {
    debug.SetMemoryLimit(512 * 1024 * 1024)  // 512 MB soft limit
}
```

---

*Resource exhaustion analysis: 2026-05-29*
