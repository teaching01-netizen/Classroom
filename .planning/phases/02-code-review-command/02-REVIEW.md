---
phase: 02-code-review-command
reviewed: 2026-05-29T19:00:00Z
depth: standard
files_reviewed: 2
files_reviewed_list:
  - web/src/components/QRModal.jsx
  - web/src/pages/CheckinDetail.jsx
findings:
  critical: 0
  warning: 1
  info: 4
  total: 5
status: issues_found
---

# Phase 02: Code Review Report — Task 5: Expired state for QRModal

**Reviewed:** 2026-05-29T19:00:00Z
**Depth:** standard
**Files Reviewed:** 2
**Status:** issues_found

## Summary

Reviewed T5 implementation — expired state overlay with final stats and "Get New QR" button on `QRModal.jsx`, with `onRefresh` prop wired in `CheckinDetail.jsx`. The conditional rendering is clean: ternary branch for expired vs active state, Fragment-wrapped active path with no changes. `onRefresh` is guarded with `{onRefresh && (...)}`, preserving backward compatibility.

**Tests:** 31/34 passing across 6 test files. The 3 failures (`setLoading is not a function` in `useCheckins.test.js`) are pre-existing — not caused by T5. No regressions.

**Key concern:** The expired transition has no screen-reader announcement (`role="alert"` or `aria-live`). The ⏰ emoji lacks an accessible label. These degrade the UX for assistive-tech users who won't know the check-in period has ended.

## Warnings

### WR-01: Expired state transition not announced to screen readers

**File:** `web/src/components/QRModal.jsx:109`

**Issue:** When the countdown reaches 0, the component switches from active to expired state. The expired container (line 109) has no `role="alert"`, `role="status"`, or `aria-live="polite"` attributes. A sighted user sees the visual change; a screen-reader user gets no announcement that the check-in period has ended or that a new action ("Get New QR") is available.

**Fix:** Add `role="alert"` to the expired-state container:

```jsx
{isExpired ? (
  <div
    role="alert"
    style={{
      width: 'min(75vw, 420px)',
      // ...rest unchanged
    }}
  >
```

Or use `aria-live="polite"` on a wrapping element that persists across both states.

## Info

### IN-01: ⏰ emoji lacks accessible label

**File:** `web/src/components/QRModal.jsx:121-126`

**Issue:** The alarm-clock emoji is rendered as bare text content. Screen readers will announce it as "alarm clock" or similar depending on platform/voice, but the reading is inconsistent. Better to mark it as decorative or provide an explicit label.

**Fix:** Either treat as decorative (hides from a11y tree) or add an explicit label:

```jsx
<div role="img" aria-label="Expired" style={{ fontSize: '48px', lineHeight: '1' }}>
  ⏰
</div>
```

### IN-02: Dead code in active-state countdown pill (`timeLeft <= 0 ? 'Expired'`)

**File:** `web/src/components/QRModal.jsx:210`

**Issue:** Line 210 (`{timeLeft <= 0 ? 'Expired' : `Expires in ${timeLeft}s`}`) lives inside the active-state branch, which only renders when `isExpired === false`. But `isExpired` is `timeLeft !== null && timeLeft <= 0` — so when `timeLeft <= 0`, the active branch is never reached. The `<= 0` case here is unreachable dead code.

**Note:** This was correct before T5 when there was no separate expired state. Now it's vestigial.

**Fix:** Simplify the active-state pill to only handle the non-expired range:

```jsx
{timeLeft !== null && (
  <div style={{ /* ... */ }}>
    {timeLeft <= 10 && <span>⚠️</span>}
    Expires in {timeLeft}s
  </div>
)}
```

### IN-03: No `type="button"` on "Get New QR" and "Close" buttons

**File:** `web/src/components/QRModal.jsx:153-169, 216-230`

**Issue:** Both `<button>` elements lack `type="button"`. If the modal is ever nested inside a `<form>` element (now or via future refactoring), these buttons would default to `type="submit"` and trigger a form submission. Defensive best practice is to always specify `type="button"` for non-submit buttons.

**Fix:**

```jsx
<button type="button" onClick={onRefresh} style={{ /* ... */ }}>
  Get New QR
</button>
<button type="button" onClick={onClose} style={{ /* ... */ }}>
  Close
</button>
```

### IN-04: Hardcoded pixel values in expired state (no CSS variable tokens)

**File:** `web/src/components/QRModal.jsx:121-151`

**Issue:** The expired state uses literal px values: `fontSize: '48px'` (alarm icon), `fontSize: '18px'` (heading), `fontSize: '14px'` (body), `maxWidth: '260px'` (body text). The rest of the component uses `var(--space-*)` tokens with px fallbacks. For consistency, define these values as CSS custom properties or use existing typography tokens (`--font-size-*`).

**Fix (example):**

```jsx
<div style={{ fontSize: 'var(--font-size-display, 48px)', lineHeight: '1' }}>⏰</div>
<div style={{ fontSize: 'var(--font-size-lg, 18px)', fontWeight: '600', ... }}>
  Check-in period ended
</div>
```

## Test Results

| Result | Count |
|--------|-------|
| Test files passed | 5 of 6 |
| Tests passed | 31 of 34 |
| Tests failed | 3 (all pre-existing: `setLoading is not a function` in `useCheckins.test.js`) |

The 3 failures reference `setLoading()` which does not exist on `useSessionStore` (the store exposes `setInitialLoading` and `setRefreshing`). These failures predate T5 and are unchanged by this commit.

No regressions from T5.

## Assessment

**Approve with issues.** The implementation is functionally correct:
- Expired state renders when `timeLeft <= 0`, with final stats and action button
- Active state preserved identically (wrapped in Fragment, no regressions)
- `onRefresh` is optional and backward-compatible (guarded render)
- `isExpired` correctly handles the `null` case (no expiry → not expired)
- Interval self-cleanup via `useCountdown` works correctly

The findings are all low-severity — no critical bugs, security issues, or behavioral regressions. The accessibility gap (WR-01, IN-01) should be addressed before shipping for screen-reader users. The dead code (IN-02) is harmless but worth cleaning up for clarity.

---

_Reviewed: 2026-05-29T19:00:00Z_
_Reviewer: gsd-code-reviewer_
_Depth: standard_
