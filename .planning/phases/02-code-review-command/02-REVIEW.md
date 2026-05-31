---
phase: 02-code-review-command
reviewed: 2026-05-31T00:00:00Z
depth: standard
files_reviewed: 2
files_reviewed_list:
  - internal/domain/room.go
  - internal/api/routes.go
findings:
  critical: 0
  warning: 3
  info: 2
  total: 5
status: issues_found
---

# Phase 2: Code Review Report

**Reviewed:** 2026-05-31
**Depth:** standard
**Files Reviewed:** 2
**Status:** issues_found

## Summary

Change adds `RoomLite` domain struct and `?lite=true|1` query parameter to `GET /api/rooms` for a stripped-down payload. Code compiles clean. Implementation is focused and well-scoped. Three warnings and two info items found.

## Critical Issues

None.

## Warnings

### WR-02: RoomLite conversion is not DRY

**File:** `internal/api/routes.go:149-156`
**Issue:** The `Room -> RoomLite` mapping is inline in the handler. If any other handler needs this conversion, the logic will be duplicated. There is no `Room.ToLite()` method or `NewRoomLite(Room)` constructor.
**Fix:** Add a conversion method on `Room`:
```go
func (r *Room) ToLite() RoomLite {
    return RoomLite{
        RoomID:    r.RoomID,
        ClassID:   r.ClassID,
        Name:      r.Name,
        Status:    r.Status,
        QRURL:     r.QRURL,
        ExpiresAt: r.ExpiresAt,
    }
}
```

### WR-03: Shared pointer fields between Room and RoomLite (shallow copy risk)

**File:** `internal/api/routes.go:149-156`, `internal/domain/room.go:145-152`
**Issue:** `RoomLite` copies `Name`, `QRURL`, `ExpiresAt` by pointer. The `RoomLite` holds the same pointer as the source `Room`. If a `Room`'s pointer fields are mutated after construction but before serialization, the response would reflect the mutation. Currently safe due to immediate serialization, but latent risk.
**Fix:** For production hardening, dereference and copy:
```go
func (r *Room) ToLite() RoomLite {
    lite := RoomLite{
        RoomID:    r.RoomID,
        ClassID:   r.ClassID,
        Status:    r.Status,
        ExpiresAt: r.ExpiresAt,
    }
    if r.Name != nil {
        name := *r.Name
        lite.Name = &name
    }
    if r.QRURL != nil {
        url := *r.QRURL
        lite.QRURL = &url
    }
    return lite
}
```

## Info

### IN-01: No test coverage for lite query parameter

**File:** `internal/api/routes.go:143-161`
**Issue:** No test files reference `getRoomsHandler` with the `lite` parameter. The behavior of the `?lite=true` path is untested.
**Fix:** Add an integration or unit test verifying `GET /api/rooms?lite=true` omits `WarningMessage`, `ErrorMessage`, `LastFetchAt`, and `LastUpdatedAt`.

### IN-02: Lite query parameter only accepts "true" and "1"

**File:** `internal/api/routes.go:146`
**Issue:** `lite == "true" || lite == "1"` silently falls through to full response for other truthy values like `yes` or `TRUE`. Acceptable default, but worth documenting.
**Fix:** No code change needed. Document accepted values in API docs.

---

_Reviewed: 2026-05-31_
_Reviewer: gsd-code-reviewer_
_Depth: standard_
