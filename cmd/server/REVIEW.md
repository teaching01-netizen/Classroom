---
phase: 04-review-main-wiring
reviewed: 2026-05-30T16:30:00Z
depth: standard
files_reviewed: 1
files_reviewed_list:
  - cmd/server/main.go
findings:
  critical: 1
  warning: 2
  info: 1
  total: 4
status: issues_found
---

# Phase 4: Code Review Report — `cmd/server/main.go` wiring

**Reviewed:** 2026-05-30T16:30:00Z
**Depth:** standard
**Files Reviewed:** 1
**Status:** issues_found

## Summary

Reviewed `cmd/server/main.go` — the Step 4 wiring commit (`5a245c2`). The file is well-structured with solid patterns (graceful shutdown, deferred cleanup, error handling for DB ops). `go vet` and `go build` both pass cleanly. However, there is one **Critical** nil-interface dereference that causes a runtime panic when `NewSessionPool` fails, and two maintainability concerns around naming consistency and log severity.

## Critical Issues

### CR-01: Nil interface dereference in room worker when session pool creation fails

**File:** `cmd/server/main.go:61`, `cmd/server/main.go:306`, `cmd/server/main.go:342`
**Issue:** When `warwick.NewSessionPool` fails (line 48), `sessionPool` is nil. The guard at line 60 (`if sessionPool != nil`) correctly prevents `qrClient` from being assigned, leaving it as a nil `domain.QrClient` interface. This nil interface is passed to `service.NewRoomManager(qrClient, repository)` at line 85 and stored. When `StartRoom` is called via the API handler (routes.go:193), it spawns `runRoomWorker` which calls `rm.qrClient.FetchQR(classID)` (line 306) and `rm.qrClient.FetchQRWithFreshAuth(classID)` (line 342) — both **panic on a nil interface dereference**.

Contrast with `classroomClient`: the teacher handlers guard against nil `cc` at teacher_handlers.go:16. No equivalent guard exists for `qrClient` in the room worker path.

**Fix:** Either (a) guard `StartRoom` with a `qrClient == nil` check returning an error, or (b) wrap the nil `QrClient` with a no-op stub that returns a descriptive error so the room worker transitions to `Warning` instead of crashing.

Option (a) — simplest fix in `room_manager.go`:

```go
func (rm *RoomManager) StartRoom(roomID string) error {
    rm.mu.Lock()
    defer rm.mu.Unlock()

    if rm.qrClient == nil {
        return fmt.Errorf("QR client not available; Warwick session pool is down")
    }
    // ... rest of existing code
}
```

Or alternatively in `main.go`: don't register the start-room route when `qrClient` is nil (though the guard inside `StartRoom` is more robust as it protects all call paths).

## Warnings

### WR-01: Inconsistent repository variable naming conventions

**File:** `cmd/server/main.go:84,92,95`
**Issue:** Three different naming patterns for the same concept in the same file:
- Line 84: `repository := db.NewPgRoomRepository(pool)` (full-word `repository`)
- Line 92: `favRepo := db.NewPgFavouriteRepository(pool)` (short `Repo` suffix)
- Line 95: `sessionCheckinRepo := db.NewPgSessionCheckinRepository(pool)` (full-description `Repo` suffix)

These inconsistencies make the wiring harder to scan and set a poor pattern for future additions.

**Fix:** Pick one convention and apply consistently. Given the Go standard of short-but-descriptive names, `roomRepo`, `favRepo`, `sessionCheckinRepo` (or `checkinRepo`) would be idiomatic and scannable:

```go
roomRepo := db.NewPgRoomRepository(pool)
favRepo := db.NewPgFavouriteRepository(pool)
// ...
checkinRepo := db.NewPgSessionCheckinRepository(pool)
```

### WR-02: Session pool failure logged as WARN instead of ERROR

**File:** `cmd/server/main.go:50`
**Issue:** `NewSessionPool` failure fundamentally disables all Warwick features (both QR and classroom). The log level `slog.Warn` understates severity. When `sessionPool` is nil, `qrClient`, `classroomClient`, and `refresher` all remain nil — no QR, no teacher/courses, no cache warming. This should be `slog.Error` so it's surfaced in monitoring.

```go
sessionPool, err := warwick.NewSessionPool(...)
if err != nil {
    slog.Error("Failed to create Warwick session pool; all Warwick features disabled", "error", err)
}
```

## Info

### IN-01: `classroomClient` and `refresher` declared but only assigned inside condition

**File:** `cmd/server/main.go:58-59`
**Issue:** `var classroomClient *warwick.ClassroomClient` and `var refresher *service.DataRefresher` are declared at line 58-59 but not assigned until the `if sessionPool != nil` block at lines 94-105. This is fine for Go (zero-value nil), and the nil guards in handlers handle it correctly. However, this pattern is fragile — any future code path that accesses these outside the block will get nil without compile-time protection. Consider extracting the `sessionPool != nil` branch into a single initialization function that returns a struct or uses a functional options pattern, or document the invariant in a comment.

No action required for this commit; just a note for future refactoring.

---

_Reviewed: 2026-05-30T16:30:00Z_
_Reviewer: the agent (gsd-code-reviewer)_
_Depth: standard_
