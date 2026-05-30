# ForceRefresh Cascade — Solution Design

**Analysis Date:** 2026-05-30

## Problem Summary

A single shared `sessionState` pointer in `WarwickAuth` (`internal/warwick/auth.go:32`) means one `ForceRefresh()` call invalidates the old cookie for ALL concurrent goroutines. This triggers a multiplication effect where N in-flight requests fail simultaneously, each calling `ForceRefresh()` independently, producing N rapid logins to Warwick.

## Root Mechanism

```
WarwickAuth (singleton, created in cmd/server/main.go:31)
  └── session *sessionState   ← single pointer, shared via RLock/RUnlock
       ├── cookieValue         ← one cookie for ALL goroutines
       ├── obtainedAt
       └── expiresAt

ForceRefresh():
  1. Lock sessionMu
  2. performLogin()           ← POST to Warwick, new session issued, old orphaned
  3. a.session = session      ← pointer replaced
  4. Unlock sessionMu

Result: old cookie dead, all in-flight requests using it → AuthExpired → each calls ForceRefresh
```

## Cascade Topology

```
Trigger: Any ClassroomClient method gets AuthExpired → calls ForceRefresh
   │
   ▼
Warwick invalidates old session, issues new one
   │
   ├──► In-flight QR requests (30 rooms × ~45s polling) using old cookie → ALL fail
   │        │
   │        ├──► QR FetchQR has NO ForceRefresh retry → workers die permanently
   │        └──► System-wide room death (all 30 rooms → AuthExpired, worker exits)
   │
   ├──► In-flight ClassroomClient requests (HTTP API) using old cookie → ALL fail
   │        │
   │        └──► Each calls ForceRefresh() independently → 29 more logins
   │
   └──► Warwick sees 30 rapid logins → rate-limit or account lockout
```

## Design Constraints (Immutable)

1. **Cannot modify Warwick** — ASP.NET auth behavior is fixed
2. **`ASP.NET_SessionId` cookie** is the auth mechanism — no token-based alternative
3. **Warwick invalidates old session on re-login** — no way to have overlapping valid sessions for same account
4. **Existing `QrClient` interface** must remain compatible (`FetchQR` and `FetchQRWithFreshAuth`)

---

## Solution Architecture

### Layer 1: Serialized ForceRefresh (Critical — Cascade Stopper)

**Problem:** N goroutines independently call `ForceRefresh()` → N logins.

**Fix:** Add a dedicated mutex + double-check pattern so only one goroutine actually logs in. Others detect that a refresh already happened and return the new session.

**Implementation — `internal/warwick/auth.go`:**

```go
type WarwickAuth struct {
    client    *http.Client
    email     string
    password  string
    loginURL  string
    sessionMu sync.RWMutex
    session   *sessionState

    // NEW: serializes ForceRefresh to prevent login storms
    forceRefreshMu sync.Mutex
}

func (a *WarwickAuth) ForceRefresh() (string, error) {
    a.forceRefreshMu.Lock()
    defer a.forceRefreshMu.Unlock()

    // Double-check: someone else may have refreshed while we waited
    a.sessionMu.RLock()
    if a.session != nil && time.Now().Before(a.session.expiresAt.Add(-sessionRefreshBuffer)) {
        cookie := a.session.cookieValue
        a.sessionMu.RUnlock()
        return cookie, nil
    }
    a.sessionMu.RUnlock()

    session, err := a.performLogin()
    if err != nil {
        return "", err
    }

    a.sessionMu.Lock()
    a.session = session
    a.sessionMu.Unlock()
    return session.cookieValue, nil
}
```

**Effect on cascade (ClassroomClient path):**

```
Before: Nth caller → Nth login → Nth new session → Nth orphaned cookies
After:  1st caller → 1 login → Nth callers → double-check → 0 additional logins
```

### Layer 2: GetValidSession Integration with ForceRefresh (Prevent Stale Cookie Delivery)

**Problem:** After ForceRefresh updates `a.session`, a concurrent `GetValidSession()` may still return the old cookie if it took the RLock before the Lock in ForceRefresh.

**Fix:** Use generation counter. `GetValidSession` acquires RLock, reads the generation. If the generation is stale (an ForceRefresh happened between reading session and making the request), the caller retries.

**Implementation — `internal/warwick/auth.go`:**

