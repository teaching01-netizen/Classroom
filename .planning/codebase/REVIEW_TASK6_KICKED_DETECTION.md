# Code Review: Task 6 — Staggered Re-auth / Kicked Detection

**Analysis Date:** Sat May 30 2026

**Commit:** `18fc416`
**File changed:** `internal/warwick/session_pool.go` (+98 lines)

---

## Heuristic Accuracy

**The heuristic:** If `sessionAge ≤ 55 min` and a login attempt fails → assume admin kick → enter backoff.

### Threshold rationale

| Constant | Value | Purpose |
|----------|-------|---------|
| `sessionTTL` | 60 min | Warwick session lifetime |
| `sessionRefreshBuffer` | 5 min | Trigger refresh when ≤ 5 min from expiry |
| `sessionMaxValidAge` | 55 min | Max age for kick-candidate heuristic |
| `sessionMinValidAge` | 2 min | Defined but **unused** — see Issues |

The 55 min threshold aligns with the 60 min TTL minus 5 min refresh buffer. At age 55 min, the session would normally be refreshed (entering the buffer zone). A login failure before 55 min is unlikely to be normal TTL expiry — strongly suggestive of admin invalidation.

**False positive scenario:** Transient Warwick network blip at session age 30 min → login fails → `isKickCandidate()` returns true (30 min ≤ 55 min) → 30s backoff applied. Cost: ~30s of downtime before the next retry succeeds and `resetBackoff()` clears the state. **Acceptable.** The 30s window is small enough that a false backoff is harmless while providing meaningful protection against kick ping-pong.

**False negative scenario:** Admin kicks at session age 58 min → login fails → `isKickCandidate()` returns false (58 min > 55 min) → no backoff → immediate re-login → kicks admin again. **Unlikely to matter** — at 58 min the session was 2 min from natural expiry anyway, so re-authing is the correct behaviour.

### Verdict: Sound.

---

## Backoff Algorithm

**Implementation** (`session_pool.go:77-87`):

```
applyBackoff():
  count++  (capped at 6)
  d = 30s × 2^(count-1)  [30s, 60s, 2m, 4m, 8m, 15m]
  if d > 15m → cap at 15m
  backedOffUntil = now + d
```

| Attempt | Backoff | Cumulative |
|---------|---------|------------|
| 1 | 30s | 30s |
| 2 | 60s | 90s |
| 3 | 2m | 3.5m |
| 4 | 4m | 7.5m |
| 5 | 8m | 15.5m |
| 6+ | 15m (capped) | — |

**Correctness:**
- Exponential base-2 growth ✓
- Cap at 15 min (`sessionBackoffMax`) ✓
- `resetBackoff()` clears on successful login ✓
- Only called when `isKickCandidate()` returns true ✓

### Verdict: Correct.

---

## Race Safety

All backoff fields (`backedOffUntil`, `backoffCount`) are accessed exclusively under `s.mu`:

| Function | Lock held | Fields accessed |
|----------|-----------|-----------------|
| `applyBackoff()` | write lock | `backoffCount`, `backedOffUntil` |
| `resetBackoff()` | write lock | `backoffCount`, `backedOffUntil` |
| `isBackedOff()` | read+ lock | `backedOffUntil` |
| `isKickCandidate()` | read+ lock | `obtainedAt` (set under write lock in `doLoginLocked`) |

All paths documented with "Caller must hold..." comments (`session_pool.go:76, 90, 97, 103`). ✓

**Call path audit:**

1. `ensureValidSession` — acquires `s.mu.Lock()` before calling `isBackedOff()` / `applyBackoff()` / `resetBackoff()` → safe ✓
2. `ForceRefreshOnSession` — acquires `s.mu.Lock()` before all backoff operations → safe ✓
3. `doLoginLocked` — caller holds `s.mu` write lock; sets `obtainedAt` before any backoff check → safe ✓

### Verdict: Race-safe.

---

## ErrAuthConflict Propagation

**Chain for `Acquire` path:**

```
ensureValidSession → ErrAuthConflict
       ↓
Acquire wraps: fmt.Errorf("warwick: acquire session: %w", ErrAuthConflict)
       ↓
FetchQR / FetchQRWithFreshAuth: if err != nil → return domain.ErrAuthExpired  ← ERR! Lost
```

**Chain for `ForceRefreshOnSession` path:**

```
ForceRefreshOnSession → ErrAuthConflict
       ↓
FetchQRWithFreshAuth: if _, _, err := c.pool.ForceRefreshOnSession(ref); err != nil
                       → return domain.ErrAuthExpired                          ← ERR! Lost
```

**Critical issue:** `ErrAuthConflict` is **swallowed** at the `client.go` layer and converted to the generic `domain.ErrAuthExpired`. Callers at the service layer cannot distinguish between "session naturally expired (safe to retry)" and "human admin kicked us (must back off)."

This means all calling code treats an auth conflict identically to a normal expiry — including the room recovery loop.

