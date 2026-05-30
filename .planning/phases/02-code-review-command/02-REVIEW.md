---
phase: 02-code-review-command
reviewed: 2026-05-30T13:00:00Z
depth: deep
files_reviewed: 8
files_reviewed_list:
  - internal/db/migrations/003_create_teacher_favourites.up.sql
  - internal/db/migrations/003_create_teacher_favourites.down.sql
  - internal/db/favourite_repository.go
  - internal/api/favourite_handlers.go
  - internal/api/routes.go
  - cmd/server/main.go
  - web/src/store/usePinnedCoursesStore.js
  - web/src/App.jsx
findings:
  critical: 1
  warning: 5
  info: 4
  total: 10
status: issues_found
---

# Phase 02: Code Review Report — Favourite Courses DB Migration

**Reviewed:** 2026-05-30T13:00:00Z
**Depth:** deep (cross-file call-chain analysis)
**Files Reviewed:** 8
**Status:** issues_found

## Summary

Feature adds a `teacher_favourites` table, Go repository layer, CRUD API handlers, and frontend zustand refactor to persist pinned courses in PostgreSQL instead of localStorage. Core architecture is sound — parameterized queries, embedded migrations, proper Go interfaces. **One CRITICAL bug in the frontend causes silent UI/DB desync on any API error.** Several warnings around error handling gaps, dead code, and missing tests.

## Critical Issues

### CR-01: Frontend optimistic state update ignores API failure response

**File:** `web/src/store/usePinnedCoursesStore.js:24-40, 43-53`

**Issue:** `pinCourse` and `unpinCourse` call `fetch()` then proceed to update local `pinnedCourseIds` state without checking `res.ok` or the JSON response body's `success` field. `fetch()` only rejects on network errors — HTTP 4xx/5xx responses resolve normally. If the API returns 500 (e.g., DB failure, constraint violation), the frontend silently adds/removes the course from local state, permanently desyncing the UI from the database until the next page reload.

**Exploit scenario:**
1. User clicks pin on course "CS101"
2. Server returns 500 (transient DB error, connection pool exhaustion, etc.)
3. `fetch()` resolves — no exception
4. `set({ pinnedCourseIds: [...state.pinnedCourseIds, "CS101"] })` — local state updates optimistically
5. UI shows CS101 as pinned
6. User reloads page → course is gone → data loss perceived by user

**Fix:** Check HTTP response status and `result.success` before updating local state:

```javascript
pinCourse: async (courseId) => {
    set({ isLoading: true });
    try {
      const res = await fetch(FAVOURITES_URL, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ course_id: courseId }),
      });
      const result = await res.json();
      if (!res.ok || !result.success) {
        set({ isLoading: false });
        return;
      }
      set((state) => ({
        pinnedCourseIds: state.pinnedCourseIds.includes(courseId)
          ? state.pinnedCourseIds
          : [...state.pinnedCourseIds, courseId],
        isLoading: false,
      }));
    } catch {
      set({ isLoading: false });
    }
  },
```

Same pattern required in `unpinCourse` (line 43-53).

---

## Warnings

### WR-01: DELETE handler returns 500 for "not found" instead of 404

**File:** `internal/api/favourite_handlers.go:56-58`

**Issue:** When `repo.Remove()` returns an error because the favourite doesn't exist (message: "favourite not found: ..."), the handler returns `http.StatusInternalServerError` (500). This conflates a client-error condition (resource doesn't exist) with a server-error condition (DB unreachable, etc.). REST convention: DELETE on non-existent resource should return 404 (or 204 for idempotent delete).

**Fix:** Map sentinel errors to correct HTTP status — either check error string (fragile) or define a sentinel error in the repository:

```go
// In favourite_repository.go:
var ErrFavouriteNotFound = fmt.Errorf("favourite not found")

func (r *PgFavouriteRepository) Remove(courseID string) error {
    result, err := r.pool.Exec(...)
    if err != nil {
        return fmt.Errorf("remove favourite: %w", err)
    }
    if result.RowsAffected() == 0 {
        return fmt.Errorf("%w: %s", ErrFavouriteNotFound, courseID)
    }
    return nil
}

// In favourite_handlers.go:
if err := repo.Remove(courseID); err != nil {
    if errors.Is(err, db.ErrFavouriteNotFound) {
        writeJSON(w, http.StatusNotFound, errorResponse(err.Error()))
    } else {
        writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
    }
    return
}
```

---

