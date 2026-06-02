# Codebase Structure

**Analysis Date:** 2026-06-02

## Directory Layout

```
check in auto/
├── cmd/
│   └── server/
│       └── main.go              # Server entry point, DI wiring, graceful shutdown
├── internal/
│   ├── api/
│   │   ├── routes.go            # Chi router, all API routes, CORS, SPA fallback
│   │   ├── handlers.go          # Standardized ApiResponse JSON envelope
│   │   ├── teacher_handlers.go  # /api/teacher/* handler functions
│   │   ├── favourite_handlers.go # /api/teacher/favourites CRUD handlers
│   │   ├── websocket.go         # WebSocket handler, room state push
│   │   └── websocket_test.go    # WebSocket handler tests
│   ├── cache/
│   │   ├── cache.go             # In-memory TTL cache with stale-read support
│   │   └── cache_test.go        # Cache unit tests
│   ├── db/
│   │   ├── db.go                # pgxpool connection + embedded migration runner
│   │   ├── repository.go        # RoomRepository interface + PgRoomRepository
│   │   ├── favourite_repository.go  # FavouriteRepository interface + PG impl
│   │   ├── session_checkin_repository.go # SessionCheckinRepository interface + PG impl
│   │   ├── session_checkin_repository_test.go
│   │   └── migrations/
│   │       ├── 001_create_rooms_table.up.sql
│   │       ├── 001_create_rooms_table.down.sql
│   │       ├── 002_change_room_id_to_text.up.sql
│   │       ├── 002_change_room_id_to_text.down.sql
│   │       ├── 003_create_teacher_favourites.up.sql
│   │       ├── 003_create_teacher_favourites.down.sql
│   │       ├── 004_create_session_checkins.up.sql
│   │       └── 004_create_session_checkins.down.sql
│   ├── domain/
│   │   ├── room.go              # Room model, status state machine, FetchError types
│   │   ├── room_test.go         # Room domain tests
│   │   ├── classroom.go         # Course/Session/Student domain models
│   │   └── client.go            # QrClient interface
│   ├── middleware/
│   │   ├── ratelimit.go         # Per-IP token-bucket rate limiter
│   │   └── ratelimit_test.go    # Rate limiter tests
│   ├── service/
│   │   ├── room_manager.go      # Room lifecycle, QR worker loops, event pub/sub
│   │   └── data_refresher.go    # Background cache warmer
│   └── warwick/
│       ├── client.go            # Warwick QR code client
│       ├── client_test.go       # QR client tests
│       ├── classroom_client.go  # Warwick course/session/checkin API client (1047 lines)
│       ├── classroom_client_db_test.go  # DB-backed cache integration tests
│       ├── datatable.go         # DataTables request/response encoding
│       ├── report_client.go     # Attendance report computation
│       ├── report_client_test.go # Report tests
│       ├── session_pool.go      # Session pool with tier isolation
│       └── session_pool_test.go # Pool tests
├── web/
│   ├── package.json             # React + Zustand + Vite + React Router
│   ├── vite.config.js           # Vite build config
│   ├── index.html               # SPA entry HTML
│   ├── .env                     # Frontend env vars (VITE_WS_URL)
│   └── src/
│       ├── main.jsx             # React mount point
│       ├── App.jsx              # Router, NavBar, HomePage, PinnedCourseCard
│       ├── index.css            # Global styles
│       ├── styles/
│       │   └── tokens.css       # Design tokens (CSS variables)
│       ├── components/
│       │   ├── CourseCard.jsx    # Course card with pin/attendance display
│       │   ├── SessionTable.jsx  # Session list table
│       │   ├── AttendanceTable.jsx  # Per-student attendance grid
│       │   ├── AttendanceRow.jsx # Single attendance row
│       │   ├── StudentTable.jsx  # Student list with toggle
│       │   ├── StatsBar.jsx     # Stats display bar
│       │   ├── QRModal.jsx      # QR code modal overlay
│       │   ├── QRDisplay.jsx    # QR code display component
│       │   ├── RoomCard.jsx     # Room state card
│       │   ├── Pagination.jsx   # Pagination controls
│       │   ├── ErrorBoundary.jsx # React error boundary
│       │   ├── BackBreadcrumb.jsx # Navigation breadcrumb
│       │   ├── SessionTable.jsx  # (duplicate listing, same as above)
│       │   └── ...
│       ├── hooks/
│       │   ├── useCourses.js    # Fetch courses from /api/teacher/courses
│       │   ├── useSessions.js   # Fetch sessions from /api/teacher/courses/:id
│       │   ├── useCheckins.js   # Fetch + toggle students, polling, abort
│       │   ├── useCourseAttendance.js  # Fetch attendance report
│       │   ├── useWebSocket.js  # WebSocket connection + reconnect
│       │   ├── usePolling.js    # Generic polling hook
│       │   ├── useFocusRefetch.js  # Refetch on tab focus
│       │   └── useCountdown.js  # Countdown timer for QR expiry
│       ├── store/
│       │   ├── useCourseStore.js    # Zustand store for courses list
│       │   ├── useSessionStore.js   # Zustand store for session/student data
│       │   ├── useRoomStore.js      # Zustand store for room state (from WS)
│       │   └── usePinnedCoursesStore.js # Zustand store for favourites (API-backed)
│       └── pages/
│           ├── CourseDashboard.jsx  # /courses — all courses grid
│           ├── SessionList.jsx      # /courses/:id/sessions — session list
│           ├── CheckinDetail.jsx    # /courses/:id/sessions/:sid — student check-in + QR
│           └── CourseAttendance.jsx # /courses/:id/attendance — attendance report
├── .env.example               # Required env vars documentation
├── .env                       # Local env vars (gitignored)
├── go.mod                     # Go module definition
├── go.sum                     # Go dependency checksums
├── Dockerfile                 # Multi-stage build (Node → Go → Alpine)
├── docker-compose.yml         # Local PostgreSQL for dev
├── docker-compose.prod.yml    # Production compose
├── build.sh                   # Build script
├── deploy.sh                  # Deploy script
├── dev.sh                     # Dev startup script
├── railway.json               # Railway deployment config
├── README.md
├── REVIEW.md
└── .planning/
    └── codebase/
        ├── ARCHITECTURE.md    # This file
        └── STRUCTURE.md       # This file
```

