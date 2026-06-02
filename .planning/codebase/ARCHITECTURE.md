<!-- refreshed: 2026-06-02 -->
# Architecture

**Analysis Date:** 2026-06-02

## System Overview

```text
┌─────────────────────────────────────────────────────────────┐
│                      React SPA (Vite)                        │
│  `web/src/`                                                  │
│  Pages: CourseDashboard, SessionList, CheckinDetail,         │
│         CourseAttendance                                     │
│  State: Zustand stores (courseStore, sessionStore,           │
│         roomStore, pinnedCoursesStore)                        │
└────────┬──────────────────────────────────┬──────────────────┘
         │ HTTP/REST                        │ WebSocket
         ▼                                  ▼
┌─────────────────────────────────────────────────────────────┐
│                  Go HTTP Server (chi router)                 │
│  `cmd/server/main.go`  →  `internal/api/routes.go`          │
│                                                                     │
│  /api/rooms/*        Room CRUD + QR lifecycle                     │
│  /api/teacher/*      Course/session/checkin/report + favourites   │
│  /ws                 WebSocket (room state push)                  │
│  /*                  SPA fallback (serves web/dist/)              │
└───────┬──────────┬───────────┬──────────┬─────────────────────────┘
        │          │           │          │
        ▼          ▼           ▼          ▼
  ┌──────────┐ ┌────────┐ ┌────────┐ ┌──────────────┐
  │ RoomMgr  │ │Warwick │ │ Favs   │ │ DataRefresher│
  │ Service  │ │Client  │ │ Repo   │ │ (bg cache)   │
  └────┬─────┘ └───┬────┘ └───┬────┘ └──────┬───────┘
       │           │          │              │
       ▼           ▼          ▼              ▼
┌─────────────────────────────────────────────────────────────┐
│                    PostgreSQL (pgxpool)                       │
│  Tables: rooms, teacher_favourites, session_checkins          │
│  Migrations: embedded via go:embed + golang-migrate           │
└─────────────────────────────────────────────────────────────┘
        │
        ▼
┌─────────────────────────────────────────────────────────────┐
│              Warwick External API (Humantix)                  │
│  Base URL: https://warwick.humantix.cloud                     │
│  Auth: ASP.NET_SessionId cookie (email/password login)        │
│  Protocol: DataTables server-side, form-urlencoded POST       │
│  Endpoints:                                                   │
│    /admin/api/ClassAttendanceSearch (courses)                 │
│    /admin/api/ClassAttendanceDetailSearch (sessions)          │
│    /admin/api/ClassAttendanceStudentCheckInSearch (students)  │
│    /admin/ClassAttendance/ToggleCheckin (toggle)              │
│    /admin/ClassAttendance/GetQRCode (QR codes)                │
└─────────────────────────────────────────────────────────────┘
```

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| **main.go** | Server bootstrap, DI wiring, graceful shutdown | `cmd/server/main.go` |
| **api/routes.go** | Chi router, all API route definitions, CORS, SPA fallback | `internal/api/routes.go` |
| **api/handlers.go** | Standardized JSON response envelope (`ApiResponse`) | `internal/api/handlers.go` |
| **api/teacher_handlers.go** | Handler functions for `/api/teacher/*` endpoints | `internal/api/teacher_handlers.go` |
| **api/favourite_handlers.go** | Handler functions for `/api/teacher/favourites` CRUD | `internal/api/favourite_handlers.go` |
| **api/websocket.go** | WebSocket handler, room state push to clients | `internal/api/websocket.go` |
| **service/room_manager.go** | Room lifecycle (create/start/stop/delete), QR worker loops, event pub/sub | `internal/service/room_manager.go` |
| **service/data_refresher.go** | Background cache warmer: periodic `GetCourses()` calls | `internal/service/data_refresher.go` |
| **warwick/classroom_client.go** | Warwick API client for courses, sessions, student check-ins, toggle | `internal/warwick/classroom_client.go` |
| **warwick/client.go** | Warwick QR code fetching client | `internal/warwick/client.go` |
| **warwick/session_pool.go** | Session pool with tier-based isolation (QR/Teacher/Interactive) | `internal/warwick/session_pool.go` |
| **warwick/datatable.go** | DataTables request/response encoding for Warwick API | `internal/warwick/datatable.go` |
| **warwick/report_client.go** | Attendance report computation (per-student aggregation) | `internal/warwick/report_client.go` |
| **domain/room.go** | Room domain model, status state machine, fetch error types | `internal/domain/room.go` |
| **domain/classroom.go** | Course/session/checkin domain models, status enums | `internal/domain/classroom.go` |
| **domain/client.go** | QrClient interface definition | `internal/domain/client.go` |
| **db/db.go** | PostgreSQL connection pool, embedded migration runner | `internal/db/db.go` |
| **db/repository.go** | RoomRepository interface + PgRoomRepository implementation | `internal/db/repository.go` |
| **db/favourite_repository.go** | FavouriteRepository interface + PG implementation | `internal/db/favourite_repository.go` |
| **db/session_checkin_repository.go** | SessionCheckinRepository interface + PG implementation | `internal/db/session_checkin_repository.go` |
| **cache/cache.go** | In-memory TTL cache with stale-read support | `internal/cache/cache.go` |
| **middleware/ratelimit.go** | Per-IP token-bucket rate limiter | `internal/middleware/ratelimit.go` |

