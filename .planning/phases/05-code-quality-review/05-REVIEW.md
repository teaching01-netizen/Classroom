---
phase: 05-code-quality-review
reviewed: 2026-05-30T12:15:00Z
depth: standard
files_reviewed: 1
files_reviewed_list:
  - web/src/pages/CourseDashboard.jsx
findings:
  critical: 0
  warning: 1
  info: 1
  total: 2
status: issues_found
---

# Phase 5: Code Quality Review — Commit `9e587df`

**Reviewed:** 2026-05-30T12:15:00Z  
**Depth:** standard  
**Files Reviewed:** 1  
**Commit:** `9e587df` — Show `'—'` for unpopulated stats in CourseDashboard  
**Status:** issues_found

## Summary

Minimal 2-line change to `CourseDashboard.jsx` adding placeholder display for unpopulated stats. The change is syntactically correct, well-scoped, and achieves its stated goal with no stray modifications. One warning: the `> 0` heuristic conflates "data is 0%" with "data is absent" for `avgAttendance`, which can mislead when attendance is legitimately zero.

## Warnings

### WR-01: Zero-percent attendance conflated with unpopulated data

**File:** `web/src/pages/CourseDashboard.jsx:31`  
**Issue:** The guard `avgAttendance > 0 ? … : '—'` treats an average attendance of exactly 0% the same as missing data. If `avg_attendance_rate` is legitimately 0 (e.g., a course exists with sessions that had no attendance), the dashboard displays `—` instead of `0%`, misleading users into thinking the data is unavailable.

The `avg_attendance_rate` field is confirmed nullable (`!= null` check at `App.jsx:110`), so the `> 0` check handles the null case correctly (null → 0 → `> 0` false → `—`). However, it also catches the real-0 case (actual 0% attendance → `> 0` false → `—`).

**Root cause:** The ternary is a proxy for "has data" vs "unpopulated", but `> 0` cannot distinguish `null` (absent) from `0` (present but zero).

**Fix:** Check for `null`/`undefined` explicitly instead of using `> 0` as a data-existence proxy:

```jsx
// Current (line 31):
{ value: avgAttendance > 0 ? `${avgAttendance}%` : '—', label: 'Avg Attendance' }

// Fix — use nullish check on the raw per-course field, or
// guard against NaN (which also indicates computation failure):
{
  value: Number.isFinite(avgAttendance) && avgAttendance >= 0
    ? `${avgAttendance}%`
    : '—',
  label: 'Avg Attendance',
}
```

Alternatively, if the backend guarantees 0 and null are equivalent in this domain, suppress this warning as intentional. Verify with domain owner.

## Info

### IN-01: `totalSessions` has same latent ambiguity

**File:** `web/src/pages/CourseDashboard.jsx:29`  
**Issue:** Same `> 0` pattern as WR-01 applies to `totalSessions`. If `total_sessions` can be `undefined` (not just null/0), the reduce produces `NaN`, and `NaN > 0` is false → shows `—` (correct fallback). But if `total_sessions` is legitimately 0 (sessions exist but none counted yet), the same `—` renders. Lower practical risk than attendance (0 sessions is usually "unpopulated" by domain semantics), but the asymmetry with `totalStudents` (which is never guarded) is notable.

**Fix:** Consider same explicit guard as WR-01, or document the intentional asymmetry.

---

## Strengths

1. **Minimal diff** — Exactly 2 lines changed, +2/−2. No scope creep.
2. **Correct syntax** — Template literal `\`${avgAttendance}%\`` inside a ternary works correctly. Em dash `—` (U+2014) is typographically correct for the placeholder.
3. **Good heuristic for common case** — When the backend returns `null` for unpopulated fields, JS arithmetic coercion (`null` → 0) combined with `> 0` correctly triggers the placeholder.
4. **Doesn't over-apply** — `activeCourses` and `totalStudents` are left unguarded (they are always meaningful even as 0), showing restraint.
5. **No type issues** — JSX renders numbers and strings equally well; the StatsBar doesn't care about value types.

## Assessment

**Good change, but has a semantic blind spot.** The `> 0` pattern is a common shorthand that happens to work for `null` via coercion, but it papers over the distinction between "data is absent" and "data is zero." The `avgAttendance` case is the sharper risk — showing `—` when attendance is actually 0% misrepresents course engagement. Fix by using an explicit presence check (`Number.isFinite` / `!= null` guarding) instead of `> 0`. `totalSessions` has the same structural issue but lower practical impact.

---

_Reviewed: 2026-05-30T12:15:00Z_  
_Reviewer: gsd-code-reviewer_  
_Depth: standard_
