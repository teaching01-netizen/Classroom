# Design: DB-Backed Session Check-in Cache (Phase 2)

## Context

The QR Command Center serves 100+ teachers browsing course attendance from Warwick Institute's API. Current architecture stores all session/student/check-in data ephemerally in an in-memory TTL cache. PostgreSQL is only used for `rooms` and `teacher_favourites`.

This design adds a `session_checkins` table as an L2 warm replica behind the in-memory L1 cache. The goal is to eliminate pool dependency on cache-miss reads, survive deploys/restarts without cold-cache latency, and enable future horizontal scaling.

## Decisions

| Decision | Choice | Rationale |
|---|---|---|
| DB role | L2 warm replica (not source of truth) | Warwick is always authoritative. DB never replaces Warwick reads. |
| Read path | `cache → DB → pool → Warwick` | Hot path stays 0.05ms. DB hit at ~3ms replaces 250ms-5.5s pool acquire. |
| Toggle write | **Synchronous** DB write before 200 | Fire-and-forget lost toggles permanently. +3ms vs 250ms Warwick = acceptable. |
| Startup | **Lazy load** (no bulk seed) | Seed creates 30k async refreshes in 10s → Warwick DOS. First user pays 250ms after deploy (same as today). |
| Cache growth filter | Active sessions only (`session_date > now() - 30 days`) | Unbounded cache OOMs in 3-6 months. |
| Migration guard | Fix Force(1) hazard before 004 | Force(1) on dirty state skips 004 → binary crashes on first read. |
| Staleness check | `SELECT MAX(toggled_at)` per session after stale cache hit | `LIMIT 1` without ORDER BY returns arbitrary row → misses toggles on other students. Aggregate guarantees coherence. |
| Cache metadata | Wrapper struct `CachedSession{Detail, MaxToggledAt, CachedAt}` stored in L1 | Step 2b needs a reference point to compare "is DB fresher than cache". Without stored `MaxToggledAt`, comparison is incomputable. |
| Partial index on session_date | **Removed** — no query filters by `session_date` | PK on `(session_id, student_id)` already serves all query patterns. Partial index adds write overhead with zero query benefit. |
| Refresher + toggle coordination | `toggled_at` column + CASE guard in UPSERT | Lazy refresh never overwrites teacher toggles. Column-level write ownership split. |
| `UpsertStudent` column scope | Only writes `checked_in`, `toggled_at`, `refreshed_at`. Never touches `student_name` or `session_date`. | Toggle path sends partial `StudentCheckin` (no `Name`). Writing `student_name = ''` corrupts the row. |
| Step 4 cold-path DB write | **Async** (fire-and-forget goroutine after `cache.Set()`) | User shouldn't wait 200-600ms for DB UPSERT on cold miss. Warwick is source of truth; DB write is best-effort L2 fill. |
| pgxpool | Set `MaxConns`, `MinConns`, `MaxConnLifetime` | Fix existing pool config alongside this change. |
| DB timeouts | `context.WithTimeout` on all new queries | Prevent goroutine leak on slow DB. Existing queries not refactored (scope boundary). |

## Non-goals

- Redis or any new infrastructure dependency
- Removing the SessionPool (still needed for toggle + refresher writes to Warwick)
- Cross-instance pub/sub for cache invalidation (single-instance for now; 10s staleness accepted)
- Replacing `ClassroomClient` cache abstraction with an interface (deferred to later phase)
- ETL pipeline to local attendance DB (future phase)

## Schema

```sql
CREATE TABLE session_checkins (
    session_id   TEXT NOT NULL,
    student_id   TEXT NOT NULL,
    student_name TEXT NOT NULL,
    checked_in   BOOLEAN NOT NULL DEFAULT FALSE,
    toggled_at   TIMESTAMPTZ,          -- non-null = teacher override, refresher skips
    refreshed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    session_date DATE,                -- enables cache growth filter
    PRIMARY KEY (session_id, student_id)
) WITH (fillfactor = 85);
-- No index on session_date: no query filters by it. PK serves all lookups.
```

### Design rationale

