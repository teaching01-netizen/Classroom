# External Integrations

**Analysis Date:** 2026-06-02

## APIs & External Services

**Warwick Humantix (External Attendance Platform):**
- Purpose: Primary data source — courses, sessions, student check-ins, QR codes
- Base URL: `https://warwick.humantix.cloud`
- SDK/Client: Custom Go client (`internal/warwick/classroom_client.go`, `internal/warwick/client.go`)
- Auth: ASP.NET_SessionId cookie obtained via email/password form login to `/admin/`
- Protocol: DataTables server-side protocol — form-urlencoded POST requests
- Rate limiting: Server-side 2 req/s (configurable `rate.Limiter` at `cmd/server/main.go:100`)
- Endpoints consumed:
  - `POST /admin/api/ClassAttendanceSearch` — list all courses
  - `POST /admin/api/ClassAttendanceDetailSearch` — course sessions (by CouseID)
  - `POST /admin/api/ClassAttendanceStudentCheckInSearch` — session student check-ins (by CourseCampaignID)
  - `POST /admin/ClassAttendance/ToggleCheckin` — toggle student check-in
  - `POST /admin/ClassAttendance/GetQRCode` — fetch QR code for room
- Implementation: `internal/warwick/classroom_client.go` (1047 lines), `internal/warwick/client.go` (176 lines)

## Data Storage

**Databases:**
- PostgreSQL 16 (via pgxpool)
  - Connection: `DATABASE_URL` env var
  - Client: `pgxpool.Pool` (pgx v5.9.2, SimpleProtocol mode for Supabase pooler compat)
  - Connection pool: 5-25 connections, 30min max lifetime, 5min idle timeout (`internal/db/db.go:28-31`)
  - Migrations: Embedded via `go:embed`, runner via `golang-migrate/v4`

- **Tables:**
  - `rooms` — QR check-in room state (room_id, class_id, name, status enum, qr_url, timestamps)
  - `teacher_favourites` — Pinned courses (course_id PK, created_at)
  - `session_checkins` — Student check-in state cache (session_id + student_id composite PK, checked_in, toggled_at, refreshed_at, session_date)

**File Storage:**
- Local filesystem only (no S3/blob storage)

**Caching:**
- In-memory TTL cache (`internal/cache/cache.go`) — key-value with stale-read support
  - Used for: courses list (30s TTL), course details (30s TTL), session details (10s TTL), attendance reports (30s TTL)
  - Shared between ClassroomClient, QRClient, and DataRefresher
  - No distributed cache (Redis/Memcached)

## Authentication & Identity

**Auth Provider:**
- None (the Go server itself has no user authentication)
- Warwick session auth: Email/password login to Humantix admin panel
  - Credentials: `WARWICK_EMAIL` / `WARWICK_PASSWORD` env vars
  - Session management: Pool-based with per-session independent cookies
  - Auth conflict detection: If session fails when < 2min old → human admin kicked us → exponential backoff
  - Session TTL: ~55 minutes (staggered refresh)

## Monitoring & Observability

**Error Tracking:**
- None (no Sentry, Datadog, etc.)

**Logs:**
- `log/slog` with JSON handler at Debug level → stdout
- Structured logging with key-value pairs (e.g., `slog.Warn("cache_refresh_failed", "error", err)`)

## CI/CD & Deployment

**Hosting:**
- Railway (via `railway.json`)
- Docker (multi-stage build: Node → Go → Alpine)

**CI Pipeline:**
- None detected (no `.github/workflows/`, no CI config files)

## Environment Configuration

**Required env vars:**
- `DATABASE_URL` — PostgreSQL connection string (server exits if missing)
- `WARWICK_EMAIL` — Warwick admin login email
- `WARWICK_PASSWORD` — Warwick admin login password

**Optional env vars:**
- `PORT` / `SERVER_ADDR` — Listen address (default: :3000)
- `WARWICK_CACHE_INTERVAL` — Background refresh interval (default: 30s)
- `WARWICK_QR_SESSIONS` / `WARWICK_TEACHER_SESSIONS` / `WARWICK_INTERACTIVE_SESSIONS` — Pool tier sizing
- `WARWICK_CONNS_PER_HOST` — HTTP transport limit (default: 50)
- `WARWICK_MAX_CONCURRENT_WS` — WebSocket limit (default: 500)
- `CORS_ORIGIN` — Allowed CORS origin
- `VITE_WS_URL` — Frontend WebSocket URL

**Secrets location:**
- `.env` file (gitignored, local development)
- Railway environment variables (production)
- `docker-compose.yml` contains hardcoded dev DB credentials (`qruser`/`qrpassword`)

## Webhooks & Callbacks

**Incoming:**
- None

**Outgoing:**
- None (polling-based architecture — no webhooks to Warwick)

---

*Integration audit: 2026-06-02*