### WR-02: POST favourites returns 200 instead of 201 Created

**File:** `internal/api/favourite_handlers.go:45`

**Issue:** Successful creation returns `200 OK`. REST best practice: POST that creates a resource should return `201 Created`. Minor, but affects API client expectations and HTTP cache semantics.

**Fix:** `writeJSON(w, http.StatusCreated, successResponse(nil))`

---

### WR-03: Repository interface and methods lack context.Context propagation

**File:** `internal/db/favourite_repository.go:10-14`

**Issue:** `GetAll()`, `Add()`, `Remove()` don't accept `context.Context`. All implementations hardcode `context.Background()`. This prevents request-scoped cancellation from propagating to the database. If a client disconnects, the DB query continues. Consistent with pre-existing `RoomRepository` pattern, but still a quality gap.

**Fix:** Add `ctx context.Context` as first parameter to all interface methods, thread it from handler's `r.Context()`:

```go
type FavouriteRepository interface {
    GetAll(ctx context.Context) ([]string, error)
    Add(ctx context.Context, courseID string) error
    Remove(ctx context.Context, courseID string) error
}
```

Then in handlers:
```go
ids, err := repo.GetAll(r.Context())
```

---

### WR-04: No tests for new functionality

**File:** (missing)

**Issue:** Zero tests exist for any of the new code:
- No Go tests for `FavouriteRepository` (even with pgxmock or integration test)
- No Go tests for `favourite_handlers` (httptest)
- No JS tests for `usePinnedCoursesStore` (even basic Vitest)
- No test for the migration (up/down)

Risk: regression on future changes to these paths will be undetectable.

---

### WR-05: Frontend silent catch blocks — no error logging or user feedback

**File:** `web/src/store/usePinnedCoursesStore.js:19-21, 38-40, 51-53`

**Issue:** All three `catch {}` blocks only set `{ isLoading: false }` without logging the error or exposing it to the UI. If an API call fails silently, the user has no indication that their action didn't persist. Combined with CR-01, this makes debugging failures in production nearly impossible.

**Fix:** Log caught errors and surface minimal error state:

```javascript
catch (err) {
  console.error('Failed to toggle favourite:', err);
  set({ isLoading: false, error: err.message });
}
```

(Add `error: null` to initial store state.)

---

## Info

### IN-01: Dead code — `cleanupStalePins` is defined but never called

**File:** `web/src/store/usePinnedCoursesStore.js:65-70`

**Issue:** `cleanupStalePins` was part of the localStorage-based implementation. Now that favourites are server-authoritative, this method is unreferenced anywhere. Adds noise and suggests incomplete refactor.

**Fix:** Remove the function and export.

### IN-02: `toggleCourse` doesn't set `isLoading` state

**File:** `web/src/store/usePinnedCoursesStore.js:56-63`

**Issue:** `toggleCourse` calls `pinCourse`/`unpinCourse` which both set `isLoading: true`, but `toggleCourse` itself doesn't set loading before dispatching. If the toggle logic grows (e.g., optimistic local toggle before API call), the loading state won't be accurate during the brief synchronous window.

**Fix:** Add `set({ isLoading: true })` at the top of `toggleCourse`, or make the loading state handling consistent by letting pinCourse/unpinCourse handle it entirely (they already do).

### IN-03: No request body size limit on POST /api/teacher/favourites

**File:** `internal/api/favourite_handlers.go:33`

**Issue:** `json.NewDecoder(r.Body).Decode(&req)` reads the entire request body with no size limit. An attacker could send a multi-GB payload to exhaust server memory. The existing handlers (`createRoomHandler`, `toggleCheckinHandler`) share the same gap, so this is a pre-existing pattern; still worth noting.

**Fix:** Use `http.MaxBytesReader`:
```go
r.Body = http.MaxBytesReader(w, r.Body, 4096)
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
```

### IN-04: Missing `user_id` column — favourites are global across all teachers

**File:** `internal/db/migrations/003_create_teacher_favourites.up.sql:1-4`

**Issue:** The `teacher_favourites` table has no `user_id` column, making the favourites list shared across all teachers. Not a bug per the requirement ("shared across all users"), but notable because:
1. If multi-user isolation is ever needed, a migration will be required
2. Two teachers pinning different courses will see each other's pins

Document this design decision for future maintainers.

---

_Reviewed: 2026-05-30T13:00:00Z_
_Reviewer: gsd-code-reviewer (deep cross-file analysis)_
_Depth: deep_
