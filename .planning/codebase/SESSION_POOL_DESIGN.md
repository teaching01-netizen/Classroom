# Session Pool Architecture — Head-of-Line Blocking Fix

**Date:** 2026-05-30

## Problem Summary

All Go → Warwick requests share **one** `ASP.NET_SessionId` cookie. ASP.NET's default `InProc` session mode serializes requests per session ID — only one request executes at a time. With N room workers polling QR codes (~5s each) and teacher API handlers interleaved, `p99 latency = Σ(latency of all concurrent requests)`.

**Root cause chain:**
```
Shared WarwickAuth.session (one cookie)
  → One ASP.NET_SessionId
    → ASP.NET InProc session lock
      → All goroutines block on each other
```

**Constraint:** Zero changes to Warwick ASP.NET config. No `ReadOnly`, no `StateServer`, no `<sessionState>` tweaks.

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                        Go App                                │
│                                                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │  Room Worker 1│  │  Room Worker 2│  │  Teacher API  │       │
│  │  (QR Poll)    │  │  (QR Poll)    │  │  Handlers     │       │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘       │
│         │                 │                 │                │
│         ▼                 ▼                 ▼                │
│  ┌─────────────────────────────────────────────────────┐     │
│  │               SessionPool (acquire/release)          │     │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────┐ │     │
│  │  │ Session 1 │ │ Session 2 │ │ Session 3 │ │  ...   │ │     │
│  │  │ (QR pool) │ │ (QR pool) │ │ (Teacher) │ │(Spare) │ │     │
│  │  └─────┬─────┘ └─────┬─────┘ └─────┬─────┘ └────┬───┘ │     │
│  └────────┼──────────────┼──────────────┼────────────┼─────┘     │
│           │              │              │            │          │
│           ▼              ▼              ▼            ▼          │
│  ┌─────────────────────────────────────────────────────┐     │
│  │  http.Client  │  CookieJar  │  sessionState         │     │
│  │  (per session)│  (per sess) │  + auth creds         │     │
│  └─────────────────────────────────────────────────────┘     │
│                                                              │
│           │              │              │                     │
│           ▼              ▼              ▼                     │
│     ┌─────────────────────────────────────┐                   │
│     │       Warwick ASP.NET (external)     │                   │
│     │  ┌───────────────────────────────┐  │                   │
│     │  │  Session 1  │  Session 2  │ … │  │  ← parallel exec │
│     │  │  (SID=a)    │  (SID=b)    │   │  │  (no cross-block) │
│     │  └───────────────────────────────┘  │                   │
│     └─────────────────────────────────────┘                   │
└─────────────────────────────────────────────────────────────┘
```

## Key Data Structures

### `WarwickSession`

```go
// internal/warwick/session_pool.go

// WarwickSession is an independent authenticated session with Warwick.
// Each instance has its own http.Client, CookieJar, and ASP.NET_SessionId.
type WarwickSession struct {
    id       int
    client   *http.Client
    cookie   string          // ASP.NET_SessionId value
    obtained time.Time
    expires  time.Time
    mu       sync.RWMutex    // guards cookie/expires within this session
    auther   *sessionAuthenticator // shared creds, no session state
}

// SessionToken is an acquired session handle with usage tracking.
type SessionToken struct {
    session   *WarwickSession
    acquired  time.Time
    released  chan struct{}
}
```

### `SessionPool`

```go
// SessionPool manages a pool of independent Warwick sessions.
// Sessions are organized by tier (QR polling vs teacher API).
type SessionPool struct {
    qrSessions      []*WarwickSession
    teacherSessions []*WarwickSession
    spareSessions   []*WarwickSession  // fallback for either tier

    qrNext      uint64   // round-robin index for QR sessions
    teacherNext uint64   // round-robin index for teacher sessions

    auth       *sessionAuthenticator
    poolSize   int
    healthCh   chan *WarwickSession  // sessions needing re-auth
    stopCh     chan struct{}

    logger     *slog.Logger
}

