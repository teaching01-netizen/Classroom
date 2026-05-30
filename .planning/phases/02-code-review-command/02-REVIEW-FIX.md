---
phase: 02-code-review-command
fixed_at: 2026-05-30T16:30:00Z
review_path: .planning/phases/02-code-review-command/02-REVIEW.md
iteration: 3
findings_in_scope: 2
fixed: 2
skipped: 0
status: all_fixed
---

# Phase 2: Code Review Fix Report

**Fixed at:** 2026-05-30T16:30:00Z
**Source review:** .planning/phases/02-code-review-command/02-REVIEW.md
**Iteration:** 3

**Summary:**
- Findings in scope: 2
- Fixed: 2
- Skipped: 0

## Fixed Issues

### CR-01: Mock UpsertFromWarwick uses wrong toggle-preservation semantics

**Files modified:** `internal/warwick/classroom_client_db_test.go`
**Commit:** 3730a9f
**Applied fix:** Changed mock's `UpsertFromWarwick` from session-level `maxToggledAt` check to per-row `toggledAt` check. Added `toggledAt map[string]map[string]time.Time` field on `mockCheckinRepo` (sessionID → studentID → toggledAt). `UpsertStudent` now records per-student toggle timestamp. `UpsertFromWarwick` preserves `CheckedIn` per student only if that specific student has a `toggledAt` entry — matching the real DB impl which checks `toggled_at IS NULL` per row. `GetMaxToggledAtForSession` now computes MAX across individual student toggles. Updated test setups for Tests 4 and 5 to seed `toggledAt` instead of `maxToggledAt`.

### CR-02: Wrong build constraint

**Files modified:** `internal/warwick/classroom_client_db_test.go`
**Commit:** 3730a9f
**Applied fix:** Removed `//go:build integration` line. These tests use mocks and httptest servers, not a real database — they should run with normal `go test ./...`. Matches the pattern of `client_test.go` and `session_pool_test.go` which have no build constraint.

## Verification

All tiers passed:

1. `go vet ./internal/warwick/...` — **PASS** (no output)
2. `go build ./internal/warwick/...` — **PASS** (no output)
3. `go test -count=1 -v ./internal/warwick/ -run "TestClassroomClient"` — **PASS** (5/5 tests)
4. `go test -count=1 ./internal/warwick/` — **PASS** (full suite)

---

_Fixed: 2026-05-30T16:30:00Z_
_Fixer: the agent (gsd-code-fixer)_
_Iteration: 3_