## Pattern Overview

**Overall:** Monolithic Go server with embedded SPA, proxying Warwick external API

**Key Characteristics:**
- Go server acts as a **proxy/caching layer** between frontend and Warwick Humantix API
- Courses, sessions, and student data are **NOT stored in the local DB** — they are fetched live from Warwick and cached in-memory (30s TTL)
- Only rooms, favourites, and session check-ins are persisted to PostgreSQL
- Session pool provides **traffic-tier isolation** (QR polling vs teacher browsing vs interactive toggle)
- WebSocket pushes room state changes to all connected clients in real time
- Frontend is a React SPA served by the Go server (SPA fallback at `/*`)

## Layers

**Presentation Layer (Frontend):**
- Purpose: Teacher-facing dashboard for course management, session attendance, QR check-in
- Location: `web/src/`
- Contains: React components, Zustand stores, custom hooks
- Depends on: Backend API (`/api/*`), WebSocket (`/ws`)

**API Layer (Handlers):**
- Purpose: HTTP request handling, input validation, response formatting
- Location: `internal/api/`
- Contains: Chi router, handler functions, standardized JSON envelope
- Depends on: Service layer, Warwick client, DB repositories

**Service Layer:**
- Purpose: Business logic for room lifecycle and background data refresh
- Location: `internal/service/`
- Contains: RoomManager (room state machine + QR worker), DataRefresher (cache warmer)
- Depends on: Domain models, DB repositories, Warwick client

**Domain Layer:**
- Purpose: Domain models, status enums, error types, interfaces
- Location: `internal/domain/`
- Contains: Room, CourseSummary, SessionDetail, StudentCheckin, QrClient interface
- Depends on: Nothing (pure domain)

**Data Access Layer:**
- Purpose: Database queries, repository pattern
- Location: `internal/db/`
- Contains: RoomRepository, FavouriteRepository, SessionCheckinRepository
- Depends on: pgxpool, domain models

**External Client Layer:**
- Purpose: Warwick Humantix API integration, session management, caching
- Location: `internal/warwick/`
- Contains: ClassroomClient, WarwickQrClient, SessionPool, DataTables encoding
- Depends on: Domain models, cache, DB repositories

**Infrastructure:**
- Purpose: Caching, rate limiting, middleware
- Location: `internal/cache/`, `internal/middleware/`
- Contains: In-memory TTL cache, per-IP token-bucket rate limiter

## Data Flow

### Primary Request Path: GET /api/teacher/courses

1. Frontend calls `fetch('/api/teacher/courses')` (`web/src/hooks/useCourses.js:16`)
2. Teacher rate limiter middleware checks IP (`internal/api/routes.go:22`)
3. `getCoursesHandler` calls `ClassroomClient.GetCourses()` (`internal/api/teacher_handlers.go:23`)
4. Check in-memory cache for "courses" key — 30s TTL (`internal/warwick/classroom_client.go:121-124`)
5. If cache miss: acquire session from `SessionPool` (`internal/warwick/classroom_client.go:214`)
6. POST to Warwick `/admin/api/ClassAttendanceSearch` with DataTables body (`internal/warwick/classroom_client.go:274`)
7. Parse response, compute `CourseStatus` (upcoming/active/finished) from dates (`internal/warwick/classroom_client.go:291-319`)
8. **Enrich** courses: concurrently fetch `GetCourseDetail` for each non-finished course to populate `TotalSessions`/`CompletedSessions` (`internal/warwick/classroom_client.go:181-211`)
9. Cache enriched result (30s TTL), return to handler
10. Handler wraps in `{ success: true, data: { courses: [...] } }` envelope (`internal/api/teacher_handlers.go:40`)
11. Frontend receives, stores in Zustand `useCourseStore` (`web/src/hooks/useCourses.js:19`)

