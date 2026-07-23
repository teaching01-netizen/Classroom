# Remove Upstream Data Caching Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Warwick the only source of truth for upstream-owned data by removing memory caches, database replicas, stale refreshers, report snapshots, and browser cache reuse.

**Architecture:** `ClassroomClient` will perform live requests through the existing isolated Warwick session pool. Course, session, profile, check-in, and report reads will not consult or populate local data stores. The database will retain only rooms, favourites, and saved dashboard views; API responses and frontend requests will explicitly disable HTTP caching.

**Tech Stack:** Go, `net/http`, Warwick session pool, PostgreSQL migrations via `golang-migrate`, Chi, React 18, Vite, Zustand, Vitest, Testing Library.

---

## Scope and invariants

The implementation is complete only when these invariants hold:

1. `GetCourses`, `GetCourseDetail`, `GetSessionDetail`, and `FetchStudentProfiles` perform a live upstream read for every non-coalesced request.
2. `GetCourseAttendanceReport`, batch attendance, and the absence dashboard compute from live session data and never read or write a local report snapshot.
3. A live upstream error is returned to the caller; no stale or database fallback is allowed.
4. Successful check-in toggles are sent to Warwick and are not mirrored into a local cache table.
5. No background worker performs upstream data synchronization outside an admitted business request.
6. API JSON responses and frontend API requests cannot be reused from browser or intermediary HTTP caches.
7. Auth/session cookies, rate limiting, connection pooling, room state, favourites, saved views, and current-rendered UI state remain available.

## File map

Create:

- `internal/db/migrations/008_remove_upstream_data_cache.up.sql` — drops the two cache-replica tables.
- `internal/db/migrations/008_remove_upstream_data_cache.down.sql` — recreates empty compatibility tables for emergency binary rollback; it cannot restore deleted rows.
- `web/src/api/fetchFresh.js` — one frontend wrapper that forces `cache: 'no-store'` for API requests.
- `web/src/__tests__/fetchFresh.test.js` — wrapper contract tests.
- `scripts/verify-no-upstream-cache.sh` — CI/static guard for removed production cache patterns.

Modify:

- `internal/warwick/classroom_client.go` — remove cache fields, stale refresh state, report-cache setters, and cache constructors.
- `internal/warwick/classroom_courses.go` — make course reads live and use request-local course names during enrichment.
- `internal/warwick/classroom_sessions.go` — remove memory and PostgreSQL session paths; retain live pool fetches.
- `internal/warwick/classroom_profiles.go` — remove profile cache and refresh path.
- `internal/warwick/classroom_checkin.go` — remove cache invalidation and PostgreSQL toggle persistence.
- `internal/warwick/classroom_report.go` — compute every report live without report storage or stale flags being set.
- `internal/service/teacher.go` — remove cache invalidation/persistence interfaces and use a live session source for all report paths.
- `internal/app/bootstrap.go` — remove cache construction, DB cache repositories, warmers, hydrator, report persister, and cache health wiring.
- `internal/app/config.go` — remove cache/prewarm configuration fields and environment parsing.
- `internal/api/routes.go` and `internal/api/handlers.go` — remove cache health fields and add no-store response headers.
- `internal/metrics/metrics.go` — remove cache/prewarm/persistence metrics; keep live report compute duration.
- `internal/db/db.go` — require schema version 8 after the removal migration.
- `.env.example`, `README.md`, and current architecture documentation — remove cache/prewarm configuration and describe live-source behavior.
- Frontend API hooks/stores under `web/src/hooks`, `web/src/store`, and the API-calling components — use `fetchFresh` for JSON API calls while preserving refetch triggers.
- Existing Warwick/client/API tests — rewrite cache assertions into live-source assertions.

Delete after references are removed:

- `internal/cache/cache.go`, `internal/cache/cache_test.go`
- `internal/service/data_refresher.go`
- `internal/service/session_prewarmer.go`, `internal/service/session_prewarmer_test.go`
- `internal/service/report_hydrator.go`, `internal/service/report_hydrator_test.go`
- `internal/service/report_persister.go`, `internal/service/report_persister_test.go`
- `internal/db/session_checkin_repository.go`, `internal/db/session_checkin_repository_test.go`, `internal/db/session_checkin_repository_contract_test.go`
- `internal/db/session_fetcher.go`, `internal/db/session_fetcher_test.go`
- `internal/db/attendance_report_repository.go`, `internal/db/attendance_report_repository_test.go`, `internal/db/attendance_report_unit_test.go`
- `internal/warwick/report_db_source.go`, `internal/warwick/report_db_source_test.go`
- `internal/warwick/classroom_client_db_test.go`, `internal/warwick/async_refresh_test.go`

