# External Integrations

**Analysis Date:** 2026-06-04

## APIs & External Services

**Warwick HumanTix:**
- Service: External attendance management system
- Base URL: `https://warwick.humantix.cloud`
- Endpoints Used:
  - `POST /admin/api/ClassAttendanceSearch` - List courses
  - `POST /admin/api/ClassAttendanceDetailSearch` - Course sessions
  - `POST /admin/api/ClassAttendanceStudentCheckInSearch` - Session students
  - `POST /admin/ClassAttendance/ToggleCheckin` - Toggle student check-in
- SDK/Client: Custom implementation (`internal/warwick/classroom_client.go`)
- Auth: Session cookies (`ASP.NET_SessionId`)
- Session Management: Pool-based with traffic tiers (`internal/warwick/session_pool.go`)

## Data Storage

**Databases:**
- PostgreSQL
  - Connection: `DATABASE_URL` environment variable
  - Client: pgx/v5 (`github.com/jackc/pgx/v5`)
  - Connection Pool: `pgxpool` for concurrent access
  - Migrations: `internal/db/migrations/` (6 migrations)

**Tables:**
- `rooms` - QR code rooms
- `session_checkins` - Student check-ins per session
- `attendance_reports` - Cached computed reports (JSONB)
- `saved_dashboard_views` - Dashboard filter configurations
- `teacher_favourites` - Pinned courses

**File Storage:**
- Local filesystem only (no cloud storage)

**Caching:**
- In-memory TTL cache (`internal/cache/cache.go`)
- Cache keys: `courses`, `course:{id}`, `session:{id}`, `report:{id}`
- TTLs: 30s for courses/reports, 10s for sessions
- Stale-while-revalidate pattern for background refresh

## Authentication & Identity

**Auth Provider:**
- Warwick Session-based Authentication
- Implementation: Cookie-based sessions (`ASP.NET_SessionId`)
- Session Pool: Isolated sessions for different traffic tiers
  - QR tier: QR code generation
  - Teacher tier: Course/session data fetching
  - Interactive tier: Check-in toggles
  - Pre-warm tier: Background data refresh

**User Identification:**
- Warwick UserID: `WARWICK_USER_ID` environment variable
- Auto-detection: Extracts from Warwick admin page JavaScript (`internal/warwick/classroom_client.go:989-990`)

## Monitoring & Observability

**Error Tracking:**
- Structured JSON logging via `slog` (`cmd/server/main.go:28-30`)
- Log levels: Debug, Info, Warn, Error

**Metrics:**
- Prometheus metrics (`internal/metrics/metrics.go`)
- Metrics endpoint: `GET /metrics`
- Tracked: Request duration, cache hits, report computation time, queue depth

## CI/CD & Deployment

**Hosting:**
- Docker container
- Railway (based on `railway.json`)

**CI Pipeline:**
- Not detected in codebase

**Build Process:**
- Frontend: `npm run build` in `web/` directory
- Backend: `go build` in project root
- Docker: `Dockerfile` for containerization

## Environment Configuration

**Required env vars:**
- `DATABASE_URL` - PostgreSQL connection string
- `WARWICK_EMAIL` - Warwick login email
- `WARWICK_PASSWORD` - Warwick login password
- `WARWICK_USER_ID` - Warwick user ID (optional, auto-detected)
- `PORT` or `SERVER_ADDR` - Server listen port
- `CORS_ORIGIN` - Allowed CORS origin

**Optional env vars:**
- `WARWICK_QR_SESSIONS` - QR session pool size (default: 2)
- `WARWICK_TEACHER_SESSIONS` - Teacher session pool size (default: 2)
- `WARWICK_INTERACTIVE_SESSIONS` - Interactive session pool size (default: 2)
- `WARWICK_PREWARM_SESSIONS` - Pre-warm session pool size (default: 1)
- `WARWICK_CONNS_PER_HOST` - Max connections per host (default: 50)
- `WARWICK_CACHE_INTERVAL` - Cache refresh interval (default: 30s)
- `WARWICK_PREWARM_INTERVAL` - Pre-warm refresh interval (default: 20s)
- `WARWICK_MAX_CONCURRENT_WS` - Max WebSocket connections (default: 500)

**Secrets location:**
- Environment variables (not in code)
- `.env` file for local development (not committed)

## Webhooks & Callbacks

**Incoming:**
- None detected

**Outgoing:**
- Warwick API calls (as described above)

## Data Flow Patterns

**Course Data:**
1. Frontend requests courses via API
2. Backend fetches from Warwick API (with cache)
3. Response cached for 30s
4. Stale data served with async refresh

**Session Data:**
1. Frontend requests session students
2. Backend checks DB pre-warmed data first
3. Falls back to live Warwick API if DB empty
4. Data persisted to DB for future requests

**Attendance Reports:**
1. Frontend requests report for course
2. Backend computes report using DB/live data
3. Report cached for 30s
4. Report persisted to DB asynchronously (non-blocking)

---

*Integration audit: 2026-06-04*