### Session Detail Flow: GET /api/teacher/courses/:courseId/sessions/:sessionId

1. Frontend calls `fetch('/api/teacher/courses/.../sessions/...')` (`web/src/hooks/useCheckins.js:23`)
2. `getSessionDetailHandler` → `ClassroomClient.GetSessionDetail()` (`internal/api/teacher_handlers.go:77-109`)
3. Cache check: `Get("session:<id>")` (`internal/warwick/classroom_client.go:533-534`)
4. **If stale + DB-backed**: Compare `DB.maxToggledAt` vs `cache.MaxToggledAt` (`internal/warwick/classroom_client.go:544-587`)
   - If DB is fresher → repopulate cache from DB, serve fresh data
   - If same → serve stale + async refresh
5. **If cold cache + DB**: Query `session_checkins` table (`internal/warwick/classroom_client.go:605-639`)
6. **If DB miss**: Acquire pool session, POST to Warwick `/admin/api/ClassAttendanceStudentCheckInSearch` (`internal/warwick/classroom_client.go:725-776`)
7. If DB-backed: persist fetched students to `session_checkins` table async (`internal/warwick/classroom_client.go:659-671`)
8. Return to frontend, stored in Zustand `useSessionStore`

### Toggle Check-In Flow: POST /api/teacher/courses/:courseId/sessions/:sessionId/toggle-checkin

1. Frontend calls `fetch(..., { method: 'POST', body: { student_id, checked } })` (`web/src/hooks/useCheckins.js:47`)
2. `toggleCheckinHandler` → `ClassroomClient.ToggleCheckin()` (`internal/api/teacher_handlers.go:112-158`)
3. Acquire session from pool (TierInteractive) (`internal/warwick/classroom_client.go:811`)
4. POST to Warwick `/admin/ClassAttendance/ToggleCheckin` (form-encoded) (`internal/warwick/classroom_client.go:860-886`)
5. On success: persist to DB via `checkinRepo.UpsertStudent()` (`internal/warwick/classroom_client.go:828-837`)
6. Invalidate caches: "course:<id>", "courses", "session:<id>" (`internal/warwick/classroom_client.go:839-843`)
7. Frontend optimistically updates local state via `updateStudentCheckin()` (`web/src/hooks/useCheckins.js:54`)

### Room QR Flow: WebSocket + polling

1. Frontend opens WebSocket → receives `FullStateSync` of all rooms (`internal/api/websocket.go:41-51`)
2. `CheckinDetail` page auto-starts a room for the session ID (`web/src/pages/CheckinDetail.jsx:36-131`)
3. Creates room via `POST /api/rooms/from-session`, starts worker via `POST /api/rooms/{id}/start`
4. `RoomManager.runRoomWorker` goroutine polls Warwick for QR codes (`internal/service/room_manager.go:260-501`)
5. Room state changes pushed to all WebSocket subscribers via fan-out loop (`internal/service/room_manager.go:78-92`)

### Background Cache Warmup

1. `DataRefresher.Run()` calls `GetCourses()` on interval (default 30s) (`internal/service/data_refresher.go:32-47`)
2. Runs in background goroutine, warming shared cache for all frontend requests
3. On startup: `WarmOnce()` pre-fetches courses + active course details (`cmd/server/main.go:105-109`)

**State Management:**
- **Frontend**: Zustand stores (`useCourseStore`, `useSessionStore`, `useRoomStore`, `usePinnedCoursesStore`)
- **Backend**: In-memory cache (`cache.Cache`) + RoomManager in-memory map + PostgreSQL for persistence
- **Real-time**: WebSocket pushes room state changes; polling (10s) for session data

## Key Abstractions

**SessionPool:**
- Purpose: Isolate Warwick sessions across traffic tiers to prevent head-of-line blocking
- Examples: `internal/warwick/session_pool.go`
- Pattern: Pool with acquire/release semantics, exponential backoff on auth conflicts, per-session independent HTTP clients

**ClassroomClient:**
- Purpose: Unified proxy to Warwick API with multi-layer caching (memory → DB → Warwick)
- Examples: `internal/warwick/classroom_client.go`
- Pattern: Stale-while-revalidate with async background refresh, singleflight deduplication

**RoomManager:**
- Purpose: Room lifecycle management with goroutine-per-room workers
- Examples: `internal/service/room_manager.go`
- Pattern: Pub/sub event system with fan-out, rate-limited event emission (5s min interval)