## Directory Purposes

**`cmd/server/`:**
- Purpose: Application entry point
- Contains: `main.go` — wires all dependencies, starts HTTP server, runs graceful shutdown
- Key files: `cmd/server/main.go`

**`internal/api/`:**
- Purpose: HTTP layer — routing, handlers, middleware application
- Contains: Chi router config, handler functions, WebSocket handler, JSON response helpers
- Key files: `internal/api/routes.go` (route definitions), `internal/api/teacher_handlers.go` (course/session handlers)

**`internal/cache/`:**
- Purpose: Shared in-memory TTL cache used by Warwick clients
- Contains: Generic key-value cache with `Get`, `GetStale`, `Set`, `Invalidate`, `Size`
- Key files: `internal/cache/cache.go`

**`internal/db/`:**
- Purpose: Database access layer with repository pattern
- Contains: pgxpool setup, migration runner, three repository implementations
- Key files: `internal/db/db.go` (pool + migrations), `internal/db/repository.go` (rooms), `internal/db/session_checkin_repository.go` (check-ins)

**`internal/db/migrations/`:**
- Purpose: SQL migration files for schema evolution
- Contains: 4 migration pairs (up/down), embedded via `go:embed`
- Key files: `001_create_rooms_table.up.sql` through `004_create_session_checkins.up.sql`

**`internal/domain/`:**
- Purpose: Pure domain models with zero dependencies on infrastructure
- Contains: Room, CourseSummary, SessionDetail, StudentCheckin, status enums, error types, QrClient interface
- Key files: `internal/domain/room.go`, `internal/domain/classroom.go`, `internal/domain/client.go`

**`internal/middleware/`:**
- Purpose: HTTP middleware for cross-cutting concerns
- Contains: Per-IP token-bucket rate limiter with cleanup loop
- Key files: `internal/middleware/ratelimit.go`

**`internal/service/`:**
- Purpose: Business logic layer between API handlers and data access
- Contains: RoomManager (room lifecycle + event pub/sub), DataRefresher (background cache warmer)
- Key files: `internal/service/room_manager.go` (503 lines), `internal/service/data_refresher.go`

**`internal/warwick/`:**
- Purpose: External Warwick Humantix API integration
- Contains: HTTP clients, session pool, DataTables protocol encoding, report computation
- Key files: `internal/warwick/classroom_client.go` (1047 lines — largest file), `internal/warwick/session_pool.go` (476 lines)

**`web/src/`:**
- Purpose: React SPA frontend for teacher dashboard
- Contains: Pages, components, hooks, Zustand stores, styles
- Key files: `web/src/App.jsx` (router + HomePage), `web/src/hooks/useCourses.js`, `web/src/hooks/useCheckins.js`

**`web/src/hooks/`:**
- Purpose: Custom React hooks that encapsulate API fetching + state management
- Contains: One hook per data domain (courses, sessions, checkins, attendance, websocket, polling, focus-refetch)
- Key files: `web/src/hooks/useCourses.js`, `web/src/hooks/useCheckins.js`, `web/src/hooks/useWebSocket.js`

**`web/src/store/`:**
- Purpose: Zustand global state stores
- Contains: Four stores — courseStore (courses list), sessionStore (current session + students), roomStore (room state from WS), pinnedCoursesStore (favourites via API)
- Key files: `web/src/store/useCourseStore.js`, `web/src/store/useSessionStore.js`, `web/src/store/usePinnedCoursesStore.js`

