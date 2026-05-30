---
phase: 02-code-review-command
reviewed: 2026-05-30T15:00:00Z
depth: deep
files_reviewed: 12
files_reviewed_list:
  - cmd/server/main.go
  - internal/service/data_refresher.go
  - internal/warwick/classroom_client.go
  - internal/warwick/session_pool.go
  - internal/warwick/client.go
  - internal/warwick/client_test.go
  - internal/api/routes.go
  - internal/api/teacher_handlers.go
  - internal/cache/cache.go
  - internal/domain/room.go
  - internal/domain/classroom.go
  - internal/service/room_manager.go
findings:
  critical: 0
  warning: 7
  info: 5
  total: 12
status: issues_found
---

# Phase 2: Final Holistic Code Review — Tasks 1-3

**Reviewed:** 2026-05-30T15:00:00Z
**Depth:** deep (cross-file call-chain tracing)
**Files Reviewed:** 12 (implementation + supporting context)
**Status:** issues_found

## Summary

Holistic review of the "leader-based cache warmer" implementation across 3 tasks (pool tier isolation + shared cache + DataRefresher service). Architecture is sound: the wiring from pool → sharedCache → classroomClient → refresher → handlers is clean, no circular dependencies, all nil guards present, shutdown via ctx cancellation works. Tier isolation (QR/Teacher/Interactive) correctly partitions traffic.

The core design achieves its goal: REST reads for courses and course details hit the shared cache (30s TTL, refreshed every 30s by the single background goroutine), eliminating pool contention for those paths. ToggleCheckin gets its own tier (`TierInteractive`, 2 sessions), avoiding head-of-line blocking.

**Key architectural risk:** The DataRefresher shares `TierTeacher` (1 session) with REST handlers. During the ~2-20s refresh window each cycle, REST reads for cache-missed data compete with the refresher for the same single session. Session details (5s TTL, not background-refreshed) are affected most — a read arriving after TTL expiry always needs the pool.

**Resolved from prior review:** Nil guard on `*cache.Cache` in health handler (WR-02) was fixed in `426825c`. Session cache invalidation in non-pool toggle path was also added.

**Still unresolved from prior review:** Unsafe type assertions on cache reads (WR-01) and bare `!=` for sentinel error in toggle retry (WR-03) remain unaddressed.

## Warnings

### WR-01: Teacher-tier contention between DataRefresher and REST handlers

**File:** `internal/service/data_refresher.go:61-96` & `internal/warwick/session_pool.go:194-196`
**Files affected:** All REST handlers using `TierTeacher`

The DataRefresher is constructed with a `ClassroomClient` configured for `TierTeacher` (1 session). During each 30s refresh cycle, the refresher iterates active courses calling `GetCourseDetail` — each call acquires and holds the sole teacher session for ~0.5-5s. Total hold time per cycle: ~2-20s depending on active course count and network latency.

