# Check-in QR Command Center

Go and React command center for Warwick HumanTix attendance, QR rooms, teacher
views, and derived attendance reporting.

## Runtime model

Warwick remains the source of truth. Teacher reads can be served from typed,
eventually consistent PostgreSQL snapshots when `SNAPSHOT_READS_ENABLED=true`.
The scraper validates each target on an adaptive schedule and keeps the last
good value available only within that target's maximum serve age.

- Freshness is measured from the last successful validation, not the age of the
  content version.
- A changed canonical value creates a new version. An unchanged response or
  complete-representation HTTP `304` advances validation freshness without
  creating a version.
- PostgreSQL is the only durable snapshot, lease, fencing, host-admission, and
  version store. Raw HTTP responses, HTML, cookies, credentials, auth headers,
  screenshots, and avatar bytes are never persisted.
- Warwick request rate and concurrency are enforced cluster-wide with short
  PostgreSQL transactions. Network I/O never holds a host-state row lock.
- WebSocket events announce committed version metadata only. Clients refetch
  payloads over REST and ignore duplicate or out-of-order versions; polling and
  focus refresh remain repair paths.
- Check-in writes go to Warwick first, then trigger bounded snapshot
  reconciliation. Snapshot JSON is never patched as a write shortcut.
- QR rooms retain their existing `RoomManager` worker and Warwick TTL ownership.
- Attendance reports and the absence dashboard are derived from current
  snapshots and are not stored as report replicas.
- API responses remain `no-store`; application snapshots are a server-side
  consistency mechanism, not an HTTP/browser cache.

The historical live-only teacher-read design is retained as migration history
and is superseded by [ADR 009](docs/adr/009-postgresql-snapshot-scraper.md).

## Requirements

- Go 1.26.5
- Node.js 20 and npm
- PostgreSQL 16 for production
- Warwick credentials and user ID

Copy `.env.example` to `.env`, set `DATABASE_URL`, `WARWICK_EMAIL`,
`WARWICK_PASSWORD`, and `WARWICK_USER_ID`, then leave both rollout flags off for
an initial live-compatible boot:

```env
SCRAPER_ENABLED=false
SNAPSHOT_READS_ENABLED=false
```

Invalid scraper bounds or unsafe lease/fetch/grace relationships fail startup.
Snapshot reads also fail startup unless scraper writes remain enabled.

## Build and run

Docker Compose:

```bash
docker-compose -f docker-compose.prod.yml up -d --build
```

Native:

```bash
./build.sh
./target/release/qr-command-center-server
```

Development:

```bash
go run ./cmd/server
```

```bash
cd web
npm ci
npm run dev
```

## Scraper operation

Always-on deployments run the scheduler when `SCRAPER_ENABLED=true`.
Serverless deployments do not run a sleeping scheduler; they require an
external protected tick:

```bash
curl --fail-with-body \
  -X POST \
  -H "Authorization: Bearer $SCRAPER_TRIGGER_TOKEN" \
  https://app.example/api/internal/scraper/tick
```

Use a platform scheduler at a cadence no slower than the active-session policy.
The handler has a bounded 50-second context and a bounded tick limit. The token
is accepted only in the `Authorization` header, must live in deployment secrets,
and must never be placed in a URL or log.

The protected aggregate status endpoint exposes no target identifiers or
payloads:

```bash
curl --fail-with-body \
  -H "Authorization: Bearer $SCRAPER_TRIGGER_TOKEN" \
  https://app.example/api/internal/scraper/status
```

It reports target/current/due counts by bounded kind, oldest validation and
content ages, expired current snapshots, failures, leases, permit state, and
the current host rate/concurrency/pause.

In serverless snapshot mode the PostgreSQL notification listener remains
persistent for committed-version repair, while scheduled scraping is owned by
the protected tick. `SERVERLESS_IDLE_GRACE` still controls QR-room cleanup and
Warwick idle connection closure.

## Rollout and rollback

Deploy write-only first:

```env
SCRAPER_ENABLED=true
SNAPSHOT_READS_ENABLED=false
```

Do not enable reads until catalog/profile roots are current, active-course and
known-session coverage are 100%, expired current snapshots are zero, and the
observation window is free of auth storms, repeated `429`s, permit leaks, lease
loss, DB-pool exhaustion, and notification drops.

Then set:

```env
SNAPSHOT_READS_ENABLED=true
```

Rollback is immediate and does not require a schema rollback:

```env
SNAPSHOT_READS_ENABLED=false
```

This returns all teacher reads to live Warwick while safe scraper writes may
continue. Do not drop migration 009 data as part of operational rollback.
Detailed gates, queries, diagnostics, and rollback checks are in the
[snapshot scraper runbook](docs/snapshot-scraper-runbook.md).

## Verification

```bash
go test ./... -count=1
go test -race ./internal/scraper ./internal/db ./internal/service \
  ./internal/api ./internal/warwick -count=1
go vet ./...
./scripts/verify-no-upstream-cache.sh
node --test scripts/check-snapshot-rollout-readiness.test.mjs
```

Database integration tests require a disposable database:

```bash
TEST_DATABASE_URL='postgres://user:pass@localhost/snapshot_test?sslmode=disable' \
  go test ./internal/db ./internal/integration -count=1 -v
```

Frontend:

```bash
cd web
npm ci
npm test -- --run
npm run lint
npm run build
```

Opt-in release diagnostics are documented in the runbook. Never point tests or
diagnostics at production.
