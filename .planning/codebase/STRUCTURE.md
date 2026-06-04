# Codebase Structure

**Analysis Date:** 2026-06-04

## Directory Layout

```
check in auto/
├── cmd/
│   └── server/
│       └── main.go              # Server entry point
├── internal/
│   ├── api/                     # HTTP handlers and routing
│   ├── cache/                   # In-memory TTL cache
│   ├── db/                      # Database repositories and migrations
│   ├── domain/                  # Domain models and types
│   ├── metrics/                 # Prometheus metrics
│   ├── middleware/               # HTTP middleware (rate limiting)
│   ├── service/                 # Business logic services
│   └── warwick/                 # Warwick API client
├── web/
│   ├── src/
│   │   ├── components/          # React components
│   │   ├── hooks/               # React hooks
│   │   ├── pages/               # Page components
│   │   ├── store/               # Zustand stores
│   │   └── styles/              # CSS tokens
│   └── dist/                    # Built frontend assets
├── docs/                        # Documentation
├── go.mod                       # Go module definition
├── go.sum                       # Go dependencies
└── docker-compose.yml           # Docker configuration
```

## Directory Purposes

**cmd/server:**
- Purpose: Application entry point
- Contains: `main.go` - server initialization, dependency wiring
- Key files: `cmd/server/main.go`

**internal/api:**
- Purpose: HTTP layer - routing, handlers, WebSocket
- Contains: Route definitions, HTTP handlers, middleware
- Key files: `internal/api/routes.go`, `internal/api/teacher_handlers.go`, `internal/api/websocket.go`

**internal/cache:**
- Purpose: In-memory TTL cache with stale-while-revalidate
- Contains: Generic cache implementation with async refresh
- Key files: `internal/cache/cache.go`

**internal/db:**
- Purpose: Database access layer
- Contains: Repository interfaces and PostgreSQL implementations, migrations
- Key files: `internal/db/repository.go`, `internal/db/session_checkin_repository.go`, `internal/db/attendance_report_repository.go`, `internal/db/migrations/`

**internal/domain:**
- Purpose: Domain models and business types
- Contains: Structs for Course, Session, Student, Room, Dashboard
- Key files: `internal/domain/classroom.go`, `internal/domain/room.go`, `internal/domain/dashboard.go`

**internal/metrics:**
- Purpose: Prometheus metrics collection
- Contains: Metric definitions and collectors
- Key files: `internal/metrics/metrics.go`

**internal/middleware:**
- Purpose: HTTP middleware
- Contains: Rate limiting implementation
- Key files: `internal/middleware/ratelimit.go`

**internal/service:**
- Purpose: Business logic services
- Contains: RoomManager, DataRefresher, ReportPersister, SessionPreWarmer
- Key files: `internal/service/room_manager.go`, `internal/service/data_refresher.go`, `internal/service/report_persister.go`, `internal/service/session_prewarmer.go`

**internal/warwick:**
- Purpose: Warwick external API integration
- Contains: HTTP client, auth management, session pool, report computation
- Key files: `internal/warwick/classroom_client.go`, `internal/warwick/auth.go`, `internal/warwick/session_pool.go`, `internal/warwick/report_db_source.go`

**web/src/components:**
- Purpose: Reusable React UI components
- Contains: CourseCard, SessionTable, AttendanceTable, StudentTable, QRDisplay
- Key files: `web/src/components/CourseCard.jsx`, `web/src/components/AttendanceTable.jsx`

**web/src/hooks:**
- Purpose: React hooks for data fetching and state
- Contains: useCourses, useSessions, useCourseAttendance, useWebSocket
- Key files: `web/src/hooks/useCourses.js`, `web/src/hooks/useCourseAttendance.js`

**web/src/pages:**
- Purpose: Page-level React components
- Contains: CourseDashboard, SessionList, CheckinDetail, CourseAttendance, AbsenceDashboard
- Key files: `web/src/pages/CourseDashboard.jsx`, `web/src/pages/CourseAttendance.jsx`

**web/src/store:**
- Purpose: Zustand state management
- Contains: useCourseStore, useSessionStore, useRoomStore, useDashboardFiltersStore
- Key files: `web/src/store/useCourseStore.js`, `web/src/store/useSessionStore.js`

## Key File Locations

**Entry Points:**
- `cmd/server/main.go`: Server initialization and startup
- `web/src/main.jsx`: React app entry point
- `web/src/App.jsx`: React router and layout

**Configuration:**
- `go.mod`: Go module dependencies
- `web/package.json`: Frontend dependencies
- `web/vite.config.js`: Vite build configuration
- `.env.example`: Environment variable template

**Core Logic:**
- `internal/warwick/classroom_client.go`: Warwick API proxy (1231 lines)
- `internal/domain/classroom.go`: Course/Session/Student models (168 lines)
- `internal/api/teacher_handlers.go`: Teacher API handlers (810 lines)

**Testing:**
- `internal/warwick/*_test.go`: Warwick client tests
- `internal/db/*_test.go`: Repository tests
- `web/src/__tests__/`: Frontend component tests

## Naming Conventions

**Files:**
- Go: snake_case.go (e.g., `classroom_client.go`)
- React: PascalCase.jsx (e.g., `CourseCard.jsx`)
- Tests: `*_test.go` (Go), `*.test.js` or `*.test.jsx` (React)

**Directories:**
- Go packages: snake_case (e.g., `session_checkin_repository.go`)
- React components: PascalCase directory names

**Variables/Functions:**
- Go: camelCase (e.g., `GetCourses`, `CourseSummary`)
- React: camelCase for functions/variables, PascalCase for components

## Where to Add New Code

**New API Endpoint:**
- Route: `internal/api/routes.go`
- Handler: `internal/api/teacher_handlers.go` (for teacher endpoints)

**New Domain Model:**
- File: `internal/domain/classroom.go` (for course-related) or create new file

**New Database Table:**
- Migration: `internal/db/migrations/` (increment migration number)
- Repository: `internal/db/` (create new repository file)

**New React Page:**
- Component: `web/src/pages/`
- Route: `web/src/App.jsx`

**New React Hook:**
- File: `web/src/hooks/`

**New Zustand Store:**
- File: `web/src/store/`

## Special Directories

**internal/db/migrations:**
- Purpose: Database schema migrations
- Generated: Manually created
- Committed: Yes

**web/dist:**
- Purpose: Built frontend assets for production
- Generated: Yes (by `npm run build`)
- Committed: No (in .gitignore)

**target/:**
- Purpose: Go build artifacts
- Generated: Yes (by `go build`)
- Committed: No

---

*Structure analysis: 2026-06-04*