## Key File Locations

**Entry Points:**
- `cmd/server/main.go`: Go server entry point
- `web/src/main.jsx`: React SPA entry point
- `web/index.html`: HTML shell

**Configuration:**
- `.env.example`: Required env vars (WARWICK_EMAIL, WARWICK_PASSWORD, DATABASE_URL, pool sizing)
- `go.mod`: Go module dependencies
- `web/package.json`: Frontend dependencies
- `web/vite.config.js`: Vite build configuration
- `docker-compose.yml`: Local PostgreSQL config
- `railway.json`: Railway deployment config

**Core Logic:**
- `internal/warwick/classroom_client.go`: All Warwick API interaction (courses, sessions, students, toggle, attendance report)
- `internal/warwick/session_pool.go`: Session pool with tier isolation and auth conflict detection
- `internal/service/room_manager.go`: Room lifecycle management with goroutine workers
- `internal/domain/classroom.go`: All course/session/student domain models

**Testing:**
- `internal/warwick/classroom_client_db_test.go`: DB-backed cache integration tests (559 lines, most thorough)
- `internal/warwick/report_client_test.go`: Report computation tests
- `internal/warwick/session_pool_test.go`: Pool behavior tests
- `internal/warwick/client_test.go`: QR client tests
- `internal/db/session_checkin_repository_test.go`: Repository tests
- `internal/cache/cache_test.go`: Cache tests
- `internal/middleware/ratelimit_test.go`: Rate limiter tests
- `web/src/__tests__/*.test.js`: Frontend hook tests

## Naming Conventions

**Files:**
- Go: `snake_case.go` (e.g., `session_checkin_repository.go`, `classroom_client.go`)
- Go tests: `snake_case_test.go` (co-located with source)
- Frontend: `camelCase.js` for hooks/stores (e.g., `useCourses.js`, `useCourseStore.js`), `PascalCase.jsx` for components/pages (e.g., `CourseCard.jsx`, `CourseDashboard.jsx`)
- SQL migrations: `NNN_description.up.sql` / `NNN_description.down.sql`

**Directories:**
- Go: `snake_case` (e.g., `db/migrations/`, `session_checkin_repository.go`)
- Frontend: `camelCase` for stores/hooks (e.g., `useCourseStore.js`), `PascalCase` for components/pages (e.g., `CourseCard.jsx`)

**Go packages:** Single-word lowercase (`api`, `cache`, `db`, `domain`, `middleware`, `service`, `warwick`)

## Where to Add New Code

**New API Endpoint:**
- Route definition: `internal/api/routes.go` — add to appropriate `r.Route()` block
- Handler function: `internal/api/teacher_handlers.go` (teacher endpoints) or `internal/api/handlers.go` (generic)
- Domain model: `internal/domain/classroom.go` if new data structures needed

**New Warwick API Integration:**
- Client method: `internal/warwick/classroom_client.go` — follow `fetchCourses()` / `fetchCourseDetail()` pattern
- DataTables types: `internal/warwick/datatable.go` — add response struct
- Domain model: `internal/domain/classroom.go` — add request/response types

**New Database Table:**
- Migration: `internal/db/migrations/005_<name>.up.sql` + `005_<name>.down.sql`
- Repository interface: New file `internal/db/<name>_repository.go`
- Repository implementation: Same file, PG implementation with pgxpool
- Update `db.go` if new setup needed (currently auto-discovers via embed)

**New Zustand Store:**
- File: `web/src/store/use<Name>Store.js` — follow `useCourseStore.js` pattern
- Consumed by: hooks in `web/src/hooks/` that call API and update store

**New React Page:**
- File: `web/src/pages/<Name>.jsx`
- Route: Add to `web/src/App.jsx` `<Routes>` block
- Hook: Create matching hook in `web/src/hooks/use<Name>.js`

**New React Component:**
- File: `web/src/components/<Name>.jsx`
- Follow existing pattern: functional component, inline styles with CSS variable fallbacks

## Special Directories

**`web/dist/`:**
- Purpose: Built frontend assets served by Go server SPA fallback
- Generated: Yes (by `vite build`)
- Committed: No (in `.gitignore`)

**`internal/db/migrations/`:**
- Purpose: Embedded SQL migration files
- Generated: No (manually written)
- Committed: Yes

**`.env`:**
- Purpose: Local environment variables
- Generated: No (manually created from `.env.example`)
- Committed: No (in `.gitignore`)

**`.planning/`:**
- Purpose: Codebase analysis documents for GSD workflow
- Generated: Yes (by `/gsd-map-codebase`)
- Committed: Yes

---

*Structure analysis: 2026-06-02*
