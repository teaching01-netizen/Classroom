# Technology Stack

**Analysis Date:** 2026-06-04

## Languages

**Primary:**
- Go 1.21+ - Backend server, API handlers, Warwick client
- JavaScript (JSX) - Frontend React application

**Secondary:**
- SQL - Database migrations
- CSS - Styling tokens

## Runtime

**Environment:**
- Node.js (for frontend build)
- Go runtime (for backend)

**Package Manager:**
- Go modules (`go.mod`, `go.sum`)
- npm (`web/package-lock.json`)
- Lockfile: Present for both

## Frameworks

**Backend:**
- Chi v5 - HTTP router and middleware
- pgx/v5 - PostgreSQL driver
- godotenv - Environment variable loading

**Frontend:**
- React 18 - UI library
- Vite - Build tool and dev server
- Zustand - State management
- React Router v6 - Client-side routing

**Testing:**
- Go testing package - Backend tests
- Vitest or Jest - Frontend tests (based on test files)

## Key Dependencies

**Critical:**
- `github.com/go-chi/chi/v5` - HTTP routing
- `github.com/jackc/pgx/v5` - PostgreSQL client
- `github.com/prometheus/client_golang` - Metrics
- `golang.org/x/time/rate` - Rate limiting
- `golang.org/x/sync/singleflight` - Request deduplication

**Frontend:**
- `react`, `react-dom` - UI framework
- `react-router-dom` - Routing
- `zustand` - State management
- `vite` - Build tool

## Configuration

**Environment:**
- `.env` file for local development (not committed)
- `.env.example` for documentation
- Key configs: `DATABASE_URL`, `WARWICK_EMAIL`, `WARWICK_PASSWORD`, `WARWICK_USER_ID`, `PORT`, `CORS_ORIGIN`

**Build:**
- `web/vite.config.js` - Frontend build configuration
- `go.mod` - Go module configuration
- `Dockerfile` - Container build
- `docker-compose.yml` - Local development services

## Platform Requirements

**Development:**
- Go 1.21+
- Node.js 18+
- PostgreSQL 14+
- Network access to `warwick.humantix.cloud`

**Production:**
- Docker container
- PostgreSQL database
- Environment variables configured
- Network access to Warwick API

---

*Stack analysis: 2026-06-04*
