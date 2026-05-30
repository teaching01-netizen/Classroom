# Deployment Safety Audit

**Analysis Date:** 2026-05-29

**Project:** QR Command Center
**Deployment target:** Single-container via `docker-compose.prod.yml` on a VPS (no orchestrator)

---

## 1. Expand-Contract Migration

### Current state

Only two migrations exist:

| Migration | Up | Down | Risk |
|-----------|-----|------|------|
| `001_create_rooms_table.up.sql` | `CREATE TABLE IF NOT EXISTS rooms (room_id UUID PRIMARY KEY, ...)` | `DROP TABLE rooms; DROP TYPE room_status;` | Safe (idempotent, `IF NOT EXISTS`) |
| `002_change_room_id_to_text.up.sql` | `ALTER TABLE rooms ALTER COLUMN room_id TYPE TEXT;` | Delete non-UUID rows then cast `room_id::UUID` |

### Assessment: **Not expand-contract**

Migration `002` is a direct `ALTER COLUMN ... TYPE TEXT` — not an expand-contract pattern (add new column → dual-write → drop old).

**Mixed-version behavior:**

- **Old code reading new schema:** Safe. Go struct `Room.RoomID` is `string` (`internal/domain/room.go:111`). pgx scans TEXT into string without issue.
- **Old code writing new schema:** Safe. `uuid.New().String()` produces a UUID-format string that fits in TEXT.
- **New code reading old schema:** Safe. pgx scans UUID into string without issue.

**Locking:** `ALTER COLUMN ... TYPE` acquires an `ACCESS EXCLUSIVE` lock on `rooms` table — blocks all reads and writes for the duration of the migration. On a small table (<1000 rows) this is sub-second, but still a full-table lock.

### Fix

Adopt expand-contract for future migrations:

1. **Expand:** `ALTER TABLE rooms ADD COLUMN room_id_new TEXT;` — add new column alongside old
2. **Migrate data:** Backfill `room_id_new` in batches
3. **Dual-write:** Write to both columns for one deploy cycle
4. **Contract:** Drop `room_id` column, rename `room_id_new` to `room_id`
5. **Drop old code support:** Remove old column reads in next deploy

---

## 2. Rollback Safety

### Assessment: **Not safely rollbackable**

| Scenario | What happens | Data loss? |
|----------|-------------|------------|
| Deploy v2 → migrate to 002 → rollback to v1 (runs 002 down) | `DELETE FROM rooms WHERE room_id !~ UUID regex` removes any non-UUID room_ids. `ALTER COLUMN TYPE UUID USING room_id::UUID` fails if any non-UUID rows remain. | **Yes** — non-UUID rows silently deleted |
| Deploy v1 → migrate to 001 → rollback to v0 (runs 001 down) | `DROP TABLE rooms; DROP TYPE room_status;` | **Catastrophic** — all data gone |
| New code + old schema | Works (pgx scans UUID into string) | No |
| Old code + new schema | Works (pgx scans TEXT into string) | No |

**Worst-case rollback path:** If a deployment adds a new migration and is rolled back, the `Down` migration runs. Migration 001 down drops the entire `rooms` table. Migration 002 down silently destroys non-UUID data.

### Detectability

- Migration 002 down will silently delete rows without warning or confirmation
- No dry-run mode for migrations
- No backup taken before rollback

### Fix

1. **Never use destructive down migrations in production** — make down migrations a no-op or just mark the migration as irreversible
2. **Backup DB before any deploy** (`pg_dump` or Supabase snapshot)
3. **Test rollback in staging** with production-like data volume
4. **Consider using `migrate.Force(dirtyVersion)` as a safer alternative** — the code already has a dirty-state bypass at `internal/db/db.go:52-57`

---

## 3. Blue-Green / Canary

### Assessment: **None**

Deployment model is single-container (`docker-compose.prod.yml`):

```yaml
services:
  app:
    build: .
    container_name: qr-command-center-app-prod
    ports:
      - "3000:3000"
    restart: unless-stopped
```

No load balancer, no multiple replicas, no canary, no blue-green.

### Mixed-version scenario

**Impossible with current setup** — only one container runs at a time. If you manually ran two containers on different ports behind a reverse proxy:

- **Shared DB:** Both versions read/write the same `rooms` table. If migration 002 has run, old code reads/writes TEXT fine. But if a new migration adds a column the old code ignores, that's safe (backward-compatible). If new code expects a column that doesn't exist in old schema (forward-incompatible), it crashes.
- **Shared Warwick session:** `WarwickAuth` (`internal/warwick/auth.go:26-33`) holds an in-memory session. Each container independently authenticates. No cross-container session sharing — both would need valid credentials. This is safer than shared state.
- **No shared in-memory state:** `RoomManager` (`internal/service/room_manager.go:25-31`) is purely in-memory with rooms loaded from DB. Each container has its own view. Events are not cross-container — WebSocket clients connected to container A don't see changes made on container B.

### Fix

If blue-green is needed:
1. Put a reverse proxy (nginx/Caddy) in front with `upstream` pointing to two app instances
2. Add a `/healthz` endpoint returning 200
3. Use sticky sessions or broadcast events via DB (LISTEN/NOTIFY) for cross-instance WebSocket sync
4. **Trade-off:** WebSocket is inherently stateful — blue-green with mixed versions is harder than for stateless HTTP

---

## 4. Migration Lock Time

### Assessment: **Concurrent start vulnerability**

`internal/db/db.go:29-61` runs `m.Up()` on every startup:

```go
func RunMigrations(databaseURL string) error {
    // ...
    m, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
    // ...
    if err := m.Up(); err != nil && err != migrate.ErrNoChange { ... }
}
```

**golang-migrate** uses `pg_advisory_lock` for concurrency control. The first container to start acquires the lock and runs pending migrations. The second container waits on `pg_advisory_lock`.

**Problem:** There is **no startup timeout**. If migration 002 (`ALTER COLUMN ... TYPE TEXT`) blocks for X seconds due to the `ACCESS EXCLUSIVE` lock, the second container waits indefinitely until the first releases `.Up()`. If the first container finishes migrations and crashes during startup (e.g., Warwick auth fails at line 36), it never releases the migration lock cleanly.

**Lock scoping:** `pg_advisory_lock` is session-scoped. If the first container's migration connection closes (any reason), the lock is released. But a crash between `m.Up()` success and server start would leave the second container waiting on the lock until its connection times out — which could be minutes.

### Fix

1. **Add a startup timeout** around `RunMigrations` (e.g., 30s `context.WithTimeout`)
2. **Separate migration into an init container** or one-shot script (run once, not on every pod start)
3. **Use `migrate.NewWithDatabaseInstance` with `pgx`** instead of `sql.Open` + `lib/pq` — reduces connection count
4. **Run `m.Up()` with a retry/skip-if-already-running** pattern

---

## 5. Hot Reload / Zero-Downtime Deploy

### Assessment: **Not possible**

**Server lifecycle** (`internal/cmd/server/main.go:81-109`):

```go
srv := &http.Server{
    Addr:         addr,
    Handler:      router,
    ReadTimeout:  15 * time.Second,
    WriteTimeout: 15 * time.Second,
    IdleTimeout:  60 * time.Second,
}

// Graceful shutdown
shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := srv.Shutdown(shutdownCtx); err != nil { ... }
```

Shutdown is graceful (10s timeout, drains HTTP). But:

### WebSocket disconnection

`nhooyr.io/websocket` (`internal/api/websocket.go`) connections are **entirely in-memory**:

```go
type wsClient struct {
    conn *websocket.Conn
    send chan []byte
}
```

- Server restart kills all WebSocket connections
- Clients must reconnect and receive `FullStateSync`
- During reconnect window, clients miss events — no event replay mechanism exists
- `RoomManager.Subscribe()` creates per-connection channels (`internal/service/room_manager.go:42-55`) — these are ephemeral

### User-visible impact

- **WebSocket clients (walk-in display):** QR code displays briefly disconnect, then re-render on reconnect. 
- **HTTP API:** Stateless — no visible impact during shutdown. New requests fail with `connection refused` for ~1-2 seconds during restart. `restart: unless-stopped` ensures container comes back up.

### Zero-downtime strategy

| Approach | Effort | Wins |
|----------|--------|------|
| Add reverse proxy with graceful reload (nginx reload / Caddy) | Low | Zero-downtime HTTP; WS reconnects still happen but are faster |
| Persist room events to DB with sequence numbers, replay on reconnect | Medium | True WS resilience |
| SIGTERM → drain connections for 10s → shutdown | Already done | Best-effort graceful shutdown |

### Fix