Do not delete historical migrations `004`–`007`; they remain part of migration history. Migration `008` removes the resulting tables for current deployments.

## Task 1: Add the freshness contract tests first

**Files:**

- Test: `internal/api/handlers_test.go` or the existing API test file that owns `writeJSON` coverage.
- Test: `internal/warwick/classroom_client_fetch_test.go`.
- Test: `internal/warwick/classroom_client_report_test.go`.

- [ ] **Step 1: Add a response-header test.**

Add a test that calls `writeJSON` through an `httptest.ResponseRecorder` and asserts:

```go
require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
require.Equal(t, "no-store, no-cache, must-revalidate, max-age=0", recorder.Header().Get("Cache-Control"))
require.Equal(t, "no-cache", recorder.Header().Get("Pragma"))
require.Equal(t, "0", recorder.Header().Get("Expires"))
```

- [ ] **Step 2: Add failing sequential-refresh tests.**

For courses, session details, profiles, and reports, configure the fake Warwick server to return payload A on the first request and payload B on the second request. Call the public method twice and assert payload B is returned on the second call and the fake server saw two upstream reads. These tests must fail against the current cache implementation because the second call returns payload A.

- [ ] **Step 3: Add the failure contract test.**

Make the fake Warwick server return payload A once and HTTP 500 on the next request. Assert the second public method returns an error and does not return payload A.

- [ ] **Step 4: Run the focused tests and record the expected red state.**

Run:

```bash
go test ./internal/api ./internal/warwick -run 'NoStore|AlwaysFetch|LiveReadError' -count=1
```

Expected before implementation: the new cache-related tests fail; existing tests may pass. Do not weaken the assertions to accommodate the old behavior.

- [ ] **Step 5: Commit the red tests.**

```bash
git add internal/api internal/warwick/classroom_client_fetch_test.go internal/warwick/classroom_client_report_test.go
git commit -m "test: define live upstream freshness contract"
```

## Task 2: Remove database cache storage safely

**Files:**

- Create: `internal/db/migrations/008_remove_upstream_data_cache.up.sql`
- Create: `internal/db/migrations/008_remove_upstream_data_cache.down.sql`
- Modify: `internal/db/db.go`
- Delete later: the session-checkin and attendance-report repositories and their tests.

- [ ] **Step 1: Capture a pre-migration backup procedure in the plan/runbook.**

Before staging or production migration, run:

```bash
backup_path="/secure/backup/qr-command-center-before-cache-removal-$(date +%Y%m%d%H%M%S).dump"
pg_dump --format=custom --file="$backup_path" "$DATABASE_URL"
pg_restore --list "$backup_path" | rg 'session_checkins|attendance_reports|rooms|teacher_favourites|saved_dashboard_views'
```

The restore-list command must show both cache tables and all retained business tables before migration proceeds.

- [ ] **Step 2: Add the forward migration.**

`internal/db/migrations/008_remove_upstream_data_cache.up.sql`:

```sql
DROP TABLE IF EXISTS attendance_reports;
DROP TABLE IF EXISTS session_checkins;
```

Dropping `session_checkins` also removes `last_warwick_sync_at`, its sync index, nickname/school columns, and all stored upstream snapshots.

- [ ] **Step 3: Add an emergency structural rollback migration.**

`internal/db/migrations/008_remove_upstream_data_cache.down.sql`:

```sql
CREATE TABLE IF NOT EXISTS session_checkins (
    session_id TEXT NOT NULL,
    student_id TEXT NOT NULL,
    student_name TEXT NOT NULL,
    nickname TEXT NOT NULL DEFAULT '',
    school TEXT NOT NULL DEFAULT '',
    checked_in BOOLEAN NOT NULL DEFAULT FALSE,
    toggled_at TIMESTAMPTZ,
    refreshed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    session_date DATE,
    last_warwick_sync_at TIMESTAMPTZ,
    PRIMARY KEY (session_id, student_id)
);

CREATE INDEX IF NOT EXISTS idx_session_checkins_sync
    ON session_checkins (session_id, last_warwick_sync_at DESC);

CREATE TABLE IF NOT EXISTS attendance_reports (
    course_id TEXT PRIMARY KEY,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    threshold INTEGER NOT NULL,
    duration_ms BIGINT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_attendance_reports_computed
    ON attendance_reports (computed_at DESC);
```