- `fillfactor = 85`: leaves 15% free space per page → enables HOT updates when only `checked_in`/`toggled_at`/`refreshed_at` change (no b-tree index write). Without this, every UPSERT creates a dead heap tuple + b-tree index entry (83k dead tuples/hr at scale). **Note**: initial pages are packed at 100% fill. Run `VACUUM FULL session_checkins` after first data load to apply fillfactor to existing pages.
- `session_date`: enables cache growth filter. Lazy refresh only loads sessions from last 30 days. Historical data stays in DB (audit trail) but never enters L1 cache. **Not included in `DO UPDATE SET`** (preserves HOT updates — see lazy refresh SQL below).
- `toggled_at`: the load-bearing consistency mechanism. Lazy refresh `UPSERT` has a `CASE` guard — if a teacher has toggled, the refresher preserves the teacher's value. This is the column-level write ownership split.
- `refreshed_at`: records when DB row was last refreshed. Used for diagnostics, not for staleness detection (cross-instance coherence uses `MAX(toggled_at)`).
- **`CachedSession` metadata wrapper**: cached `SessionDetail` in L1 is wrapped in a struct that stores the `MaxToggledAt` value at population time. Step 2b compares the fresh `MAX(toggled_at)` from DB against the stored value to detect cross-instance toggles. Without this stored reference, step 2b ("if DB has newer toggled_at than cache") is incomputable.

## Architecture

### Data flow

```
      Read (hot path, 0.05ms)
   ┌── in-memory Cache (L1) ─────────────────────────────────┐
   │  TTL: 10s session detail, 30s course detail, 30s courses │
   ├── GetStale hit → verify MAX(toggled_at) from DB (0.1ms)
   │   → if DB max_toggled_at ≠ cached max_toggled_at:
   │   │   populate cache from DB, return
   │   └→ else: serve stale, async tryRefresh from Warwick
   ├── cache miss → PostgreSQL (L2, ~3ms) → return, populate cache
   └── DB miss → SessionPool (L3, 250ms-5.5s) → Warwick
       → populate cache, async UpsertFromWarwick → return

     Toggle (write-through)
  ┌── Interactive Pool → Warwick POST
  ├── Synchronous UPSERT session_checkins (+3ms)
  ├── Invalidate in-memory cache keys (session + course + courses)
  └── Return 200

     DataRefresher (unchanged from today)
  └── GetCourses() → cache.Set() — courses + course detail only
      No session detail iteration. No DB writes from refresher.
```

### Read path — `GetSessionDetail`

```
1. cache.Get(key) → hit? return

2. cache.GetStale(key) → hit?
   a. Lightweight SELECT MAX(toggled_at) AS max_toggled_at
      FROM session_checkins WHERE session_id = $1
   b. Compare dbMaxToggledAt against cached CachedSession.MaxToggledAt
      → if different: populate cache from full DB read (step 3), return
      → if same: serve stale from cache, async tryRefresh from Warwick

3. ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
   rows, err := repo.GetStudentsBySession(ctx, sessionID)
   cancel()
   → hit? wrap in CachedSession{Detail, MaxToggledAt, CachedAt}, cache.Set, return

4. pool.AcquireWithTimeout(TierTeacher, 5*time.Second) → Warwick
   → populate cache (wrapped in CachedSession)
   → go func() { ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second); defer cancel()
       c.checkinRepo.UpsertFromWarwick(ctx, sessionID, sessionDate, students) }()
   → return
```

Key properties:
- Step 2a is the cross-instance coherence fix: a 0.2ms aggregate SELECT detects if another instance toggled any student in this session. Using `MAX(toggled_at)` instead of `LIMIT 1` prevents the 99% miss rate (arbitrary-row problem).
- Stored `MaxToggledAt` in `CachedSession` makes step 2b comparison possible. Without it, `"newer toggled_at than cache"` is incomputable.
- Step 3 uses `context.WithTimeout(5s)` — prevents goroutine leak if pgxpool is exhausted. If DB is slow, step 4 still catches it.
- Step 4 is the rare cold path (brand-new session never fetched). DB write is async — user doesn't wait for it. After one fetch, subsequent reads hit step 1 or step 3.

### Toggle path — `ToggleCheckin`

```
1. pool.Acquire(TierInteractive)
2. Warwick POST
3. If Warwick fails → return error. No DB write.
4. ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
   err := repo.UpsertStudent(ctx, sessionID, studentID, checked)
   cancel()
5. If DB write fails → log error. Still return 200 (Warwick confirmed).
6. cache.Invalidate("session:" + sessionID)
7. cache.Invalidate("course:" + courseID)
8. cache.Invalidate("courses")
9. Return 200
```