**Session leak:** In `Acquire` (`session_pool.go:197`), when `ensureValidSession` returns `ErrAuthConflict`:
- `s.inUse = false` is set (session released back to pool) ✓
- No `SessionRef` is returned to caller (so no double-release risk) ✓
- **Not a leak** — but the session is immediately available for re-acquisition while still in backoff, which is correct (next acquirer will hit `isBackedOff()`).

### Verdict: Error swallowing is a design gap.

---

## Integration with Room Recovery Loop

**Full flow when admin kicks a session:**

```
FetchQR(classID) → gets 302/html → returns ErrAuthExpired
       ↓
Room transitions to AuthExpired status
       ↓
Recovery loop starts (1s initial backoff, 10 attempts, 30s max)
       ↓
FetchQRWithFreshAuth(classID)
  → Acquire(session) → ensureValidSession → isBackedOff()? No (first time)
  → doLoginLocked → fails → isKickCandidate()? Yes → applyBackoff(30s)
  → returns ErrAuthConflict
  → Acquire wraps with "warwick: acquire session: ..."
  → FetchQRWithFreshAuth: err != nil → returns ErrAuthExpired
       ↓
Room recovery: resp, err = FetchQRWithFreshAuth(...)
  - err IS *domain.FetchError with Kind=ErrKindAuthExpired
  - NOT InvalidPayload → falls through to backoff *= 2 branch
  - backoff goes 1s → 2s → 4s → 8s → 16s → 30s (capped) ...
  - Next iteration: same thing, but ensureValidSession hits isBackedOff() immediately
       ↓
Recovery exhausts 10 attempts (~3 min total)
       ↓
Room → Stopped (permanent failure)
       ↓
Meanwhile: session pool backoff still ticking (could be 4/8/15 min remaining)
```

**Two uncoordinated backoff systems:**

| System | Start | Growth | Cap | Duration |
|--------|-------|--------|-----|----------|
| Session pool (per-session) | 30s | 2× | 15 min | Single window |
| Room recovery (per-room) | 1s | 2× | 30s | ~3 min (10 attempts) |

**Consequence:** After room recovery gives up (~3 min), the session pool backoff may still be active. The room is `Stopped` and won't retry unless the user manually re-enables it or the room's polling loop picks it up on next cycle. In practice this is acceptable because:

- Once the room is stopped, no further auth attempts occur → no admin ping-pong
- The session backoff eventually expires (30s–15 min)
- The room polling loop (`shouldFetch` check at `room_manager.go:224`) would re-evaluate after the room metadata is updated (e.g., by user action)

### Verdict: Functional but noisy. The error swallowing means the recovery loop wastes ~3 min retrying into a backoff that can't succeed.

---

## Backoff Persistence

**Current state:** In-memory only (`pooledSession.backedOffUntil`). Server restart → all backoff state lost → sessions immediately eligible for re-auth.

**Assessment:** Acceptable. After a server restart:
- All pooled sessions are fresh (no cookies, no state)
- First `Acquire` will login fresh
- If the admin is still logged in, the kick will be re-detected and backoff will restart

The alternative (persisting to DB/disk) adds complexity with no real benefit — a restart implies the server process was recycled anyway.

### Verdict: Acceptable.

---

## Issues

### Critical

**CRIT-1: `ErrAuthConflict` swallowed by client layer — recovery cannot differentiate kick from expiry**

**Files:**
- `internal/warwick/client.go:100-101` — `ForceRefreshOnSession` error → `domain.ErrAuthExpired`
- `internal/warwick/client.go:76-78` — `Acquire` error → `domain.ErrAuthExpired`

**What happens:** Both `FetchQR` and `FetchQRWithFreshAuth` convert ALL pool errors to `domain.ErrAuthExpired`, including `ErrAuthConflict`. The room recovery loop at `internal/service/room_manager.go:272` calls `FetchQRWithFreshAuth` and receives `ErrAuthExpired` regardless of whether the underlying cause was a kicked session or a normal expiry.

**Impact:** The room recovery loop burns its 10 retry attempts (~3 min) polling a session that's in backoff and can't succeed. After exhaustion, the room is marked `Stopped`. No ping-pong risk — but 3 min of futile retries, then premature room termination.

**Fix approach:** Either:
(a) Have `FetchQRWithFreshAuth` propagate `ErrAuthConflict` directly to callers (not via `domain.ErrAuthExpired`) so the recovery loop can distinguish cases, or
(b) Have the recovery loop check `errors.Is(err, warwick.ErrAuthConflict)` on the inner pool error (requires not swallowing it).

---

### Important

**IMP-1: `sessionMinValidAge` (2 min) is dead code**

**Files:** `internal/warwick/session_pool.go:24-26`

**What happens:** `sessionMinValidAge = 2 * time.Minute` is defined but never referenced anywhere in the codebase. `isKickCandidate()` only checks `sessionMaxValidAge`. The intended tiered design (guaranteed kick vs. probable kick) is incomplete.

**Impact:** A session obtained 30 seconds ago that gets a login failure is treated the same as a session obtained 50 minutes ago — both get the standard exponential backoff. The `sessionMinValidAge` suggests there was intent to handle the "guaranteed kick" case differently (max backoff? no retry?).