Document that this restores schema only. Data restoration requires the backup from Step 1.

- [ ] **Step 4: Raise the minimum schema version.**

Change the `internal/db/db.go` post-migration check from version 5 to version 8 and update its error message to require schema version 8. Keep the migration runner’s dirty-state failure behavior unchanged.

- [ ] **Step 5: Add migration integration tests.**

Extend the database migration test coverage to seed both cache tables at version 7, apply version 8, and assert:

```sql
SELECT to_regclass('public.session_checkins') IS NULL;
SELECT to_regclass('public.attendance_reports') IS NULL;
SELECT to_regclass('public.rooms') IS NOT NULL;
SELECT to_regclass('public.teacher_favourites') IS NOT NULL;
SELECT to_regclass('public.saved_dashboard_views') IS NOT NULL;
```

Also verify the down migration recreates both tables empty and that the retained tables are still present.

- [ ] **Step 6: Run migration tests.**

Run:

```bash
go test ./internal/db -run 'Migration|Schema' -count=1
```

Expected: all migration tests pass against a disposable PostgreSQL database.

- [ ] **Step 7: Commit the migration independently.**

```bash
git add internal/db/migrations/008_remove_upstream_data_cache.* internal/db/db.go internal/db/*test.go
git commit -m "feat: remove database upstream cache tables"
```

## Task 3: Make ClassroomClient live-only

**Files:**

- Modify: `internal/warwick/classroom_client.go`
- Modify: `internal/warwick/classroom_courses.go`
- Modify: `internal/warwick/classroom_sessions.go`
- Modify: `internal/warwick/classroom_profiles.go`
- Modify: `internal/warwick/classroom_checkin.go`
- Modify: `internal/warwick/classroom_report.go`

- [ ] **Step 1: Simplify client construction.**

Remove `cache.Cache`, `db.SessionCheckinRepository`, `reportCache`, `refreshing`, `disableAsyncRefresh`, and all setters/getters that expose them. The live constructors become:

```go
func NewClassroomClient(auth *WarwickAuth) *ClassroomClient
func NewClassroomClientFromPool(pool *SessionPool, tier SessionTier) *ClassroomClient
```

Retain `SetTransport`, `SetBaseURL`, `SetUserID`, `SetRateLimiter`, `Auth`, and the existing pool/auth request behavior.

- [ ] **Step 2: Remove cache branches from course reads.**

`GetCourses` must always fetch the upstream course list and enrich it. It must not call `Get`, `GetStale`, `Set`, `Invalidate`, or start a goroutine. `refreshCoursesCache` and all associated logging are removed.

Refactor enrichment so it does not recursively fetch the whole course list. `GetCourses` already owns the raw list, so pass each source course name into a request-local enrichment helper. Direct `GetCourseDetail(courseID)` may fetch the raw course list once only when the upstream detail endpoint does not provide a name; this data is request-local and is never retained.

- [ ] **Step 3: Remove memory and DB paths from session reads.**

`GetSessionDetail` must call the live session fetch path for every request. Delete `CachedSession`, `getSessionFromFreshCache`, `getSessionFromStaleCheck`, `tryDBCoherenceCheck`, `getSessionFromDBColdHit`, `refreshSessionDetailCache`, and all DB persistence calls. Keep `FetchSessionDetailLive` as the report-facing live fetch method and preserve pool acquisition, rate limiting, auth retry, and response parsing.

- [ ] **Step 4: Remove profile caching.**

`FetchStudentProfiles` must perform a live upstream request for every call. Delete its cached/stale branches and `refreshStudentProfilesCache`. Keep pool/auth retry behavior and the existing payload parsing.

- [ ] **Step 5: Remove local writes from toggles.**

After a successful Warwick toggle, return success without DB writes and without invalidating local cache keys. If Warwick fails, return the existing error. The next session/report read will query Warwick live.

- [ ] **Step 6: Make reports compute without retained state.**