type SessionTier int
const (
    TierQR       SessionTier = iota  // QR polling (low-priority, slow)
    TierTeacher                       // Teacher API (medium, browsing)
    TierInteractive                   // Toggle check-in (high-priority, fast)
)
```

## Session Assignment Strategy

### Tier mapping

| Caller | Endpoint | Tier | Typical Duration | Priority |
|--------|----------|------|-----------------|----------|
| Room workers (QR poll) | `FetchQR` | `TierQR` | ~5s | Low |
| Teacher browsing | `GetCourses` | `TierTeacher` | ~5s | Medium |
| Teacher browsing | `GetCourseDetail` | `TierTeacher` | ~3s | Medium |
| Teacher browsing | `GetSessionDetail` | `TierTeacher` | ~3s | Medium |
| Teacher action | `ToggleCheckin` | `TierInteractive` | ~200ms | High |

### Assignment rules

1. **QR pool sessions** → only room workers. Round-robin across `qrSessions`.
   - N rooms / M QR sessions (M << N, e.g. 3 QR sessions for 20 rooms).
   - Each QR session handles at most 1 room at a time → each room poll serializes only within its assigned session.
   - Two rooms on different QR sessions → fully parallel.

2. **Teacher sessions** → dedicated for teacher API handlers (`/api/teacher/*`).
   - Few concurrent teachers (1–5). Allocate 1 session per concurrent request.
   - `ToggleCheckin` steals from the teacher pool if all QR sessions are busy — or gets its own tier.

3. **Interactive sessions** → `ToggleCheckin` only. Fast query (~200ms). Dedicated 1–2 sessions so it never queues behind a 5s DataTables query.

### Round-robin logic

```go
func (p *SessionPool) Acquire(tier SessionTier) (*SessionToken, error) {
    var pool []*WarwickSession
    var idx *uint64
    switch tier {
    case TierQR:
        pool = p.qrSessions
        idx = &p.qrNext
    case TierTeacher, TierInteractive:
        pool = p.teacherSessions
        idx = &p.teacherNext
    }

    // Round-robin with busy check
    start := atomic.AddUint64(idx, 1) % uint64(len(pool))
    for i := 0; i < len(pool); i++ {
        sess := pool[(start+uint64(i)) % uint64(len(pool))]
        if sess.TryAcquire() {
            return &SessionToken{session: sess, acquired: time.Now()}, nil
        }
    }
    // All busy — fall back to spare sessions
    for _, sess := range p.spareSessions {
        if sess.TryAcquire() {
            return &SessionToken{session: sess, acquired: time.Now()}, nil
        }
    }
    return nil, ErrAllSessionsBusy
}
```

## `WarwickAuth` → `SessionPool` Migration

### What changes

| Current | New |
|---------|-----|
| `WarwickAuth` struct | `WarwickAuth` becomes internal to `sessionAuthenticator` |
| `WarwickAuth.GetValidSession()` → returns cookie string | `SessionPool.Acquire(TierQR)` → returns `*SessionToken` |
| `WarwickAuth.ForceRefresh()` → re-login, replace cookie | `SessionPool.ForceRefresh(sessionID)` → re-auth one session |
| `ClassroomClient.auth *WarwickAuth` | `ClassroomClient.pool *SessionPool` |
| `WarwickQrClient.auth *WarwickAuth` | `WarwickQrClient.pool *SessionPool` |
| Manual cookie in `doRequest` | `sessionToken.Session().SetCookieOn(req)` |
| Sync retry-with-refresh in client methods | Pool handles re-auth transparently |

### Cookie management change

**Current (auth.go):** Cookie is a `string` stored in `sessionState`. Set via `req.Header.Set("Cookie", fmt.Sprintf("ASP.NET_SessionId=%s", cookie))`.

**New:** Each `WarwickSession` has its own `*http.Client` with a proper `cookieJar`. The cookie is set automatically by the jar after the login response. All subsequent `client.Do(req)` calls automatically send the cookie. No manual `Set-Cookie` header manipulation needed.

This means `doRequest` and `doFetch` no longer take a `cookie string` parameter. Instead they call the pool to get a session, then use that session's client directly.

```go
func (c *ClassroomClient) GetCourses() ([]domain.CourseSummary, error) {
    tok, err := c.pool.Acquire(TierTeacher)
    if err != nil {
        return nil, err
    }
    defer c.pool.Release(tok)

    // Use session's own http.Client — cookie sent automatically
    return c.fetchCourses(tok.Session())
}
```

## ForceRefresh — Cascading Mitigation

**Problem:** If Warwick session expires, `ForceRefresh()` currently replaces the shared session, blocking all callers. With a pool, we must handle per-session expiry without cascading.

**Per-session re-auth flow:**

```go
// Called by the pool health goroutine, not inline in request paths.
func (p *SessionPool) reauthSession(sess *WarwickSession) error {
    sess.mu.Lock()
    defer sess.mu.Unlock()

    newCookie, err := p.auth.performLogin(sess.client)  // re-auth using same client
    if err != nil {
        return err
    }
    sess.cookie = newCookie
    sess.obtained = time.Now()
    sess.expires = time.Now().Add(sessionTTL)
    return nil
}
```

**Key design:** Each session gets re-authenticated independently. When session A expires:
1. The request on A fails with auth-expired response from Warwick.
2. The Health goroutine kicks A into re-auth.
3. Sessions B, C, D continue serving unaffected.
4. When A comes back, it rejoins the pool.

**No global ForceRefresh.** The old `ForceRefresh()` method is removed. Each session re-authenticates itself.

**Detecting expiry:** The pool health goroutine runs every 30s. It checks:
- `time.Now().After(sess.expires.Add(-sessionRefreshBuffer))` → preemptive re-auth before expiry
- Count of recent auth-expired errors per session (from request callbacks) → reactive re-auth

## Session Exhaustion

### What happens when all sessions are busy

1. **`ErrAllSessionsBusy`** returned immediately (fail-fast). Caller receives a 503 or a retryable error.

2. **Caller-side retry:** Teacher handlers can retry after a short delay (e.g. 500ms). Room workers already loop at 1s intervals — they'll pick up a session next tick.

3. **Queueing alternative (future):** Add a simple buffered channel per tier as a wait queue (e.g. `chan *WarwickSession` with length = number of sessions). Use `select` with timeout for acquire. Not implementing in V1 — KISS.

4. **Monitoring:** `slog.Warn("session pool exhausted", "tier", tier, "queue_depth", pendingCount)` when all sessions are busy.

### Sizing guidance

| Tier | Sessions | Rationale |
|------|----------|-----------|
| QR | max(3, N_rooms/5) | 3 for 20 rooms; 5 for 30 rooms. Room polls are ~5s each. |
| Teacher | max(2, concurrent_teachers) | Typically 1–2. One per active teacher browser tab. |
| Interactive | 2 | Toggle is fast, two handles bursts. |
| Spare | 1 | Overflow for both tiers. |

**Start small:** 2 QR + 2 Teacher + 1 Interactive + 1 Spare = **6 Warwick logins total**. This is safe — Warwick likely has no login limit for <10 sessions from one IP. Validate with Warwick team after deploy.

## Implementation Plan

### Files to create

| File | LOC (est) | Description |
|------|-----------|-------------|
| `internal/warwick/session_pool.go` | ~200 | `SessionPool`, `WarwickSession`, `SessionToken`, acquire/release/re-auth |
| `internal/warwick/session_pool_test.go` | ~150 | Unit tests for pool lifecycle |

### Files to modify

| File | Changes |
|------|---------|
| `internal/warwick/auth.go` | Remove `ForceRefresh()`, expose `performLogin` as `loginWithClient(client)`. Keep `FromEnv()` for creds extraction. |
| `internal/warwick/client.go` | `WarwickQrClient` takes `*SessionPool`, uses `Acquire(TierQR)`, delegates to session's client. |
| `internal/warwick/classroom_client.go` | `ClassroomClient` takes `*SessionPool`, uses `Acquire(TierTeacher)` or `Acquire(TierInteractive)`. |
| `internal/warwick/client_test.go` | Update to inject pool mock. |
| `cmd/server/main.go` | Create `SessionPool`, inject into both clients. |
| `internal/domain/client.go` | Optional: add `Tier` to `QrClient` interface or leave as implicit. |

### Migration path (incremental, zero-downtime)

**Phase 1 — 2 sessions (same-day deploy):**
1. Create `SessionPool` with 1 QR + 1 Teacher session = 2 total.
2. Both sessions log in independently.
3. Existing cookie-jar management is unchanged (`Set-Cookie` header manually set).
4. Room workers get QR session; teacher handlers get Teacher session.
5. **Immediate benefit:** QR polling and teacher browsing never block each other.
6. ToggleCheckin still shares teacher session — but that's fast (~200ms) so impact is low.

**Phase 2 — isolate ToggleCheckin (next deploy):**
1. Add TierInteractive with dedicated session(s).
2. ToggleCheckin always gets its own session → never blocked by DataTables queries.

**Phase 3 — cookie jar migration (optional refactor):**
1. Switch from manual `Set-Cookie` header to real `http.CookieJar`.
2. Each session's client handles cookies automatically.
3. Eliminates manual cookie-string-parsing code.

## Key Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Warwick rate-limits multiple logins | Monitor 429s. Start with 2 sessions. Add exponential backoff to login. |
| Warwick sessions expire faster than 60min | Pool health goroutine re-auths at 55min. Reactive re-auth on auth-expired response. |
| Session pool exhaustion under load | Fail-fast 503. Room workers retry naturally. Add `wait` queue later if needed. |
| Race on Warwick for the same classroom data | No race — each session is independent. Warwick handles its own locking. |
| `http.Client` connection pool per session consumes too many sockets | Each client has default `MaxIdleConnsPerHost=2`. With 6 sessions → 12 idle conns max. Negligible. |

## Open Questions

1. **Does Warwick have a per-IP connection limit?** Need to verify. If yes, pool size capped at that limit.
2. **Does ToggleCheckin really use the session?** Verify by inspecting Warwick's `ToggleCheckin` action attribute — if it calls `Session["key"]`, yes it locks. If it's a pure POST with no session read, it might not lock. The `ASP.NET_SessionId` cookie is still sent, so ASP.NET still serializes.
3. **Does `GetQRCode` use the session?** The endpoint needs auth (redirects to login without cookie), so yes, session is in use.

## Rollback Plan

1. Keep `WarwickAuth` constructor and `GetValidSession()`/`ForceRefresh()` in place but deprecated.
2. `SessionPool` has a `LegacyFallback() *WarwickAuth` method that returns the old auth if pool creation fails.
3. Feature flag: `WARWICK_SESSION_POOL_ENABLED=true` env var. Off → old single-session path. On → pool path.
4. If pool has issues, toggle env var, restart. No code rollback needed.
