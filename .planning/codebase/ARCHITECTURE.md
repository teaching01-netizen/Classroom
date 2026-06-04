<!-- refreshed: 2026-06-04 -->
# Architecture

**Analysis Date:** 2026-06-04

## System Overview

```text
┌─────────────────────────────────────────────────────────────┐
│                      Frontend (React)                        │
├──────────────────┬──────────────────┬───────────────────────┤
│   CourseDashboard │ CourseAttendance │  AbsenceDashboard     │
│  `web/src/pages/` │ `web/src/pages/` │  `web/src/pages/`     │
└────────┬─────────┴────────┬─────────┴──────────┬────────────┘
         │                  │                     │
         ▼                  ▼                     ▼
┌─────────────────────────────────────────────────────────────┐
│                    API Layer (Go/Chi)                        │
│         `internal/api/routes.go` + handlers                 │
└─────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│                  Service Layer (Go)                          │
├──────────────────┬──────────────────┬───────────────────────┤
│  ClassroomClient │   RoomManager    │   ReportPersister     │
│ `internal/warwick/` │ `internal/service/` │ `internal/service/` │
└────────┬─────────┴────────┬─────────┴──────────┬────────────┘
         │                  │                     │
         ▼                  ▼                     ▼
┌─────────────────────────────────────────────────────────────┐
│              External: Warwick API + PostgreSQL              │
│         `warwick.humantix.cloud` + `DATABASE_URL`           │
└─────────────────────────────────────────────────────────────┘
```

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| CourseDashboard | Display all courses with search/filter | `web/src/pages/CourseDashboard.jsx` |
| CourseAttendance | Show attendance report for a course | `web/src/pages/CourseAttendance.jsx` |
| AbsenceDashboard | Cross-course absence matrix | `web/src/pages/AbsenceDashboard.jsx` |
| SessionList | List sessions for a course | `web/src/pages/SessionList.jsx` |
| CheckinDetail | Show/check-in students for a session | `web/src/pages/CheckinDetail.jsx` |
| ClassroomClient | Proxy to Warwick API with caching | `internal/warwick/classroom_client.go` |
| RoomManager | Manage QR code rooms | `internal/service/room_manager.go` |
| ReportPersister | Async DB write for attendance reports | `internal/service/report_persister.go` |
| SessionPreWarmer | Background refresh of session data | `internal/service/session_prewarmer.go` |

## Pattern Overview

**Overall:** Proxy + Cache + Pre-warm Pattern

**Key Characteristics:**
- Frontend proxies all requests through backend to Warwick API
- Aggressive caching (30s for courses, 10s for sessions, 30s for reports)
- Background pre-warming of session data for fast report computation
- Stale-while-revalidate for cache freshness
- Singleflight deduplication for concurrent requests

## Layers

**Frontend (React/Vite):**
- Purpose: User interface for teachers to manage attendance
- Location: `web/src/`
- Contains: Pages, components, hooks, stores (Zustand)
- Depends on: Backend API
- Used by: Teachers (end users)

**API Layer (Go/Chi):**
- Purpose: HTTP routing and request handling
- Location: `internal/api/`
- Contains: Route definitions, HTTP handlers, WebSocket
- Depends on: Service layer, domain models
- Used by: Frontend

**Service Layer (Go):**
- Purpose: Business logic and external API integration
- Location: `internal/service/`, `internal/warwick/`
- Contains: ClassroomClient, RoomManager, ReportPersister, SessionPreWarmer
- Depends on: Domain models, database repositories
- Used by: API layer

**Data Layer (PostgreSQL):**
- Purpose: Persistent storage for rooms, check-ins, reports
- Location: `internal/db/`
- Contains: Repository implementations, migrations
- Depends on: PostgreSQL
- Used by: Service layer

## Data Flow

### Course List Request

1. Frontend calls `GET /api/teacher/courses` (`web/src/hooks/useCourses.js:16`)
2. Handler calls `ClassroomClient.GetCourses()` (`internal/api/teacher_handlers.go:27`)
3. Client checks cache, falls back to Warwick API (`internal/warwick/classroom_client.go:130-157`)
4. Response cached for 30s, returned to frontend

