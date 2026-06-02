# Coding Conventions

**Analysis Date:** 2026-06-02

## Naming Patterns

**Go Files:**
- `snake_case.go` — e.g., `session_checkin_repository.go`, `classroom_client.go`, `room_manager.go`
- Test files co-located: `*_test.go` in same package

**Frontend Files:**
- Hooks: `use<PascalCase>.js` — e.g., `useCourses.js`, `useCheckins.js`, `useWebSocket.js`
- Stores: `use<PascalCase>Store.js` — e.g., `useCourseStore.js`, `useSessionStore.js`
- Components: `PascalCase.jsx` — e.g., `CourseCard.jsx`, `StatsBar.jsx`, `QRModal.jsx`
- Pages: `PascalCase.jsx` — e.g., `CourseDashboard.jsx`, `SessionList.jsx`, `CheckinDetail.jsx`

**Go Functions:**
- Exported: `PascalCase` — e.g., `GetCourses()`, `FetchQR()`, `NewSessionPool()`
- Unexported: `camelCase` — e.g., `fetchCourses()`, `doLoginLocked()`, `scanRoom()`
- Handler factories: `<verb><Noun>Handler` — e.g., `getCoursesHandler()`, `getRoomHandler()`
- Constructor pattern: `New<Type>` — e.g., `NewClassroomClient()`, `NewPgRoomRepository()`

**Go Types:**
- Exported: `PascalCase` — e.g., `ClassroomClient`, `RoomStatus`, `CourseSummary`
- Interfaces: Method-name-based — e.g., `RoomRepository`, `QrClient`, `SessionCheckinRepository`
- Sentinel errors: `Err<Description>` — e.g., `ErrAuthExpired`, `ErrRateLimited`

**JSON Fields:**
- Backend API: `snake_case` — e.g., `course_id`, `total_sessions`, `enrolled_count`
- Frontend (attendance report): `camelCase` — e.g., `courseId`, `sessionId`, `attendedSessions`, `atRisk`

## Code Style

**Go Formatting:**
- Tool: `gofmt` (standard)
- No custom `.golangci-lint` or linter config detected

**Frontend Formatting:**
- No `.prettierrc` or formatter config detected
- No ESLint config beyond `package.json` plugin declarations

**Linting:**
- Go: No linter config (no `.golangci.yml`)
- Frontend: ESLint 8 with `eslint-plugin-react`, `eslint-plugin-react-hooks`, `eslint-plugin-react-refresh`

## Import Organization

**Go Import Order:**
1. Standard library
2. Third-party packages
3. Internal packages (`qr-command-center/internal/...`)

Example from `cmd/server/main.go`:
```go
import (
    "context"
    "log/slog"
    // ... stdlib

    "github.com/joho/godotenv"
    "golang.org/x/time/rate"
    // ... third-party

    "qr-command-center/internal/api"
    "qr-command-center/internal/cache"
    // ... internal
)
```

**Frontend Import Order:**
1. React/React-DOM
2. Third-party (react-router-dom, zustand)
3. Local hooks/stores
4. Local components
5. Local styles

**Path Aliases:**
- None — all imports use relative paths (`../hooks/useCourses`, `../store/useCourseStore`)

## Error Handling

**Patterns:**
- Handler layer: Check domain sentinel errors, map to HTTP status codes
  ```go
  if errors.Is(err, domain.ErrAuthExpired) {
      writeJSON(w, http.StatusUnauthorized, errorResponse("Warwick session expired"))
      return
  }
  ```
- Service/client layer: Return domain error types (`*FetchError` with `Kind` enum)
- Panic recovery: `defer recover()` in goroutines (room workers, data refresher)
- Frontend: `try/catch` with error state in hooks; `ErrorBoundary` component for render errors

## Logging

**Framework:** `log/slog` (JSON handler)

**Patterns:**
- Info: Startup events, cache refresh completion, room state changes
- Warn: Pool failures, cache refresh failures, slow subscribers
- Debug: Individual API call failures, cache staleness, DB query failures
- Error: Panics, failed DB writes, server errors
- Always structured: `slog.Warn("msg", "key", value)`

## Comments

**When to Comment:**
- Package-level: Brief description of purpose (e.g., `// CachedSession wraps a SessionDetail...`)
- Function-level: Doc comments on exported functions, especially complex ones
- Inline: Explanatory comments for non-obvious logic (e.g., `// Skip straight to max backoff (15 min) to avoid ping-pong`)

**JSDoc/TSDoc:**
- Not used — frontend has no JSDoc annotations
- No TSDoc patterns (plain JSX, not TypeScript)

## Function Design

**Size:**
- Handler functions: 20-40 lines (request parsing → business logic → response)
- Client methods: 50-100 lines (cache check → pool acquire → API call → cache set)
- Room worker: 400+ lines (complex state machine — this is an exception)

**Parameters:**
- Go: Explicit parameters, no options pattern (except variadic for optional args like `checkinRepo ...db.SessionCheckinRepository`)
- Frontend: Single config object for hooks (e.g., `useCourseAttendance(courseId, { threshold = 0.8 })`)

**Return Values:**
- Go: `(result, error)` tuple for all fallible operations
- Frontend: Hook returns `{ data, isLoading, error, refetch }` pattern

## Module Design

**Exports:**
- Go: Exported types/functions via PascalCase; unexported for internal implementation
- Frontend: Named exports for hooks/stores (`export const useCourses = ...`); default export for App

**Barrel Files:**
- None — no index.js re-exports

---

*Convention analysis: 2026-06-02*
