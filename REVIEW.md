---
phase: step0-fix-force-hazard
reviewed: 2026-05-30T12:30:00Z
depth: standard
files_reviewed: 1
files_reviewed_list:
  - internal/db/db.go
findings:
  critical: 0
  warning: 2
  info: 1
  total: 3
status: issues_found
---

# Code Review — Step 0: Fix Force(1) Hazard + pgxpool Config

**Commit:** `a48255b`
**Message:** `fix(db): Fix Force(1) hazard and configure pgxpool`
**Reviewed:** 2026-05-30T12:30:00Z
**Depth:** standard
**Files Reviewed:** 1
**Status:** issues_found

## Summary

Commit `a48255b` rewrites `internal/db/db.go` (+7/−5). Three changes:

1. **Force(1) removal** — replaced blind `m.Force(1)` with type-assertion check for `migrate.ErrDirty` + `slog.Error` + `fmt.Errorf`. **Correct in spirit** — stops startup instead of skipping past dirty state.
2. **Version assertion** — added `SELECT version FROM schema_migrations` check after migration, returning error if version < 4.
3. **pgxpool config** — set MaxConns=25, MinConns=5, MaxConnLifetime=30m, MaxConnIdleTime=5m. Types and values are correct.

**Overall: Functional intent is correct but two error-handling gaps reduce diagnostic quality.**

## Warnings

### WR-01: Actual query error silently discarded — misleading error message

**File:** `internal/db/db.go:69-71`
**Severity:** Warning

**Issue:**
The condition `if rowErr != nil || version < 4` bundles two distinct failure modes into one error path:

```go
rowErr := db.QueryRowContext(context.Background(), "SELECT version FROM schema_migrations").Scan(&version)
if rowErr != nil || version < 4 {
    slog.Error("schema version below required minimum", "have", version, "need", 4)
    return fmt.Errorf("schema version %d below required minimum 4", version)
}
```

When `rowErr != nil` (query fails — connection refused, table missing, permissions error):

1. **`rowErr` is never logged** — the actual error is completely discarded. The `slog.Error` call only logs the version (which is `0` after a failed `Scan`), not the root cause.
2. **Error message is misleading** — `fmt.Errorf("schema version 0 below required minimum 4", version)` frames the problem as "version too low" when the real issue could be a DB connection failure.
3. **Debugging is misdirected** — operator sees "schema version 0" and investigates migration state instead of connectivity/permissions.

**Production impact:** If the `schema_migrations` query fails (pgBouncer reset, permissions change, SSL negotiation issue), ops sees a plausible-sounding but wrong error. This can add hours to incident response.

**Fix:** Split the two cases:

```go
var version int
if err := db.QueryRowContext(context.Background(), "SELECT version FROM schema_migrations").Scan(&version); err != nil {
    slog.Error("failed to verify schema version", "error", err)
    return fmt.Errorf("schema version check failed: %w", err)
}
if version < 4 {
    slog.Error("schema version below required minimum", "have", version, "need", 4)
    return fmt.Errorf("schema version %d below required minimum 4", version)
}
```

This preserves error cause (via `%w`) and adds structured logging of the actual error.

---

### WR-02: Type assertion on dirty error is fragile — should use `errors.As`

**File:** `internal/db/db.go:59`
**Severity:** Warning

**Issue:**
```go
if _, ok := err.(migrate.ErrDirty); ok {
```

This is a **direct type assertion** on the `error` interface. It works when `m.Up()` returns `migrate.ErrDirty{Version: V}` directly (which golang-migrate v4.19.1 does). However, it **silently fails** (`ok = false`) if the error is wrapped (e.g., via `fmt.Errorf("...: %w", ...)`) for any reason — library upgrade, intermediate helper, or driver wrapping.

Go 1.13+ idiom for deep error inspection is `errors.As`:

