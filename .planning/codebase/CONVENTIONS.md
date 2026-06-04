# Coding Conventions

**Analysis Date:** 2026-06-04

## Naming Patterns

**Files:**
- Go: snake_case.go (e.g., `classroom_client.go`, `session_checkin_repository.go`)
- React: PascalCase.jsx (e.g., `CourseCard.jsx`, `AttendanceTable.jsx`)
- Tests: `*_test.go` (Go), `*.test.js` or `*.test.jsx` (React)

**Functions:**
- Go: PascalCase for exported, camelCase for unexported (e.g., `GetCourses`, `fetchCourses`)
- React: camelCase for hooks and functions (e.g., `useCourses`, `fetchSessions`)

**Variables:**
- Go: camelCase (e.g., `courseID`, `sessionDetail`)
- React: camelCase (e.g., `searchQuery`, `statusFilter`)

**Types:**
- Go: PascalCase (e.g., `CourseSummary`, `SessionDetail`, `StudentCheckin`)
- React: PascalCase for components (e.g., `CourseCard`, `AttendanceTable`)

## Code Style

**Formatting:**
- Go: `gofmt` (standard Go formatting)
- React: Prettier (implied by consistent formatting)

**Linting:**
- Go: Standard Go vet/lint
- React: ESLint (implied by test structure)

## Import Organization

**Go:**
1. Standard library
2. Third-party packages
3. Internal packages

```go
import (
    "context"
    "fmt"
    
    "github.com/go-chi/chi/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    
    "qr-command-center/internal/domain"
)
```

**React:**
1. React and React Router
2. Third-party libraries
3. Internal components/hooks/stores

```javascript
import React, { useState, useEffect } from 'react';
import { useParams, Link } from 'react-router-dom';

import { useCourseAttendance } from '../hooks/useCourseAttendance';
import { AttendanceTable } from '../components/AttendanceTable';
```

**Path Aliases:**
- None detected (relative imports used)

## Error Handling

**Patterns:**
- Go: Return error as last value, wrap with context
- React: Try-catch with state management

```go
// Go pattern
courses, err := cc.GetCourses()
if err != nil {
    if errors.Is(err, domain.ErrAuthExpired) {
        writeJSON(w, http.StatusUnauthorized, errorResponse("Warwick session expired"))
        return
    }
    writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
    return
}
```

```javascript
// React pattern
try {
    const res = await fetch('/api/teacher/courses');
    const result = await res.json();
    if (result.success) {
        setCourses(result.data.courses);
    } else {
        setError(result.error || 'Failed to fetch courses');
    }
} catch (err) {
    setError(err.message || 'Network error');
}
```

## Logging

**Framework:** `slog` (structured logging)

**Patterns:**
- Debug: Detailed technical information
- Info: Significant events (startup, requests, completions)
- Warn: Recoverable issues (cache misses, pool exhaustion)
- Error: Unrecoverable failures (database errors, auth failures)

```go
slog.Info("warwick_courses_fetch",
    "user_id", userID,
    "http_status", resp.StatusCode,
    "records_total", data.RecordsTotal,
)
```

## Comments

**When to Comment:**
- Complex business logic (e.g., cache coherence patterns)
- Non-obvious workarounds (e.g., Warwick API quirks)
- TODO/FIXME for known issues

**JSDoc/TSDoc:**
- Go: Package and function comments (standard Go doc)
- React: No JSDoc detected

## Function Design

**Size:**
- Go: Functions tend to be short (20-50 lines)
- React: Components are focused (100-200 lines typical)

**Parameters:**
- Go: Context as first parameter for async operations
- React: Destructured props, options objects for hooks

**Return Values:**
- Go: Multiple return values (value, error)
- React: Arrays (for hooks) or JSX (for components)

## Module Design

**Exports:**
- Go: Exported types/functions start with uppercase
- React: Named exports for components, default export for pages

**Barrel Files:**
- Not detected (explicit imports used)

## React Patterns

**Hooks:**
- Custom hooks for data fetching (e.g., `useCourses`, `useSessions`)
- State management via Zustand stores
- Abort controllers for cleanup

**Components:**
- Functional components only (no class components)
- Inline styles (no CSS modules or styled-components)
- Props drilling avoided via stores

## Go Patterns

**Repository Pattern:**
- Interface + PostgreSQL implementation
- Example: `RoomRepository` + `PgRoomRepository`

**Client Pattern:**
- HTTP client with retry logic
- Session pool for connection management
- Cache-aside pattern for performance

---

*Convention analysis: 2026-06-04*
