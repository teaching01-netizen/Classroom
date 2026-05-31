---
phase: 02-pagination-component
reviewed: 2026-05-31T00:00:00Z
depth: standard
files_reviewed: 1
files_reviewed_list:
  - web/src/components/Pagination.jsx
findings:
  critical: 0
  warning: 4
  info: 2
  total: 6
status: issues_found
---

# Phase 2: Code Review Report

**Reviewed:** 2026-05-31
**Depth:** standard
**Files Reviewed:** 1
**Status:** issues_found

## Summary

Single-file review of `Pagination.jsx`. Component renders pagination controls with Previous/Next buttons and numbered page buttons. Follows existing codebase patterns (inline styles with CSS variables, `onMouseEnter`/`onMouseLeave` for hover, functional component style). The page generation logic is correct across all boundary conditions. Findings are concentrated around missing accessibility landmarks, no input validation for edge-case props, and `e.target` vs `e.currentTarget` in hover handlers.

---

## Critical Issues

No critical issues found.

---

## Warnings

### WR-01: No `aria-label` on Previous/Next buttons — screen readers get "Previous" and "Next" which is acceptable, but numbered page buttons have no accessible name beyond the visual number

**File:** `web/src/components/Pagination.jsx:93-108, 141-156`
**Issue:** The Previous and Next buttons have text content ("Previous"/"Next") which screen readers can announce, so this is fine for those. However, the overall `<div>` container (line 70) has no `role="navigation"` or `aria-label` to identify it as a pagination landmark. Screen readers won't announce "pagination" for this region.

**Fix:**
```jsx
<nav aria-label="Pagination" style={{ /* ... */ }}>
```

### WR-02: No prop validation — `currentPage`, `perPage`, or `totalItems` could be 0, negative, or undefined

**File:** `web/src/components/Pagination.jsx:57-61`
**Issue:** If `perPage` is `0` or `undefined`, `Math.ceil(totalItems / perPage)` produces `Infinity` or `NaN`. This cascades: `totalPages` becomes `Infinity`, and `getPageNumbers` will push an infinite number of pages (the `totalPages <= 7` guard passes since `Infinity > 7`, but then `Math.min(totalPages - 1, currentPage + 1)` and the loop produce unexpected results). There is no PropTypes or runtime guard. The component will render but produce garbage output or potentially crash.

**Fix:**
```jsx
// Add at top of component:
const safePerPage = Math.max(1, perPage || 1);
const safeTotalItems = Math.max(0, totalItems || 0);
const totalPages = Math.max(1, Math.ceil(safeTotalItems / safePerPage));
```

### WR-03: Inline `onMouseEnter`/`onMouseLeave` handlers directly mutate `e.target.style` — fragile and conflicts with React's declarative model

**File:** `web/src/components/Pagination.jsx:100-105, 129-134, 148-153`
**Issue:** Using `e.target.style.background = ...` directly mutates the DOM element's style attribute. This bypasses React's rendering cycle and can conflict with any React-driven style changes. If React re-renders and resets inline styles, the hover state could persist visually. This is an existing codebase pattern (seen in `StudentRow.jsx:21-24`), so it's consistent — but the `e.target` vs `e.currentTarget` distinction matters. Line 101 uses `e.target` which could be a child element of the button if the button ever contains markup. Lines 21-23 in `StudentRow.jsx` correctly use `e.currentTarget`.

**Fix:** Use `e.currentTarget` instead of `e.target` on lines 101, 104, 130, 133, 149, 152 to ensure style is set on the button itself, not a child element.

### WR-04: Ellipsis span uses array index as key — could cause reconciliation issues if page numbers change frequently

**File:** `web/src/components/Pagination.jsx:112`
**Issue:** `key={`ellipsis-${index}`}` uses the array index. If the ellipsis appears at different indices as the user pages through, React may incorrectly reuse DOM nodes. In practice this is low-risk for pagination (the ellipsis is purely decorative text), but it's a React anti-pattern. The page number buttons correctly use the page number as key.

**Fix:** Use a stable key like `key="ellipsis-start"` / `key="ellipsis-end"` by differentiating the two ellipses in the data structure, or keep the index key and accept the minor risk since the span has no state.

---

## Info

### IN-01: Magic number `7` in `getPageNumbers` threshold — unclear intent

**File:** `web/src/components/Pagination.jsx:4`
**Issue:** `if (totalPages <= 7)` — the threshold of 7 is a magic number. It determines when the component switches from "show all pages" to "show abbreviated pages with ellipsis." The value 7 isn't documented or named.

**Fix:** Extract to a named constant:
```jsx
const MAX_FULL_PAGE_DISPLAY = 7;
```

### IN-02: Component is exported but not imported anywhere yet

**File:** `web/src/components/Pagination.jsx:57`
**Issue:** The component is exported (`export const Pagination`) but no file in `web/src/` imports it. This is expected if the feature is still being built, but worth noting.

---

## Structural Findings (fallow)

No structural findings were provided for this review.

---

_Reviewed: 2026-05-31T00:00:00Z_
_Reviewer: the agent (gsd-code-reviewer)_
_Depth: standard_