```go
var dirty migrate.ErrDirty
if errors.As(err, &dirty) {
    slog.Error("migration dirty", "version", dirty.Version)
    return fmt.Errorf("migration dirty at version %d: %w", dirty.Version, err)
}
```

**Impact:** If the dirty error is ever wrapped, the check silently falls through to the generic `return err` on line 63. The startup shutdown still works (it returns an error), but the specific "dirty — manual investigation required" message is lost, and the dirty version number isn't surfaced to the operator.

**Fix:**
```go
import "errors"

// ...

var dirty migrate.ErrDirty
if errors.As(err, &dirty) {
    slog.Error("migration dirty — manual investigation required", "version", dirty.Version)
    return fmt.Errorf("migration dirty at version %d: %w", dirty.Version, err)
}
```

Note: The `errors` package is not currently imported. Need to add `"errors"` to the import block.

---

## Info

### IN-01: Dirty error log lacks structured data

**File:** `internal/db/db.go:60`
**Severity:** Info

**Issue:**
```go
slog.Error("migration dirty — manual investigation required")
```

The log message doesn't attach the actual error or dirty version. Structured logging with slog should include the error to allow log aggregation tools to correlate:

```go
slog.Error("migration dirty — manual investigation required", "error", err)
```

This pairs with WR-02: if `errors.As` is used, the version can be added as `"version", dirty.Version`.

---

## Strengths

| Aspect | Assessment |
|---|---|
| **Force(1) removal** | ✅ Correct. Stopping startup on dirty is safer than blindly forcing past it. |
| **pgxpool config types** | ✅ Correct. `MaxConns`/`MinConns` are `int32`, `MaxConnLifetime`/`MaxConnIdleTime` are `time.Duration`. All expressions are typed correctly. |
| **Import hygiene** | ✅ Removed unused `"os"`. Added `"fmt"`, `"log/slog"`, `"time"`. All used. |
| **ErrNoChange handling** | ✅ `err != migrate.ErrNoChange` correctly treats "no change" as success. |
| **Dirty version attribute** | ✅ `migrate.ErrDirty` has a `Version` field — captured if the type assertion were replaced with `errors.As`. |
| **slog key-value pairs** | ✅ `slog.Error("...", "have", version, "need", 4)` — correct key-value syntax. |

## Specific Questions Answered

1. **schema_migrations table missing?** golang-migrate creates it before the first migration. `m.Up()` succeeds (runs all 4) → table exists at version check. Safe.

2. **Error message when version=0?** "schema version 0 below required minimum 4" — technically correct but misleading if caused by query failure (see WR-01).

3. **pgxpool config types?** All correct (`time.Duration` for lifetime/idle time, `int32` for counts).

4. **Panic on fresh DB?** No. Table created by golang-migrate before version check runs. `Scan` into `int` always succeeds for a valid row.

5. **slog key-value pairs?** Correct. Keys and values are alternated as required by structured logging API.

---

## Assessment

**Overall: ⚠️ Minor issues — fix WR-01 before merging**

The commit correctly removes the Force(1) hazard and configures pgxpool properly. The version assertion provides a safety net. However, WR-01 (error swallowed, misleading diagnostic) should be fixed before this ships — it directly impacts the ability to debug production failures. WR-02 is a best-practice improvement but won't cause issues with the current library version.

| Criteria | Verdict | Notes |
|---|---|---|
| Force(1) removed | ✅ | Replaced with proper dirty handling + error return |
| Version >= 4 enforced | ✅ | Correct check, proper fail-closed |
| pgxpool configured | ✅ | Values reasonable, types correct |
| Error handling complete | ❌ WR-01 | rowErr swallowed on query failure |
| Idiomatic Go | ❌ WR-02 | Type assertion instead of errors.As |
| No unnecessary changes | ✅ | Minimal diff, focused on stated goals |
| Import cleanup | ✅ | Unused `"os"` removed |

---

_Reviewed: 2026-05-30T12:30:00Z_
_Reviewer: agent (gsd-code-reviewer)_
_Depth: standard_
