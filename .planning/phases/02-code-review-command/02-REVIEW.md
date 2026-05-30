---
phase: 02-code-review-command
reviewed: 2026-05-30T12:00:00Z
depth: deep
files_reviewed: 9
files_reviewed_list:
  - internal/db/db.go
  - internal/db/migrations/004_create_session_checkins.up.sql
  - internal/db/migrations/004_create_session_checkins.down.sql
  - internal/db/session_checkin_repository.go
  - internal/db/session_checkin_repository_test.go
  - internal/warwick/classroom_client.go
  - internal/warwick/classroom_client_db_test.go
  - internal/service/data_refresher.go
  - cmd/server/main.go
findings:
  critical: 0
  blocker: 0
  warning: 4
  info: 3
  total: 7
status: issues_found
---

# Phase 2: Code Review Report

**Reviewed:** 2026-05-30T12:00:00Z
**Depth:** deep
**Files Reviewed:** 9
**Status:** issues_found

## Summary

Cross-cutting review of DB-backed session check-in cache implementation. Traced end-to-end flow (incoming request → cache check → DB → Warwick → response). Ran `go vet ./...` (pass), `go build ./...` (pass), `go test ./...` (all pass, including integration tests). No BLOCKER issues found — no regression when checkinRepo is nil, CachedSession wrapper handles transition from `*domain.SessionDetail` correctly, all nil guards in place. However, **4 WARNING** issues found: a shutdown race in the async goroutine, a data inconsistency in UpsertStudent (NULL session_date), a silent field-loss risk when DB repopulates from stale cache, and a resource leak anti-pattern.

## Warnings

### WR-01: Shutdown race — async goroutine can outlive pool.Close()

**File:** `internal/warwick/classroom_client.go:645`
**Issue:** `refreshSessionDetailCache` spawns a fire-and-forget goroutine that calls `UpsertFromWarwick` on the DB pool with a 30s context. The goroutine is not tracked or awaited during shutdown. In `main.go`, signal handling → server shutdown (10s timeout) → `defer pool.Close()` runs. The goroutine can be still executing during or after `pool.Close()`, causing a panic on a closed pool or leaked goroutine.

```go
// classroom_client.go:645
go func() {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    if err := c.checkinRepo.UpsertFromWarwick(ctx, sessionID, sessionDate, detail.Students); err != nil {
        slog.Warn("failed to persist session checkins to DB", "session_id", sessionID, "error", err)
    }
}()
```