1. **Front with nginx/Caddy** configured for health-checked upstreams and `proxy_pass` with `proxy_http_version 1.1`
2. **Add `/healthz` endpoint** returning 200 so reverse proxy can detect liveness
3. **WebSocket reconnection backoff** on the client (React via zustand) — verify this exists or add it
4. **Consider `SIGHUP` reload** for config changes without full restart

---

## 6. Secrets Management

### Assessment: **CRITICAL — secrets in git**

**Current state:**

```
.env file is TRACKED in git
.gitignore contains: .env
```

`git ls-files .env` returns the file — it was committed before being added to `.gitignore`. The `.env` file contains live credentials:

```
WARWICK_EMAIL=adisak@warwick-institute.com
WARWICK_PASSWORD=0857982972Ff
DATABASE_URL=postgresql://postgres.nnoccwlmqcdelizdpqom:Woa1R4lvQ5zWhJAw@aws-1-ap-southeast-1.pooler.supabase.com:6543/postgres
```

**In `docker-compose.prod.yml`**, secrets are loaded via `env_file: .env` — the file is in git, so anyone with repo access has production credentials.

### Secret rotation

- **No rotation procedure** documented
- **Expiry detection:** `WarwickAuth.GetValidSession()` (`internal/warwick/auth.go:61-83`) refreshes session automatically (60min TTL, 5min buffer). If credentials themselves expire, `performLogin()` at line 77 returns an error, and the server emits `AuthExpired` status per-room. But the server **starts up by validating credentials at line 31-40 and exits if they fail** — so a credential expiry prevents startup entirely.
- **No alerting** on credential expiry — only per-room `AuthExpired` status propagated via WebSocket

### Fix

1. **Remove `.env` from git history** — use `git rm --cached .env` and `git filter-branch` or `bfg` to purge from history. Rotate all exposed credentials immediately.
2. **Use a secrets manager** for production: Supabase secrets panel, 1Password CLI, or at minimum a separate `.env` file scp'd onto the server and never committed
3. **Separate dev vs prod env** — dev env should use mock credentials
4. **Add credential expiry monitoring** — check Warwick login on a cron, alert before production credentials expire
5. **Make Warwick auth non-fatal at startup** — allow server to start even if Warwick is down, handle it at room level instead of crashing

---

## 7. Container Resource Limits

### Assessment: **None set**

`docker-compose.prod.yml` has no `deploy.resources.limits`.

### What happens under pressure

- **No CPU limit:** Container can consume all host CPU, starving other processes
- **No memory limit:** Go runtime will allocate until OOM. The Go runtime's `GC` will try to reclaim, but if the working set exceeds available memory, the kernel OOM killer terminates the container process. Go's default `GOMEMLIMIT` (Go 1.19+) is not set, so the GC triggers based on heap growth, not absolute memory.
- **No `GOMAXPROCS` configuration** — defaults to host CPU count

### OOM impact

- Container gets `SIGKILL` by the kernel
- `restart: unless-stopped` brings it back
- WebSocket clients disconnect and reconnect
- In-memory room state is lost — reloaded from DB on restart
- Active QR workers for each room are killed mid-cycle

### Fix

```yaml
services:
  app:
    build: .
    deploy:
      resources:
        limits:
          cpus: '0.5'
          memory: 256M
    environment:
      - GOMEMLIMIT=200MiB
      - GOMAXPROCS=1
```

Recommended: 256MB RAM, 0.5 CPU for this app (lightweight Go binary, few concurrent rooms).

---

## 8. DB Connection String

### Assessment: **Configurable but fragile**

```go
databaseURL := os.Getenv("DATABASE_URL")
pool, err := db.NewPool(databaseURL)
```

`pgxpool.NewWithConfig` creates a connection pool that handles **transient reconnection internally**. However:

### Connection string change during deploy

- If the DB host changes (e.g., Supabase migration), `DATABASE_URL` must be updated and the container restarted
- `docker-compose.prod.yml` uses `env_file: .env` — update `.env` then `docker compose up -d`
- During the restart window, all in-flight requests fail

### Migration connection fragility

```go
func RunMigrations(databaseURL string) error {
    db, err := sql.Open("postgres", databaseURL)  // Uses lib/pq, not pgxpool
    // ...
    driver, err := postgres.WithInstance(db, &postgres.Config{})
    m, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
    err = m.Up()
}
```

- Uses a separate `sql.Open` connection (via `lib/pq`) — not the pgx pool
- No retry logic — if Postgres is momentarily unavailable, startup fails with `os.Exit(1)`
- If migration takes too long and TCP connection drops, migration state becomes dirty