**Repository Pattern:**
- Purpose: Abstract DB access behind interfaces for testability
- Examples: `internal/db/repository.go`, `internal/db/favourite_repository.go`, `internal/db/session_checkin_repository.go`
- Pattern: Interface + PG implementation, pgxpool for connection management

## Entry Points

**Server Entry:**
- Location: `cmd/server/main.go`
- Triggers: `go run ./cmd/server` or `./qr-command-center-server` (binary)
- Responsibilities: Bootstrap all dependencies, wire DI, start HTTP server, run background refresher

**Frontend Entry:**
- Location: `web/src/main.jsx`
- Triggers: Browser loads SPA
- Responsibilities: Mount React app with BrowserRouter

## Architectural Constraints

- **Threading**: Go's goroutine model; each room has its own worker goroutine; session pool uses mutex + sync.Cond for blocking acquire
- **Global state**: Package-level rate limiters in `internal/api/routes.go` (teacherLimiter, toggleLimiter, roomLimiter); session pool is a singleton
- **No circular imports**: Clean dependency flow: domain → db → service → api
- **ASP.NET Session locking**: Warwick uses ASP.NET sessions which lock on the server side; the session pool isolates concurrent requests into separate session IDs to avoid this
- **Session conflict detection**: If a human admin logs in, the pool detects the "kick" (session < 2 min old + login failure) and backs off exponentially to avoid ping-ponging

## Anti-Patterns

### Direct Warwick API Calls Without Pool

**What happens:** Code paths that don't use the session pool fall back to a single WarwickAuth session
**Why it's wrong:** ASP.NET session locking causes head-of-line blocking under concurrent load
**Do this instead:** Always use `NewClassroomClientFromPool` (the current production constructor) — the single-auth constructors exist for backward compatibility only

### Hardcoded UserID in API Calls

**What happens:** The `fetchCourses()` function sends a hardcoded `UserID` of `f21992ca-e6d2-424d-a188-90e37018ab38` (`internal/warwick/classroom_client.go:272`)
**Why it's wrong:** This is a specific user's ID baked into the code — likely the original developer's or a service account. Any future multi-tenant or user-specific filtering would be broken.
**Do this instead:** Make UserID configurable via env var or derive from the authenticated user

### Session Date Fallback to `time.Now()`

**What happens:** When persisting session check-ins to DB, the session date is always `time.Now()` (`internal/warwick/classroom_client.go:662`)
**Why it's wrong:** Sessions may occur on past dates; the DB `session_date` column will be incorrect for historical data
**Do this instead:** Extract session date from CourseSummary or session metadata when available

### `FetchError` Type Assertions Use `errors.Is` Instead of `errors.As`

**What happens:** Handlers check `errors.Is(err, domain.ErrAuthExpired)` which compares pointer identity (`internal/api/teacher_handlers.go:25`)
**Why it's wrong:** This works only because `ErrAuthExpired` is a package-level sentinel pointer. If someone creates `&FetchError{Kind: ErrKindAuthExpired}` it would fail the `Is` check.
**Do this instead:** Use `errors.As` with a `*FetchError` variable for robust type matching (the pattern is partially used elsewhere in the codebase)

## Error Handling

**Strategy:** Sentinel error values + domain error types for Warwick errors; HTTP status mapping in handlers

**Patterns:**
- `domain.ErrAuthExpired`, `domain.ErrRateLimited`, `domain.ErrAuthConflict`, `domain.ErrPoolExhausted` — sentinel errors checked in handlers (`internal/api/teacher_handlers.go:25-36`)
- `domain.FetchError` with `Kind` enum for structured error classification (`internal/domain/room.go:81-118`)
- Handler layer maps domain errors to HTTP status codes (401, 503, 500)
- `api.ApiResponse` envelope wraps all responses with `{ success, data, error }` (`internal/api/handlers.go:8-12`)
- Panic recovery in room worker goroutines and data refresher (`internal/service/room_manager.go:261-265`, `internal/service/data_refresher.go:50-57`)

## Cross-Cutting Concerns

**Logging:** `log/slog` with JSON handler at Debug level (`cmd/server/main.go:27-29`)
**Validation:** Minimal — required field checks in handlers (e.g., `courseId != ""`), no schema validation library
**Authentication:** None — the Go server itself has no auth; it authenticates to Warwick via email/password (`WARWICK_EMAIL`/`WARWICK_PASSWORD` env vars)
**Rate Limiting:** Per-IP token-bucket rate limiters on teacher (5 req/s) and toggle (2 req/s) routes; Warwick-side rate limiting at 2 req/s for live session fetches

---

*Architecture analysis: 2026-06-02*
