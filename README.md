# Check-in QR Command Center

## Quick Start

The easiest way to deploy is with Docker:

```bash
./deploy.sh
```

This will:
1. Build the Docker images
2. Start the application
3. Automatically use DATABASE_URL from your .env file
4. Make it available at http://localhost:3000

## Deployment Options

### Option 1: Docker Compose (Recommended)

```bash
docker-compose -f docker-compose.prod.yml up -d --build
```

### Option 2: Native Go Build

1. Build everything:
```bash
./build.sh
```

2. Make sure your `.env` file has the correct `DATABASE_URL`:
```env
DATABASE_URL=postgres://user:password@host:port/dbname
```

3. Run the application (it will automatically load from `.env`):
```bash
./target/release/qr-command-center-server
```

## Development

### Backend
```bash
go run ./cmd/server
```

### Frontend
```bash
cd web
npm run dev
```

## Scripts

- `build.sh`: Builds frontend and backend
- `deploy.sh`: Deploys using Docker Compose (or native if Docker not available)

## Railway Serverless

The service supports Railway Serverless (formerly App Sleeping) without leaving periodic Warwick or PostgreSQL traffic running while nobody is using the app.

Set these service variables in Railway:

```env
SERVERLESS_ENABLED=true
SERVERLESS_IDLE_GRACE=2m
```

When enabled:

- Teacher, room, and WebSocket requests activate the idle lease; Warwick-owned data is read live within the request path.
- Repeated business requests extend the activity deadline.
- No background Warwick data refreshers or detached stale-data jobs run; active QR rooms are safely persisted as stopped after the idle grace, Warwick keep-alive connections close, and PostgreSQL connections drain to zero.
- Health checks, Prometheus scrapes, and static files do not activate background work.
- A later business request starts a fresh active generation automatically.

Railway detects inactivity after ten minutes without outbound packets. The two-minute application grace leaves time for in-flight requests and database connections to finish draining. The first request after sleep can have cold-boot latency and Railway may return a first-request 502; this occurs before the application can respond.

`railway.json` enables Serverless and pins the deployment to one replica. Single-replica operation is required because QR room ownership is currently held in process memory. You can also inspect or change the platform toggle in **Service Settings → Deploy → Serverless**. See [Railway Serverless documentation](https://docs.railway.com/deployments/serverless).

To roll back to the previous always-on behavior:

```env
SERVERLESS_ENABLED=false
```

`SERVERLESS_IDLE_GRACE` accepts values from `30s` through `6m`. Invalid values fail startup instead of silently selecting an unsafe configuration. The six-minute ceiling preserves at least four minutes of Railway's ten-minute quiet budget for cleanup and connection draining.

## Data freshness contract

Warwick is the source of truth for courses, sessions, student profiles, check-ins, and attendance reports. Each admitted read fetches the current upstream value through the bounded Warwick session pool; live errors are returned without stale or PostgreSQL snapshot fallback. Reports are computed per request and are not persisted as upstream replicas.

PostgreSQL retains room state, teacher favourites, and saved dashboard views. API JSON responses send `Cache-Control: no-store, no-cache, must-revalidate, max-age=0`, `Pragma: no-cache`, and `Expires: 0`; frontend API calls use browser `cache: 'no-store'`.
