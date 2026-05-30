# Codebase Concerns

**Analysis Date:** Fri May 29 2026

## 1. Concurrent Request Handling Audit

### 1A. Shared Warwick Session (`WarwickAuth`) — Thread-Safe ✓

`internal/warwick/auth.go:26-33`:
```go
type WarwickAuth struct {
    sessionMu sync.RWMutex
    session   *sessionState
}
```

**Verdict:** Correct. `GetValidSession()` (line 61) uses double-checked locking:
1. RLock → check cache → RUnlock (fast path)
2. Lock → re-check → performLogin → Unlock (slow path with re-check)

`ForceRefresh()` (line 85) acquires write lock directly. `performLogin()` (line 97) is only called while holding write lock.

No data race on the session cookie itself.

---

### 1B. RoomManager `rooms` Map — Correct Locking (with one gap)

`internal/service/room_manager.go:25-31`:
```go
type RoomManager struct {
    mu         sync.RWMutex
    rooms      map[string]*RoomState
    eventCh    chan RoomManagerEvent
    ...
}
```

All map access is under the mutex:
- `GetRoom` (line 110): RLock ✓
- `GetAllRooms` (line 120): RLock ✓
- `CreateRoom` (line 84-86): Lock → write ✓
- `DeleteRoom` (line 97): Lock → delete ✓
- `StartRoom` (line 131): Lock ✓
- `StopRoom` (line 164): Lock ✓
- `LoadRoomsFromDB` (line 62): Lock ✓