```go
type sessionState struct {
    cookieValue string
    obtainedAt  time.Time
    expiresAt   time.Time
    generation  uint64          // NEW: incremented on each ForceRefresh
}

type WarwickAuth struct {
    // ... existing fields
    currentGen  uint64          // NEW: global generation counter
}

func (a *WarwickAuth) GetValidSession() (string, error) {
    a.sessionMu.RLock()
    if a.session != nil && time.Now().Before(a.session.expiresAt.Add(-sessionRefreshBuffer)) {
        cookie := a.session.cookieValue
        gen := a.session.generation
        a.sessionMu.RUnlock()
        return cookie, gen, nil  // NOTE: return type changes
    }
    a.sessionMu.RUnlock()
    // ... existing refresh logic
}

func (a *WarwickAuth) ForceRefresh() (string, error) {
    a.forceRefreshMu.Lock()
    defer a.forceRefreshMu.Unlock()
    // double-check ...
    session, err := a.performLogin()
    if err != nil { return "", err }

    a.sessionMu.Lock()
    a.currentGen++
    session.generation = a.currentGen
    a.session = session
    a.sessionMu.Unlock()
    return session.cookieValue, nil
}
```

**Alternative (simpler, no generation tracking):**

Record `lastRefreshAt` timestamp. `GetValidSession()` returns a cookie AND a timestamp. Callers pass the timestamp with the request. After ForceRefresh, a `lastRefreshAt` check tells callers their session is stale.

**Simplest approach (recommended):** Modify `GetValidSession` to peek at `forceRefreshMu`:

```go
func (a *WarwickAuth) getValidSessionWithGen() (string, uint64, error) {
    a.sessionMu.RLock()
    if a.session != nil && time.Now().Before(a.session.expiresAt.Add(-sessionRefreshBuffer)) {
        cookie := a.session.cookieValue
        gen := a.session.generation // always 0 if not implemented, but we add the field
        a.sessionMu.RUnlock()
        return cookie, gen, nil
    }
    a.sessionMu.RUnlock()
    // ... login path
}
```

Then in the client methods, after a failed request:
```go
// If we detect AuthExpired, it might be because another goroutine just refreshed
// Don't immediately ForceRefresh — check if there's a new session
cookie, err := c.auth.GetValidSession() // may return fresh cookie if another goroutine already refreshed
if err == nil {
    // retry with this cookie
}
```

### Layer 3: Auto-Recovery for Room Workers (Prevent Mass Death)

**Problem:** `runRoomWorker` in `internal/service/room_manager.go` terminates permanently on `AuthExpired` (lines 243-265). No recovery path.

**Fix:** Instead of cancelling the worker and returning, enter a recovery loop with exponential backoff. Each retry acquires a fresh session (via ForceRefresh, which is now serialized) and retries the QR fetch.

**State machine change — `internal/domain/room.go`:**
```go
var allowedTransitions = map[RoomStatus][]RoomStatus{
    // ... existing
    AuthExpired: {Fetching, Stopped},  // ADD Fetching as valid transition
}
```

**Worker change — `internal/service/room_manager.go`:**

Replace the permanent death on AuthExpired with a backoff loop:

```go
const (
    maxBackoff      = 60 * time.Second
    baseBackoff     = 1 * time.Second
    recoveryRetries = 5
)

func (rm *RoomManager) runRoomWorker(state *RoomState) {
    // ... existing setup ...
    backoff := baseBackoff

    for {
        select {
        case <-state.ctx.Done():
            return
        case <-time.After(1 * time.Second):
            // ... existing fetch logic ...

            resp, err := rm.qrClient.FetchQR(classID)
            if err != nil {
                if errors.Is(err, domain.ErrAuthExpired) {
                    // Don't kill the worker — enter recovery
                    rm.mu.Lock()
                    if err := state.room.TransitionTo(domain.AuthExpired); err != nil {
                        slog.Warn("invalid transition", "error", err)
                    }
                    msg := "Session expired, attempting recovery"
                    state.room.ErrorMessage = &msg
                    roomCopy := state.room
                    rm.mu.Unlock()

                    rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})

                    // Recovery backoff loop
                    if !rm.recoverSession(state, backoff) {
                        return // recovery exhausted, worker dies
                    }
                    // Exponential backoff for next time
                    backoff *= 2
                    if backoff > maxBackoff {
                        backoff = maxBackoff
                    }
                    continue // retry the fetch
                }
                // ... other error handling ...
            }

            // Success — reset backoff
            backoff = baseBackoff
            // ... process QR response ...
        }
    }
}

func (rm *RoomManager) recoverSession(state *RoomState, backoff time.Duration) bool {
    for attempt := 0; attempt < recoveryRetries; attempt++ {
        select {
        case <-state.ctx.Done():
            return false
        case <-time.After(backoff):
            // Use FetchQRWithFreshAuth which calls ForceRefresh
            // ForceRefresh is now serialized — only 1 login even if N rooms recover
            _, err := rm.qrClient.FetchQRWithFreshAuth(state.room.ClassID)
            if err == nil {
                slog.Info("session recovered", "room_id", state.room.RoomID, "attempt", attempt+1)
                rm.mu.Lock()
                state.room.ErrorMessage = nil
                if err := state.room.TransitionTo(domain.Fetching); err != nil {
                    slog.Warn("invalid transition", "error", err)
                }
                rm.mu.Unlock()
                return true
            }
            slog.Warn("session recovery failed", "room_id", state.room.RoomID,
                "attempt", attempt+1, "backoff", backoff)
        }
    }
    slog.Error("session recovery exhausted", "room_id", state.room.RoomID)
    return false
}
```

