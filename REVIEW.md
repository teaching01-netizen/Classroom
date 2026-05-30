---
phase: 02-code-review-command
reviewed: 2026-05-30T14:00:00Z
depth: standard
files_reviewed: 4
files_reviewed_list:
  - internal/warwick/classroom_client.go
  - internal/api/teacher_handlers.go
  - internal/api/routes.go
  - cmd/server/main.go
findings:
  critical: 0
  warning: 3
  info: 3
  total: 6
status: issues_found
---

# Phase 2: Code Review Report — Task 2

**Reviewed:** 2026-05-30T14:00:00Z
**Depth:** standard
**Files Reviewed:** 4
**Status:** issues_found

## Summary

Commit `a8e0124` implements shared cache injection, session-detail cache-aside with 5s TTL, `ErrNoAvailableSessions` → `ErrPoolExhausted` mapping in all 4 pool methods, `TierInteractive` for toggle, session-cache invalidation on toggle, `errors.Is` in handlers, and 503 for pool-exhaustion/auth-conflict.

Architecture is clean — shared cache passed via constructor, no new package coupling. Core behaviors (caching, tier choice, error remapping) are correct. Three warnings found: unsafe type assertion on cache-read (panic risk), missing nil guard on `*cache.Cache` in health handler, and bare `!=` comparison for sentinel error in toggle retry (inconsistent with `errors.Is` pattern used everywhere else).

## Warnings

### WR-01: Unsafe type assertion on cache read in getSessionDetailWithPool

**File:** `internal/warwick/classroom_client.go:368`
**Issue:** `cached.(*domain.SessionDetail)` panics if the cached value is not `*domain.SessionDetail`. The same unsafe pattern exists for `[]domain.CourseSummary` (line 74, 112, 131, 251, 270) and `*domain.CourseDetail` (line 212, 251). If any future code path stores a different type under a colliding key (`"session:"+sessionID`, `"course:"+courseID`, `"courses"`), this produces an unrecoverable panic instead of a cache-miss fallback.

**Fix:** Use the two-value comma-ok form:
```go
if cached, ok := c.cache.Get(key); ok {
    if detail, ok := cached.(*domain.SessionDetail); ok {
        return detail, nil
    }
    // log unexpected type and fall through to fetch
}
```

### WR-02: Nil dereference on `*cache.Cache` in healthHandler

**File:** `internal/api/routes.go:114`
**Issue:** `healthHandler(c)` receives a `*cache.Cache` and calls `c.Size()` without a nil guard. If `c` is nil (e.g., someone refactors main.go or calls `healthHandler(nil)` directly), this panics with nil pointer dereference.

**Fix:** Add nil guard before the method call:
```go
cacheSize := 0
if c != nil {
    cacheSize = c.Size()
}
```

### WR-03: Bare `!=` comparison for sentinel error in toggle retry

**File:** `internal/warwick/classroom_client.go:502`
**Issue:** `err != domain.ErrAuthExpired` uses pointer equality. If `doToggleCheckin` ever returns a wrapped error (e.g., `fmt.Errorf("...: %w", domain.ErrAuthExpired)`), this comparison silently fails and the retry logic is skipped. All handler code in `teacher_handlers.go` correctly uses `errors.Is`, but this internal pool method does not.

Same issue exists in the legacy `ToggleCheckin` non-pool path (line 469), but the new `toggleCheckinWithPool` introduced in this commit should use the correct pattern from the start.

**Fix:**
```go
if !errors.Is(err, domain.ErrAuthExpired) || attempt == 1 {
    break
}
```

## Info

### IN-01: Hardcoded `NewCount: 0` in toggle response

**File:** `internal/api/teacher_handlers.go:149`
**Issue:** `ToggleCheckinResponse.NewCount` is always returned as `0`. The frontend receives no meaningful count. This predates this commit but remains a quality gap — the count should reflect the actual check-in state.

### IN-02: Cache-aside invalidation race window

**File:** `internal/warwick/classroom_client.go:496-498`
**Issue:** After a successful toggle write, the session cache is invalidated (lines 496-498). A concurrent read that started between the write completing and the `Invalidate` call returning will serve stale data from cache. This is inherent to cache-aside; with 5s TTL the window is small and acceptable for a toggle UI. Not actionable but worth documenting in a comment.

### IN-03: Repetitive error-handling blocks in teacher_handlers.go

**File:** `internal/api/teacher_handlers.go:22-35, 55-68, 90-103, 130-143`
**Issue:** The same 3 `errors.Is` checks (`ErrAuthExpired` → 401, `ErrPoolExhausted` → 503, `ErrAuthConflict` → 503) are duplicated across all 4 handler functions. The pattern is identical modulo the handler body. Extracting to a helper function would reduce duplication and prevent drift.

```go
func mapWarwickError(err error) (int, string) {
    switch {
    case errors.Is(err, domain.ErrAuthExpired):
        return http.StatusUnauthorized, "Warwick session expired"
    case errors.Is(err, domain.ErrPoolExhausted):
        return http.StatusServiceUnavailable, "Too many concurrent requests, try again"
    case errors.Is(err, domain.ErrAuthConflict):
        return http.StatusServiceUnavailable, "Warwick session in use, try again"
    default:
        return http.StatusInternalServerError, err.Error()
    }
}
```

## Assessment

**Strengths:**
- Shared cache injection via constructor — clean DI, no package-level globals
- Session detail cache-aside with 5s TTL correctly scoped to pool path
- Session-cache invalidation on toggle write is present and correct
- `TierInteractive` used for toggle (correct tier isolation)
- `ErrNoAvailableSessions` → `ErrPoolExhausted` mapped consistently in all 4 pool methods
- `errors.Is` in handler layer correct
- 503 for both `ErrPoolExhausted` and `ErrAuthConflict` is appropriate
- Code compiles, types align, no new circular dependencies

**Key Risks:**
1. **Cache type-assertion panics** are the highest-risk finding. While unlikely in practice (keys are namespaced per type), a single programming error or future refactor can trigger an unrecoverable panic in production.
2. The toggle retry path uses bare pointer equality instead of `errors.Is`, creating a fragile dependency on direct error returns rather than wrapped errors.
3. No structural or safety changes needed; commit is focused and scoped correctly to Task 2 requirements.

---

_Reviewed: 2026-05-30T14:00:00Z_
_Reviewer: gsd-code-reviewer_
_Depth: standard_