**Gap: Room worker goroutine (`runRoomWorker`, line 200) accesses `state` fields under the mutex correctly but transitions may conflict with concurrent `StopRoom` (see #3 below).**

---

### 1C. Room Worker Goroutine Locking — Correct (sporadic extra fetch on stop)

The worker at `internal/service/room_manager.go:200` holds `rm.mu` when accessing `state.room`:
- Line 213: RLock for read → RUnlock
- Line 221: Lock for `TransitionTo(Fetching)` → Unlock
- Line 232: Lock for error handling → Unlock
- Line 271: Lock for QR update → Unlock

**No data race on `state.room` fields.** `rm.mu` serializes all access.

---

## 2. ToggleCheckin Race: Shared Session Cookie Contention

**Severity: Medium**

**Files:** `internal/warwick/classroom_client.go:267-287`, `internal/warwick/auth.go:61-83`

**Failure sequence (two concurrent POST requests for different students):**

```
Time│ Req A (toggle studentA)         │ Req B (toggle studentB)        │ Warwick server
────┼──────────────────────────────────┼────────────────────────────────┼─────────────────
t1  │ GetValidSession() → cookie C1   │                                │
t2  │                                  │ GetValidSession() → cookie C1 │
t3  │ doToggleCheckin(C1, studentA)   │                                │ processes toggle-A
t4  │                                  │ doToggleCheckin(C1, studentB) │ processes toggle-B
t5  │ ← 200 OK                        │                                │
t6  │                                  │ ← 200 OK                      │
```

The session cookie is **shared state with no per-request isolation**. This works under low concurrency. Under high concurrency:

```
Time│ Req A                            │ Req B                         │ Warwick server
────┼──────────────────────────────────┼────────────────────────────────┼─────────────────
t1  │ GetValidSession() → cookie C1   │                                │
t2  │ doToggleCheckin(C1, studentA)   │                                │
t3  │ ← redirect (session expired)    │                                │
t4  │                                  │ GetValidSession() → cookie C1 │ (still cached!)
t5  │ ForceRefresh() → cookie C2      │                                │
t6  │ doToggleCheckin(C2, studentA)   │                                │ processes toggle-A
t7  │ ← 200 OK                        │                                │
t8  │                                  │ doToggleCheckin(C1, studentB) │ stale cookie!
t9  │                                  │ ← redirect (session expired)  │
t10 │                                  │ ForceRefresh() → cookie C2    │ (already refreshed)
t11 │                                  │ doToggleCheckin(C2, studentB) │ processes toggle-B
t12 │                                  │ ← 200 OK                      │
```

**Impact:** Req B uses a stale cookie (C1) at t8 because it fetched the session before A's ForceRefresh invalidated it. B wastes one round-trip + a refresh + a retry. Functional correctness is preserved (retry logic works), but latency doubles for the second request.

**Worse scenario under load:** If Warwick rate-limits per-session (many concurrent toggles), ALL concurrent requests could get auth-expired simultaneously, triggering a **thundering herd of ForceRefresh()** calls. While `sessionMu` serializes them, each ForceRefresh performs a POST login to Warwick. This could trigger Warwick anti-bot measures.

**Fix approaches:**
1. Accept current behavior (retry handles it, functional correctness holds).
2. Add per-request session pinning: snapshot the cookie at request start and fail-fast if it changes mid-request.
3. Add a distributed semaphore on ToggleCheckin to limit concurrent Warwick toggles to N (e.g., 5).
4. Rate-limit the `/api/teacher/*` endpoints to prevent abuse.

---

## 3. StartRoom / StopRoom Races

### 3A. Cannot start two workers for the same room ✓

`internal/service/room_manager.go:130-140`:
```go
func (rm *RoomManager) StartRoom(roomID string) error {
    rm.mu.Lock()
    defer rm.mu.Unlock()
    state, ok := rm.rooms[roomID]
    if !ok { return fmt.Errorf("room not found") }
    if state.cancel != nil { return nil }  // ← guards: already running
    ...
    go rm.runRoomWorker(state)
```

Second concurrent `StartRoom` for same room: `state.cancel != nil` is true under the mutex → returns nil. **Safe.**

### 3B. StopRoom can race with worker goroutine — Spurious extra fetch

**Severity: Low**

**Timeline (TOCTOU window in worker loop):**

```
Worker iteration:              StopRoom(roomID):
┌──────────────────────┐       ┌───────────────────────────┐
│ RLock (line 213)     │       │                           │
│ read expiresAt       │       │                           │
│ RUnlock (line 216)   │       │                           │
│                      │       │ Lock (line 164)           │
│ shouldFetch = true   │       │ state.cancel()            │
│                      │       │ cancel = nil              │
│                      │       │ TransitionTo(Stopped)     │
│                      │       │ Unlock (line 165)         │
│ Lock (line 221)      │ ←──── │                           │
│ TransitionTo(Fetch) │       │                           │
│  → invalid! (Stopped→Fetch) │                           │
│ slog.Warn (line 223) │       │                           │
│ FetchQR (line 230)   │  ←─── spurious HTTP call!        │
│   → wasteful network │       │                           │
│ Lock (line 232)      │       │                           │
│ ...process result... │       │                           │
│ Unlock (line 254)    │       │                           │
│ ...continue...       │       │                           │
│ next select: ctx.Done│  ←─── finally notices cancel     │
│ return               │       │                           │
```

**Root cause:** Worker checks `state.ctx.Done()` only in the `select` at line 208. Between `select` wakeup and the next `Lock` acquisition, `StopRoom` can preempt. The worker does **not** re-check `ctx.Done()` after acquiring the lock.

`internal/service/room_manager.go:207-211`:
```go
for {
    select {
    case <-state.ctx.Done():
        return
    case <-time.After(1 * time.Second):
        // ↓↓↓ no ctx.Done() check before acquiring lock ↓↓↓
    }
    // ... RLock/RUnlock (pure read) ...
    // ... Lock ...
    state.room.TransitionTo(Fetching)  // may be invalid
    FetchQR(...)                        // may be wasteful
```

**Impact:** A single spurious QR fetch to Warwick after room is stopped. **No crash, no data corruption** — just a wasted HTTP call (~200ms) and an invalid-transition warning log.

**Fix:**
```go
case <-time.After(1 * time.Second):
    rm.mu.RLock()
    if state.ctx.Err() != nil {  // ← check if cancelled
        rm.mu.RUnlock()
        return
    }
    expiresAt := state.room.ExpiresAt
    classID := state.room.ClassID
    rm.mu.RUnlock()
```

---

## 4. createRoomHandler ID Generation — Duplicate Risk with SessionID

### 4A. UUID-based (`createRoomHandler`) — Safe ✓

`internal/api/routes.go:111`:
```go
room, err := rm.CreateRoom(uuid.New().String(), req.ClassID, req.Name)
```

`uuid.New().String()` collision probability is negligible (~2^-122). Map write at `room_manager.go:84-86` is mutex-protected.

**Verdict:** Safe.

### 4B. SessionID-based (`createRoomFromSessionHandler`) — TOCTOU Race

`internal/api/routes.go:178`:
```go
room, err := rm.CreateRoom(req.SessionID, req.SessionID, nil)
```

`internal/service/room_manager.go:70-90`:
```go
func (rm *RoomManager) CreateRoom(roomID string, ...) (domain.Room, error) {
    existing, err := rm.repository.GetRoom(roomID)  // ← DB check, NO mutex
    if err == nil && existing.RoomID != "" {
        return existing, nil
    }
    // ... create room ...
    saved, err := rm.repository.CreateRoom(room)  // ← DB write
    ...
    rm.mu.Lock()
    rm.rooms[saved.RoomID] = &RoomState{room: saved}  // ← map write under mutex
    rm.mu.Unlock()
```

**Race timeline (two concurrent requests with same sessionID):**

```
Time│ Req A                          │ Req B                          │ DB state
────┼─────────────────────────────────┼────────────────────────────────┼────────────
t1  │ repository.GetRoom("S1")       │                                │ empty
t2  │ → not found                   │                                │
t3  │                                │ repository.GetRoom("S1")       │ empty
t4  │                                │ → not found                   │
t5  │ repository.CreateRoom(Room{S1})│                                │ inserts S1
t6  │ → saved                       │                                │
t7  │ rm.mu.Lock()                  │                                │
t8  │ rm.rooms["S1"] = state        │                                │
t9  │ rm.mu.Unlock()                │                                │
t10 │                                │ repository.CreateRoom(Room{S1})│ duplicate!
t11 │                                │ → error                       │
```

**If DB has unique constraint on room_id:** Req B gets error at t11, handler returns 500. Functional but sloppy.

**If DB does NOT have unique constraint:** Both succeed. Req B overwrites Req A's entry in the map at t10 (after acquiring mutex). Req A's room is **lost** from the map.

**Severity:** Medium (DB-dependent; most SQL schemas would have PK unique constraint).

**Fix:** Move the dedup check inside the mutex, or use `INSERT ... ON CONFLICT DO NOTHING` / `INSERT ... ON CONFLICT DO UPDATE` at the DB level.

---

## 5. REST Endpoints — No Auth, No Rate Limiting, No Body Limits

**Severity: Critical**

### 5A. No Authentication

Every endpoint in `internal/api/routes.go:33-50` is unprotected:
| Method | Endpoint | Effect |
|--------|----------|--------|
| POST | `/api/rooms` | Create room |
| DELETE | `/api/rooms/{id}` | Delete room |
| POST | `/api/rooms/{id}/start` | Start QR polling |
| POST | `/api/rooms/{id}/stop` | Stop QR polling |
| POST | `/api/teacher/courses/{cid}/sessions/{sid}/toggle-checkin` | Toggle attendance |
| GET | `/ws` | Full state stream |

Any client that reaches the server IP/port can call all of them. No cookie, no token, no header check.

### 5B. No Rate Limiting

Under 100+ concurrent users, the following breaks first:

**1. Warwick session thundering herd** (`internal/warwick/auth.go:61-83`):
- All handlers share one `WarwickAuth` with one session cookie.
- All `GetValidSession()` calls hit the same RWMutex. While fast, if Warwick invalidates the session (e.g., too many concurrent requests from the same session), ALL subsequent requests trigger `ForceRefresh()` → POST login → serialized under `sessionMu`.
- Login takes ~500ms-2s. During that window, all 100 requests queue on `sessionMu.Lock()`. Latency spikes to seconds.

**2. Event channel overflow** (`internal/service/room_manager.go:36`):
```go
eventCh: make(chan RoomManagerEvent, 100),
```
Buffer of 100. Under high room churn, events are dropped. WS clients miss state transitions. Non-fatal but unreliable.

**3. No request body size limits** (`internal/api/routes.go:103`, `internal/api/teacher_handlers.go:78`):
```go
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
```
No `http.MaxBytesReader`. An attacker can POST a multi-GB JSON payload and exhaust server memory.

**4. QR fetch amplification** (`internal/service/room_manager.go:230`):
Each active room worker calls `FetchQR()` every ~45 seconds. If 50 rooms are running, that's 50 concurrent HTTP calls to Warwick. The `WarwickQrClient` shares the same `WarwickAuth` session. Concurrent `GetValidSession()` is mutex-protected but the actual HTTP fetches are not throttled.

**5. WebSocket origin restriction bypass** (`internal/api/websocket.go:22`):
```go
conn, err := websocket.Accept(w, r, nil)
```
No origin check, no subprotocol validation. Any page that reaches the server can open a WebSocket and receive the full room state stream.

### 5C. What breaks first under 100+ concurrent users

| Priority | Failure | Trigger | Impact |
|----------|---------|---------|--------|
| 1 | Warwick session invalidation | >N concurrent toggles with same cookie | All Warwick calls fail, ForceRefresh cascade, 503s |
| 2 | Event channel saturation | Rapid room start/stop | Lost WS events, UI desync |
| 3 | OOM from large request body | Malicious POST with GB-sized body | Server crash |
| 4 | QR fetch thundering herd | 50+ running rooms | Warwick rate-limits the session |
| 5 | Unauthenticated room deletion | Any client can DELETE | Data loss |

### Fixes

**For auth:**
Add a chi middleware that validates a bearer token or session cookie before allowing mutating operations:
```go
r.With(authMiddleware).Route("/api/rooms", func(r chi.Router) {
    ...
})
```

**For rate limiting:**
Add chi middleware or use `github.com/go-chi/httprate`:
```go
r.Use(httprate.LimitByIP(100, 1*time.Minute))
// Stricter for mutation endpoints:
r.With(httprate.LimitByIP(10, 1*time.Minute)).Post("/{id}/start", ...)
```

**For request body size:**
```go
r.Body = http.MaxBytesReader(w, r.Body, 1<<20)  // 1MB max
```

**For WS origin:**
```go
conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
    OriginPatterns: []string{"localhost:3000", "localhost:3001"},
})
```

---

## 6. Minor Concurrency Issues

### 6A. `runRoomWorker` — Worker cancels own context under mutex
`internal/service/room_manager.go:246-248`:
```go
if state.cancel != nil {
    state.cancel()  // cancels ctx while holding rm.mu
}
```
Holding the mutex while calling `state.cancel()` is safe here, but if any `<-ctx.Done()` listeners acquire the same mutex, it would deadlock. Currently no such listener exists, but this is fragile.

### 6B. `runRoomWorker` — go func persists room copy outside lock
Lines 256-260 and 284-288: Persistence happens in a goroutine after releasing the lock. The `roomCopy` is a value copy, so this is safe. But stacked goroutines (one per fetch cycle) could queue up if the DB is slow. No cleanup on worker exit — these goroutines continue after the worker returns. Non-fatal but wasteful.

### 6C. No context cancellation propagation to DB operations
`internal/service/room_manager.go:182-186`:
```go
go func() {
    if _, err := rm.repository.UpdateRoom(room); err != nil { ... }
}()
```
These goroutines use `context.Background()` implicitly (via the repository's own context). During server shutdown (`main.go:106`), these goroutines may still be running and writing to the DB.

---

*Concerns audit: Fri May 29 2026*
