---
phase: 02-code-review-command
reviewed: 2026-05-30T16:00:00Z
depth: deep
files_reviewed: 3
files_reviewed_list:
  - cmd/server/main.go
  - internal/warwick/client_test.go
  - internal/warwick/session_pool.go
findings:
  critical: 1
  warning: 2
  info: 2
  total: 5
status: issues_found
---

# Phase 2: Code Review Report — Add TierInteractive Pool Tier

**Reviewed:** 2026-05-30T16:00:00Z
**Depth:** deep
**Files Reviewed:** 3
**Status:** issues_found

## Summary

Reviewed 3 files across the Warwick session pool implementation (`+31/-16` lines). The pool plumbing is mechanically correct — `SessionPool` struct, `TierInteractive` enum value, `Acquire` switch case, constructor parameter — but two significant issues remain: a **data race** in `Acquire`'s error path, and **dead sessions** because no production consumer calls `Acquire(TierInteractive)` — the tier exists but `ToggleCheckin` still routes through `TierTeacher`, defeating the isolation goal.

---

## Critical Issues

### CR-01: Data race on `pooledSession.inUse` in `Acquire` error path

**File:** `internal/warwick/session_pool.go:229`
**Issue:** On line 225, `p.mu.Unlock()` is called. On line 229, `s.inUse = false` is written *without* holding `p.mu`. All other reads/writes to `inUse` happen under `p.mu` (lines 223-224 in `Acquire`, line 253 in `Release`). This creates a data race: concurrent goroutines calling `Acquire` or `Release` may not observe the `inUse = false` write due to missing happens-before edge in the Go memory model. The Go race detector would flag this.

**Fix:** Re-acquire `p.mu` before clearing `inUse` on the error path. The cleanest approach uses a defer pattern:

```go
for offset := 0; offset < (end - start); offset++ {
    idx := start + (next+offset)%(end-start)
    s := p.sessions[idx]
    if !s.inUse {
        s.inUse = true
        p.mu.Unlock()

        cookie, gen, err := p.ensureValidSession(s)
        if err != nil {
            p.mu.Lock()
            s.inUse = false
            p.mu.Unlock()
            return nil, fmt.Errorf("warwick: acquire session: %w", err)
        }
        // ...
    }
}
```

Alternative: convert `inUse` to `atomic.Bool` and use atomic loads/stores throughout. But the simplest fix that matches existing patterns is re-locking the pool mutex.

---

## Warnings

### WR-01: TierInteractive sessions dead — no consumer acquires them

**File:** `cmd/server/main.go:46`
**Issue:** `NewSessionPool(..., 2, 1, 2)` creates 5 sessions: QR[0-1], Teacher[2], Interactive[3-4]. But `main.go` line 46 creates *one* `ClassroomClient` with `TierTeacher`:

```go
classroomClient = warwick.NewClassroomClientFromPool(sessionPool, warwick.TierTeacher)
```

`ClassroomClient` uses `c.tier` for every operation including `ToggleCheckin` (line 464: `c.pool.Acquire(c.tier)`). `TierInteractive` is **never called** by any production path. The 2 interactive sessions are idle — allocated, staggered, but never acquired. The stated goal "TierInteractive pool tier for ToggleCheckin isolation" is not achieved because `ToggleCheckin` still shares the single `TierTeacher` session with browsing operations (`GetCourses`, `GetCourseDetail`, `GetSessionDetail`).

**Fix:** Either (a) create a separate `ClassroomClient` with `TierInteractive` for toggle operations, or (b) add a `toggleTier` override to `ClassroomClient` so `ToggleCheckin` can use a different tier, or (c) wire a second client through the router for the toggle endpoint.

Example approach (option a):
```go
// In main.go
classroomClient = warwick.NewClassroomClientFromPool(sessionPool, warwick.TierTeacher)
interactiveClient = warwick.NewClassroomClientFromPool(sessionPool, warwick.TierInteractive)
// Pass interactiveClient to toggleCheckinHandler separately
```

### WR-02: No test coverage for TierInteractive behavior

