---
phase: 02-code-review
reviewed: 2026-05-31T12:00:00Z
depth: standard
files_reviewed: 3
files_reviewed_list:
  - web/src/pages/CheckinDetail.jsx
  - web/src/components/StudentTable.jsx
  - web/src/hooks/useCheckins.js
findings:
  critical: 2
  warning: 4
  info: 3
  total: 9
status: issues_found
---

# Phase 02: Code Review Report

**Reviewed:** 2026-05-31
**Depth:** standard
**Files Reviewed:** 3
**Status:** issues_found

## Summary

Reviewed three files implementing client-side pagination, safe JSON parsing, lite rooms endpoint, and a polling interval change. Pagination logic is structurally sound but has a state-clamping bug that causes blank pages and broken display text. Poll intervals are not guarded against double-start, causing leaked `setInterval` handles. CSV export has incorrect escaping. Overall the JSON parse safety and cleanup patterns are solid.

---

## Critical Issues

### CR-01: Pagination state not clamped — blank page + broken "Showing X–Y of Z" text

**File:** `web/src/pages/CheckinDetail.jsx:144-145`
**Issue:** When the filtered student list shrinks (search narrows, filter applied, students leave), the current `page` state can exceed `totalPages`. This produces an empty `<tbody>` while `filteredStudents.length > 0`, so the "No students match" empty-state block (line 416) never renders. The `Pagination` component then displays nonsensical text (e.g. "Showing 51–50 of 50") because `startItem > endItem`.

Concrete trigger: User searches, narrowing from 60 to 30 students (2 pages at perPage=25). They are on page 3 from before the search. `paginatedStudents = filteredStudents.slice(50, 75) = []`. Table body is empty. Pagination says "Showing 51–30 of 30".

**Fix:**
```jsx
// Add after line 33 (inside the search/filter reset effect) or as a separate effect:
useEffect(() => {
  if (totalPages > 0 && page > totalPages) {
    setPage(totalPages);
  }
}, [totalPages, page]);
```
Also ensure `page >= 1` after clamping (it always will be since `totalPages >= 1` when students exist).

---

### CR-02: Leaked `setInterval` when `handleStartCheckin` called multiple times

**File:** `web/src/pages/CheckinDetail.jsx:232`
**Issue:** `handleStartCheckin` assigns `pollRef.current = setInterval(...)` without first clearing any existing interval. This function is invoked from:
1. The auto-start effect (line 115) when no room exists
2. The "Retry" button (line 287)
3. The QRModal's `onRefresh` prop (line 434)

If called while a poll is already active (e.g. user clicks "Refresh" in QRModal while a poll from the initial auto-start is still running), the old `setInterval` handle is overwritten and never cleared. It continues firing fetch requests indefinitely — a memory/network leak.

**Fix:**
```jsx
// At the top of handleStartCheckin, before any fetch:
if (pollRef.current) {
  clearInterval(pollRef.current);
  pollRef.current = null;
}
```

Apply the same guard in the `autoStart` effect at line 85.

---

## Warnings

### WR-01: CSV export does not escape commas or special characters in field values

**File:** `web/src/pages/CheckinDetail.jsx:162-171`
**Issue:** Fields are joined with `row.join(',')` without quoting. If any student name, school, or nickname contains a comma (e.g. "Smith, John"), the generated CSV row will be malformed — the comma splits into an extra column.

**Fix:**
```jsx
const escapeCSV = (val) => {
  const str = String(val ?? '');
  if (str.includes(',') || str.includes('"') || str.includes('\n')) {
    return `"${str.replace(/"/g, '""')}"`;
  }
  return str;
};
const csv = [headers, ...rows].map((row) => row.map(escapeCSV).join(',')).join('\n');
```

---

### WR-02: `useCheckins` does not check `response.ok` before parsing JSON

**File:** `web/src/hooks/useCheckins.js:23-24`
**Issue:** `fetch()` is called then `.json()` is immediately invoked without checking `response.ok`. If the server returns a 500 with an HTML error page, `.json()` throws a `SyntaxError` which is caught, but the error message will be the generic parse error ("Unexpected token '<'…") instead of the actual server error. Similarly, `toggleCheckin` (line 47-52) has the same issue — a failed toggle silently returns with no user feedback beyond `console.error`.

**Fix:**
```jsx
const response = await fetch(`/api/teacher/courses/${courseId}/sessions/${sessionId}`, { signal });
if (!response.ok) {
  throw new Error(`Server error: ${response.status}`);
}
const result = await response.json();
```

Same pattern for `toggleCheckin` — add a user-facing error (e.g. toast or state setter).

---

### WR-03: `paginatedStudents` and `totalCount` not memoized — unnecessary re-renders

**File:** `web/src/pages/CheckinDetail.jsx:145, 159`
**Issue:** `paginatedStudents` is computed via `Array.slice()` on every render, producing a new reference each time. `StudentTable` receives this as the `students` prop. Since `StudentTable` is not wrapped in `React.memo`, every render of `CheckinDetail` (including during the 10-second poll) re-renders the entire table and all its rows — even when the data hasn't changed.

`totalCount` (line 159) is also recomputed every render, unlike its neighbor `checkedCount` (line 158) which is memoized.

**Fix:**
```jsx
const paginatedStudents = useMemo(
  () => filteredStudents.slice((page - 1) * perPage, page * perPage),
  [filteredStudents, page, perPage]
);

