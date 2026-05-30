---
phase: 07-code-quality-review-task7
reviewed: 2026-05-30T12:20:00Z
depth: standard
files_reviewed: 1
files_reviewed_list:
  - web/src/components/CourseCard.jsx
findings:
  critical: 0
  warning: 1
  info: 2
  total: 3
status: issues_found
---

# Phase 7: Code Review Report — Task 7

**Commit:** `9d5785d`
**Message:** `fix(web): show — placeholder for unavailable attendance in CourseCard`
**Reviewed:** 2026-05-30T12:20:00Z
**Depth:** standard
**Files Reviewed:** 1
**Status:** issues_found

## Summary

Commit `9d5785d` modifies `web/src/components/CourseCard.jsx` (+17/−15) to show `— attendance` instead of `0% attendance` when the computed attendance rate is 0, and hides the progress bar in that case.

The change is minimal, syntactically correct, and has no dead code. Three issues were found: one Warning (NaN propagation from undefined input) and two Info items (semantic blur between 0% and missing data, inconsistent boundary guard style).

## Warnings

### WR-01: NaN propagation from null/undefined input

**File:** `web/src/components/CourseCard.jsx:23`
**Severity:** Warning

**Issue:**
The computation `Math.round(course.avg_attendance_rate * 100)` is unguarded. The same file in `App.jsx:110` wraps this computation in a `!= null` check, but `CourseCard.jsx` does not.

If `course.avg_attendance_rate` is `undefined` (field absent from API response):
```
undefined * 100 = NaN
Math.round(NaN) = NaN
Math.max(NaN, 0) = NaN
Math.min(NaN, 100) = NaN  // → attendancePercent = NaN
```

The downstream effects are split:
- `NaN > 0` → `false` → bar hidden, `"— attendance"` shown ✅ (safe fallback)
- `NaN === 0` → `false` → color condition falls through to `--color-danger` (red) ❌ (should be `--color-text-secondary` gray for "unavailable")

The color logic at line 118 resolves to red instead of gray for any non-zero, non-numeric value, creating a misleading visual when data is missing.

**Fix:**
Add a nullish guard before the computation, consistent with `App.jsx:110`:

```js
const rate = course.avg_attendance_rate;
const attendancePercent = rate != null && !isNaN(rate)
  ? Math.min(Math.max(Math.round(rate * 100), 0), 100)
  : 0;
```

Or more concisely, keeping the original structure but guarding:

```js
const rawRate = course.avg_attendance_rate;
const attendancePercent = rawRate == null ? 0
  : Math.min(Math.max(Math.round(rawRate * 100), 0), 100);
```

The `== null` check catches both `null` and `undefined`.

**Alternatively**, wrap the display in a null-guard consistent with App.jsx:

```jsx
{course.avg_attendance_rate != null && ( ... )}
```

and handle the `(attendancePercent === 0) → show "—"` logic only within that branch. This would match the pattern already established in App.jsx:110.

## Info

### IN-01: Semantic blur between "0% attendance" and "missing data"

**File:** `web/src/components/CourseCard.jsx:120`
**Severity:** Info

**Issue:**
The commit treats `attendancePercent === 0` as synonymous with "unavailable". But 0% is a legitimate computed value (a course that has sessions yet recorded zero attendance). The Go backend uses `float64` (zero value `0.0`), so a course with no session data also sends `avg_attendance_rate: 0`. These two cases are indistinguishable.

A user seeing `— attendance` for an active course with genuine 0% attendance would incorrectly believe data is missing. This was already flagged in the prior phase 05 review (`.planning/phases/05-code-quality-review/05-REVIEW.md:33-35`).

**Fix:**
This is a design/API-level decision, not a code-level fix. Options:
1. Backend sends `null` (requires `*float64` in Go) when data is genuinely absent, and frontend checks `!= null` before treating 0 as "0%".
2. Document that 0% → "—" is an intentional design tradeoff.

If option 1 is chosen, the `attendancePercent` computation should mirror `App.jsx`'s pattern.

### IN-02: Inconsistent boundary guard style across three expressions

**File:** `web/src/components/CourseCard.jsx:87,118,120`
**Severity:** Info

**Issue:**
The same boundary (zero vs non-zero attendance) is checked three different ways within the same component:

| Expression | Check | Purpose |
|---|---|---|
| `attendancePercent > 0 && (...)` (line 87) | `> 0` | Show/hide progress bar |
| `attendancePercent === 0 ? 'gray' : ...` (line 118) | `=== 0` | Neutral color for zero |
| `attendancePercent > 0 ? 'N%' : '—'` (line 120) | `> 0` | Show percentage or placeholder |

All three are functionally consistent for the integer range [0, 100] given the clamping at line 23. However, if the clamping logic changes (e.g., removing `Math.max(..., 0)`), these guards would diverge: a negative value would be hidden by `> 0` but pass through `=== 0` as non-zero.

**Fix:**
Normalize to a single expression, e.g.:

```js
const hasAttendance = attendancePercent > 0;
```

Then use `hasAttendance` everywhere.

---

## Assessment

**Overall: Clean, minor change with one meaningful edge-case gap.**

| Criterion | Verdict | Notes |
|---|---|---|
| Change minimal | ✅ | +17/−15, single file |
| No dead code | ✅ | All lines serve purpose |
| Correct JSX syntax | ✅ | Standard `{condition && (...)}` pattern |
| Edge case: attendancePercent = 0 | ✅ | Bar hidden, gray text, "— attendance" |
| Edge case: attendancePercent = 1 | ✅ | Bar at 1%, red text, "1% attendance" |
| Edge case: attendancePercent = 50 | ✅ | Bar at 50%, warning text, "50% attendance" |
| Edge case: attendancePercent = 80 | ✅ | Bar at 80%, success text, "80% attendance" |
| Edge case: attendancePercent = 100 | ✅ | Full bar, success text, "100% attendance" |
| Edge case: null input | ✅ | `null * 100 = 0` → treated as 0 |
| Edge case: undefined input | ❌ WR-01 | `NaN` → wrong color (red instead of gray) |
| Distinguishes 0% from missing | ❌ IN-01 | Both treated as "—"; pre-existing issue |

The commit accomplishes its stated purpose with minimal code change. The NaN propagation issue (WR-01) is the only actionable defect — it should be fixed to match the defensive pattern already used in `App.jsx`.

---

_Reviewed: 2026-05-30T12:20:00Z_
_Reviewer: agent (gsd-code-reviewer)_
_Depth: standard_