Remove `reportCache`, `ReportPersistence`, report cache reads/writes, `MarkStaleReport`, `InvalidateReportCache`, stale mutation, and detached refresh. `GetCourseAttendanceReport` calls `ComputeCourseAttendanceReport` with live session data and returns its result. Leave `CourseAttendanceReport.Stale` in the response struct for compatibility, but it must remain the zero value `false` and no production path may set it to `true`.

- [ ] **Step 7: Update the tests to prove live behavior.**

Replace cache-hit tests with these named cases:

```text
TestGetCourses_AlwaysFetchesUpstream
TestGetCourseDetail_AlwaysFetchesUpstream
TestGetSessionDetail_AlwaysFetchesUpstream
TestFetchStudentProfiles_AlwaysFetchesUpstream
TestGetCourseAttendanceReport_AlwaysComputesLive
TestLiveReadError_DoesNotReturnPreviousPayload
TestToggleCheckin_DoesNotWriteLocalReplica
```

Each test must use a changing fake response and assert both returned data and upstream call count.

- [ ] **Step 8: Run the focused Warwick suite.**

Run:

```bash
go test ./internal/warwick -run 'Test(GetCourses|GetCourseDetail|GetSessionDetail|FetchStudentProfiles|GetCourseAttendanceReport|LiveReadError|ToggleCheckin)' -count=1
```

Expected: all focused live-source tests pass, and no test imports `internal/cache`.

- [ ] **Step 9: Commit the live client.**

```bash
git add internal/warwick
git commit -m "feat: read Warwick data without local caches"
```

## Task 4: Remove cache-oriented service and application wiring

**Files:**

- Modify: `internal/service/teacher.go`
- Modify: `internal/app/bootstrap.go`
- Modify: `internal/app/config.go`
- Modify: `internal/api/routes.go`
- Modify: `internal/api/handlers.go`
- Modify: `internal/metrics/metrics.go`
- Delete: the service/database cache files listed in the file map.

- [ ] **Step 1: Simplify `TeacherDataProvider`.**

Remove `MarkStaleReport` and report persistence arguments from the interface. Construct a live session source from `FetchSessionDetailLive` for report, batch, and dashboard computations. Remove `DBSessionFetcher` and `FallbackSessionDataSource` construction from `Wire`.

- [ ] **Step 2: Fix concurrent batch result writes while removing singleflight/cache assumptions.**

Replace the shared `map[string]courseResult` writes in `GetBatchAttendance` with indexed result storage or a mutex-protected map. The implementation must pass `go test -race`; two goroutines must never write the same Go map without synchronization.

- [ ] **Step 3: Remove cache workers from bootstrap.**

Delete shared cache construction, report cache construction, `DataRefresher`, `SessionPreWarmer`, `ReportHydrator`, `ReportPersister`, `SetAsyncRefreshEnabled`, and `SetQueueDepthFunc`. `BackgroundRuntime` may remain for the serverless activity controller and room lifecycle, but its worker list must not contain data refresh workers.

- [ ] **Step 4: Remove cache configuration.**

Delete `PreWarmSessions`, `CacheInterval`, and `PreWarmInterval` from `Config` and remove parsing of `WARWICK_PREWARM_SESSIONS`, `WARWICK_CACHE_INTERVAL`, and `WARWICK_PREWARM_INTERVAL`. Remove those variables and comments from `.env.example`.

- [ ] **Step 5: Remove cache health output.**

Change `NewRouter` and `healthHandler` so the `/api` response no longer reports cache size or warm status. Update any API tests to assert the response contains the running message and does not expose a cache object.

- [ ] **Step 6: Remove obsolete metrics.**

Delete `ReportCacheHits`, `ReportPersistQueueDepth`, `ReportPersistDropped`, `PrewarmSessions`, and `SetQueueDepthFunc`. Keep `ReportComputeDuration` with the `source` label set to `live` for upstream-backed reports.

- [ ] **Step 7: Delete cache-only implementation and tests.**

Delete the cache package, warmers, report persistence/hydration, cache repositories, DB session fetcher, fallback data source, and their tests only after all compile references are gone. Do not delete the retained room/favourite/dashboard repositories.

- [ ] **Step 8: Run the full backend suite and race detector.**

Run:

```bash
go test ./...
go test -race ./...
```

