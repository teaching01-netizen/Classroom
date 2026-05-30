# DB Layer Audit — Production-First v2

**Analysis Date:** Fri May 29 2026

**Scope:** `internal/db/`, `internal/service/room_manager.go`, `internal/domain/room.go`, `cmd/server/main.go`

---

## 1. Room State Persistence Races

### Severity: Critical | Timeline: Immediate

### Mechanism

The worker goroutine (`runRoomWorker` at `internal/service/room_manager.go:200`) and HTTP handlers (e.g., `StopRoom` at `:163`) both update room state and then persist to DB in **fire-and-forget goroutines** outside the mutex:

**Worker path** (`room_manager.go:281-288`):
```go
rm.mu.Lock()
// mutate state.room fields...
roomCopy := state.room
rm.mu.Unlock()

go func() {                          // ← fire-and-forget
    if _, err := rm.repository.UpdateRoom(roomCopy); err != nil {
        slog.Error(...)
    }
}()
```

**StopRoom path** (`room_manager.go:164-186`):
```go
rm.mu.Lock()
state.cancel()
state.room.TransitionTo(domain.Stopped)
room := state.room
rm.mu.Unlock()

go func() {                          // ← fire-and-forget
    if _, err := rm.repository.UpdateRoom(room); err != nil { ... }
}()
```

### What happens in a race

Concrete scenario — worker and `StopRoom` fire simultaneously:

| Time | Worker goroutine | StopRoom handler |
|------|-----------------|------------------|
| T0 | Acquires `rm.mu.Lock()` | — |
| T1 | Sets `state.room.QRURL = newQR`, `state.room.Status = Running` | — |
| T2 | Copies `roomCopy = state.room` (snapshot A) | — |
| T3 | Releases `rm.mu.Unlock()` | — |
| T4 | — | Acquires `rm.mu.Lock()` |
| T5 | — | Sets `state.room.Status = Stopped`, clears QRURL |
| T6 | — | Copies `room = state.room` (snapshot B) |
| T7 | — | Releases `rm.mu.Unlock()` |
| T8 | Spawns goroutine A → `UpdateRoom(A)` | Spawns goroutine B → `UpdateRoom(B)` |
| T9 | **`UPDATE ... SET status='running', qr_url='newQR', ...`** | **`UPDATE ... SET status='stopped', qr_url=NULL, ...`** |

Outcome depends on which `UPDATE` commits last — **last-writer-wins**. If B commits last: room is `stopped` with `qr_url=NULL` in DB ✓. If A commits last: room is `running` with `newQR` in DB even though `StopRoom` returned 200 to the user ✗.

There are **5 fire-and-forget `UpdateRoom` goroutines** in `room_manager.go` (lines 182, 256, 284). Any two can race.

### Evidence

- `room_manager.go:182-186` — StopRoom spawns goroutine after releasing `rm.mu`
- `room_manager.go:256-260` — Worker error branch spawns goroutine after releasing `rm.mu`
- `room_manager.go:284-288` — Worker success branch spawns goroutine after releasing `rm.mu`
- `repository.go:78-84` — `UpdateRoom` does a blind full-row `UPDATE` with no optimistic locking

### Fix

**Option A (recommended — optimistic concurrency):** Add a `version` column to `rooms` table. Use `UPDATE ... WHERE room_id = $1 AND version = $old_version RETURNING version`. If `RowsAffected() == 0`, retry with fresh read.

```sql
ALTER TABLE rooms ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
```

In `UpdateRoom`:
```go
var newVersion int
err := r.pool.QueryRow(ctx,
    `UPDATE rooms SET ..., version = version + 1
     WHERE room_id = $1 AND version = $2
     RETURNING version`,
    room.RoomID, room.Version, ...,
).Scan(&newVersion)
```

**Option B (simpler — synchronous writes within lock):** Move `UpdateRoom` inside the `rm.mu` critical section, before releasing the lock. Trade-off: holds mutex during network I/O (~5-50ms), increasing lock contention.

