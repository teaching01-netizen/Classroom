# Technology Stack

**Analysis Date:** 2026-06-02

## Languages

**Primary:**
- Go 1.26.3 - Backend server, API handlers, Warwick API client, room management, session pool
- JavaScript (JSX/ESM) - React frontend SPA

**Secondary:**
- SQL - PostgreSQL migrations (4 migration sets)
- CSS - Design tokens, inline styles with CSS variable fallbacks

## Runtime

**Backend Environment:**
- Go 1.26.3 (alpine-based Docker image)
- Single binary deployment (`qr-command-center-server`)

**Frontend Environment:**
- Node.js 20 (alpine, for build only)
- Browser runtime (React 18 SPA)

**Package Manager:**
- Go modules (`go.mod` / `go.sum`)
- npm (`web/package.json` / `web/package-lock.json`)

## Frameworks

**Backend:**
- `go-chi/chi/v5` v5.3.0 - HTTP router and middleware
- `pgxpool/v5` v5.9.2 - PostgreSQL connection pool (pgx driver)
- `golang-migrate/migrate/v4` v4.19.1 - Database migration runner
- `nhooyr.io/websocket` v1.8.17 - WebSocket implementation
- `golang.org/x/time` v0.15.0 - Rate limiter (`rate.Limiter`)
- `golang.org/x/sync` v0.18.0 - Singleflight deduplication

**Frontend:**
- React 18.2.0 - UI framework
- React Router DOM 7.16.0 - Client-side routing
- Zustand 4.5.0 - State management (4 stores)
- Vite 5.0.8 - Build tool and dev server

**Testing:**
- Go: `testing` stdlib + `stretchr/testify` v1.11.1 (assert, require)
- Frontend: Vitest 1.1.0 + @testing-library/react 16.3.2 + jsdom 29.1.1

## Key Dependencies

**Critical:**
- `jackc/pgx/v5` v5.9.2 - PostgreSQL driver (required for Supabase pooler compatibility — uses SimpleProtocol mode)
- `go-chi/chi/v5` v5.3.0 - HTTP routing (all API endpoints defined in `internal/api/routes.go`)
- `nhooyr.io/websocket` v1.8.17 - WebSocket for real-time room state push
- `joho/godotenv` v1.5.1 - Loads `.env` file at startup

**Infrastructure:**
- `golang-migrate/migrate/v4` v4.19.1 - Schema migrations (embedded via `go:embed`)
- `golang.org/x/time/rate` - Warwick-side rate limiting (2 req/s for session fetches)
- `golang.org/x/sync/singleflight` - Deduplicates concurrent report computations
- `google/uuid` v1.6.0 - Room ID generation

## Configuration

**Environment Variables (from `.env.example`):**
- `DATABASE_URL` - PostgreSQL connection string (required, exits if missing)
- `WARWICK_EMAIL` / `WARWICK_PASSWORD` - Warwick Humantix login credentials
- `WARWICK_CACHE_INTERVAL` - Background refresh interval (default: 30s)
- `WARWICK_QR_SESSIONS` / `WARWICK_TEACHER_SESSIONS` / `WARWICK_INTERACTIVE_SESSIONS` - Pool tier sizing (default: 2 each)
- `WARWICK_CONNS_PER_HOST` - HTTP transport connection limit (default: 50)
- `WARWICK_MAX_CONCURRENT_WS` - WebSocket connection limit (default: 500)
- `PORT` / `SERVER_ADDR` - Listen address (default: :3000)
- `CORS_ORIGIN` - Allowed CORS origin (optional)
- `VITE_WS_URL` - Frontend WebSocket URL (optional, defaults to `/ws`)

**Build Configuration:**
- `go.mod`: Go module `qr-command-center`, Go 1.26.3
- `web/vite.config.js`: Vite build config with React plugin
- `web/package.json`: Frontend scripts (dev, build, test, lint)
- `Dockerfile`: Multi-stage build (Node → Go → Alpine)
- `railway.json`: Railway deployment config
- `docker-compose.yml`: Local PostgreSQL (postgres:16-alpine)

## Platform Requirements

**Development:**
- Go 1.26.3+
- Node.js 20+
- PostgreSQL 16 (local via docker-compose or remote via DATABASE_URL)
- Warwick Humantix account credentials

**Production:**
- Alpine Linux (Docker)
- PostgreSQL 16 (Railway Postgres plugin or external)
- Exposed port 3000

---

*Stack analysis: 2026-06-02*