**Recovery flow:**

```
AuthExpired → recovery loop → exponential backoff → FetchQRWithFreshAuth
                                                    │
                                          ┌─────────┴──────────┐
                                          ▼                    ▼
                                    ForceRefresh (serialized)  dead session
                                          │                    (room stays AuthExpired)
                                          ▼
                                    New session obtained
                                          │
                                          ▼
                                    Room transitions back to Fetching → Running
```

### Layer 4: Session Pool (Optional — Bounded Blast Radius)

**Problem:** Even with serialized ForceRefresh, the old cookie is still orphaned and all rooms sharing it have their inflight requests fail. They recover, but the failure is system-wide.

**Fix:** Create a pool of N independent `WarwickAuth` instances. Each room worker acquires one from the pool. ForceRefresh on one session only affects rooms sharing THAT session.

**Implementation — new file `internal/warwick/session_pool.go`:**

```go
type SessionPool struct {
    auths   []*WarwickAuth
    index   uint64
    size    int
    mu      sync.Mutex
}

func NewSessionPool(email, password, loginURL string, size int) (*SessionPool, error) {
    auths := make([]*WarwickAuth, size)
    for i := 0; i < size; i++ {
        auths[i] = NewWarwickAuth(email, password, loginURL)
        // Pre-auth all sessions at startup
        if _, err := auths[i].GetValidSession(); err != nil {
            return nil, fmt.Errorf("pre-auth session %d: %w", i, err)
        }
    }
    return &SessionPool{auths: auths, size: size}, nil
}

func (sp *SessionPool) Acquire() *WarwickAuth {
    sp.mu.Lock()
    idx := sp.index
    sp.index++
    sp.mu.Unlock()
    return sp.auths[idx%uint64(sp.size)]
}
```

**Integration — `internal/service/room_manager.go`:**

```go
type RoomManager struct {
    // ...
    qrClient    domain.QrClient     // keep for backward compat or replace
    sessionPool *warwick.SessionPool // NEW
}

func (rm *RoomManager) StartRoom(roomID string) error {
    // ...
    state.auth = rm.sessionPool.Acquire() // assign session to room
    go rm.runRoomWorker(state)
}
```

**Each room worker creates its own QrClient using its assigned auth:**

```go
type RoomState struct {
    room   domain.Room
    ctx    context.Context
    cancel context.CancelFunc
    auth   *warwick.WarwickAuth   // NEW: per-room auth
    qrClient *warwick.WarwickQrClient // NEW: per-room qr client
}
```

**Isolation math:**

| Pool Size | Rooms | Rooms/Session | Cascade Blast Radius |
|-----------|-------|---------------|---------------------|
| 1         | 30    | 30            | 30 rooms (current)  |
| 5         | 30    | 6             | 6 rooms             |
| 10        | 30    | 3             | 3 rooms             |
| 30        | 30    | 1             | 1 room (full isolation) |

**Trade-off:** More pool sessions = more concurrent logins at startup. Warwick may rate-limit this. Recommend pool size = 3-5 for most deployments.

### Layer 5: Stale Cookie Grace Period (Draining)

**Problem:** When ForceRefresh issues a new session, in-flight requests using the old cookie inevitably fail. These are already in-flight — we can't recall them.

**Mitigation:** After ForceRefresh, keep the old session in a "draining" set for a short grace period. If a caller detects AuthExpired with a recently-drained session, it knows to retry with the new session instead of calling ForceRefresh again. This complements the serialized ForceRefresh.

**Implementation — `internal/warwick/auth.go`:**