Key properties:
- DB write is SYNCHRONOUS before 200 (fire-and-forget caused permanent toggle loss). +3ms vs 250ms Warwick = negligible.
- **`UpsertStudent` must NOT overwrite `student_name`** — toggle path sends `StudentCheckin` without `Name`. SQL must only SET `checked_in`, `toggled_at`, `refreshed_at`. See repository section for exact SQL.
- DB write failure ≠ user-facing error. Warwick is source of truth. Next lazy refresh will eventually sync. Log error for monitoring alert.
- Cache invalidation ensures next read from any instance triggers re-fetch.

### Lazy refresh (async cache fill) — `UpsertFromWarwick`

```sql
INSERT INTO session_checkins (session_id, student_id, student_name, checked_in, refreshed_at, session_date)
VALUES ($1, $2, $3, $4, NOW(), $5)
ON CONFLICT (session_id, student_id)
DO UPDATE SET
    refreshed_at  = NOW(),
    checked_in    = CASE WHEN session_checkins.toggled_at IS NULL
                         THEN EXCLUDED.checked_in
                         ELSE session_checkins.checked_in END
    -- student_name NOT in SET: set once on INSERT, never changes
    -- session_date NOT in SET: fixed per session, updating it prevents HOT
```

The `CASE WHEN toggled_at IS NULL` guard is the load-bearing consistency mechanism:
- If teacher toggled this student → `toggled_at` is set → `checked_in` preserved (teacher's value wins).
- If no teacher toggle → `toggled_at` is NULL → `checked_in` overwritten with Warwick's value.
- Evaluated at READ COMMITTED (pgx default) per row. Concurrent toggle sets `toggled_at` before our WHERE check → correct.

### DataRefresher — unchanged

`DataRefresher.refresh()` continues to call `d.cc.GetCourses()` which populates the in-memory cache for courses + course details. No session detail iteration. The N+1 problem (200 API calls, 60s sequential) is avoided entirely by NOT making the refresher iterate session details.

The `session_checkins` table is populated exclusively by:
- **Lazy access path** (step 4 in read flow): teacher views session → cache miss → DB miss → Warwick fetch → `UpsertFromWarwick`
- **Toggle path** (step 4 in toggle flow): teacher toggles student → Warwick POST → `UpsertStudent`

### Startup — cold cache, no seed

Server starts with a cold in-memory cache. No `SELECT * FROM session_checkins` at startup. First teacher to access a session will:
- Cache miss → DB miss (or stale data from prior session) → pool → Warwick → `UpsertFromWarwick`
- Subsequent accesses: cache hit (0.05ms)

This avoids thundering herd (30k async refreshes in 10s window). First user after deploy pays 250ms latency — same as today. Acceptable for low-frequency deploys (daily at most).

When `DataRefresher` completes its first cycle (~5s), course list + course detail cache is warm. Session detail cache warms lazily as teachers browse.

### Cache growth filter

- Only sessions with `session_date > CURRENT_DATE - 30` enter the in-memory cache.
- Historical data stays in DB (for audit trail) but never loaded into L1.
- Steady state: at 500 sessions/month × 100 students = 50k active rows × 300 bytes ≈ **15MB in cache**. Stable indefinitely.
- No OOM risk. No background cleanup job needed (filter is in the query, not a scheduled task).

## Risks (design review findings)

| Risk | Severity | Mitigation |
|---|---|---|
| Cross-instance coherence: `LIMIT 1` returns arbitrary row's `toggled_at` — misses toggles on other students | **Blocker** → fixed | Use `SELECT MAX(toggled_at)` per session instead |
| Cache stores no metadata — step 2b `"newer toggled_at than cache"` is incomputable | **Blocker** → fixed | `CachedSession` wrapper struct stores `MaxToggledAt` at population time |
| `session_date` unresolved: `SessionDetail` has no date field in domain model | **Blocker** | Must resolve before ship. Derive from `CourseSummary.StartDate` + session sequence, or fallback to today and add LRU eviction as safety net. |
| Metrics infrastructure: no prometheus/otel deps in go.mod; health endpoint only reports cache size | **Blocker** | Emit structured log metrics as stopgap; or add otel SDK. Deploy-gating table is decorative without wired metrics. |
| DB degradation → pool starvation → rooms/favourites blocked (cascade failure) | **Major** | Monitor pool saturation. Separate pool for session_checkins considered if contention emerges. Error fallthrough is correct but slow (5s timeout + Warwick). |
| `UpsertStudent` overwrites `student_name = ''` | **Major** → fixed | Only SET `checked_in`, `toggled_at`, `refreshed_at` in toggle UPSERT |
| `session_date` in `DO UPDATE SET` defeats HOT updates | **Major** → fixed | Removed from SET clause |
| Version assertion `os.Exit(1)` on schema version < 4 incompatible with canary deploy | **Major** | Change to graceful degraded-mode warning + skip DB-backed path, not hard crash |
| DB-sourced reads return partial `StudentCheckin` (no avatar_url, school, nickname) | **Minor** | Either persist all 8 fields or document regression and handle missing fields gracefully |
| Fillfactor=85 ineffective on initial pages until VACUUM FULL | **Minor** | Run `VACUUM FULL` after first data load |
| `context.Background()` in async refresh → goroutine leak on shutdown | **Minor** | Wire shutdown-aware context |

## Migration plan

### Step 0: Fix Force(1) hazard + pgxpool config (pre-requisite, ~2h)

**Fix Force(1) in `db.go:51-59`:**

```go
if _, ok := err.(migrate.ErrDirty); ok {
    slog.Error("migration dirty — manual investigation required")
    return fmt.Errorf("migration dirty: %w", err)
}
```

Add version assertion after `RunMigrations`:

```go
var version int
err := pool.QueryRow(ctx, "SELECT version FROM schema_migrations").Scan(&version)
if err != nil || version < 4 {
    // 4 = the migration that creates session_checkins
    slog.Error("schema version below required minimum", "have", version, "need", 4)
    os.Exit(1)
}
```

**Fix pgxpool config in `db.go:19-27`:**

```go
config.MaxConns = 25
config.MinConns = 5
config.MaxConnLifetime = 30 * time.Minute
config.MaxConnIdleTime = 5 * time.Minute
```

### Step 1: Migration 004

Add `004_create_session_checkins.up.sql` and `.down.sql`.

Both gated by `CREATE TABLE IF NOT EXISTS` / `DROP TABLE IF EXISTS` for idempotency.

### Step 2: Repository

New file `internal/db/session_checkin_repository.go` with interface + pgx implementation.

```go
type SessionCheckinRepository interface {
    GetStudentsBySession(ctx context.Context, sessionID string) ([]domain.StudentCheckin, error)
    UpsertFromWarwick(ctx context.Context, sessionID string, sessionDate time.Time, students []domain.StudentCheckin) error
    UpsertStudent(ctx context.Context, sessionID string, student domain.StudentCheckin) error
    GetMaxToggledAtForSession(ctx context.Context, sessionID string) (*time.Time, error)
}
```

- `UpsertFromWarwick`: single transaction wrapping N UPSERTs with the `toggled_at IS NULL` guard. `student_name` and `session_date` NOT in the `DO UPDATE SET` clause (set once on INSERT).
- `UpsertStudent`: single UPSERT with `toggled_at = NOW()`. **Only SETs** `checked_in`, `toggled_at`, `refreshed_at`. Never overwrites `student_name` or `session_date`. Uses a subquery to copy existing `student_name` on INSERT:
  ```sql
  INSERT INTO session_checkins (session_id, student_id, student_name, checked_in, toggled_at, refreshed_at)
  VALUES ($1, $2, (SELECT student_name FROM session_checkins WHERE session_id=$1 AND student_id=$2), $3, NOW(), NOW())
  ON CONFLICT (session_id, student_id) DO UPDATE SET
      checked_in   = EXCLUDED.checked_in,
      toggled_at   = NOW(),
      refreshed_at = NOW()
  ```
- `GetMaxToggledAtForSession`: `SELECT MAX(toggled_at) FROM session_checkins WHERE session_id = $1` — per-session aggregate, not per-student.
- All methods use `context.WithTimeout(ctx, 5*time.Second)` internally.

### Step 3: ClassroomClient changes

**Constructor**: accept `SessionCheckinRepository` as optional parameter (nil-safe).

```go
func NewClassroomClientFromPool(
    pool *SessionPool, tier SessionTier, sharedCache *cache.Cache,
    checkinRepo ...SessionCheckinRepository,
) *ClassroomClient
```

No change to existing `NewClassroomClient` (pre-pool constructor).

**`getSessionDetailWithPool`**: add step 3 (DB query) before the pool acquire.

```go
func (c *ClassroomClient) getSessionDetailWithPool(key, sessionID string) (*domain.SessionDetail, error) {
    if c.checkinRepo != nil {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        students, err := c.checkinRepo.GetStudentsBySession(ctx, sessionID)
        cancel()
        if err == nil && len(students) > 0 {
            // Also fetch max_toggled_at for cross-instance coherence
            toggledCtx, toggledCancel := context.WithTimeout(context.Background(), 5*time.Second)
            maxToggledAt, _ := c.checkinRepo.GetMaxToggledAtForSession(toggledCtx, sessionID)
            toggledCancel()
            detail := /* build from students */
            cached := &CachedSession{Detail: detail, MaxToggledAt: maxToggledAt, CachedAt: time.Now()}
            c.cache.Set(key, cached, 10*time.Second)
            return detail, nil
        }
    }
    // fall through to pool (existing code)
    return c.fetchSessionDetailWithPool(key, sessionID)
}
```

**`refreshSessionDetailCache`**: write to DB via `UpsertFromWarwick` after successful Warwick fetch.

```go
func (c *ClassroomClient) refreshSessionDetailCache(sessionID string) {
    detail, err := c.fetchSessionDetailWithPool("session:"+sessionID, sessionID)
    if err != nil { /* existing logging */ return }
    if c.checkinRepo != nil && detail != nil {
        sessionDate := /* extract from CourseSummary.StartDate + sequence, or today */
        if err := c.checkinRepo.UpsertFromWarwick(context.Background(), sessionID, sessionDate, detail.Students); err != nil {
            slog.Warn("failed to persist session checkins to DB", "session_id", sessionID, "error", err)
        }
    }
}
```

**`toggleCheckinWithPool`**: synchronous DB write before returning.

```go
func (c *ClassroomClient) toggleCheckinWithPool(courseID, sessionID, studentID string, checked bool) error {
    // ... existing pool acquire + Warwick POST ...
    // On success:
    if c.checkinRepo != nil {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        // Name intentionally empty — UpsertStudent subquery preserves existing student_name
        if err := c.checkinRepo.UpsertStudent(ctx, sessionID, domain.StudentCheckin{
            StudentID: studentID, CheckedIn: checked,
        }); err != nil {
            slog.Error("failed to persist toggle to DB", "student_id", studentID, "error", err)
        }
        cancel()
    }
    // existing cache invalidate + return
}
```

### Step 4: main.go wiring

```go
sessionCheckinRepo := db.NewPgSessionCheckinRepository(pool)
classroomClient = warwick.NewClassroomClientFromPool(
    sessionPool, warwick.TierTeacher, sharedCache, sessionCheckinRepo,
)
```

### Step 5: Tests

**Integration test (`internal/db/session_checkin_repository_test.go`)**:
- `UpsertFromWarwick` does not overwrite rows where `toggled_at` is set
- Concurrent `UpsertStudent` + `UpsertFromWarwick` on same row preserves toggle
- `GetStudentsBySession` returns correct rows
- `GetMaxToggledAtForSession` returns `MAX(toggled_at)` after toggle, NULL before
- `UpsertStudent` does not overwrite `student_name` (subquery preserves it)

**Integration test (`internal/warwick/classroom_client_test.go`)**:
- Read path with DB repo returns cached data after first fetch
- Read path without DB repo falls through to pool (existing behavior)
- Toggle path writes to DB synchronously

### Step 6: Deploy

**Ordering enforcement**: Step 0 AND migration 004 must land before code changes. Step 0 alone is insufficient — `os.Exit(1)` version assertion requires version >= 4. No git-level dependency exists; enforce in CI/release checklist.

1. Deploy Step 0 (Force(1) fix + pgxpool config) as separate PR. Land first.
2. Deploy migration 004. Safe — additive, no existing code depends on it.
3. Deploy code changes (repo + ClassroomClient + main.go wiring).
4. Monitor for 24h: cache hit rate, DB query latency, step 4 fallthrough count.

## Rollback

| Step | Rollback action | Data integrity |
|------|----------------|----------------|
| Migration 004 | `DROP TABLE IF EXISTS session_checkins` | No data loss (Warwick is source of truth). |
| Code change | Deploy previous binary | Old code doesn't touch `session_checkins`. Clean rollback. |
| Toggles during canary | Accepted risk | Old-instance toggles not written to DB. Warwick has them. Next cache miss re-fetches. 30s max staleness. |

Rollback plan: (1) deploy previous binary → (2) optionally run `004.down.sql` if table causes issues. No data loss in either step.

## Success criteria

| Metric | Current | Target |
|--------|---------|--------|
| Cold cache miss latency | 250ms-5.5s (pool timeout) | ~3ms (DB query) |
| Startup-to-serving | 5-10s (WarmOnce) | ~100ms (no seed; first user pays 250ms) |
| Pool dependency for reads | 100% of cache misses | ~1% of cache misses (DB catches rest) |
| Hot path regression | 0.05ms | 0.05ms (unchanged) |
| Cache growth | Unbounded (all TTLs) | Bounded to last 30 days |
| DB failure blast radius | 1 endpoint (favourites) | 1 endpoint + misses fall through to pool (no regression) |

## Monitoring

**Important**: go.mod has no prometheus/otel dependencies; health endpoint only reports cache size. The table below is aspirational. Until metrics infra is added, emit these as structured slog counters/histograms and parse from logs.

| Metric | Type | Purpose |
|--------|------|---------|
| `db_checkin_read_duration_ms` | Histogram | p50/p95/p99 of session_checkin SELECT queries |
| `db_checkin_write_duration_ms` | Histogram | p50/p95/p99 of UPSERT queries |
| `step4_fallthrough_count` | Counter | Reads that fell through to pool (should be < 5%) |
| `toggle_db_write_error_count` | Counter | Failed toggle DB writes (alert if > 0) |
| `refresh_db_write_error_count` | Counter | Failed refresh DB writes (alert if > 0 for 5min) |
| `session_checkins_active_row_count` | Gauge | Growth tracking for active session rows |
| `toggled_at_guard_skip_count` | Counter | Rows skipped by the toggled_at guard (info) |

### Deploy-gating criteria

**Prerequisite**: deploy-gating requires wired metrics (structured logs or otel). Without them, all criteria below are unreviewable. Ship Phase 1 of monitoring as structured slog counters before Phase 2 code deploy.

| Criterion | Threshold | Action |
|-----------|-----------|--------|
| Cache hit rate | < 90% after 30s | Investigate seed/lazy-load failure |
| Step 4 fallthrough | > 5% of reads | Pause roll-forward |
| Toggle DB write error | > 0 for > 1min | Rollback (data loss risk in DB) |
| DB query latency (p99) | > 200ms | Pause roll-forward, investigate pgxpool |

## Open questions for implementation

- `session_date` extraction: how to derive from Warwick API response? Current `SessionDetail` has no date field. Options:
  1. Derive from `CourseSummary.StartDate` + session sequence number
  2. Parse from session name pattern (e.g., "Week 3 - Mon 15 Jan")
  3. Default to `time.Now()` and add LRU eviction as safety net against unbounded cache growth
  **Must be resolved before ship** — cache growth filter depends on it.
- nil-safe repo pattern: is constructor parameter or setter method cleaner? Setter allows `RoomManager` (which creates `ClassroomClient`) to set repo after construction.
- Metrics infra: structured slog counters vs adding otel SDK. Slog is zero-dep but requires log parsing. Otel adds dependency but enables real dashboards. Decision deferred to implementation.
- `os.Exit(1)` vs graceful degradation on schema version mismatch: hard crash prevents accidental deploy but conflicts with canary patterns. Consider logging warning + disabling DB-backed path instead.
- `CachedSession` metadata wrapper: where to define? In `cache` package, `warwick` package, or a new shared type? If defined in `cache`, the cache package gains awareness of session semantics — currently it's generic. If defined in `warwick`, the staleness check logic lives alongside the consumer.
- Fillfactor=85 on initial pages: when to run `VACUUM FULL`? After migration 004? After first day of production traffic? Accept slower initial updates until autovacuum reorganizes?