### Attendance Report Request

1. Frontend calls `GET /api/teacher/courses/{courseId}/attendance-report` (`web/src/hooks/useCourseAttendance.js:22`)
2. Handler fetches course detail for session list (`internal/api/teacher_handlers.go:335`)
3. Handler calls `ClassroomClient.GetCourseAttendanceReport()` (`internal/api/teacher_handlers.go:357`)
4. Client checks report cache, computes if miss (`internal/warwick/classroom_client.go:1117-1146`)
5. Report computed using DB pre-warmed data with live fallback (`internal/warwick/report_db_source.go:83-103`)
6. Report cached, enqueued for async DB persistence (`internal/warwick/classroom_client.go:1181-1188`)
7. Response returned to frontend

### Check-in Toggle

1. Frontend calls `POST /api/teacher/courses/{courseId}/sessions/{sessionId}/toggle-checkin`
2. Handler calls `ClassroomClient.ToggleCheckin()` (`internal/warwick/classroom_client.go:814-843`)
3. Client calls Warwick API (`internal/warwick/classroom_client.go:895-921`)
4. On success, persists to DB (`internal/warwick/classroom_client.go:863-871`)
5. Invalidates related caches (`internal/warwick/classroom_client.go:874-878`)
6. Marks attendance report stale (not hard invalidate) (`internal/api/teacher_handlers.go:161`)

**State Management:**
- Frontend: Zustand stores (`useCourseStore`, `useSessionStore`, `useRoomStore`)
- Backend: In-memory cache with TTL + PostgreSQL for persistence
- Session pool: In-memory pool of Warwick auth sessions

## Key Abstractions

**ClassroomClient:**
- Purpose: Proxy to Warwick admin panel API with caching and pool management
- Examples: `internal/warwick/classroom_client.go`
- Pattern: Adapter + Cache-Aside + Circuit Breaker

**SessionDataSource:**
- Purpose: Abstract session student data retrieval (DB vs live)
- Examples: `internal/warwick/report_db_source.go`
- Pattern: Strategy pattern with fallback

**CachedSession:**
- Purpose: Cross-instance cache coherence via DB
- Examples: `internal/warwick/classroom_client.go:36-40`
- Pattern: Cache with DB-backed staleness detection

## Entry Points

**Server:**
- Location: `cmd/server/main.go`
- Triggers: HTTP requests, WebSocket connections
- Responsibilities: Initialize dependencies, start HTTP server, background workers

**Frontend:**
- Location: `web/src/main.jsx` → `web/src/App.jsx`
- Triggers: Browser navigation
- Responsibilities: Route handling, API calls, UI rendering

## Architectural Constraints

- **Threading:** Go goroutines for async operations; React single-threaded
- **Global state:** Room manager holds all rooms in memory (`internal/service/room_manager.go`)
- **Circular imports:** None detected
- **Rate limiting:** IP-based (5 req/s courses, 2 req/s toggles, 10 req/s rooms)

## Anti-Patterns

### Session Date Assignment

**What happens:** Session date is set to `time.Now()` when persisting checkins
**Why it's wrong:** Actual session date may differ from current time
**Do this instead:** Extract session date from Warwick API or course detail

### Student Identity by Name

**What happens:** Students identified by name across courses
**Why it's wrong:** Name collisions cause incorrect aggregation
**Do this instead:** Use a universal student identifier if available from Warwick

## Error Handling

**Strategy:** Structured error types with retry logic

**Patterns:**
- `domain.FetchError` with typed error kinds (`internal/domain/room.go:81-103`)
- Auth expiration triggers session refresh (`internal/warwick/classroom_client.go:175-183`)
- Pool exhaustion returns 503 Service Unavailable
- Rate limiting returns 429 Too Many Requests

## Cross-Cutting Concerns

**Logging:** Structured JSON logging via `slog` (`cmd/server/main.go:28-30`)
**Validation:** Request body validation in handlers, URL param validation
**Authentication:** Warwick session cookies (ASP.NET_SessionId)
**Metrics:** Prometheus metrics for request duration, cache hits, report computation (`internal/metrics/`)

---

*Architecture analysis: 2026-06-04*