**File:** `internal/warwick/client_test.go:89,116`
**Issue:** Test updates only added the third constructor parameter (`NewSessionPool(..., 1, 1, 1)`) to match the new signature. No test calls `pool.Acquire(TierInteractive)`, no test verifies round-robin across all three tiers, and no test validates that `TierInteractive` sessions are correctly isolated. The risk: any regression in the `TierInteractive` branch of `Acquire` would go undetected.

**Fix:** Add a test that acquires a `TierInteractive` session and optionally one that verifies pool exhaustion at the interactive boundary:

```go
func TestAcquireInteractiveTier(t *testing.T) {
    pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
    require.NoError(t, err)
    ref, err := pool.Acquire(TierInteractive)
    require.NoError(t, err)
    require.NotNil(t, ref)
    pool.Release(ref)
}

func TestInteractivePoolExhaustion(t *testing.T) {
    // Acquire the single interactive session, then try again
}
```

---

## Info

### IN-01: Redundant atomic operations under pool mutex

**File:** `internal/warwick/session_pool.go:212-218`
**Issue:** Lines 212-218 use `atomic.AddUint64(&p.qrNext, ...)` etc. while `p.mu.Lock()` is already held. The mutex already provides mutual exclusion for all pool fields. Mixing atomics with mutex synchronization is misleading — it suggests lock-free access that doesn't exist. Not a correctness bug (the atomic is harmless), but it obscures the actual locking strategy.

**Fix:** Either remove the mutex and use atomics for all fields (lock-free), or remove the atomics and do plain increments under the mutex. Given that `p.mu` is held, plain increments are clearer:

```go
next := p.qrNext
p.qrNext++
if tier == TierTeacher {
    next = p.teacherNext
    p.teacherNext++
}
if tier == TierInteractive {
    next = p.interactiveNext
    p.interactiveNext++
}
```

### IN-02: `Acquire` tier dispatch pattern is fragile for N tiers

**File:** `internal/warwick/session_pool.go:211-218`
**Issue:** The round-robin dispatch uses three sequential `if` statements (lines 212-218) that mirror the `switch` above but are not structurally linked. Adding a fourth tier requires remembering to update both the `switch` block and add a new `if` block in the same order. A data-driven approach (e.g., a `next` field per tier in a map or array indexed by `SessionTier`) would eliminate this coupling.

**Fix:** Minor quality suggestion — not blocking. Consider storing next-counters in a fixed array indexed by tier:

```go
var nextStart [3]uint64
// ...
idx := int(atomic.AddUint64(&nextStart[tier], 1) - 1)
```

This eliminates the if-chain entirely and makes the tier relationship structural.

---

## Cross-File Analysis

### Import graph (within scope)
```
main.go → session_pool.go (NewSessionPool)
main.go → client.go (NewWarwickQrClientFromPool)
main.go → classroom_client.go (NewClassroomClientFromPool)
client_test.go → session_pool.go (NewSessionPool, Acquire)
```

### Call chain for TierInteractive
```
main() → NewSessionPool(..., 2, 1, 2)  ✓ creates pool with 5 sessions
main() → NewClassroomClientFromPool(pool, TierTeacher)  ← uses TierTeacher, NOT TierInteractive
  → ClassroomClient.ToggleCheckin()
    → toggleCheckinWithPool()
      → pool.Acquire(c.tier)   ← c.tier = TierTeacher
```

**Result:** `TierInteractive` is reachable (`Acquire` handles it) but unreached (no caller passes `TierInteractive`).

### Error propagation
All error paths in `Acquire` properly release mutex before returning. The `s.inUse = false` race (CR-01) is the only synchronization gap.

---

## Assessment

The pool *plumbing* is correct: `TierInteractive` is a valid enum member, `SessionPool` tracks it, `NewSessionPool` allocates it, and `Acquire` routes it. The *wiring* is incomplete — the sole `ClassroomClient` instance uses `TierTeacher` for everything, including `ToggleCheckin`, making the 2 interactive sessions unreachable. Combined with the data race on `inUse`, the implementation has a correctness defect and a completeness gap that should be addressed before shipping.

**Recommendation:** Fix CR-01 (data race) before deploy. Address WR-01 (dead interactive sessions) as a follow-up to achieve the stated isolation goal. Add WR-02 tests to prevent regressions.

---

_Reviewed: 2026-05-30_
_Reviewer: gsd-code-reviewer_
_Depth: deep_