```go
rm.mu.Lock()
// mutate state
if _, err := rm.repository.UpdateRoom(state.room); err != nil { ... }
rm.mu.Unlock()
```

---

## 2. Missing Unique Constraints

### Severity: Medium | Timeline: Known design decision

### Schema

File `internal/db/migrations/001_create_rooms_table.up.sql`:

```sql
CREATE TABLE IF NOT EXISTS rooms (
    room_id TEXT PRIMARY KEY,      -- changed from UUID in migration 002
    class_id TEXT NOT NULL,         -- ← NO UNIQUE constraint
    ...
);
```

### What's missing

| Constraint | Status | Risk |
|------------|--------|------|
| `room_id` UNIQUE (PK) | ✅ Present | — |
| `class_id` UNIQUE | ❌ Missing | Multiple rooms can point to same class |
| `(class_id, status)` partial index | ❌ Missing | No fast lookup for active rooms by class |

### Consequences

- `CreateRoom` in `room_manager.go:70-90` generates a fresh UUID for `room_id` each call, so dedup is by `room_id`, not `class_id`
- The dedup check at line 72-74 only checks if the generated `room_id` exists (which it won't, being a fresh UUID), not if a room already exists for that `class_id`
- Multiple concurrent `POST /api/rooms` with the same `class_id` create duplicate rows

### Evidence

`room_manager.go:71-75`:
```go
existing, err := rm.repository.GetRoom(roomID)
if err == nil && existing.RoomID != "" {
    return existing, nil
}
```
The `roomID` is a fresh UUID generated per request — this check only catches retries of the exact same request, not dedup by `class_id`.

### Fix

Add a UNIQUE constraint on `class_id` if the domain model is 1:1 room:class. If 1:N is intentional, add a composite index and document.

Migration:
```sql
-- If 1:1 room:class
ALTER TABLE rooms ADD CONSTRAINT rooms_class_id_unique UNIQUE (class_id);

-- If 1:N is valid, at least add index for perf
CREATE INDEX idx_rooms_class_id ON rooms (class_id);
```

---

## 3. Migration Safety

### Severity: High | Timeline: On next deploy

### Non-idempotent migration

`internal/db/migrations/002_change_room_id_to_text.up.sql`:
```sql
ALTER TABLE rooms ALTER COLUMN room_id TYPE TEXT;
```

This is **not idempotent**. Running it twice on PostgreSQL < 16 will fail because the column is already TEXT (no cast needed, PostgreSQL rejects no-op type change in some versions). PG 16 is more lenient but the behavior is version-dependent.

### Force-to-version-1 hazard

`internal/db/db.go:51-59`:
```go
if err := m.Up(); err != nil && err != migrate.ErrNoChange {
    if _, ok := err.(migrate.ErrDirty); ok {
        m.Force(1)     // ← Always forces to version 1
        return nil
    }
}
```

If migration #2 fails and the schema is dirty, the code **unconditionally forces the version to 1** and returns success. This silently skips any unapplied migration, leaving the schema in an unpredictable state.

The comment says "schema already exists from Rust" — this is a time-bomb if migration #2 was the one that went dirty. After `Force(1)`, migration #2 will be re-applied on next restart, potentially failing again.

### Concurrent migration runners

`RunMigrations` uses `golang-migrate` which uses the `schema_migrations` table with advisory locks. Two processes running migrations concurrently is safe — the second will wait on the advisory lock. However:

- The app runs migrations at startup in `main.go:61` (`db.RunMigrations(databaseURL)`)
- If two containers start simultaneously (rolling deploy), one will hold the lock and the other will wait
- With HTTP timeout of 15s (`main.go:84`), a migration that takes long could cause the waiting container to fail health checks

### Evidence

- `db.go:51-59` — Conditional force-to-1 when dirty
- `internal/db/migrations/002_change_room_id_to_text.up.sql` — Non-idempotent ALTER COLUMN

### Fix

1. Add idempotency check to migration 002:
```sql
ALTER TABLE rooms ALTER COLUMN room_id TYPE TEXT;
-- If already TEXT, this is a no-op in PG 16. For older PG:
-- DO $$ BEGIN
--     ALTER TABLE rooms ALTER COLUMN room_id TYPE TEXT;
-- EXCEPTION WHEN undefined_column THEN null;
-- END $$;
```

2. Replace the `Force(1)` with proper dirty-state investigation:
```go
if _, ok := err.(migrate.ErrDirty); ok {
    // Log the dirty version, don't blindly force
    slog.Error("migration dirty — manual investigation required")
    return fmt.Errorf("migration dirty: %w", err)
}
```

3. Add migration timeout context:
```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
```

---

## 4. pgxpool Exhaustion

### Severity: High | Timeline: Under load

### Configuration

`internal/db/db.go:19-27`:
```go
func NewPool(databaseURL string) (*pgxpool.Pool, error) {
    config, err := pgxpool.ParseConfig(databaseURL)
    // No MaxConns, MaxConnLifetime, MaxConnIdleTime configured
    config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
    return pgxpool.NewWithConfig(context.Background(), config)
}
```

### Default pool size

pgxpool v5 default `MaxConns` = `max(4, runtime.GOMAXPROCS * 2 + 4)`. On a typical deployment (4-8 vCPUs), that's **12-20 connections**.

### Exhaustion under 50 concurrent users

Each DB operation holds a connection for the duration of the query. Under 50 concurrent users hitting API endpoints:

| Endpoint | DB calls per request | Connection time |
|----------|---------------------|-----------------|
| `GET /api/rooms` | 0 (in-memory) | 0 |
| `POST /api/rooms` | 1-2 (CreateRoom + GetRoom) | ~10-50ms |
| `POST /api/rooms/{id}/start` | 0 (in-memory) | 0 |
| `POST /api/rooms/{id}/stop` | 1 (fire-and-forget) | ~10-50ms |
| `DELETE /api/rooms/{id}` | 1 | ~10-50ms |

But the **real risk** is the worker goroutines. Each running room has a worker that fires `UpdateRoom` roughly every 45 seconds. With 50 rooms:
- 50 workers × 1 update/45s ≈ 1.1 DB calls/second from workers alone
- Plus HTTP handler calls under 50 concurrent users
- All using `context.Background()` — **no timeout**, so a slow DB call blocks indefinitely

### What happens at exhaustion

`pgxpool.Exec()` calls `Acquire()` internally. At pool exhaustion:
- `Acquire` **blocks** (does not fail fast) until a connection becomes available
- With `context.Background()`, it blocks **forever**
- All HTTP handlers that touch DB hang
- No new connections can be acquired until existing ones complete

### Evidence

- `db.go:19-27` — No pool configuration
- `repository.go:31, 47, 54, 78, 95` — All use `context.Background()`
- `room_manager.go:256-260, 284-288` — Fire-and-forget goroutines with no timeout

### Fix

```go
func NewPool(databaseURL string) (*pgxpool.Pool, error) {
    config, err := pgxpool.ParseConfig(databaseURL)
    if err != nil {
        return nil, err
    }
    config.MaxConns = 25                          // Cap well below Supabase pooler limit
    config.MinConns = 5                           // Keep baseline warm
    config.MaxConnLifetime = 30 * time.Minute     // Rotate connections
    config.MaxConnIdleTime = 5 * time.Minute      // Reclaim idle
    config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
    return pgxpool.NewWithConfig(context.Background(), config)
}
```

Also: pass request contexts (with deadlines) to repository methods instead of `context.Background()`:
```go
func (r *PgRoomRepository) UpdateRoom(ctx context.Context, room domain.Room) (domain.Room, error) {
    // use ctx instead of context.Background()
}
```

---

## 5. Read-After-Write Consistency

### Severity: Medium | Timeline: Understood architecture

### Architecture

The app uses a **primary-learner** pattern with `RoomManager` as the single source of truth:

```
Reads:  HTTP handler → rm.GetRoom() → in-memory map (under RLock)
Writes: HTTP handler → rm.StartRoom/StopRoom → mutate in-memory map (under Lock)
                                                  → fire-and-forget UpdateRoom goroutine
```

### What's consistent ✓

- After a HTTP handler calls `StopRoom`, subsequent in-memory reads (from the same process) always see the updated state because `state.room` was mutated under `rm.mu.Lock()` before the handler returns
- WebSocket `FullStateSync` is populated from `rm.GetAllRooms()` (in-memory) at connect time

### What's inconsistent ✗

- **Cross-process reads:** If a second instance starts (horizontal scaling) or a cold restart happens, the in-memory state is loaded from DB via `LoadRoomsFromDB()`. But the DB may have stale state from the race condition described in #1.
- **DB-isolation-level:** All queries use `context.Background()` with default `READ COMMITTED`. Within a single connection this is fine. Across connections (pool), `READ COMMITTED` guarantees committed data is visible to new reads — no stale reads at the DB level.

### Evidence

- `room_manager.go:110-118` — `GetRoom` reads from in-memory map
- `room_manager.go:120-128` — `GetAllRooms` reads from in-memory map
- `room_manager.go:57-68` — `LoadRoomsFromDB` populates in-memory map at startup

### Fix

The architecture is reasonable for single-instance operation. If horizontal scaling is needed:
1. Move to a subscription-based consistency model (e.g., LISTEN/NOTIFY for cross-instance cache invalidation)
2. Or delegate reads to DB with a short-lived cache

---

## 6. Connection Handling

### Severity: High | Timeline: Under load

### No timeouts on any DB operation

Every repository method uses `context.Background()` — no cancellation, no deadline:

```go
func (r *PgRoomRepository) GetRoom(roomID string) (domain.Room, error) {
    row := r.pool.QueryRow(context.Background(), ...)   // ← hangs forever
```

If the database becomes slow or unreachable, every goroutine that calls a repository method will block indefinitely. This includes:

- HTTP handlers (`createRoomHandler`, `deleteRoomHandler`)
- Fire-and-forget goroutines from workers (spawned at `room_manager.go:182`, `:256`, `:284`)
- `LoadRoomsFromDB` at startup (`main.go:69`)

### Connection leak risk in worker goroutines

The fire-and-forget pattern in `room_manager.go`:
```go
go func() {
    if _, err := rm.repository.UpdateRoom(roomCopy); err != nil {
        slog.Error(...)
    }
}()
```

If `UpdateRoom` blocks (e.g., pool exhausted, DB slow), the goroutine never completes. Over hours of operation, this leaks:
- Goroutines (unbounded)
- Pool connections (each blocked goroutine holds a connection for the duration)
- The connection is only released when the query times out (which it won't, with `context.Background()`)

### Total goroutine count under load

| Source | Goroutines per room | For 50 rooms |
|--------|-------------------|--------------|
| Worker loop (`runRoomWorker`) | 1 | 50 |
| Fire-and-forget per fetch cycle (~45s) | 2 (success) or 2 (error) | ~100 |
| WS clients | 3 per connection | Variable |
| HTTP handlers | Short-lived (hundreds of ms) | Variable |

With all 50 rooms hitting DB write contention, the number of blocked goroutines can grow unbounded.

### Evidence

- `repository.go:31` — `context.Background()` for CreateRoom Exec
- `repository.go:47` — `context.Background()` for GetRoom QueryRow
- `repository.go:54` — `context.Background()` for GetAllRooms Query
- `repository.go:78` — `context.Background()` for UpdateRoom Exec
- `repository.go:95` — `context.Background()` for DeleteRoom Exec

### Fix

1. Thread `context.Context` through from HTTP handlers to repository:
```go
func (r *PgRoomRepository) UpdateRoom(ctx context.Context, room domain.Room) (domain.Room, error) {
    _, err := r.pool.Exec(ctx, ...)
}
```

2. Add timeout to fire-and-forget goroutines:
```go
go func() {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if _, err := rm.repository.UpdateRoom(ctx, roomCopy); err != nil {
        slog.Error(...)
    }
}()
```

3. Add `MaxConnLifetime` and `MaxConnIdleTime` to pool config (see #4 fix).

---

## 7. Deadlock Potential

### Severity: Low | Timeline: Not imminent

### Lock ordering analysis

All goroutines acquire only the single `RoomManager.mu` (sync.RWMutex):

| Goroutine | Locks acquired | Order |
|-----------|---------------|-------|
| `GetRoom` | RLock → RUnlock | Single lock |
| `GetAllRooms` | RLock → RUnlock | Single lock |
| `StartRoom` | Lock → Unlock | Single lock |
| `StopRoom` | Lock → Unlock | Single lock |
| `DeleteRoom` | Lock → Unlock | Single lock |
| `CreateRoom` | Lock → Unlock | Single lock |
| `LoadRoomsFromDB` | Lock → Unlock | Single lock |
| Worker loop body | RLock → RUnlock → Lock → Unlock | Single lock, non-nested |

No nested lock acquisition. No AB-BA pattern. No lock ordering violation.

### Not a deadlock but a mutex contention risk

The `Lock()` → fire-and-forget `UpdateRoom` → `Unlock()` pattern in `StopRoom` is safe for deadlocks (no lock held across the DB call), but:

- Worker loop at `room_manager.go:213` does `RLock()` to read `state.room.ExpiresAt` and `state.room.ClassID`
- Between the `RUnlock()` at line 216 and `Lock()` at line 221, the state can change (HTTP handler calls `StopRoom`)
- This is **stale read**, not deadlock
- After acquiring `Lock()`, the worker re-reads `state.room` fields directly, so the stale read only affects the `shouldFetch` decision

### Evidence

- No mutex held across blocking I/O calls in any goroutine
- No multiple mutexes acquired by any goroutine
- `context.Background()` in DB calls won't deadlock (no lock held) but will hang (see #6)

### Recommendation

No deadlock fix needed. But consider upgrading the `shouldFetch` stale-read pattern to run entirely under `Lock()` to avoid spurious fetch decisions:

```go
rm.mu.Lock()
expiresAt := state.room.ExpiresAt
classID := state.room.ClassID
shouldFetch := ...
if shouldFetch {
    state.room.TransitionTo(domain.Fetching)
    // all mutation here
}
rm.mu.Unlock()
```

---

## Summary Table

| # | Issue | Severity | Timeline | File | Fix complexity |
|---|-------|----------|----------|------|----------------|
| 1 | Room state race (last-writer-wins) | **Critical** | Immediate | `room_manager.go:182,256,284` | 3 days (optimistic locking) |
| 4 | pgxpool exhaustion under load | **High** | Under load | `db.go:19-27` | 1 hour (config) |
| 6 | Connection/gouroutine leak on slow DB | **High** | Under load | `repository.go:31,47,54,78,95` | 2 days (context threading) |
| 3 | Migration safety (Force(1) hazard) | **High** | Next deploy | `db.go:51-59` | 1 day (fix Force logic) |
| 2 | Missing unique constraint on class_id | **Medium** | Known | `migrations/001_*.up.sql` | 1 hour (add constraint) |
| 5 | Read-after-write cross-process | **Medium** | Architectural | `room_manager.go:110-118` | Acceptable for single-instance |
| 7 | Deadlock | **Low** | Not imminent | — | No fix needed |

**Estimated total remediation:** 3-4 days of engineering work. Priority order: #1 → #4 → #6 → #3 → #2.

---

*Audit generated: Fri May 29 2026*