### Supabase pooler note

```go
config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
```

This is explicitly configured for Supabase's PgBouncer pooler — the app is tied to Supabase's connection pooling model.

### Fix

1. **Add retry logic** to startup — at minimum 3 retries with backoff for both `NewPool` and `RunMigrations`
2. **Add connection timeout** to migration connection (`sql.Open` doesn't actually connect — need to `db.Ping()` with a context)
3. **Use pgx for migrations too** — either via `pgx-go-migrations` or by using the pool's connection for migrations

---

## 9. Startup Dependency Order

### Assessment: **No ordering, no health checks**

`docker-compose.prod.yml`:
```yaml
services:
  app:
    build: .
    restart: unless-stopped
```

**Missing:**
- `depends_on` (doesn't wait for postgres)
- `healthcheck` (no way for Docker to know app is ready)
- No `wait-for-it.sh` or equivalent

### What happens on startup

1. Container starts
2. Go binary runs `main()`:
   - Loads `.env`
   - **Validates Warwick credentials** (line 31-40) — calls external HTTP API, fails if Warwick is down
   - **Connects to DB** (line 53) — `pgxpool.NewWithConfig` fails if Postgres is down
   - **Runs migrations** (line 61) — fails if Postgres is down
   - **Loads rooms from DB** (line 69) — fails if Postgres is down
3. Any failure → `os.Exit(1)`
4. Docker restarts (`restart: unless-stopped`)

**Without `depends_on`:** If the Postgres container (run separately or on Supabase) is slow to respond, the app races ahead, crashes, and loops in restart until Postgres is available.

### Postgres health check (in dev `docker-compose.yml`)
```yaml
postgres:
  healthcheck:
    test: ["CMD-SHELL", "pg_isready -U qruser -d qrcommandcenter"]
    interval: 5s
    timeout: 5s
    retries: 5
```

This exists for the local postgres service but is **not referenced by the app service**.

### Warwick dependency
Warwick auth validation at startup is a **hard dependency**. If the Warwick admin site is down for maintenance (e.g., 2am), the server won't start. This is a single point of failure for a non-critical upstream.

### Fix

1. **Add `depends_on`** to app service:
   ```yaml
   depends_on:
     postgres:
       condition: service_healthy
   ```
2. **Add retry loop** in `main()` for DB connection with exponential backoff (up to 30s)
3. **Make Warwick auth non-fatal at startup** — log a warning, start server anyway, handle `AuthExpired` on a per-room basis when workers try to fetch QR codes
4. **Add Docker healthcheck** to app container:
   ```yaml
   healthcheck:
     test: ["CMD", "wget", "-qO-", "http://localhost:3001/api/"]
     interval: 10s
     timeout: 5s
     retries: 3
   ```

---

## Summary: Severity Matrix

| # | Issue | Severity | Detectability | Fix Complexity |
|---|-------|----------|---------------|----------------|
| 1 | Non-expand-contract migration | Medium | Low (no alert) | Low |
| 2 | Destructive rollback | **Critical** | Low | Low (make down no-op) |
| 3 | No blue-green/canary | Low | N/A | Medium |
| 4 | Migration lock on concurrent start | Medium | Medium (logs) | Low |
| 5 | No zero-downtime | Medium | N/A | Medium |
| 6 | **Secrets in git** | **Critical** | **High** (anyone can clone) | High (requires credential rotation) |
| 7 | No resource limits | Medium | Low (no alert on OOM) | Low |
| 8 | DB config fragile | Medium | Medium (crash logs) | Low |
| 9 | No startup ordering | Medium | Medium (restart loops) | Low |

### Immediate actions (pre-production)

1. **Remove `.env` from git** and rotate all credentials (WARWICK_EMAIL, WARWICK_PASSWORD, DATABASE_URL)
2. **Make down migrations no-op** — prevent accidental data loss on rollback
3. **Add retry loop** for DB connection on startup
4. **Set resource limits** and `GOMEMLIMIT`

### Short-term (next 2 weeks)

5. **Add `/healthz` endpoint** and Docker healthcheck
6. **Make Warwick auth non-fatal** at startup
7. **Add startup timeout** to migrations
8. **Front with reverse proxy** for graceful restarts

### Medium-term

9. Implement expand-contract migration pattern
10. Add event persistence for WebSocket reconnect resilience
11. Add credential expiry monitoring and alerting

---

*Deployment safety audit: 2026-05-29*