Expected: zero test failures, zero race reports, and no compile references to deleted packages.

- [ ] **Step 9: Commit service and wiring cleanup.**

```bash
git add internal/app internal/api internal/service internal/metrics internal/db internal/warwick internal/cache .env.example
git commit -m "refactor: remove cache workers and local upstream replicas"
```

## Task 5: Enforce HTTP and frontend no-store behavior

**Files:**

- Create: `web/src/api/fetchFresh.js`
- Create: `web/src/__tests__/fetchFresh.test.js`
- Modify: `internal/api/handlers.go`
- Modify: all API-calling frontend hooks/stores/components under `web/src`.

- [ ] **Step 1: Add the shared frontend wrapper.**

`web/src/api/fetchFresh.js`:

```js
export function fetchFresh(input, init = {}) {
  return fetch(input, {
    ...init,
    cache: 'no-store',
  });
}
```

The caller’s method, body, signal, and headers remain unchanged; the wrapper always wins for the browser cache mode.

- [ ] **Step 2: Add wrapper tests.**

Mock `globalThis.fetch`, call `fetchFresh('/api/teacher/courses')`, and assert it received `{ cache: 'no-store' }`. Call it with an existing `cache: 'force-cache'` value and assert the final value is `no-store`. Assert a POST body, signal, and headers are preserved.

- [ ] **Step 3: Replace raw API fetches.**

Use `fetchFresh` in `useCourses`, `useSessions`, `useCheckins`, `useCourseAttendance`, `useBatchAttendance`, `useAbsenceDashboard`, `useDashboardViews`, `usePinnedCoursesStore`, `RoomCard`, and `CheckinDetail`. Keep WebSocket construction and QR countdown behavior unchanged.

- [ ] **Step 4: Preserve active synchronization behavior.**

Keep course/session focus refetch, check-in polling, WebSocket reconnect refetch, request aborts, and optimistic toggle rollback. Remove comments describing stale-while-revalidate. Add a test that a failed refresh leaves the error visible and does not silently present an older response as fresh.

- [ ] **Step 5: Run frontend tests and lint.**

Run:

```bash
npm test -- --run
npm run lint
npm run build
```

Run from `/Users/rd-cream/Downloads/check in auto/web`. Expected: 133 existing tests plus the new wrapper/freshness tests pass, lint has zero errors, and Vite build exits 0.

- [ ] **Step 6: Commit frontend freshness enforcement.**

```bash
git add web/src internal/api/handlers.go
git commit -m "feat: disable browser and API response caching"
```

## Task 6: Add a static regression guard and update documentation

**Files:**

- Create: `scripts/verify-no-upstream-cache.sh`
- Modify: `README.md`, `.env.example`, and current architecture documentation.

- [ ] **Step 1: Add the static guard.**

Create an executable script with this behavior:

```bash
#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
forbidden='internal/cache|GetStale|MarkStale|reportCache|SessionPreWarmer|DataRefresher|ReportHydrator|ReportPersister|session_checkins|attendance_reports|WARWICK_CACHE_INTERVAL|WARWICK_PREWARM_INTERVAL|WARWICK_PREWARM_SESSIONS'

if rg -n -i --glob '*.go' --glob '*.js' --glob '*.jsx' --glob '*.ts' --glob '*.tsx' --glob '*.env*' "$forbidden" \
    "$root/internal" "$root/web/src" "$root/.env.example"; then
  echo "upstream-data cache pattern found in production source"
  exit 1
fi

rg -n 'Cache-Control.*no-store|cache:[[:space:]]*['"'"']no-store['"'"']' \
  "$root/internal/api" "$root/web/src/api" >/dev/null
echo "upstream cache guard passed"
```

Historical migration files and historical design/review documents are intentionally outside the scan because they are audit history, not runtime behavior.

- [ ] **Step 2: Add the guard to the repository validation command.**

Run it in CI alongside Go and frontend tests. The command must fail with exit code 1 if a removed cache symbol is reintroduced.

- [ ] **Step 3: Update current documentation.**

Describe live Warwick reads, request-level bounded concurrency, no-store API behavior, and the retained non-data state. Remove current instructions for cache intervals, prewarm intervals, cache health, report hydration, and DB-backed report/session replicas. Leave historical design documents unchanged except for an explicit superseded note if the repository documentation index links to them.