During this window, REST handlers (`GET /courses/{id}`, `GET /courses/{id}/sessions/{sid}`, `GET /courses`) that miss cache (TTL expiry, startup transient, or refresher hadn't stored new value yet) attempt pool acquisition on the same single-session tier → `ErrPoolExhausted` → 503.

Session details (5s TTL, not refreshed by DataRefresher) are the most vulnerable — every cache miss requires pool access while the refresher may be holding the tier.

**Impact:** Intermittent 503 on REST endpoints during refresher activity. Higher probability at startup (cold cache), lower steady-state (warm cache absorbs most reads).

**Fix options:**
1. Give DataRefresher its own tier (e.g., `TierCacheWarmer` with 1 dedicated session) — fully isolates refresher from REST. Cost: +1 session.
2. Stagger refresh to avoid holding session for the entire active-course loop: release session between each `GetCourseDetail` call (cache already populated by previous call). Currently `getCourseDetailWithPool` acquires, fetches, caches, releases per-call via `defer` — so this is already per-call. But the refresher's iteration through courses is sequential; each call is an independent acquire/release cycle. So the window is per-`GetCourseDetail` call, not the entire loop. **Mitigation is already in place**, but with 1 teacher session, collision probability is still non-zero during peak.

---

### WR-02: No minimum floor on WARWICK_CACHE_INTERVAL

**File:** `cmd/server/main.go:45`
```go
cacheInterval := getEnvDuration("WARWICK_CACHE_INTERVAL", 30*time.Second)
```

**File:** `cmd/server/main.go:145-160`
```go
func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
    ...
    if d <= 0 {
        slog.Warn(...)
        return defaultVal
    }
    return d
}
```

Validation rejects ≤0 but accepts any positive value. Setting `WARWICK_CACHE_INTERVAL=1ms` would cause the refresher to issue ~1000 requests/second to Warwick, rate-limiting the IP and disrupting all sessions. Minimum should be at least 5s.

**Impact:** Misconfiguration or user error can cause a DoS on the upstream Warwick server.

**Fix:**
```go
const minCacheInterval = 5 * time.Second
if d < minCacheInterval {
    slog.Warn("cache interval too short, using minimum", "key", key, "value", val, "min", minCacheInterval)
    return minCacheInterval
}
```

---

### WR-03: Context not propagated to HTTP requests — WarmOnce timeout is ineffective

**File:** `internal/warwick/classroom_client.go:546-560`
```go
func (c *ClassroomClient) doRequest(method, path, cookie string, body io.Reader) (*http.Response, error) {
    u := c.baseURL + path
    req, err := http.NewRequest(method, u, body)  // context.Background() implicit
    ...
    return c.client.Do(req)  // not cancellable
}
```

No method on `ClassroomClient` accepts a `context.Context`. All HTTP calls use `http.NewRequest(method, u, body)` which defaults to `context.Background()`. This means:
- `WarmOnce(ctx)` with 10s timeout: if `GetCourseDetail` HTTP calls exceed 10s total, the timeout fires and returns `ctx.Err()` but the HTTP response continues, eventually updating the cache after shutdown has started.
- Server shutdown: in-flight refresher HTTP requests aren't cancelled by ctx cancellation.
- Session detail reads from handlers: HTTP request continues even if client disconnects.

**Impact:** Delayed shutdown (goroutines persist until HTTP timeout/complete). Warmup timeout doesn't actually bound network work.

**Fix:** Thread context through:
```go
func (c *ClassroomClient) doRequest(ctx context.Context, method, path, cookie string, body io.Reader) {
    req, err := http.NewRequestWithContext(ctx, method, u, body)
    ...
}
```
Then propagate from callers. This is a larger refactor but critical for proper lifecycle management.

---

### WR-04: Session detail cache (5s TTL) not refreshed by DataRefresher — partial pool contention remains

**File:** `internal/service/data_refresher.go:61-96`

`DataRefresher.refresh()` calls `GetCourses()` + `GetCourseDetail()` for active courses. It does NOT call `GetSessionDetail()`. This means session details are only cached on-demand by REST handlers with a 5s TTL. After TTL expiry, every REST read for session detail must acquire the teacher pool session — potentially colliding with the refresher or other REST reads.

**Impact:** The design goal of "zero pool acquisition for REST read paths" is only achieved for courses and course details, not session details. Session detail reads remain pool-dependent.

**Fix:** Add session detail pre-fetching to the refresher:
```go
// In refresh(), after caching course details:
for _, detail := range courseDetails {
    for _, session := range detail.Sessions {
        select {
        case <-ctx.Done():
            return
        default:
        }
        d.cc.GetSessionDetail(detail.CourseID, session.SessionID)  // warms session cache
    }
}
```
This increases refresher workload but eliminates pool contention for session reads. Alternative: increase session detail cache TTL to match the refresh interval (30s) and accept staleness.

---

### WR-05: Unsafe type assertions on all cache reads (unresolved from prior review)

**File:** `internal/warwick/classroom_client.go` — lines 74, 112, 131, 212, 251, 270, 368

Every cache-read path uses bare type assertion:
```go
if cached, ok := c.cache.Get("courses"); ok {
    return cached.([]domain.CourseSummary), nil  // panics if wrong type
}
```

A cache key collision or programming error (wrong type stored under `"courses"`, `"course:"+id`, or `"session:"+id`) causes an unrecoverable panic. With the shared cache now used across the refresher (background goroutine) and REST handlers (HTTP goroutines), the risk surface has expanded.

**Impact:** Single wrong-type store crashes the server process (panic in handler or refresher goroutine).

**Fix:**
```go
if cached, ok := c.cache.Get(key); ok {
    if result, ok := cached.([]domain.CourseSummary); ok {
        return result, nil
    }
    slog.Error("cache type mismatch", "key", key, "expected", "[]domain.CourseSummary", "got", fmt.Sprintf("%T", cached))
    c.cache.Invalidate(key)
}
```
Apply this pattern to all 6 cache-read sites.

---

### WR-06: Bare `!=` comparison for sentinel error in toggle retry (unresolved from prior review)

**File:** `internal/warwick/classroom_client.go:505, 472`

```go
if err != domain.ErrAuthExpired || attempt == 1 {
    break
}
```

Uses pointer equality instead of `errors.Is`. If `doToggleCheckin` ever wraps `ErrAuthExpired` (e.g., with `fmt.Errorf("...: %w", ...)`) or if a middleware wraps the error, the retry logic silently breaks — the function exits without attempting force-refresh.

This exists in both `toggleCheckinWithPool` (pool path, line 505) and the legacy `ToggleCheckin` (non-pool, line 472). The non-pool path is dead code in the current deployment, but the pool path is live.

**Impact:** Missed retry opportunity on auth expiry during toggle — user gets unexpected error instead of transparent session refresh.

**Fix:**
```go
if !errors.Is(err, domain.ErrAuthExpired) || attempt == 1 {
    break
}
```

---

### WR-07: Data race on `pooledSession.inUse` in Acquire error path

**File:** `internal/warwick/session_pool.go:229`

```go
for offset := 0; offset < (end - start); offset++ {
    idx := start + (next+offset)%(end-start)
    s := p.sessions[idx]
    if !s.inUse {
        s.inUse = true
        p.mu.Unlock()         // lock released
        cookie, gen, err := p.ensureValidSession(s)
        if err != nil {
            s.inUse = false   // <-- written without p.mu held
            return nil, ...
        }
```

`Acquire` sets `s.inUse = true` while holding `p.mu`, then releases `p.mu`. On error from `ensureValidSession`, `s.inUse = false` is written without `p.mu` held. Meanwhile `Release()` (line 248-255) writes `s.inUse = false` with `p.mu` held. This is a data race per Go's memory model — the Go race detector would flag concurrent non-atomic writes to the same field.

**Impact:** Theoretical memory corruption. In practice on x86, a bool write is architecturally atomic, but the race detector will flag this, and the Go memory model does not guarantee visibility across goroutines without synchronization.

**Fix:**
```go
// Hold p.mu for the error path write:
p.mu.Lock()
s.inUse = false
p.mu.Unlock()
return nil, ...
```
Or make `inUse` an `atomic.Bool`.

## Info

### IN-01: Code duplication between DataRefresher.refresh and WarmOnce

**File:** `internal/service/data_refresher.go:61-96` and `lines 112-149`

`refresh()` and `WarmOnce()` are ~90% identical — same course iteration, same `ctx.Done()` check pattern, same error handling. If a bug is fixed in one but not the other, they diverge. 

**Suggestion:** Extract shared logic:
```go
func (d *DataRefresher) refreshCourses(ctx context.Context) (int, error) {
    courses, err := d.cc.GetCourses()
    if err != nil {
        return 0, err
    }
    detailCount := 0
    for _, course := range courses {
        if course.Status != domain.CourseStatusFinished {
            select {
            case <-ctx.Done():
                return detailCount, ctx.Err()
            default:
            }
            if _, err := d.cc.GetCourseDetail(course.CourseID); err != nil {
                slog.Warn("...", "course_id", course.CourseID, "error", err)
                continue
            }
            detailCount++
        }
    }
    d.warm.Store(true)
    d.lastFetch.Store(time.Now())
    return detailCount, nil
}
```

---

### IN-02: Non-pool code paths don't check cache for GetCourseDetail/GetSessionDetail

**File:** `internal/warwick/classroom_client.go:208-246` and `335-363`

`GetCourseDetail()` non-pool path doesn't check cache before fetching. `GetSessionDetail()` non-pool path doesn't check cache either. Only the pool variants (`getCourseDetailWithPool`, `getSessionDetailWithPool`) check cache. Meanwhile `GetCourses()` checks cache in both paths consistently.

These non-pool paths are dead code in the current deployment (pool is always used), but inconsistency invites bugs if someone later swaps constructors.

---

### IN-03: "CouseID" typo in fetchCourseDetail parameter

**File:** `internal/warwick/classroom_client.go:294`
```go
"CouseID": courseID,
```

Parameter name is `"CouseID"` — likely a typo for `"CourseID"`. If the Warwick API actually accepts `"CouseID"`, this is fine. If it expects `"CourseID"`, course detail requests silently fail (return empty results). Pre-existing, not introduced by these tasks.

---

### IN-04: Hardcoded NewCount:0 in toggle response (pre-existing)

**File:** `internal/api/teacher_handlers.go:149`
```go
NewCount: 0,
```

`doToggleCheckin` discards the response body on success. The actual check-in count from Warwick is never returned to the frontend.

---

### IN-05: DataRefresher has no unit tests

**File:** `internal/service/data_refresher.go` (entire file)

No tests for `Run`, `WarmOnce`, `refresh`, or panic recovery. The only service tests are in `room_test.go`. The background refresh loop and its interaction with context cancellation, cache state, and error conditions are untested.

---

## Assessment

**Strengths:**
- Clean DI: pool → cache → client → refresher → handler wiring without globals or circular deps
- Tier isolation is correct: QR (sessions 0-1), Teacher (session 2), Interactive (sessions 3-4)
- Error mapping consistent: `errors.Is` in handlers, 503 for `ErrPoolExhausted`/`ErrAuthConflict`
- DataRefresher matches existing patterns: panic recovery, atomic.Bool, slog logging
- Cache invalidation on toggle write covers all 3 affected keys (course, courses, session)
- Graceful shutdown: refresher goroutine stops on ctx cancellation (with caveat WR-03)
- `WarmOnce` has a bounded 10s timeout preventing indefinite startup block

**Key Risks:**

1. **Teacher-tier contention (WR-01, WR-04):** The refresher and REST handlers share 1 teacher session. Session detail reads (5s TTL, no background refresh) are especially vulnerable. During a refresh cycle, REST reads for cache-missed data get 503. This partially undermines the "zero pool acquisition for reads" design goal.

2. **Context isolation failure (WR-03):** No HTTP request carries a cancellable context. The WarmOnce 10s timeout doesn't actually bound network work. Server shutdown waits for in-flight HTTP calls to complete naturally.

3. **Unsafe type assertions (WR-05)** remain unfixed from the prior review. With the shared cache now accessed from both the background refresher and REST goroutines, a single wrong-type store crashes the process.

4. **Sentinel error comparison (WR-06)** also unfixed — the toggle retry path uses `!=` instead of `errors.Is`, making it fragile to error wrapping.

5. **No minimum interval (WR-02)** allows misconfiguration to DoS the upstream Warwick server.

**Overall:** The implementation is well-structured and follows existing codebase patterns. The architectural risks are manageable — the teacher-tier contention is partially mitigated by per-call session acquire/release (not per-refresh-cycle hold). The context-propagation gap and unsafe assertions are the highest-impact items to address before production deployment.

---

_Reviewed: 2026-05-30T15:00:00Z_
_Reviewer: gsd-code-reviewer (final holistic)_
_Depth: deep_
