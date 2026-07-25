# Remove Upstream Data Caching Design

**Status:** Superseded for teacher-read storage by
[ADR 009](../../adr/009-postgresql-snapshot-scraper.md). Retained as migration
history and as the tested `SNAPSHOT_READS_ENABLED=false` rollback contract.

## Goal

Ensure that data owned by Warwick is never served from a local snapshot. Every completed read of courses, course details, sessions, student profiles, check-ins, and attendance reports must be based on a live Warwick response from the current request.

## Current problem

The application has several independent stale-data paths:

- `internal/cache` stores courses, course details, sessions, profiles, and reports in process memory.
- `GetStale`, `MarkStale`, and detached refresh goroutines implement stale-while-revalidate.
- `session_checkins` is used as a PostgreSQL L2 replica for session reads.
- `attendance_reports` stores computed report snapshots and is hydrated on startup.
- `DataRefresher` and `SessionPreWarmer` proactively populate those snapshots.
- Browser and intermediary HTTP caches are not explicitly disabled for API responses.

Invalidating one key cannot guarantee freshness across replicas, restarts, or all data paths.

## Decision

Remove all caches whose contents originate from Warwick and make Warwick the sole source of truth for those reads. Add a destructive, backup-gated migration that drops `session_checkins` and `attendance_reports`; these tables are documented as warm replicas, not business records.

Keep only state that is not a Warwick data cache:

- Warwick authentication cookies and session-pool state required to make requests.
- Rate limiter state and HTTP connection pooling.
- QR-room runtime state and its durable `rooms` table.
- Teacher favourites and saved dashboard views.
- React/Zustand state used to render the current response; it is refreshed by request, focus, polling, and WebSocket reconnect paths.

## Target read flow

```text
HTTP request
  -> ClassroomClient live fetch
  -> Warwick session pool
  -> parse and return response

Attendance report
  -> fetch course detail live
  -> fetch every session live through bounded live fetcher
  -> compute report
  -> return report; do not store it locally
```

A failed live read returns an error. The application must not return the last successful response as a fallback.

## API freshness contract

All JSON API responses receive `Cache-Control: no-store, no-cache, must-revalidate, max-age=0`, `Pragma: no-cache`, and `Expires: 0`. Frontend API requests use `cache: 'no-store'`. This covers browser, proxy, and service-worker-style HTTP caching even though the application has no service worker today.

## Safety constraints

- Preserve existing Warwick session-pool isolation, rate limiting, request timeouts, and bounded report concurrency.
- Remove background data workers so a user request is the only trigger for a Warwick data read.
- Keep report singleflight only if it is explicitly treated as in-flight request coalescing, not a retained result. The preferred implementation removes it so every report request owns a live computation.
- Do not edit historical migrations `004` through `007`; add migration `008` so deployed databases have an auditable schema transition.
- Take a PostgreSQL backup before applying migration `008`; the down migration can recreate empty compatibility tables, but dropped cache contents cannot be recovered without the backup.

## Success criteria

1. Two sequential reads of the same upstream resource return changed upstream data without waiting for a TTL or invalidation.
2. A live read failure returns an error and never returns an older snapshot.
3. No production Go code references the removed cache package, cache tables, stale-while-revalidate, prewarming, or report hydration.
4. API and frontend cache-control contracts prevent intermediary reuse.
5. Rooms, favourites, saved views, authentication, rate limiting, polling, and reconnect behavior remain functional.
6. Backend tests, frontend tests, race tests, lint, build, migration tests, and the live-source integration suite pass.