- [ ] **Step 4: Run the guard.**

Run:

```bash
chmod +x scripts/verify-no-upstream-cache.sh
scripts/verify-no-upstream-cache.sh
```

Expected: `upstream cache guard passed` and exit code 0.

- [ ] **Step 5: Commit the guard and documentation.**

```bash
git add scripts/verify-no-upstream-cache.sh README.md .env.example docs
git commit -m "docs: define live upstream data contract"
```

## Task 7: Add end-to-end freshness and failure coverage

**Files:**

- Create or extend: `internal/integration/live_sync_test.go` or the repository’s existing integration-test location.
- Modify: `internal/api` and `internal/warwick` test helpers as needed.

- [ ] **Step 1: Build a deterministic fake Warwick server.**

The fake server must expose the same course, course-detail, profile, session-detail, and toggle paths used by `ClassroomClient`. Each route returns a version selected by an atomic counter. The handler records request count, path, and request body.

- [ ] **Step 2: Test sequential freshness.**

For each public read, call the application twice. Change the fake upstream payload between calls. Assert the second response contains the changed value, the upstream count increments, and no local database query is required for the upstream-owned resource.

- [ ] **Step 3: Test stale-error behavior.**

Return version A first and an upstream 500, timeout, malformed JSON, auth expiry, and rate-limit response on the second call. Assert the second call returns the corresponding error and no version-A payload.

- [ ] **Step 4: Test report freshness.**

Change one session’s check-in value between two identical attendance-report requests. Assert the second report reflects the changed student state, `stale` is false, and no `attendance_reports` row is created.

- [ ] **Step 5: Test toggle freshness.**

Return successful Warwick toggle response, then change the fake Warwick session response. Assert the next session read reflects the upstream response and no local replica write occurs.

- [ ] **Step 6: Test request concurrency and cancellation.**

Issue concurrent course/report/session requests with a bounded fake server. Assert the configured pool/rate limits are respected, canceled requests do not update later results, and `go test -race` reports no data races.

- [ ] **Step 7: Test HTTP cache headers through routes.**

Call representative GET endpoints with `httptest` and assert the no-store headers are present on success and error responses. Assert `/api` contains no `cache.size` or `cache.warm` fields.

- [ ] **Step 8: Run the integration and full verification suite.**

Run:

```bash
go test ./internal/integration ./internal/api ./internal/warwick -count=1
go test -race ./...
cd web && npm test -- --run && npm run lint && npm run build
cd .. && scripts/verify-no-upstream-cache.sh
```

Expected: every command exits 0; the existing frontend warning output is allowed only if test results remain passing.

- [ ] **Step 9: Commit end-to-end coverage.**

```bash
git add internal/integration internal/api internal/warwick web/src scripts
git commit -m "test: verify live upstream synchronization"
```

## Task 8: Stage, deploy, and verify the destructive migration

**Files:**

- No additional source files; use the migration, test, and runbook changes above.

- [ ] **Step 1: Verify repository state before deployment.**

Run:

```bash
git status --short
go test ./...
go test -race ./...
cd web && npm test -- --run && npm run lint && npm run build
cd .. && scripts/verify-no-upstream-cache.sh
```

Do not proceed if any command fails or if unexpected files are present in the diff.

- [ ] **Step 2: Apply to a staging database.**

Create the custom-format backup, run the application migration, and verify:

```sql
SELECT version FROM schema_migrations;
SELECT to_regclass('public.session_checkins');
SELECT to_regclass('public.attendance_reports');
SELECT to_regclass('public.rooms');
SELECT to_regclass('public.teacher_favourites');
SELECT to_regclass('public.saved_dashboard_views');
```

Expected: version 8; both upstream cache tables return `NULL`; retained tables return their relation names.

- [ ] **Step 3: Run staging smoke tests.**

With the fake or controlled Warwick endpoint, verify courses, course details, sessions, profiles, reports, toggles, rooms, favourites, saved views, WebSocket reconnect, and API error handling. Change upstream data between repeated requests and confirm the next response changes immediately.

- [ ] **Step 4: Observe upstream and application metrics.**

Confirm there are no `cache_refresh`, `cache_warmup`, `session_prewarmer`, `report_persister`, or `hydrator` logs. Confirm live report duration and Warwick request rate remain within the session-pool/rate-limit budget. Watch latency, upstream 429/5xx responses, pool exhaustion, and error rate for at least one normal traffic window.