```go
const sessionDrainPeriod = 5 * time.Second

type WarwickAuth struct {
    // ...
    draining     map[uint64]time.Time  // generation → drain deadline
    drainMu      sync.Mutex
}

func (a *WarwickAuth) ForceRefresh() (string, error) {
    a.forceRefreshMu.Lock()
    defer a.forceRefreshMu.Unlock()

    // double-check...
    a.sessionMu.RLock()
    oldGen := a.currentGen
    a.sessionMu.RUnlock()

    session, err := a.performLogin()
    if err != nil { return "", err }

    a.sessionMu.Lock()
    a.currentGen++
    session.generation = a.currentGen
    a.session = session
    a.sessionMu.Unlock()

    // Mark old generation as draining
    a.drainMu.Lock()
    a.draining[oldGen] = time.Now().Add(sessionDrainPeriod)
    a.drainMu.Unlock()

    // Cleanup expired draining entries
    go a.cleanDraining()

    return session.cookieValue, nil
}

func (a *WarwickAuth) IsGenerationDraining(gen uint64) bool {
    a.drainMu.Lock()
    defer a.drainMu.Unlock()
    deadline, ok := a.draining[gen]
    if !ok { return false }
    if time.Now().After(deadline) {
        delete(a.draining, gen)
        return false
    }
    return true
}
```

**Usage in client layer:**

When a caller detects AuthExpired and has a generation number, it checks if that generation is draining. If yes, it retries with `GetValidSession()` (which now returns the new session) instead of calling `ForceRefresh()`.

## Files Affected

| File | Change | Priority |
|------|--------|----------|
| `internal/warwick/auth.go` | Serialized ForceRefresh + generation tracking + draining | **Critical** |
| `internal/domain/room.go` | Add `AuthExpired → Fetching` transition | **High** |
| `internal/domain/client.go` | No changes needed (interface already has `FetchQRWithFreshAuth`) | None |
| `internal/service/room_manager.go` | Auto-recovery loop in `runRoomWorker` | **High** |
| `internal/warwick/classroom_client.go` | Remove redundant ForceRefresh retry (now handled by serialized ForceRefresh) | Medium |
| `internal/warwick/session_pool.go` | NEW — session pool implementation | Optional |
| `cmd/server/main.go` | Pass session pool to RoomManager | Optional |
| `internal/warwick/client.go` | No changes needed (uses auth.ForceRefresh which will be serialized) | None |

## Implementation Order

### Phase 1 (Critical — Stop the Cascade)
1. `internal/warwick/auth.go`: Add `forceRefreshMu`, double-check pattern in `ForceRefresh`
2. Test: 30 concurrent `ForceRefresh()` calls → exactly 1 login to Warwick

### Phase 2 (High — Prevent Mass Death)
3. `internal/domain/room.go`: Allow `AuthExpired → Fetching`
4. `internal/service/room_manager.go`: Replace permanent worker death with recovery backoff loop
5. Test: Simulate AuthExpired → verify room recovers within backoff bounds

### Phase 3 (Medium — Reduce Blast Radius)
6. `internal/warwick/session_pool.go`: Create session pool
7. `internal/service/room_manager.go`: Per-room auth assignment
8. `cmd/server/main.go`: Initialize pool, inject into RoomManager

### Phase 4 (Low — Grace Period)
9. `internal/warwick/auth.go`: Add draining set + generation tracking
10. Client methods: Check `IsGenerationDraining` before ForceRefresh

## Key Decisions

### Why serialized ForceRefresh + double-check is the core fix

It directly prevents the 29→ForceRefresh multiplication. With this fix:
- 30 rooms hit AuthExpired simultaneously
- All 30 call ForceRefresh (from ClassroomClient retry or recovery loop)
- **Only 1 performs the login**
- The other 29 get the new session without additional logins
- Warwick sees 1 login, not 30

### Why auto-recovery is necessary even with serialized ForceRefresh

The QR workers (`runRoomWorker`) call `FetchQR()` which does NOT retry with ForceRefresh. They just die. Serialized ForceRefresh doesn't help them because they never call it. Auto-recovery gives them a path back to life.

### Why session pool is optional but valuable

Serialized ForceRefresh prevents the login storm. Auto-recovery prevents mass death. But session pool bounds the blast radius — when a ForceRefresh happens, only rooms sharing that session see their in-flight requests fail. With 3 sessions and 30 rooms, at most 10 rooms are affected per refresh.

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Warwick rate-limits pre-auth of multiple sessions at startup | Pool size small (3-5); stagger pre-auth with delays |
| Recovery backoff loop causes duplicate QR fetches during recovery | Recovery loop uses `FetchQRWithFreshAuth` which combines refresh + fetch |
| Generation tracking adds complexity | Start without it (Phase 1-2); add in Phase 4 if needed |
| Draining period delays session cleanup | Short window (5s); old cookie is already dead on Warwick side |
| `AuthExpired → Fetching` transition breaks existing state reasoning | It's additive — `AuthExpired → Stopped` still works for manual stop |

---

*Design review: 2026-05-30*