**Fix approach:** Either implement the threshold (apply max backoff immediately when age ≤ 2 min) or remove the constant.

**IMP-2: Room recovery and session pool have independent, uncoordinated backoffs**

**Files:** 
- `internal/warwick/session_pool.go:77-87` — session backoff (30s→15m)
- `internal/service/room_manager.go:265-328` — room recovery backoff (1s→30s, 10 attempts)

**What happens:** When a kick is detected, both systems independently track backoff without any shared state or signal. The room recovery loop retries into a session that's in backoff, wasting all 10 attempts. After the room gives up (~3 min), the session pool backoff is still active.

**Impact:** The room enters `Stopped` state while the session pool could still have minutes of backoff remaining. On restart (manual or server restart), the pool backoff is lost and the admin could be re-kicked.

**Fix approach:** If the recovery loop could detect `ErrAuthConflict` (see CRIT-1), it could coordinate: "I know the session is backed off, I'll wait until the backoff expires before retrying." This would likely require exposing the backoff deadline or using a longer, aligned retry schedule.

**IMP-3: `inUse` flag set/read outside pool mutex in error path**

**Files:** `internal/warwick/session_pool.go:197`

**What happens:** After `p.mu.Unlock()` at line 193, `s.inUse = false` is set at line 197 without the pool lock held. While not a data race (no concurrent writer to this session's `inUse`), this is a pre-existing fragility. The Go memory model makes the write visible eventually, but the pattern is inconsistent with the rest of the code.

**Impact:** Low risk currently, but a future modification that adds concurrent `inUse` access could introduce a race. The pattern also complicates reasoning about the pool's invariants.

**Fix approach:** Defer `p.mu.Unlock()` until after the `s.inUse = false` cleanup, or restructure to hold `p.mu` during the entire acquire (though that would serialize logins).

---

### Minor

**MIN-1: No tests for backoff or kick detection**

**Files:** No test file exists for `session_pool.go` backoff logic.

**What's missing:** No unit tests for:
- `applyBackoff()` arithmetic
- `isBackedOff()` boundary conditions
- `isKickCandidate()` age threshold
- `ensureValidSession` backoff path
- `ForceRefreshOnSession` backoff path

**Risk:** Low — the logic is simple and linear. But regressions on edge cases (zero time, overflow, wrap-around) would go undetected.

**MIN-2: Backoff arithmetic uses `1<<uint(s.backoffCount-1)` — potential overflow on 32-bit arch**

**Files:** `internal/warwick/session_pool.go:82`

**Detail:** With `sessionBackoffMaxAttempts = 6`, `1<<uint(5) = 32` — safe. But if max attempts were ever increased beyond 30, `1<<uint(n)` would overflow on 32-bit platforms. Not actionable now, but worth noting for future edits.

**MIN-3: Error message has em dash instead of plain dash**

**Files:** `internal/warwick/session_pool.go:45`

**Detail:** `"warwick: auth conflict — human admin likely logged in, backing off"` uses `—` (U+2014). Inconsistent with the rest of the codebase which uses plain ASCII. Some log aggregators or terminal emulators may not render this correctly.

---

## Strengths

1. **Clean sentinel error** — `ErrAuthConflict` is a package-level `var` sentinel, enabling `errors.Is()` checks. Good practice.

2. **Lock discipline is clear and documented** — every backoff-accessing function has a "Caller must hold..." comment. The lock boundaries are easy to audit.

3. **Double-checked locking in `ensureValidSession`** — fast path with read lock, slow path with write lock and re-check. Standard and correct.

4. **Backoff reset on success** — `resetBackoff()` is called after every successful login in both `ensureValidSession` and `ForceRefreshOnSession`. Prevents permanent lockout from a single kick.

5. **`doLoginLocked` caller requirement** — clear contract prevents accidental misuse. The function updates `obtainedAt` under the write lock, which `isKickCandidate()` needs for correctness.

6. **`obtainedAt` is updated on every login** (`session_pool.go:327`) — not just on initial login. So a session that was refreshed at age 55 min will have its `obtainedAt` bumped, keeping the heuristic accurate across refresh cycles.

7. **No session leak on backoff** — `Acquire` sets `s.inUse = false` before returning the error, ensuring the session is available for future re-acquisition (where it will hit `isBackedOff()` and return immediately).

---

## Assessment: **Changes needed**

**Rationale:** The core backoff mechanism is sound, race-safe, and mathematically correct. However, the **error swallowing** in `client.go` (CRIT-1) constitutes a design gap that undermines the entire feature — the room recovery loop cannot distinguish kick from expiry, wasting 3 min of retries and terminating the room prematurely.

**Minimum changes required before approve:**
1. Fix CRIT-1 — propagate `ErrAuthConflict` so callers can make informed retry decisions
2. Either implement or remove `sessionMinValidAge` (IMP-1)

**Nice-to-have:**
3. Unit tests for backoff logic (MIN-1)
4. Align room recovery with session backoff timing (IMP-2) — prevents futile retry spam