- [ ] **Step 5: Deploy production with the backup gate.**

Take and verify the production backup, deploy the binary/frontend, run migration 008, then execute the same smoke tests. The deployment operator must record the backup path, schema version, migration result, and smoke-test result.

- [ ] **Step 6: Roll back if freshness or load gates fail.**

For code-only failure, deploy the previous binary while keeping migration 008 applied only if that binary does not require the removed tables. For a binary that still requires the tables, restore the backup and run the documented down migration before rollback. Do not treat the down migration as data recovery.

## Rigorous test plan

### Unit and contract coverage

| Area | Required assertions |
|---|---|
| Course list | Two calls read two upstream versions; upstream errors are returned; no local snapshot is consulted. |
| Course detail | Same freshness/error assertions; request-local name enrichment does not persist. |
| Session detail | Two calls read changed student/check-in data; DB/cache fallback is impossible. |
| Profiles | Two calls read changed profile data; no stale return on error. |
| Toggle | Warwick is called; local replica is never written; upstream failure is returned. |
| Reports | Every request recomputes from live session data; report changes after upstream change; `stale` remains false. |
| Batch/dashboard | Each course uses live fetches; partial errors are represented without stale substitution; concurrent writes are race-free. |
| HTTP | Success and error JSON responses include no-store headers; health output has no cache fields. |
| Frontend wrapper | `cache: 'no-store'` is forced and request options are preserved. |
| Refetch behavior | Focus, polling, WebSocket reconnect, and manual refresh still issue fresh requests. |

### Boundary and failure matrix

- Empty course/session/profile lists are returned as valid empty results from the upstream response.
- Nil or missing report source is rejected at construction or returned as an explicit error; it is never silently replaced with DB data.
- Malformed upstream JSON returns an invalid-payload error.
- Auth expiry retries according to the existing session-pool contract and then returns `ErrAuthExpired`.
- Rate limiting returns `ErrRateLimited` without returning a previous payload.
- Pool exhaustion returns `ErrPoolExhausted` without stale fallback.
- Context cancellation stops in-flight report/session work and prevents late result publication.
- Concurrent batch requests do not race and do not write unsynchronized maps.
- Migration is idempotent through `IF EXISTS`/`IF NOT EXISTS` and leaves retained tables intact.
- Frontend API errors leave the error state visible instead of labeling older data as fresh.

### Verification commands

```bash
go test ./...
go test -race ./...
cd web && npm test -- --run
cd web && npm run lint
cd web && npm run build
cd .. && scripts/verify-no-upstream-cache.sh
```

## Rollout and rollback gates

Before rollout:

- Backup exists and contains both removed tables.
- Staging migration reaches schema version 8.
- All live-source tests pass.
- No-store headers are verified on representative routes.
- Static guard passes.

During rollout:

- Compare upstream request volume and 429/5xx rate with the bounded pool budget.
- Watch p50/p95/p99 latency for course, session, and report endpoints.
- Verify no stale/cache-worker logs appear.
- Verify room QR refresh and non-Warwick database features remain healthy.

Rollback triggers:

- Upstream rate-limit or error rate exceeds the agreed operational budget.
- Live reads return incorrect or incomplete data.
- Report/session request latency exceeds the agreed SLO for the normal traffic window.
- Any retained feature regresses due to wiring or migration changes.

Rollback actions:

1. Stop rollout and preserve logs/metrics.
2. Deploy the previous code if it does not require removed tables.
3. Otherwise restore the pre-migration backup and run the structural down migration, then deploy the previous code.
4. Re-run the pre-deploy test and smoke checklist.

## Plan self-review

- Spec coverage: upstream read freshness, cache removal, DB migration, HTTP/browser cache prevention, preserved non-data state, tests, rollout, and rollback each have dedicated tasks.
- Placeholder scan: all steps contain concrete paths, commands, assertions, or code; no unresolved implementation decision is required.
- Type/interface consistency: the plan removes cache and persistence arguments together from constructors, providers, and call sites; live fetcher usage is kept consistent across reports, batch, and dashboard paths.
- Scope check: room state, favourites, saved views, auth, rate limiting, connection pooling, polling, and WebSocket behavior remain outside the upstream-data cache removal.