**Fix:** Several options — (a) track goroutine via `sync.WaitGroup` and await during shutdown, (b) use a background context tied to server lifecycle (e.g., `context.Background()` that's cancelled on shutdown), or (c) at minimum add a `select` on ctx.Done() before the DB call so the goroutine can exit early when the server is shutting down:

```go
go func() {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    // Check if server is already shutting down
    select {
    case <-ctx.Done():
        return
    default:
    }
    if err := c.checkinRepo.UpsertFromWarwick(ctx, sessionID, sessionDate, detail.Students); err != nil {
        slog.Warn("failed to persist session checkins to DB", ...)
    }
}()
```

**Severity:** Warning — no data loss, error is logged, no crash in practice since pool.Close() blocks. But goroutine lifetime is untracked.

---

### WR-02: UpsertStudent creates rows with NULL session_date

**File:** `internal/db/session_checkin_repository.go:89-91`
**Issue:** `UpsertStudent` INSERT statement omits `session_date` column. The migration schema has no DEFAULT for `session_date` (`session_date DATE` with no default clause). When this upsert creates a row for a student never synced via `UpsertFromWarwick`, `session_date` is NULL. This is inconsistent with `UpsertFromWarwick` which always provides a non-NULL `session_date`. If any future query filters or groups by `session_date`, these rows would be invisible.

```sql
INSERT INTO session_checkins (session_id, student_id, student_name, checked_in, toggled_at, refreshed_at)
--                                                                                  ^^^^^^^^^^^^
--                                           no session_date ^
```

Compare with `UpsertFromWarwick` which includes `session_date`:
```sql
INSERT INTO session_checkins (session_id, student_id, student_name, checked_in, refreshed_at, session_date)
VALUES ($1, $2, $3, $4, NOW(), $5)
```

**Fix:** Add `session_date` to the INSERT in `UpsertStudent`, defaulting to `NOW()::date` or `time.Now()` from Go:

```go
`INSERT INTO session_checkins (session_id, student_id, student_name, checked_in, toggled_at, refreshed_at, session_date)
 VALUES ($1, $2, COALESCE((SELECT student_name FROM session_checkins WHERE session_id=$1 AND student_id=$2), ''), $3, NOW(), NOW(), NOW()::date)
 ON CONFLICT (session_id, student_id) DO UPDATE SET
     checked_in   = EXCLUDED.checked_in,
     toggled_at   = NOW(),
     refreshed_at = NOW()`,
```

**Severity:** Warning — latent data quality issue, no current query depends on session_date being non-null.

---

### WR-03: DB-repopulated SessionDetail silently drops embedded SessionSummary fields

**File:** `internal/warwick/classroom_client.go:540-553`
**Issue:** In the stale-to-DB path (Step 2b), when DB data is fresher than cache, the code creates a brand-new `SessionSummary` instead of copying from the stale `cachedSession.Detail.SessionSummary`. This means `Name`, `SessionNumber`, `Date`, `Status` fields are set to zero values:

```go
detail := &domain.SessionDetail{
    SessionSummary: domain.SessionSummary{
        SessionID:     sessionID,
        TotalStudents: len(students),
    },
    // Name, SessionNumber, Date, Status — all zero!
}
```

Currently these fields are also zero in the Warwick `fetchSessionDetail` path (line 748-754), so there is **no observable regression today**. However, if `fetchSessionDetail` is later updated to populate these fields (e.g., `Name` from the session list), the DB-repopulated path will silently drop them while the fresh-cache and Warwick paths return them. This creates a latent correctness bug.

**The same issue exists in the cold-DB path at line 599-612**, which additionally loses `QRActive` and `QRExpiresAt` (though these are also never set by `fetchSessionDetail`).

**Fix:** Copy SessionSummary from the stale entry when building DB-repopulated detail:

```go
detail := &domain.SessionDetail{
    SessionSummary: cachedSession.Detail.SessionSummary, // preserve all fields
    Students:       students,
    QRActive:       cachedSession.Detail.QRActive,
    QRExpiresAt:    cachedSession.Detail.QRExpiresAt,
}
// Then overwrite fields that come from DB
detail.TotalStudents = len(students)
detail.CheckedInCount = 0 // recompute below
```

**Severity:** Warning — latent bug, not a regression today but will become one when session metadata is added.

---

### WR-04: context cancel() not deferred in toggleCheckinWithPool

**File:** `internal/warwick/classroom_client.go:809-816`
**Issue:** The context created for the DB toggle write uses `context.WithTimeout` but calls `cancel()` after the `if` block instead of using `defer cancel()`. If `UpsertStudent` panics (e.g., nil pointer on `c.checkinRepo` despite the guard, or panic inside pgx), the context won't be cancelled, keeping resources alive until GC. While panics are rare, the idiomatic pattern is `defer cancel()` right after context creation.

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
if dbErr := c.checkinRepo.UpsertStudent(ctx, sessionID, domain.StudentCheckin{
    ...
}); dbErr != nil {
    slog.Error(...)
}
cancel() // should be defer cancel() right after creation
```

**Severity:** Warning — resource leak on panic, minor.

---

## Info

### IN-01: session_date uses time.Now() as placeholder

**File:** `internal/warwick/classroom_client.go:643`
`refreshSessionDetailCache` uses `sessionDate := time.Now()` as noted in code comment. The `session_date` in the schema is intended for "cache growth filter" purposes. If this is used for filtering stale data, using `time.Now()` means every refresh records today's date, which may not match the actual session date. This is documented as an open question in the spec.

---

### IN-02: GetStudentsBySession scans session_date but discards it

**File:** `internal/db/session_checkin_repository.go:41-42`
The query selects `session_date` from the table and scans it into a local `var sessionDate time.Time` variable that is never used. This is dead code — remove the column from the SELECT query or use the scanned value.

---

### IN-03: Test 5 doesn't verify async refresh outcome

**File:** `internal/warwick/classroom_client_db_test.go:397-427`
`TestClassroomClient_GetSessionDetail_StaleCache_DBSame_ServesStale` serves stale data and triggers an async refresh but doesn't verify that the refresh eventually updates the cache. The test correctly documents this limitation ("we don't synchronize with it") but it means the test only covers the synchronous read path, not the full stale→fresh transition. Consider adding a helper that polls the cache or waits for the refresh to complete.

---

## Structural Findings (fallow)

No structural pre-pass was provided for this review.

## Verification Results

| Command | Result |
|---------|--------|
| `go vet ./...` | ✅ PASS |
| `go build ./...` | ✅ PASS |
| `go test ./internal/...` (unit) | ✅ PASS (all packages) |

---

_Reviewed: 2026-05-30T12:00:00Z_
_Reviewer: the agent (gsd-code-reviewer)_
_Depth: deep_
