# PostgreSQL snapshot scraper runbook

This runbook covers the staged release, observation, read cutover, rollback,
and repeatable diagnostics for ADR 009. PostgreSQL 16 is the production target.

## Safety rules

- Use a unique random `SCRAPER_TRIGGER_TOKEN` stored as a deployment secret.
- Send the token only as `Authorization: Bearer <token>`. Never put it in a URL,
  command history, dashboard label, or log field.
- Run integration and diagnostic tests only against a disposable database.
- Never persist raw Warwick responses, HTML, cookies, credentials, auth
  headers, screenshots, or avatar bytes/files.
- Operational rollback changes `SNAPSHOT_READS_ENABLED`; it does not drop
  migration 009 tables or roll back a database migration.

## Protected controls

For serverless deployments, invoke the bounded tick from the platform scheduler:

```bash
curl --fail-with-body \
  --request POST \
  --header "Authorization: Bearer $SCRAPER_TRIGGER_TOKEN" \
  "$APP_ORIGIN/api/internal/scraper/tick"
```

Inspect aggregate state:

```bash
curl --fail-with-body \
  --header "Authorization: Bearer $SCRAPER_TRIGGER_TOKEN" \
  "$APP_ORIGIN/api/internal/scraper/status" |
  jq '.data'
```

The status response intentionally contains aggregate counts only. For each
kind, coverage is `current_by_kind[kind] / targets_by_kind[kind]`. A missing
root current count is not ready; a zero target denominator for discovered
course/session kinds is not evidence of coverage.

## Stage 1: write-only

Deploy:

```env
SCRAPER_ENABLED=true
SNAPSHOT_READS_ENABLED=false
```

Keep live Warwick reads as the product path while the scraper builds coverage.
Wait for all of these gates:

- catalog current is 1;
- profiles current is 1;
- active-course target coverage is 100%;
- known-session target coverage is 100%;
- `expired_current` is 0;
- active permits return to 0 after the configured permit TTL;
- lease-loss and WebSocket-drop counters do not increase under healthy load;
- no repeated `429`, auth renewal storm, DB-pool exhaustion, or notification
  reconnect loop occurs.

Observe for at least one complete maximum active-session interval. Serverless
ticks must run frequently enough that this interval can actually be met.

Useful Prometheus series:

```text
warwick_scrape_runs_total
warwick_scrape_duration_seconds
warwick_scrape_due_targets
warwick_scrape_active_leases
warwick_scrape_active_host_permits
warwick_scrape_snapshot_age_seconds
warwick_scrape_validation_age_seconds
warwick_scrape_host_paused
warwick_scrape_host_requests_per_second
warwick_scrape_host_concurrency
warwick_scrape_claim_conflicts_total
warwick_scrape_lease_lost_total
warwick_snapshot_websocket_events_total
warwick_snapshot_websocket_drops_total
warwick_upstream_request_duration_seconds
warwick_session_pool_wait_seconds
```

All labels are bounded. Do not add target, course, session, student, worker, URL,
or error-message labels.

## Stage 2: snapshot reads

Set:

```env
SNAPSHOT_READS_ENABLED=true
```

Verify courses, course details, sessions, check-ins, attendance reports,
absence dashboard, favourites, saved views, QR rooms, and serverless ticks.
For snapshot-backed teacher reads, inspect:

```text
X-Snapshot-Version
X-Snapshot-Validation-Seq
X-Snapshot-Validated-At
X-Snapshot-Stale
```

An unchanged or complete-representation `304` advances validation sequence and
validated time without changing version. An overdue snapshot may be served as
stale within its maximum serve age. After that age, the API fails closed with
`503`; it does not silently call live Warwick in snapshot mode.

Continue polling/focus repair during this rollout. WebSocket
`SnapshotCommitted` events contain metadata only and are hints to refetch REST.

## Stage 3: rollback drill

Set:

```env
SNAPSHOT_READS_ENABLED=false
```

Confirm two sequential teacher reads reach Warwick, fresh upstream changes are
visible, and the scraper can continue writing safely. Check-in writes remain
Warwick-first in both modes. Keep migration 009 data intact so reads can be
re-enabled after diagnosis.

Rollback immediately if expired/`503` rates rise unexpectedly, coverage
regresses, validation age exceeds policy, host admission remains paused,
lease-loss or notification-drop counters grow, or DB pool wait becomes
material.

## Release diagnostics

Canonicalization and scheduler allocation benchmarks:

```bash
go test ./internal/scraper -run '^$' \
  -bench 'Benchmark(CanonicalizeByKind|SchedulerDispatch)$' \
  -benchmem -count=5
```

CPU profile:

```bash
go test ./internal/scraper -run '^$' \
  -bench BenchmarkSchedulerDispatch \
  -cpuprofile /tmp/snapshot-scheduler-cpu.pprof
go tool pprof /tmp/snapshot-scheduler-cpu.pprof
```

PostgreSQL plan, commit, JSONB-size, pool-wait, and four-client permit
contention diagnostics:

```bash
RUN_SNAPSHOT_DIAGNOSTICS=1 \
TEST_DATABASE_URL='postgres://user:pass@localhost/snapshot_test?sslmode=disable' \
  go test ./internal/db ./internal/scraper \
  -run 'PerformanceDiagnostics|ContentionDiagnostics' -count=1 -v
```

The query-plan diagnostic seeds 5,000 targets and asserts the due query uses
`idx_scrape_targets_due`, but deliberately sets no machine-specific latency
threshold. The HTTP transport suite also proves sequential connection reuse:

```bash
go test ./internal/warwick \
  -run TestSharedTransportReusesConnectionForSequentialRequests -count=1 -v
```

### Local reference, 2026-07-26

Environment: Apple M1 arm64, Go 1.26.3, local PostgreSQL 14.23 over loopback.
These numbers are a developer reference, not a production SLO; rerun on
PostgreSQL 16 and the deployed instance/database topology before cutover.

| Diagnostic | Representative result |
| --- | ---: |
| Canonicalize 250 courses | 148.9 µs/op, 80,575 B/op, 6 allocs/op |
| Canonicalize course with 50 sessions | 15.5 µs/op, 13,398 B/op, 6 allocs/op |
| Canonicalize session with 500 students | 346.0 µs/op, 186,179 B/op, 1,006 allocs/op |
| Canonicalize 2,000 profiles | 838.2 µs/op, 380,080 B/op, 7 allocs/op |
| Dispatch 32 scheduler targets | 87.5 µs/op, 51,428 B/op, 407 allocs/op |
| Claim query, 5,000 targets | 0.258 ms; due index used |
| Commit transaction, 50 samples | p50 0.429 ms; p95 0.917 ms |
| Canonical JSONB fixture | 89 bytes min/p50/p95/max |
| DB empty-acquire wait | 1.62 ms total, 1 occurrence |
| Host permit transaction, 4 clients/200 ops | p50 2.07 ms; p95 5.13 ms; 200 admitted |
| Sequential HTTP connection reuse | 20 requests on 1 connection |

Record the same set during staging, including production-shaped payload-size
distribution. Investigate changes with profiles and query plans before raising
host rate or concurrency.