const totalCount = students.length; // minor — value is cheap; memoize for consistency
```
Additionally, wrap `StudentTable` in `React.memo` and `Pagination` in `React.memo` to avoid re-renders when props haven't changed.

---

### WR-04: `handleStartCheckin` calls `setIsStarting(false)` in `finally` even on the auto-start path

**File:** `web/src/pages/CheckinDetail.jsx:262-264`
**Issue:** When `autoStart` (line 115) calls `handleStartCheckin()`, it sets `isStarting = true` (line 182) and resets it in `finally` (line 263). This is benign during the initial render, but if a user triggers the "Retry" button (line 287) while the auto-start `handleStartCheckin` is still in-flight, both calls share the same `isStarting` state. The first to complete will set `false`, potentially hiding the loading state of the second.

**Fix:** This is a minor race condition. A more robust approach is to use a ref counter or separate the auto-start loading state from the user-triggered loading state. For now, document the known limitation.

---

## Info

### IN-01: Redundant `totalPages` computation — duplicate between CheckinDetail and Pagination

**File:** `web/src/pages/CheckinDetail.jsx:144` and `web/src/components/Pagination.jsx:59`
**Issue:** `totalPages` is computed independently in both `CheckinDetail.jsx:144` (`Math.ceil(filteredStudents.length / perPage)`) and `Pagination.jsx:59` (`Math.max(1, Math.ceil(totalItems / safePerPage))`). The parent's `totalPages` can be 0 when no students match, while the Pagination component clamps to `Math.max(1, ...)`. This means the parent's conditional rendering at line 416 (`filteredStudents.length === 0`) is the real guard, but the Pagination component's own `totalPages` diverges from the parent's. If Pagination were ever rendered without this guard, its `totalPages = 1` (from the clamp) would show a single page with 0 items.

**Fix:** No immediate bug, but the Pagination component should not clamp `totalPages` to `Math.max(1, ...)` — it should accept 0 and hide itself (which `StudentTable` already does via the `totalItems > 0` guard). Remove the `Math.max(1, ...)` from Pagination's `totalPages` computation to keep sources of truth consistent.

---

### IN-02: `reset()` on navigation causes "Loading students..." flash

**File:** `web/src/hooks/useCheckins.js:66-67`
**Issue:** When navigating between sessions, `reset()` clears the store and sets `hasLoadedRef.current = false`. The next `fetchStudents` call enters the `setInitialLoading()` branch (line 20), causing a flash of "Loading students…" even though cached/stale data could be shown immediately. This is a UX decision, not a bug, but worth noting if the flash feels jarring.

**Fix:** Consider showing stale data during the loading phase (e.g. "Syncing..." badge pattern already used in `CheckinDetail.jsx:296-311`) instead of replacing the entire view with "Loading students...".

---

### IN-03: Empty `catch` blocks in polling silently swallow all errors

**File:** `web/src/pages/CheckinDetail.jsx:109-111` and `256-258`
**Issue:** The `catch { // ignore poll errors }` blocks suppress all errors including non-network errors (e.g. `AbortError`, programming errors). While network errors during polling are expected, silently swallowing everything makes debugging harder.

**Fix:**
```jsx
} catch (err) {
  // Only suppress network errors during polling
  if (err.name !== 'AbortError') {
    console.debug('Poll error (suppressed):', err.message);
  }
}
```

---

_Reviewed: 2026-05-31T12:00:00Z_
_Reviewer: the agent (gsd-code-reviewer)_
_Depth: standard_
