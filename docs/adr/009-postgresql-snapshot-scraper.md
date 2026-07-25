# ADR 009: PostgreSQL snapshot scraper

- Status: Accepted
- Date: 2026-07-26
- Supersedes for teacher reads: `docs/superpowers/specs/2026-07-22-remove-upstream-data-caching-design.md`

## Decision

Teacher-facing Warwick reads may use a typed, eventually consistent snapshot
subsystem. PostgreSQL is the only durable snapshot and coordination store.
Snapshots contain canonical JSON produced from existing domain types; raw
responses, HTML, screenshots, avatar bytes, cookies, credentials, authorization
headers, and session-pool state are never persisted.

Targets use the full identity `(host, kind, parent_key, resource_key)`. Workers
claim targets with expiring leases, and every claim increments a fencing
generation. A completion is accepted only for the current generation.
PostgreSQL-backed host permits enforce Warwick rate and concurrency limits
across all application instances. The scheduler owns claims, permits, retry
timing, prefetch, and concurrency; a fetch performs one bounded attempt.

Freshness is based on the last successful validation. Changed, unchanged, and
HTTP 304 results advance `validation_seq` and `last_validated_at`; only changed
canonical content creates a new version. Conditional validators are sent only
when one response represents the complete typed target. Paginated profiles
remain unconditional.

Committed version notifications contain metadata only and are emitted through
PostgreSQL `LISTEN/NOTIFY` after commit. REST remains the payload source and
polling remains a repair path. QR-room fetching and its TTL stay owned by
`RoomManager`. Attendance reports remain derived and are never persisted.
Check-in writes go to Warwick first and only schedule snapshot reconciliation.

`SCRAPER_ENABLED` and `SNAPSHOT_READS_ENABLED` default to false. The live read
provider remains available as the system-wide rollback path for this release;
request-level live overrides are not part of snapshot mode.

## Preflight evidence

- Toolchain: Go 1.26.3.
- WebSocket dependency: `nhooyr.io/websocket` 1.8.17 is retained for this
  rollout; a dependency migration is separate work.
- Shared transport: `internal/warwick/transport.go` already centralizes the
  process transport and session-pool clients reuse it.
- Baseline backend: `go test ./... -count=1` and `go vet ./...` passed.
- Baseline frontend: 135 tests and the Vite production build passed.
- Baseline lint: `npm run lint` failed because no ESLint configuration existed;
  the snapshot rollout will add the missing configuration and verify it.
- Profile snapshots are assembled from paginated requests, so conditional
  validators are disabled for that kind.

## Consequences

Teacher reads become boundedly stale and survive upstream outages until their
maximum serve age. The cost is additional PostgreSQL coordination state,
retention work, and operational monitoring. At-least-once execution remains
safe through target identity, lease fencing, content hashes, transactional
current pointers, and idempotent `(target_id, lease_generation)` runs.
