# Warwick PostgreSQL Snapshot Scraper — Production Implementation Plan

> **Document status:** Replacement plan for the earlier snapshot-scraper implementation draft.  
> **Audience:** Agentic coding workers and senior reviewers.  
> **Execution style:** Implement task-by-task with test-first checkpoints. Each implementation step uses checkbox syntax (`- [ ]`).  
> **Primary rule:** The scheduler/dispatcher owns admission, concurrency, and rate limits. Fetch workers perform one bounded attempt and never create independent worker pools.

## Executive summary

**Goal:** Add an adaptive, host-aware Warwick scraper that stores typed snapshots durably in PostgreSQL and announces committed changes through the existing WebSocket gateway, without object storage, raw-response persistence, or persisted authentication cookies.

**Architecture:** Keep `internal/warwick.ClassroomClient` as the only authenticated upstream adapter. Move teacher reads behind a PostgreSQL snapshot provider. A scheduler claims typed targets with expiring leases and fencing generations, obtains a cluster-wide Warwick host permit, fetches through the existing session pool and shared `net/http.Transport`, canonicalizes typed data, commits the last-good snapshot transactionally, discovers child targets, and emits compact post-commit metadata notifications. QR-room fetching remains owned by `RoomManager` because QR expiration is a separate real-time TTL contract.

**Target runtime:** Go 1.26 on the latest supported security patch, PostgreSQL 16, `pgx/v5`, Chi, Prometheus, React 18, Zustand, and Vitest. Keep the current WebSocket package during this change unless the repository has already approved migration to the maintained `github.com/coder/websocket` module; do not combine a dependency migration with the snapshot rollout without a separate compatibility commit.

### Refinements introduced by this plan

This version corrects several production risks in the original draft:

1. **Cluster-wide host admission:** PostgreSQL-backed host permits enforce aggregate rate and concurrency across always-on instances and overlapping serverless ticks. An in-process limiter alone is not authoritative in a multi-instance deployment.
2. **Lease fencing:** Every target claim increments `lease_generation`. Commits verify the exact generation, preventing a stale worker from committing after its lease was reclaimed.
3. **Validation-based freshness:** Freshness and expiry are based on `last_validated_at`, not the creation time of the current content version. A successful `304 Not Modified` refreshes freshness without creating a new version.
4. **Refresh coalescing by validation sequence:** Synchronous cold refresh waits for `validation_seq` to advance, not snapshot version. This works when the result is unchanged or `304`.
5. **Complete target identity:** Resource identity is `(host, kind, parent_key, resource_key)`, preventing parent-key ambiguity.
6. **Safe conditional requests:** `ETag`/`Last-Modified` are used only when one validator covers the complete typed resource. Paginated or multi-request aggregates remain unconditional unless that guarantee is proven.
7. **No retry amplification:** The scheduler owns transient retries. `ClassroomClient` may perform one coordinated authentication renewal, but it must not independently retry generic network or `5xx` failures.
8. **Bounded prefetch:** The scheduler claims only enough targets for available execution slots, avoiding long-lived idle leases.
9. **Version-aware WebSocket repair:** Initial state sync and committed events are deduplicated by resource version. Polling remains the repair path for dropped notifications.
10. **Operational hardening:** The plan adds body limits, relational config validation, batched pruning, query-plan checks, secure status output, profiling, and rollout rollback criteria.
11. **Idempotent ambiguous retries:** A unique `(target_id, lease_generation)` run key and permit key make database completion and permit acquisition safe to retry after connection ambiguity.

---

## 1. Project context

The repository is a Go/React attendance dashboard for Warwick/Humantix:

- `cmd/server/main.go` starts the HTTP server and managed background runtime.
- `internal/warwick` owns Warwick login, isolated authenticated sessions, direct HTTP calls, parsing, and upstream report inputs.
- `internal/service/teacher.go` is the teacher-facing application service.
- `internal/service/room_manager.go` owns QR-room lifecycle and room events.
- `internal/api` exposes REST endpoints and `/ws`.
- `internal/db` contains PostgreSQL repositories and embedded migrations.
- `web/src/hooks/useWebSocket.js` consumes the current room-event stream.

Migration `008` and `docs/superpowers/specs/2026-07-22-remove-upstream-data-caching-design.md` removed the former `session_checkins` and `attendance_reports` replicas. This plan intentionally changes the live-read-only product contract, but it does **not** restore those tables or their previous stale-while-revalidate behavior.

The new subsystem is explicit and typed:

```text
Warwick authenticated HTTP
        ↓
ClassroomClient / SnapshotSource
        ↓
cluster-wide host admission
        ↓
typed canonicalization
        ↓
transactional PostgreSQL snapshots
        ↓
SnapshotProvider / REST
        ↓
metadata-only WebSocket notification
```

The documentation, integration tests, feature flags, and rollback instructions must change in the same release. Leaving the old live-only contract active in documentation after enabling snapshot reads is a correctness defect.

---

## 2. Required invariants and non-goals

### 2.1 Required invariants

1. PostgreSQL is the only durable snapshot store. Do not add S3, Railway buckets, filesystem persistence, Redis, Kafka, or a browser-rendering queue.
2. Persist parsed domain JSON only. Do not persist raw HTML, raw Warwick JSON, response bodies, screenshots, QR images, login responses, or downloaded avatar bytes.
3. Never persist `ASP.NET_SessionId`, Warwick credentials, request `Cookie` headers, authorization headers, or session-pool state.
4. Continue using the shared `http.Transport` and Warwick session pool. Never create a new `http.Client` or `http.Transport` per scrape.
5. The scheduler/dispatcher owns admission, rate limiting, target prefetch, and concurrency. A scrape attempt must not create an independent worker pool.
6. Cluster-wide Warwick rate and concurrency limits are authoritative in PostgreSQL. Local process state may optimize waiting but may not increase the database-authorized limit.
7. A failed scrape never overwrites the last successful content version or advances `validation_seq`.
8. A successful changed, unchanged, or `304` result advances `last_validated_at` and `validation_seq`.
9. New snapshot versions are created only when canonical content changes.
10. Publish a snapshot event only for a committed new version and only after the database transaction commits.
11. WebSocket events contain metadata only. Clients refetch payloads through REST.
12. A last-good snapshot may be served while overdue but inside its maximum serve age. After that, return `503 snapshot_expired`.
13. QR-room fetching remains in `RoomManager`; the snapshot scheduler must never create QR targets.
14. Attendance reports remain derived data. Compute them from course/session snapshots and do not add an `attendance_reports` table.
15. Check-in toggles write to Warwick first. Never directly patch snapshot JSON after a toggle.
16. Target claim commits use fencing generation, not worker ID alone.
17. All queues, goroutine counts, response bodies, JSON payloads, event buffers, and database batches are bounded.
18. Metrics use bounded labels. Full resource keys belong in structured logs/traces, never Prometheus labels.
19. The application must remain correct with multiple instances, overlapping serverless ticks, worker crashes, lease expiry, and listener reconnects.
20. Database permit acquisition and attempt completion must be idempotent for the same `(target_id, lease_generation)`.
21. The implementation must honor the authorized upstream access contract and must not add bypass behavior for access controls.

### 2.2 Non-goals

- General-purpose internet crawling.
- Browser automation or JavaScript rendering.
- Raw-response replay.
- Exactly-once distributed execution.
- Persisted login sessions.
- Querying individual students directly from JSONB for product behavior.
- Replacing the existing QR-room TTL system.
- Migrating every WebSocket dependency in the same feature unless separately approved.
- Removing live Warwick reads before the snapshot rollout observation window succeeds.

### 2.3 Delivery semantics

The queue is **at least once**. Correctness comes from:

```text
unique target identity
+ lease_generation fencing
+ canonical content hash
+ transactional current pointer update
+ unique attempt key `(target_id, lease_generation)`
+ idempotent discovered-target upsert
```

Do not attempt distributed exactly-once execution.

---

## 3. Resource model and typed contracts

### 3.1 Target identities

Use these exact identities:

| Kind | `resource_key` | `parent_key` | Payload |
| --- | --- | --- | --- |
| `course_catalog` | `catalog` | empty | `[]domain.CourseSummary` |
| `course_detail` | Warwick course ID | empty | `domain.CourseDetail` |
| `session_detail` | Warwick session ID | Warwick course ID | `domain.SessionDetail` |
| `student_profiles` | `profiles` | empty | `[]domain.StudentProfile` |

The database uniqueness key is:

```text
(host, kind, parent_key, resource_key)
```

Do not assume that `resource_key` alone is globally unique across parent resources.

### 3.2 Canonical snapshot meaning

A canonical snapshot is an existing typed domain value that has been:

1. copied so caller-owned values are not mutated;
2. normalized for list semantics;
3. sorted by stable domain keys;
4. normalized for timestamps where applicable;
5. serialized once to JSON;
6. hashed with SHA-256 over those exact serialized bytes.

Canonicalization is **not** one row per student. One Warwick resource is replaced atomically. JSONB keeps the current product model decoupled from upstream field-by-field migrations while PostgreSQL provides durable current state and bounded history.

### 3.3 Stable ordering and normalization

At minimum:

- catalog: `CourseID`, then `Name`;
- course sessions: `SessionNumber`, then `SessionID`;
- session students: `StudentID`, then `Name`;
- profiles: `StudentGuid`, then `StudentID`;
- all semantically-list fields: normalize `nil` to empty slices when `null` and `[]` mean the same product state;
- timestamps: normalize to UTC and remove monotonic clock data before serialization;
- map fields: either convert to a typed stable representation or add explicit recursive canonicalization tests.

Do not remove fields merely to reduce change frequency unless a product decision explicitly marks them as non-semantic.

### 3.4 Domain contracts

Create `internal/domain/snapshot.go` with contracts equivalent to:

```go
package domain

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "time"
)

type SnapshotKind string

const (
    SnapshotCourseCatalog   SnapshotKind = "course_catalog"
    SnapshotCourseDetail    SnapshotKind = "course_detail"
    SnapshotSessionDetail   SnapshotKind = "session_detail"
    SnapshotStudentProfiles SnapshotKind = "student_profiles"
)

type TargetRef struct {
    Host        string
    Kind        SnapshotKind
    ResourceKey string
    ParentKey   string
}

var (
    ErrSnapshotNotFound = errors.New("snapshot not found")
    ErrSnapshotExpired  = errors.New("snapshot expired")
    ErrNotModified      = errors.New("upstream not modified")
    ErrLeaseLost        = errors.New("scrape lease lost")
    ErrHostPaused       = errors.New("scrape host paused")
)

type ConditionalHeaders struct {
    ETag         string
    LastModified string
}

type ScrapeTarget struct {
    ID                  int64
    Ref                 TargetRef
    Attributes          json.RawMessage
    MissingCount        int
    CurrentContentHash  [32]byte
    HasCurrentSnapshot  bool
    CurrentVersion      int64
    ValidationSeq       int64
    CurrentInterval     time.Duration
    MinInterval         time.Duration
    MaxInterval         time.Duration
    MaxServeAge         time.Duration
    NextRunAt           time.Time
    LastValidatedAt     *time.Time
    ConsecutiveFailures int
    RecentChanges       []bool
    Conditional         ConditionalHeaders
    LeaseOwner          string
    LeaseGeneration     int64
    LeaseExpiresAt      *time.Time
}

type TargetSeed struct {
    Ref             TargetRef
    Attributes      json.RawMessage
    InitialInterval time.Duration
    MinInterval     time.Duration
    MaxInterval     time.Duration
    MaxServeAge     time.Duration
    NextRunAt       time.Time
}

type Snapshot struct {
    ID               int64           `json:"-"`
    TargetID         int64           `json:"-"`
    Ref              TargetRef       `json:"-"`
    Version          int64           `json:"version"`
    ValidationSeq    int64           `json:"validation_seq"`
    ContentHash      [32]byte         `json:"-"`
    Payload          json.RawMessage  `json:"-"`
    ContentFetchedAt time.Time        `json:"content_fetched_at"`
    ValidatedAt      time.Time        `json:"validated_at"`
    NextRunAt        time.Time        `json:"next_run_at"`
    MaxServeAge      time.Duration    `json:"-"`
}

func (s Snapshot) Stale(now time.Time) bool {
    return now.After(s.NextRunAt)
}

func (s Snapshot) Expired(now time.Time) bool {
    if s.MaxServeAge <= 0 {
        return true
    }
    return now.Sub(s.ValidatedAt) > s.MaxServeAge
}

type SnapshotMetadata struct {
    Kind          SnapshotKind `json:"kind"`
    ResourceKey   string       `json:"resource_key"`
    ParentKey     string       `json:"parent_key"`
    Version       int64        `json:"version"`
    ValidationSeq int64        `json:"validation_seq"`
    ValidatedAt   time.Time    `json:"validated_at"`
    Stale         bool         `json:"stale"`
}

type UpstreamStatusError struct {
    StatusCode int
    RetryAfter time.Duration
}

func (e *UpstreamStatusError) Error() string {
    return fmt.Sprintf("upstream returned HTTP %d", e.StatusCode)
}

type SessionFetcher interface {
    FetchSessionForReport(
        context.Context,
        string, // courseID
        string, // sessionID
    ) (*SessionDetail, error)
}
```

Use helper methods for zero-hash handling rather than comparing uninitialized arrays throughout business code.

---

## 4. Freshness, retry, and adaptive policy

### 4.1 Resource policy

Start with these values:

| Resource | Initial | Minimum | Maximum | Maximum serve age |
| --- | ---: | ---: | ---: | ---: |
| Course catalog | 1 hour | 15 minutes | 24 hours | 48 hours |
| Course detail | 1 hour | 15 minutes | 24 hours | 48 hours |
| Active session detail | 5 minutes | 1 minute | 30 minutes | 2 hours |
| Not-started session detail | 1 hour | 15 minutes | 12 hours | 24 hours |
| Finished session detail | 12 hours | 1 hour | 30 days | 45 days |
| Student profiles | 24 hours | 6 hours | 7 days | 14 days |

### 4.2 Successful result policy

```text
changed:
    validation_seq += 1
    reset consecutive failures
    append true to latest-10 change history
    interval = max(minimum, current / 2)
    create a new content version

unchanged:
    validation_seq += 1
    reset consecutive failures
    append false to latest-10 change history
    if latest 10 are all unchanged:
        interval = min(maximum, current * 2)
    else:
        interval = min(maximum, current * 1.5)
    do not create a content version

HTTP 304:
    same scheduling and freshness behavior as unchanged
    merge response validators without erasing a stored validator when the 304 omits it
    do not create a content version
```

Schedule ordinary success with deterministic bounded jitter to avoid synchronized waves:

```text
next_run_at = validated_at + interval + jitter
jitter range = ±10% of interval
```

Use a seeded/injected random source for deterministic tests. Never add jitter to an explicit `Retry-After` deadline.

### 4.3 Failure policy

Classify errors once at the scheduler/coordinator boundary:

| Class | Examples | Target action | Host action |
| --- | --- | --- | --- |
| `rate_limited` | `429` | retry at host resume | decrease rate/concurrency; pause |
| `transient_error` | DNS/connect/reset/timeout, `408`, `425`, `500`, `502`, `503`, `504` | exponential full-jitter retry | AIMD decrease after threshold |
| `auth_error` | authentication still invalid after one coordinated renewal | retain last-good; retry after host pause | pause 15 minutes |
| `not_found` | `404`, `410` | parent-aware missing handling; no hot retry | none |
| `permanent_error` | other stable `4xx` | long target pause; alert | none |
| `invalid_payload` | wrong content type, decode failure, canonical payload limit | pause target 1 hour; retain last-good | none |
| `canceled` | application shutdown before attempt completion | release lease; do not increment failure | none |

Transient delay before jitter:

```text
1m, 2m, 4m, 8m, ... capped at 1h
```

Use full jitter in `[0, cap]`, except when `Retry-After` supplies a later valid deadline.

### 4.4 `429` policy

```text
first 429:
    honor valid Retry-After
    otherwise pause host for 15 minutes
    current_rps = max(0.25, current_rps / 2)
    current_concurrency = 1

repeated 429 within one hour:
    pause host for max(valid Retry-After, 60 minutes)

20 consecutive healthy observations:
    current_rps += 0.25 toward baseline
    current_concurrency += 1 toward baseline
    reset healthy streak after applying an increase
```

Do not restore the maximum immediately after a pause.

### 4.5 Retry ownership

- A scheduler attempt represents one generic upstream attempt.
- `ClassroomClient` may renew authentication once, coalesced so concurrent requests do not create a login stampede.
- `ClassroomClient` must not retry generic DNS, network, timeout, `429`, or `5xx` failures.
- The scheduler decides all transient requeues.
- Only idempotent GET-like reads are retried.
- Check-in writes are not generically retried unless the existing write contract has an explicit idempotency mechanism.

---

## 5. Runtime and deployment model

### 5.1 Always-on mode

With `SERVERLESS_ENABLED=false` and `SCRAPER_ENABLED=true`:

- run one `SnapshotScheduler` managed worker per application instance;
- instances may overlap safely because target leases and host permits are database-coordinated;
- each scheduler claims only enough targets for available slots;
- a context-aware timer drives the next poll; do not use `time.Sleep` in the run loop;
- shutdown stops new claims, cancels active requests, releases unstarted leases, and waits for bounded active work.

### 5.2 Serverless mode

With `SERVERLESS_ENABLED=true`:

- the process cannot wake itself;
- `POST /api/internal/scraper/tick` runs a bounded batch;
- the deployment scheduler must invoke the endpoint;
- overlapping ticks remain safe through leases and host permits;
- the endpoint timeout must be shorter than the platform hard timeout;
- `RunDue(limit)` counts attempted target executions, not only successful executions.

### 5.3 Cold reads

A cold teacher read may create or schedule a missing target and invoke the same refresh path. It must not bypass:

- target claim;
- lease generation;
- cluster-wide host permit;
- fetch timeout/body limit;
- canonicalization;
- transactional commit;
- error classification.

Concurrent cold misses coalesce by observing `validation_seq`.

### 5.4 Rollout flags

```text
SCRAPER_ENABLED=false
SNAPSHOT_READS_ENABLED=false
```

Rollout sequence:

```text
migration 009
  -> enable scraper writes
  -> verify target coverage and host health
  -> enable snapshot reads
  -> observe one full policy window
  -> retain live rollback path
  -> remove request-level live override
```

Do not remove the system-wide live rollback flag in this release.

---

## 6. Low-level HTTP and memory design

### 6.1 Shared transport

Reuse the repository's existing transport. If it lacks equivalent protection, clone and configure one transport at bootstrap:

```go
transport := http.DefaultTransport.(*http.Transport).Clone()
transport.MaxIdleConns = 100
transport.MaxIdleConnsPerHost = 8
transport.MaxConnsPerHost = 8 // hard ceiling above adaptive operating limit
transport.IdleConnTimeout = 90 * time.Second
transport.TLSHandshakeTimeout = 10 * time.Second
transport.ResponseHeaderTimeout = 15 * time.Second
transport.ExpectContinueTimeout = time.Second
transport.ForceAttemptHTTP2 = true
transport.MaxResponseHeaderBytes = 1 << 20
```

Rules:

- one transport per application process;
- one shared client/session infrastructure, not one client per scrape;
- transport `MaxConnsPerHost` is a safety ceiling, not the operating rate limiter;
- use per-attempt context deadlines as the total timeout; prefer `http.Client.Timeout == 0` if the existing client can safely adopt that model;
- validate redirect destinations using the existing destination policy;
- close every body on every path;
- read successful bodies to completion within a bound to preserve connection reuse;
- for error responses, drain only a small bounded amount before close and never include body content in logs/errors.

### 6.2 Response and canonical payload limits

Add configuration:

```text
SCRAPER_RESPONSE_BODY_LIMIT=16MiB
SCRAPER_CANONICAL_PAYLOAD_LIMIT=16MiB
```

Hard cap both at 50 MiB during configuration validation.

- Apply the response limit to the decoded stream, not only `Content-Length`.
- Reject unsupported content types before expensive decoding when possible.
- Do not truncate successful payloads.
- Treat limit overflow as `invalid_payload`.
- `bytes_read` means decoded response bytes unless a separate wire-byte metric is explicitly available.

### 6.3 Fetch/parse separation

The authenticated Warwick adapter may parse the target response because it already owns endpoint-specific decoding, but expensive report calculation, database writes, and unrelated parsing must not hold a host permit longer than necessary.

Target attempt phases:

```text
claim target
  -> acquire host permit
  -> HTTP fetch + endpoint decode
  -> release host permit
  -> canonicalize/hash
  -> transactional commit/discovery
```

Release the host permit immediately after the complete typed upstream resource is available. PostgreSQL commit time must not consume an upstream concurrency slot.

### 6.4 Authentication behavior

- Keep sessions isolated according to the current pool contract.
- Coalesce login renewal per session with a lock or singleflight-style mechanism.
- Never log cookies, login bodies, or credentials.
- After one auth renewal, return a typed auth error; do not loop.
- A host-wide auth outage pauses new attempts while preserving current snapshots.

### 6.5 Conditional requests

Send `If-None-Match` and `If-Modified-Since` only when the validator covers the **entire target representation**.

Safe examples:

- one endpoint response fully defines a course catalog snapshot;
- one endpoint response fully defines a session detail snapshot.

Unsafe by default:

- a target assembled from multiple independent requests;
- paginated profiles where the first page validator does not guarantee all pages are unchanged;
- helper methods whose first request returns `304` but later requests could change the final typed value.

For unsafe targets, do a normal fetch until upstream semantics are verified and covered by integration tests.

A `304` without an existing current snapshot is an invalid upstream/protocol state and must not create an empty snapshot.

### 6.6 Instrumentation

Record total attempt latency for every attempt. Sample detailed `httptrace` timing, for example 1%, for:

- DNS;
- connect;
- TLS;
- connection-pool wait;
- TTFB;
- reused connection state.

Do not attach full URLs to metrics. Place target references only in sampled traces and sanitized structured logs.

---
## 7. PostgreSQL data model and coordination

### 7.1 Why PostgreSQL coordinates both snapshots and admission

Target leases alone prevent duplicate work for one target, but they do not enforce one aggregate Warwick rate across multiple application instances. A per-process `rate.Limiter` would multiply the configured rate by the number of instances.

At Warwick's starting rate of one request per second, a short PostgreSQL transaction per upstream admission is negligible compared with network latency and gives simple cluster-wide correctness. If measured throughput later reaches a level where the host-state row becomes a bottleneck, move to stable host ownership/sharding as a separate design.

### 7.2 Migration 009 schema

Create `internal/db/migrations/009_create_scrape_snapshots.up.sql` with the following structure. Adapt naming style to repository conventions, but preserve the semantics and constraints.

```sql
CREATE TABLE scrape_host_state (
    host TEXT PRIMARY KEY,

    baseline_requests_per_second NUMERIC(8,3) NOT NULL
        CHECK (baseline_requests_per_second BETWEEN 0.25 AND 5),
    current_requests_per_second NUMERIC(8,3) NOT NULL
        CHECK (current_requests_per_second BETWEEN 0.25 AND 5),

    burst SMALLINT NOT NULL CHECK (burst BETWEEN 1 AND 5),
    available_tokens NUMERIC(12,6) NOT NULL
        CHECK (available_tokens >= 0),
    tokens_updated_at TIMESTAMPTZ NOT NULL,

    baseline_concurrency SMALLINT NOT NULL
        CHECK (baseline_concurrency BETWEEN 1 AND 4),
    current_concurrency SMALLINT NOT NULL
        CHECK (current_concurrency BETWEEN 1 AND 4),

    consecutive_429s INTEGER NOT NULL DEFAULT 0
        CHECK (consecutive_429s >= 0),
    last_429_at TIMESTAMPTZ,
    healthy_streak INTEGER NOT NULL DEFAULT 0
        CHECK (healthy_streak >= 0),
    paused_until TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CHECK (current_requests_per_second <= 5),
    CHECK (current_concurrency <= 4),
    CHECK (available_tokens <= burst)
);

CREATE TABLE scrape_targets (
    id BIGSERIAL PRIMARY KEY,
    host TEXT NOT NULL REFERENCES scrape_host_state(host),

    kind TEXT NOT NULL CHECK (kind IN (
        'course_catalog',
        'course_detail',
        'session_detail',
        'student_profiles'
    )),
    resource_key TEXT NOT NULL,
    parent_key TEXT NOT NULL DEFAULT '',
    attributes JSONB NOT NULL DEFAULT '{}'::jsonb,

    missing_count SMALLINT NOT NULL DEFAULT 0
        CHECK (missing_count BETWEEN 0 AND 2),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,

    current_interval_seconds INTEGER NOT NULL
        CHECK (current_interval_seconds > 0),
    min_interval_seconds INTEGER NOT NULL
        CHECK (min_interval_seconds > 0),
    max_interval_seconds INTEGER NOT NULL
        CHECK (max_interval_seconds >= min_interval_seconds),
    max_serve_age_seconds INTEGER NOT NULL
        CHECK (max_serve_age_seconds >= max_interval_seconds),

    next_run_at TIMESTAMPTZ NOT NULL,
    last_attempt_at TIMESTAMPTZ,
    last_validated_at TIMESTAMPTZ,
    last_content_change_at TIMESTAMPTZ,
    validation_seq BIGINT NOT NULL DEFAULT 0
        CHECK (validation_seq >= 0),

    consecutive_failures INTEGER NOT NULL DEFAULT 0
        CHECK (consecutive_failures >= 0),
    recent_changes BOOLEAN[] NOT NULL DEFAULT ARRAY[]::BOOLEAN[],

    etag TEXT,
    last_modified TEXT,
    cache_control TEXT,

    current_snapshot_id BIGINT,
    current_version BIGINT NOT NULL DEFAULT 0
        CHECK (current_version >= 0),
    current_content_hash BYTEA,

    lease_owner TEXT,
    lease_generation BIGINT NOT NULL DEFAULT 0
        CHECK (lease_generation >= 0),
    lease_expires_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (host, kind, parent_key, resource_key),
    CHECK (cardinality(recent_changes) <= 10),
    CHECK (current_content_hash IS NULL OR octet_length(current_content_hash) = 32),
    CHECK ((lease_owner IS NULL) = (lease_expires_at IS NULL)),
    CHECK (
        (current_snapshot_id IS NULL AND current_version = 0 AND current_content_hash IS NULL)
        OR
        (current_snapshot_id IS NOT NULL AND current_version > 0 AND current_content_hash IS NOT NULL)
    )
);

CREATE TABLE scrape_runs (
    id BIGSERIAL PRIMARY KEY,
    target_id BIGINT NOT NULL REFERENCES scrape_targets(id) ON DELETE CASCADE,
    worker_id TEXT NOT NULL,
    lease_generation BIGINT NOT NULL CHECK (lease_generation > 0),

    outcome TEXT NOT NULL CHECK (outcome IN (
        'changed',
        'unchanged',
        'not_modified',
        'rate_limited',
        'auth_error',
        'transient_error',
        'not_found',
        'permanent_error',
        'invalid_payload',
        'canceled'
    )),

    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ NOT NULL,
    http_status INTEGER,
    duration_ms BIGINT NOT NULL CHECK (duration_ms >= 0),
    bytes_read BIGINT NOT NULL DEFAULT 0 CHECK (bytes_read >= 0),
    error_kind TEXT,
    error_message TEXT CHECK (length(error_message) <= 512),
    next_run_at TIMESTAMPTZ NOT NULL,
    validation_seq_after BIGINT CHECK (validation_seq_after >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (target_id, lease_generation),
    CHECK (finished_at >= started_at)
);

CREATE TABLE scrape_snapshots (
    id BIGSERIAL PRIMARY KEY,
    target_id BIGINT NOT NULL REFERENCES scrape_targets(id) ON DELETE CASCADE,
    run_id BIGINT REFERENCES scrape_runs(id) ON DELETE SET NULL,
    version BIGINT NOT NULL CHECK (version > 0),
    content_hash BYTEA NOT NULL CHECK (octet_length(content_hash) = 32),
    payload JSONB NOT NULL,
    content_fetched_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (target_id, version)
);

ALTER TABLE scrape_targets
    ADD CONSTRAINT scrape_targets_current_snapshot_fk
    FOREIGN KEY (current_snapshot_id)
    REFERENCES scrape_snapshots(id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE scrape_host_permits (
    id BIGSERIAL PRIMARY KEY,
    host TEXT NOT NULL REFERENCES scrape_host_state(host) ON DELETE CASCADE,
    target_id BIGINT NOT NULL REFERENCES scrape_targets(id) ON DELETE CASCADE,
    worker_id TEXT NOT NULL,
    lease_generation BIGINT NOT NULL CHECK (lease_generation > 0),
    acquired_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    UNIQUE (target_id, lease_generation),
    CHECK (expires_at > acquired_at)
);

CREATE INDEX idx_scrape_targets_due
    ON scrape_targets (next_run_at, id)
    WHERE enabled = TRUE;

CREATE INDEX idx_scrape_targets_lease_expiry
    ON scrape_targets (lease_expires_at)
    WHERE lease_expires_at IS NOT NULL;

CREATE INDEX idx_scrape_targets_parent
    ON scrape_targets (host, kind, parent_key)
    WHERE enabled = TRUE;

CREATE INDEX idx_scrape_runs_target_finished
    ON scrape_runs (target_id, finished_at DESC);

CREATE INDEX idx_scrape_runs_finished
    ON scrape_runs (finished_at);

CREATE INDEX idx_scrape_snapshots_target_fetched
    ON scrape_snapshots (target_id, content_fetched_at DESC);

CREATE INDEX idx_scrape_snapshots_target_hash
    ON scrape_snapshots (target_id, content_hash);

CREATE UNIQUE INDEX idx_scrape_snapshots_run_unique
    ON scrape_snapshots (run_id)
    WHERE run_id IS NOT NULL;

CREATE INDEX idx_scrape_host_permits_host_expiry
    ON scrape_host_permits (host, expires_at);
```

The hash index is intentionally non-unique. Content may change A → B → A, and the third observation is a new historical version.

### 7.3 Down migration

```sql
DROP TABLE IF EXISTS scrape_host_permits;
ALTER TABLE scrape_targets
    DROP CONSTRAINT IF EXISTS scrape_targets_current_snapshot_fk;
DROP TABLE IF EXISTS scrape_snapshots;
DROP TABLE IF EXISTS scrape_runs;
DROP TABLE IF EXISTS scrape_targets;
DROP TABLE IF EXISTS scrape_host_state;
```

### 7.4 Target claim with fencing generation

Claim only a bounded batch. Use one atomic statement:

```sql
WITH due AS (
    SELECT target.id
    FROM scrape_targets AS target
    JOIN scrape_host_state AS host_state
      ON host_state.host = target.host
    WHERE target.enabled = TRUE
      AND target.next_run_at <= $1
      AND (
          target.lease_expires_at IS NULL
          OR target.lease_expires_at <= $1
      )
      AND (
          host_state.paused_until IS NULL
          OR host_state.paused_until <= $1
      )
    ORDER BY target.next_run_at, target.id
    FOR UPDATE OF target SKIP LOCKED
    LIMIT $2
)
UPDATE scrape_targets AS target
SET lease_owner = $3,
    lease_generation = target.lease_generation + 1,
    lease_expires_at = $1 + make_interval(secs => $4),
    last_attempt_at = $1,
    updated_at = $1
FROM due
WHERE target.id = due.id
RETURNING target.*;
```

Validate:

- `limit > 0`;
- non-empty worker ID;
- lease duration positive;
- lease duration is greater than fetch timeout plus commit/shutdown grace;
- returned `lease_generation > 0`.

A commit validates `(target_id, lease_generation)`. Do **not** reject a commit solely because wall-clock lease expiry passed while the same generation still owns the target. If another worker reclaimed it, generation changed and the stale commit is fenced out.

### 7.5 Claim one target for refresh

Add a separate atomic `ClaimOne` query using full `TargetRef`. Do not implement `RefreshNow` by claiming an arbitrary batch and searching it in memory.

If the target is leased by another worker:

- read baseline `validation_seq`;
- wait with bounded 100–500 ms jitter;
- finish when `validation_seq` advances;
- return the other worker's terminal error if status metadata exposes one and the caller needs it;
- stop when caller context expires;
- never issue a duplicate Warwick request.

### 7.6 Cluster-wide host permit

Define:

```go
type HostPermit struct {
    ID              int64
    Host            string
    TargetID        int64
    LeaseGeneration int64
    ExpiresAt       time.Time
}

type PermitDecision struct {
    Permit   *HostPermit
    RetryAt  time.Time
    Paused   bool
}
```

`AcquireHostPermit` performs a short PostgreSQL transaction. First, look for an existing non-expired permit with the same `(target_id, lease_generation)` and return it without consuming another token. Otherwise:

1. lock the host row `FOR UPDATE`;
2. delete expired permits for that host;
3. if `paused_until > now`, return `RetryAt=paused_until`;
4. count non-expired permits;
5. if active permits reach `current_concurrency`, return the earliest permit expiry;
6. refill tokens using elapsed time and `current_requests_per_second`;
7. clamp tokens to `burst`;
8. if tokens are below one, persist the refill state and return the calculated next-token time;
9. decrement one token;
10. insert a permit with TTL `fetch_timeout + permit_grace`;
11. persist token state and commit.

Pseudo-code for token refill:

```text
elapsed_seconds = max(0, now - tokens_updated_at)
refilled = min(burst, available_tokens + elapsed_seconds * current_rps)

if refilled < 1:
    retry_after = (1 - refilled) / current_rps
else:
    available_tokens = refilled - 1
```

Release the permit in a `defer` immediately after the typed fetch completes. `ReleaseHostPermit` deletes by permit ID and should be idempotent.

A worker crash leaks a permit only until its TTL. A cleanup query runs during every acquisition and during daily maintenance.

### 7.7 Host observations

Persist AIMD-like changes under a host-row lock. Do not mutate host state from unsynchronized process-local callbacks.

```go
type HostObservation struct {
    Host       string
    Outcome    string
    StatusCode int
    RetryAfter time.Duration
    Latency    time.Duration
    ObservedAt time.Time
}
```

For a `429`, refill token state first, then lower rate/concurrency and clamp tokens to the new burst. For healthy recovery, never exceed configured baselines.

### 7.8 Transactional completion

Use one repository method:

```go
type CommitInput struct {
    TargetID             int64
    WorkerID             string
    LeaseGeneration      int64
    Outcome              string
    StartedAt            time.Time
    FinishedAt           time.Time
    HTTPStatus           *int
    BytesRead            int64
    ErrorKind            string
    ErrorMessage         string
    NextRunAt            time.Time
    CurrentInterval      time.Duration
    ConsecutiveFailures  int
    RecentChanges        []bool
    ValidatedAt          *time.Time
    ValidationSeqAfter   *int64
    ETag                  string
    LastModified          string
    CacheControl          string
    Changed               bool
    ContentHash           [32]byte
    Payload               json.RawMessage
    Discovered            []domain.TargetSeed
    SeenChildRefs         []domain.TargetRef
}

type CommitResult struct {
    Snapshot *domain.Snapshot
    Metadata *domain.SnapshotMetadata
}
```

Inside one `pgx.Tx`:

1. check for an existing `scrape_runs` row with `(target_id, lease_generation)` and return its already-committed result idempotently;
2. lock the target `FOR UPDATE`;
3. verify target `lease_generation` exactly matches input generation;
4. verify lease owner for diagnostics, but generation is the fencing authority;
5. insert one `scrape_runs` row;
6. on successful changed content, insert `current_version + 1` into `scrape_snapshots`;
7. on successful changed/unchanged/304, increment `validation_seq` exactly once and update `last_validated_at`;
8. on changed content, advance current snapshot pointer/hash/version and `last_content_change_at`;
9. upsert discovered targets using full identity;
10. update missing-child counts only after a successful changed parent snapshot;
11. disable a child only after two consecutive successful changed parent snapshots omit it;
12. clear target lease fields;
13. call `pg_notify('snapshot_committed', payload)` only when a new version was inserted;
14. commit.

`pg_notify` remains inside the transaction. PostgreSQL delivers it only after commit. Keep the JSON payload far below 8 KiB.

Trim error messages to 512 UTF-8-safe characters. Never include body content, cookies, credentials, or student payloads.

### 7.9 Lease release without a run

Add a token-checked `ReleaseLease` method for:

- shutdown before an attempt starts;
- host-permit wait canceled before fetch;
- scheduler dispatch failure;
- invalid local configuration discovered before upstream I/O.

It must clear the lease only when generation matches. Do not increment target failure counters for orderly shutdown.

### 7.10 Current snapshot reads

`Current(ref)` joins target and current snapshot and returns:

- current payload/version/hash;
- target `validation_seq`;
- `last_validated_at` as `ValidatedAt`;
- target `next_run_at`;
- target `max_serve_age`.

`ErrSnapshotNotFound` means no successful current version exists. A target row with only failures is still “not found” for teacher reads.

### 7.11 Batched pruning

Once per UTC day:

- delete `scrape_runs` older than configured retention in batches;
- delete expired host permits;
- delete non-current snapshots older than retention while retaining at least the latest three versions per target;
- never delete the row referenced by `current_snapshot_id`;
- cap each delete batch, for example 1,000–5,000 rows, to reduce lock duration and WAL spikes.

Use a window-function candidate query and repeat until no rows remain. Test the exact SQL against PostgreSQL, not an in-memory fake.

---

## 8. WebSocket and event contract

### 8.1 Existing room messages

Keep existing shapes unchanged:

```json
{"FullStateSync": []}
{"RoomCreated": {}}
{"RoomUpdated": {}}
{"RoomDeleted": "room-id"}
```

### 8.2 Snapshot state sync

```json
{
  "SnapshotStateSync": [
    {
      "kind": "session_detail",
      "resource_key": "session-1",
      "parent_key": "course-1",
      "version": 4,
      "validation_seq": 9,
      "validated_at": "2026-07-26T10:15:00Z",
      "stale": false
    }
  ]
}
```

### 8.3 Snapshot committed event

A committed event always means content changed and a new version exists:

```json
{
  "SnapshotCommitted": {
    "kind": "session_detail",
    "resource_key": "session-1",
    "parent_key": "course-1",
    "version": 5,
    "validation_seq": 10,
    "validated_at": "2026-07-26T10:20:00Z",
    "stale": false
  }
}
```

Do not include a redundant `changed` field. An unchanged or `304` validation creates no `SnapshotCommitted` event.

Never include payloads, student names, student IDs, cookies, response bodies, or credentials.

### 8.4 Listener initialization and reconnect

Use one dedicated `pgx.Conn`:

1. execute and commit `LISTEN snapshot_committed`;
2. query current snapshot metadata after listening is active;
3. publish or cache the initial state;
4. call `WaitForNotification(ctx)`;
5. reconnect with bounded 1s, 2s, 4s, 8s, 30s backoff;
6. after reconnect, query current metadata and reconcile versions before relying on new notifications.

This ordering handles the documented initial `LISTEN` race. Notifications are repairable hints, not durable payload delivery.

### 8.5 WebSocket connection sync ordering

On `/ws` connection:

1. subscribe to `EventHub`;
2. read room state and current snapshot metadata;
3. send `FullStateSync`;
4. send `SnapshotStateSync`;
5. enter the single writer loop;
6. discard buffered snapshot events whose version is less than or equal to the state-sync version for that resource.

The browser also keeps a version map and ignores duplicate/out-of-order events.

### 8.6 Event hub behavior

```go
type AppEvent struct {
    Type string
    Data any
}

type EventHub struct {
    publishCh chan AppEvent
    // private subscriber state
}
```

Preserve bounded buffers. A slow subscriber must not block the publisher. Count dropped events and emit a rate-limited structured warning, not one log line per drop.

Polling remains a repair path during the first rollout.

---

## 9. API behavior and freshness metadata

### 9.1 Snapshot provider read path

For each typed read:

1. call `Current(ref)`;
2. if not found, call `RefreshNow(ref)` once;
3. read `Current(ref)` again;
4. reject expired data using `ValidatedAt`;
5. decode into the exact expected domain type;
6. return data.

Do not synchronously refresh merely because a snapshot is overdue. The scheduler owns regular recrawls.

### 9.2 Error mapping

Keep the existing `ApiResponse` body contract:

```go
if errors.Is(err, domain.ErrSnapshotNotFound) {
    writeJSON(w, http.StatusServiceUnavailable,
        errorResponse("snapshot unavailable"))
    return true
}
if errors.Is(err, domain.ErrSnapshotExpired) {
    writeJSON(w, http.StatusServiceUnavailable,
        errorResponse("snapshot expired"))
    return true
}
```

Snapshot read failures are not authentication failures and must not map to `401`.

### 9.3 Compatible freshness headers

Without changing JSON response shapes, add when snapshot mode is active:

```text
X-Snapshot-Version: <version>
X-Snapshot-Validation-Seq: <seq>
X-Snapshot-Validated-At: <RFC3339>
X-Snapshot-Stale: true|false
Cache-Control: private, no-store
```

Do not expose content hashes if they are not needed by clients.

### 9.4 Reports

Reports use PostgreSQL session snapshots via:

```go
type SessionFetcher interface {
    FetchSessionForReport(
        context.Context,
        courseID string,
        sessionID string,
    ) (*domain.SessionDetail, error)
}
```

After report computation, check whether required session snapshots are overdue. Preserve existing partial-result semantics:

- missing/expired session → add `ReportError`, set `Truncated=true`;
- freshness query failure → add one bounded error, set `Truncated=true`;
- any overdue required session → set `report.Stale=true`;
- never persist the derived report.

---

## 10. Check-in write and reconciliation contract

### 10.1 Warwick-first write

The write path remains:

```text
optimistic browser state
  -> live Warwick write
  -> schedule/attempt snapshot refresh
  -> committed version event
  -> browser REST reconciliation
```

Never update PostgreSQL snapshot JSON directly.

### 10.2 Service contract

```go
type ToggleCheckinResponse struct {
    StudentID              string `json:"student_id"`
    CheckedIn              bool   `json:"checked_in"`
    NewCount               int    `json:"new_count"`
    SnapshotRefreshPending bool   `json:"snapshot_refresh_pending"`
}
```

`TeacherService.ToggleCheckin`:

1. calls the live `CheckinWriter`;
2. returns immediately on Warwick write failure;
3. marks the session target due now;
4. invokes `RefreshNow` with a 10-second child timeout;
5. reads the committed/current snapshot and verifies the desired student state when practical;
6. if the upstream read is still eventually stale, schedules another refresh in a short bounded interval and returns `SnapshotRefreshPending=true`;
7. returns success even when reconciliation is pending because the source write succeeded.

A refresh that merely advanced `validation_seq` but still does not reflect the desired toggle is not considered fully reconciled.

### 10.3 Concurrent toggles

Target lease generation serializes snapshot refresh commits. It does not serialize Warwick writes themselves unless the existing write API requires it.

Tests must cover two concurrent toggles for the same session and ensure:

- no direct snapshot patch;
- no stale-generation commit;
- final REST state converges to Warwick;
- only committed versions produce events.

---

## 11. Configuration

Add:

```go
type ScraperConfig struct {
    Enabled                  bool
    SnapshotReadsEnabled     bool
    Host                     string
    BaselineRequestsPerSecond float64
    Burst                    int
    BaselineConcurrency      int
    LeaseDuration            time.Duration
    FetchTimeout             time.Duration
    PermitGrace              time.Duration
    CommitGrace              time.Duration
    TickLimit                int
    ClaimPrefetchFactor      int
    ResponseBodyLimit        int64
    CanonicalPayloadLimit    int64
    SnapshotRetention        time.Duration
    RunRetention             time.Duration
    TriggerToken             string
    HTTPTraceSampleRate      float64
}
```

Defaults:

```text
SCRAPER_ENABLED=false
SNAPSHOT_READS_ENABLED=false
SCRAPER_REQUESTS_PER_SECOND=1
SCRAPER_BURST=1
SCRAPER_MAX_CONCURRENCY=2
SCRAPER_LEASE_DURATION=2m
SCRAPER_FETCH_TIMEOUT=30s
SCRAPER_PERMIT_GRACE=10s
SCRAPER_COMMIT_GRACE=15s
SCRAPER_TICK_LIMIT=50
SCRAPER_CLAIM_PREFETCH_FACTOR=2
SCRAPER_RESPONSE_BODY_LIMIT=16777216
SCRAPER_CANONICAL_PAYLOAD_LIMIT=16777216
SCRAPER_SNAPSHOT_RETENTION=720h
SCRAPER_RUN_RETENTION=720h
SCRAPER_HTTPTRACE_SAMPLE_RATE=0.01
```

Hard bounds:

```text
requests/second:       0.25–5
burst:                  1–5
concurrency:            1–4
lease:                  30s–10m
fetch timeout:          5s–60s
permit grace:           1s–60s
tick limit:             1–500
prefetch factor:        1–4
body/payload limit:     1MiB–50MiB
retention:              24h–2160h
httptrace sample rate:  0–1
```

Relational validation:

```text
lease_duration >= fetch_timeout + permit_grace + commit_grace
transport.MaxConnsPerHost >= configured concurrency
canonical_payload_limit <= response_body_limit * reasonable expansion factor
SERVERLESS_ENABLED && SCRAPER_ENABLED -> trigger token required
SNAPSHOT_READS_ENABLED -> scraper repository and refresher must be wired
```

Pin CI/container builds to the current supported Go 1.26 patch, not a stale initial patch.

---

## 12. Observability and operations

### 12.1 Metrics

Use fixed enums only:

```text
warwick_scrape_runs_total{kind,outcome}
warwick_scrape_duration_seconds{kind,outcome}
warwick_scrape_due_targets{kind}
warwick_scrape_active_leases
warwick_scrape_active_host_permits{host_class}
warwick_scrape_snapshot_age_seconds{kind}
warwick_scrape_validation_age_seconds{kind}
warwick_scrape_host_paused{host_class}
warwick_scrape_host_requests_per_second{host_class}
warwick_scrape_host_concurrency{host_class}
warwick_scrape_claim_conflicts_total
warwick_scrape_lease_lost_total
warwick_snapshot_websocket_events_total{kind}
warwick_snapshot_websocket_drops_total
```

Allowed `host_class` is a bounded value such as `warwick`. Never label by URL, target ID, course, session, student, worker ID, or error message.

### 12.2 Structured logs

Include, when needed:

- target kind;
- target ID;
- sanitized resource key only in logs, never metrics;
- worker ID;
- lease generation;
- run ID;
- permit ID;
- outcome;
- duration;
- HTTP status;
- retry time.

Never include student payloads, cookies, authorization headers, login responses, or upstream body excerpts.

### 12.3 Protected endpoints

```text
POST /api/internal/scraper/tick
GET  /api/internal/scraper/status
```

Require:

```http
Authorization: Bearer <SCRAPER_TRIGGER_TOKEN>
```

Rules:

- token only in header;
- use constant-time comparison after length check;
- no token logging;
- no permissive CORS;
- `tick` uses a child context shorter than platform timeout;
- status returns aggregates only: due, leased, failed, expired, validation age, host pause/rate/concurrency, and permit count;
- no payloads, cookies, names, student identifiers, or raw target attributes.

### 12.4 Database pool planning

Reserve capacity for:

- normal API traffic;
- scheduler transactions;
- one dedicated LISTEN connection;
- migration/health traffic;
- short bursts from serverless ticks.

Do not let scheduler concurrency consume every pool connection. Add a startup warning when configured `pgxpool.MaxConns` is below the documented minimum for the deployment mode.

### 12.5 Runtime profiling

Before tuning production limits:

```bash
go test -race ./...
go test ./internal/scraper -bench . -benchmem
go test ./internal/scraper -bench BenchmarkScheduler -cpuprofile cpu.pprof
go tool pprof cpu.pprof
go test ./internal/scraper -trace trace.out
go tool trace trace.out
```

Operational guidance:

- set `GOMEMLIMIT` below the container memory limit;
- tune `GOGC` only after heap profiles;
- watch goroutine count, connection reuse, DB wait time, heap, and JSON payload size;
- use representative CPU profiles for PGO;
- monitor file descriptors and idle connections;
- benchmark database permit acquisition under expected instance count.

---
## 13. File map

### 13.1 Create

- `internal/domain/snapshot.go`
- `internal/db/migrations/009_create_scrape_snapshots.up.sql`
- `internal/db/migrations/009_create_scrape_snapshots.down.sql`
- `internal/db/snapshot_repository.go`
- `internal/db/snapshot_repository_test.go`
- `internal/scraper/policy.go`
- `internal/scraper/policy_test.go`
- `internal/scraper/canonical.go`
- `internal/scraper/canonical_test.go`
- `internal/scraper/coordinator.go`
- `internal/scraper/coordinator_test.go`
- `internal/scraper/host_controller.go`
- `internal/scraper/host_controller_test.go`
- `internal/scraper/scheduler.go`
- `internal/scraper/scheduler_test.go`
- `internal/warwick/snapshot_source.go`
- `internal/warwick/snapshot_source_test.go`
- `internal/service/snapshot_provider.go`
- `internal/service/snapshot_provider_test.go`
- `internal/service/event_hub.go`
- `internal/service/event_hub_test.go`
- `internal/service/snapshot_notification_listener.go`
- `internal/service/snapshot_notification_listener_test.go`
- `internal/api/scraper_handlers.go`
- `internal/api/scraper_handlers_test.go`
- `internal/integration/snapshot_sync_test.go`
- `web/src/hooks/useSnapshotEvents.js`
- `web/src/__tests__/snapshot-events.test.js`
- `docs/adr/009-postgresql-snapshot-scraper.md` or the repository's equivalent ADR path

### 13.2 Modify

- `internal/domain/client.go`
- `internal/warwick/classroom_client.go`
- `internal/warwick/classroom_courses.go`
- `internal/warwick/classroom_sessions.go`
- `internal/warwick/classroom_profiles.go`
- `internal/service/teacher.go`
- `internal/service/room_manager.go`
- `internal/api/websocket.go`
- `internal/api/routes.go`
- `internal/api/teacher_handlers.go`
- `internal/app/config.go`
- `internal/app/bootstrap.go`
- `internal/app/runtime.go`
- `internal/db/db.go`
- `internal/db/migration_test.go`
- `internal/metrics/metrics.go`
- `internal/integration/live_sync_test.go`
- `web/src/hooks/useWebSocket.js`
- `web/src/hooks/useCourses.js`
- `web/src/hooks/useSessions.js`
- `web/src/hooks/useCheckins.js`
- `web/src/hooks/useCourseAttendance.js`
- `.env.example`
- `README.md`
- `docs/system-architecture.html`
- `scripts/verify-no-upstream-cache.sh`

---

## 14. Implementation tasks

### Task 0: Repository preflight and decision lock

**Purpose:** Validate assumptions before changing contracts. Do not implement against filenames or method names that do not exist.

**Files:**

- Create: ADR/spec file
- Inspect: `go.mod`, migrations, Warwick client, teacher service, room manager, API routes, WebSocket code, tests

- [x] **Step 1: Capture the baseline.**

Run and save output in the implementation notes:

```bash
go version
go env GOMOD GOVERSION
rg -n 'nhooyr.io/websocket|github.com/coder/websocket' .
rg -n 'FetchSessionDetailLive|GetSessionDetail|ToggleCheckin' internal
rg -n 'http.Client|http.Transport|DefaultTransport|MaxConnsPerHost' internal cmd
rg -n 'SERVERLESS_ENABLED|SNAPSHOT_READS_ENABLED|SCRAPER_' .
rg -n 'session_checkins|attendance_reports' internal scripts docs
```

- [x] **Step 2: Run the unchanged suite.**

```bash
go test ./... -count=1
go vet ./...
cd web && npm test -- --run && npm run lint && npm run build
```

Document pre-existing failures separately. Do not hide them inside snapshot commits.

- [x] **Step 3: Inspect current HTTP ownership.**

Confirm:

- one shared transport exists or identify where to create it;
- response bodies are always closed;
- current body limits;
- current auth retry behavior;
- session-pool concurrency;
- whether course/session/profile targets require one or multiple upstream requests;
- whether upstream validators cover full representations.

- [x] **Step 4: Decide WebSocket package scope.**

If the repository imports `nhooyr.io/websocket`, choose one:

1. keep it for this feature and create a separate migration issue; or
2. migrate to `github.com/coder/websocket` in an isolated compatibility commit before Task 8.

Do not mix API migration debugging with snapshot event debugging.

- [x] **Step 5: Write the ADR.**

Record:

- PostgreSQL-only durability;
- parsed typed JSON only;
- cluster-wide host permits;
- lease-generation fencing;
- validation-based freshness;
- metadata-only events;
- live rollback flag;
- QR-room exclusion;
- conditional-request safety rule.

- [x] **Step 6: Commit.**

```bash
git add docs/adr docs/superpowers/specs
git commit -m "docs: lock snapshot scraper architecture"
```

---

### Task 1: Define snapshot domain types and adaptive policy

**Files:**

- Create: `internal/domain/snapshot.go`
- Create: `internal/scraper/policy.go`
- Create: `internal/scraper/policy_test.go`
- Modify: `internal/domain/client.go`
- Modify: report/session implementations and test doubles

- [x] **Step 1: Add failing policy tests.**

Cover:

- changed halves interval and respects minimum;
- ordinary unchanged increases by 50%;
- ten unchanged results double interval;
- maximum bound;
- input history is copied and trimmed to ten;
- deterministic jitter stays within ±10%;
- `304` follows unchanged policy;
- transient delay is 1m, 2m, 4m and caps at 1h before jitter;
- valid `Retry-After` wins over local `429` pause;
- repeated `429` within one hour pauses at least 60 minutes;
- active/not-started/finished policies match section 4;
- maximum serve age is independent from content version age.

Example:

```go
func TestNextSchedule(t *testing.T) {
    policy := Policy{
        Initial:     5 * time.Minute,
        Min:         time.Minute,
        Max:         30 * time.Minute,
        MaxServeAge: 2 * time.Hour,
    }

    tests := []struct {
        name    string
        current time.Duration
        history []bool
        outcome Outcome
        want    time.Duration
    }{
        {"changed halves", 10 * time.Minute, nil, OutcomeChanged, 5 * time.Minute},
        {"changed minimum", time.Minute, nil, OutcomeChanged, time.Minute},
        {"unchanged adds half", 10 * time.Minute, []bool{true}, OutcomeUnchanged, 15 * time.Minute},
        {"ten unchanged doubles", 10 * time.Minute, make([]bool, 9), OutcomeUnchanged, 20 * time.Minute},
        {"maximum", 30 * time.Minute, nil, OutcomeUnchanged, 30 * time.Minute},
    }

    // table loop
}
```

- [x] **Step 2: Verify red state.**

```bash
go test ./internal/scraper -run 'Policy|Schedule|FailureDelay|Jitter' -count=1
```

- [x] **Step 3: Add domain types.**

Implement the contracts in sections 3 and 4. Use a full `TargetRef` throughout.

Rename report fetch semantics to:

```go
FetchSessionForReport(ctx, courseID, sessionID)
```

Do not keep a live-specific method name on an interface that will be implemented by the snapshot provider.

- [x] **Step 4: Implement policy functions.**

```go
type Outcome int

const (
    OutcomeChanged Outcome = iota
    OutcomeUnchanged
    OutcomeNotModified
)

type Policy struct {
    Initial     time.Duration
    Min         time.Duration
    Max         time.Duration
    MaxServeAge time.Duration
}

type Schedule struct {
    Interval time.Duration
    History  []bool
}

func PolicyFor(kind domain.SnapshotKind, status domain.SessionStatus) Policy
func NextSchedule(policy Policy, current time.Duration, history []bool, outcome Outcome) Schedule
func FailureDelay(consecutiveFailures int) time.Duration
func HostPauseFor429(now time.Time, last429 *time.Time, retryAfter time.Duration) time.Duration
func ApplyJitter(base time.Duration, fraction float64, rng *rand.Rand) time.Duration
```

Avoid global random state in deterministic tests.

- [x] **Step 5: Add domain edge tests.**

Test:

- `Snapshot.Expired` uses `ValidatedAt`;
- a 30-day-old content version validated today is not expired;
- invalid/zero max serve age is fail-closed;
- target identities with different parents are unequal;
- zero content hash behavior.

- [x] **Step 6: Run affected packages.**

```bash
go test ./internal/domain ./internal/scraper ./internal/service ./internal/warwick -count=1
```

- [x] **Step 7: Commit.**

```bash
git add internal/domain internal/scraper/policy* internal/service internal/warwick
git commit -m "feat: define snapshot contracts and policy"
```

---

### Task 2: Add migration 009 and the PostgreSQL repository

**Files:**

- Create: migration up/down
- Create: `internal/db/snapshot_repository.go`
- Create: repository integration tests
- Modify: schema version and migration tests

- [x] **Step 1: Add failing migration tests.**

Using `TEST_DATABASE_URL`, assert migration 009 creates:

- `scrape_host_state`;
- `scrape_targets`;
- `scrape_runs`;
- `scrape_snapshots`;
- `scrape_host_permits`;
- indexes and current-snapshot FK.

Also assert migration 008's removed tables remain absent.

- [x] **Step 2: Add failing repository tests.**

Use independent `pgxpool.Conn` values and real transactions. Cover:

1. two workers cannot claim the same target;
2. an expired target lease is reclaimable;
3. reclaim increments `lease_generation`;
4. a stale generation cannot commit;
5. same worker ID with old generation cannot commit;
6. changed completion inserts version 1 and advances current fields;
7. unchanged completion increments validation sequence but inserts no version;
8. `304` increments validation sequence and freshness but inserts no version;
9. failed completion retains current pointer and validation sequence;
10. `Current` returns not found before first success;
11. full identity distinguishes parent keys;
12. discovered upsert uses `LEAST(next_run_at)`;
13. one omitted child increments `missing_count` but stays enabled;
14. second successful omission disables the child;
15. reappearance resets `missing_count` and enables it;
16. pruning preserves current and latest three versions;
17. error text is UTF-8 safely truncated;
18. no stored field contains a fixture cookie;
19. retrying `Commit` after an ambiguous connection result returns the existing run without a second version or notification.

- [x] **Step 3: Verify red state.**

```bash
TEST_DATABASE_URL="$TEST_DATABASE_URL" \
  go test ./internal/db -run 'Migration009|Snapshot|Lease|Permit|Prune' -count=1 -v
```

- [x] **Step 4: Add migration.**

Implement section 7.2 and down migration. Use repository naming conventions and explicit comments for fencing and freshness columns.

- [x] **Step 5: Implement target seed and current reads.**

Repository interfaces:

```go
type SnapshotRepository interface {
    Seed(context.Context, []domain.TargetSeed) error
    ClaimDue(context.Context, ClaimRequest) ([]domain.ScrapeTarget, error)
    ClaimOne(context.Context, ClaimOneRequest) (domain.ScrapeTarget, error)
    ReleaseLease(context.Context, ReleaseLeaseRequest) error
    Commit(context.Context, CommitInput) (CommitResult, error)
    Current(context.Context, domain.TargetRef) (domain.Snapshot, error)
    Metadata(context.Context, domain.TargetRef, time.Time) (domain.SnapshotMetadata, error)
    ListMetadata(context.Context, time.Time) ([]domain.SnapshotMetadata, error)
    SetDueNow(context.Context, domain.TargetRef, time.Time) error
    CountDue(context.Context, time.Time) (int, error)
    Prune(context.Context, PruneRequest) (PruneResult, error)
}
```

- [x] **Step 6: Implement claim queries.**

Use `FOR UPDATE SKIP LOCKED`, deterministic ordering, bounded limits, and lease generation increment.

Add `EXPLAIN (ANALYZE, BUFFERS)` fixtures or assertions in an integration-only diagnostic test to verify the due index is used at realistic row counts. Do not make CI depend on exact planner cost values.

- [x] **Step 7: Implement transactional completion.**

Follow section 7.8 exactly. Important tests:

- commit checks generation, not only owner/time;
- an expired but unreclaimed matching generation may still commit;
- a reclaimed target rejects the old generation;
- notification is emitted only for inserted versions;
- `validation_seq` increments exactly once per successful validation.

- [x] **Step 8: Implement batched pruning.**

Test with enough rows to cross more than one batch.

- [x] **Step 9: Raise schema floor and run tests.**

```bash
TEST_DATABASE_URL="$TEST_DATABASE_URL" \
  go test ./internal/db -run 'Snapshot|Migration|Lease|Prune' -count=1 -v
```

- [x] **Step 10: Commit.**

```bash
git add internal/db
git commit -m "feat: add fenced PostgreSQL snapshot storage"
```

---

### Task 3: Add cluster-wide Warwick host admission

**Files:**

- Create: `internal/scraper/host_controller.go`
- Create: `internal/scraper/host_controller_test.go`
- Modify: repository host/permit methods

- [x] **Step 1: Add failing permit tests.**

Using PostgreSQL and an injected clock, assert:

- two instances together never exceed configured concurrency;
- tokens enforce aggregate request rate across instances;
- burst allows only configured tokens;
- active permit blocks a new permit until release/expiry;
- expired permits are cleaned and no longer count;
- host pause returns the exact resume time;
- first `429` halves rate and sets concurrency 1;
- repeated `429` pauses for at least 60 minutes;
- healthy recovery never exceeds baseline;
- permit release is idempotent;
- repeated permit acquisition for the same target generation returns one permit and consumes one token;
- permit TTL exceeds fetch deadline;
- target ID and lease generation are recorded for diagnostics.

- [x] **Step 2: Verify red state.**

```bash
TEST_DATABASE_URL="$TEST_DATABASE_URL" \
  go test ./internal/scraper -run 'HostController|Permit|TokenBucket|429' -count=1 -v
```

- [x] **Step 3: Implement repository permit transaction.**

Keep transactions short and never hold a host-row lock during network I/O.

Return a decision rather than sleeping inside the transaction:

```go
Acquire(ctx, request) (PermitDecision, error)
```

The scheduler waits with a context-aware timer and retries acquisition.

- [x] **Step 4: Implement observation updates.**

All rate changes occur under host-row lock. Refill token state before changing rate/burst.

- [x] **Step 5: Add contention benchmark.**

Create an opt-in benchmark that runs multiple goroutines/process-like clients against one host row and records permit-acquisition latency. This is diagnostic, not a strict CI threshold.

- [x] **Step 6: Run race and integration tests.**

```bash
go test -race ./internal/scraper -run 'HostController' -count=5
TEST_DATABASE_URL="$TEST_DATABASE_URL" \
  go test ./internal/scraper -run 'Permit|TokenBucket' -count=5
```

- [x] **Step 7: Commit.**

```bash
git add internal/scraper/host_controller* internal/db/snapshot_repository.go
git commit -m "feat: coordinate Warwick host admission in PostgreSQL"
```

---

### Task 4: Canonicalize typed payloads and harden Warwick fetches

**Files:**

- Create: canonicalization files
- Create: snapshot source files
- Modify: Warwick HTTP/parser files

- [x] **Step 1: Add failing canonicalization tests.**

For every kind, pass logically identical values in different orders and assert identical JSON and hash.

Also assert:

- caller-owned slices are not reordered;
- nil/empty list normalization is stable;
- timestamp offsets normalize consistently;
- wrong Go type is rejected;
- typed nil pointers are rejected;
- payload ceiling is enforced without truncation;
- A → B → A hashes behave as expected;
- any map-bearing domain fields have deterministic tests.

- [x] **Step 2: Add failing HTTP metadata tests.**

Use `httptest.Server` and a real client to test:

- `If-None-Match` and `If-Modified-Since`;
- `304` returns typed `ErrNotModified` plus metadata;
- `304` without a current snapshot is rejected by coordinator;
- `Retry-After` delta seconds;
- `Retry-After` HTTP date;
- typed status classification;
- unsupported content type;
- body limit;
- response bodies close on all paths;
- one authentication renewal only;
- concurrent auth renewal is coalesced;
- no generic network retry in `ClassroomClient`.

- [x] **Step 3: Verify representation completeness.**

For each target, document whether the typed value is produced from:

- one response with collection-wide validators; or
- multiple/paginated responses.

Enable conditionals only for the first category. Add a test proving profile validators cover every page before enabling profile `304` behavior.

- [x] **Step 4: Implement `Canonicalize`.**

```go
func Canonicalize(
    kind domain.SnapshotKind,
    value any,
    maxBytes int64,
) (json.RawMessage, [32]byte, error)
```

Marshal once. Check the resulting byte length before hashing/commit.

- [x] **Step 5: Implement typed source.**

```go
type ResponseMetadata struct {
    StatusCode   int
    ETag         string
    LastModified string
    CacheControl string
    RetryAfter   string
}

type SnapshotFetchResult struct {
    Value     any
    Metadata  ResponseMetadata
    BytesRead int64
}

type SnapshotSource struct {
    client *ClassroomClient
}

func (s *SnapshotSource) Fetch(
    context.Context,
    domain.ScrapeTarget,
) (SnapshotFetchResult, error)
```

Dispatch by kind and reject unknown kinds before network I/O.

- [x] **Step 6: Add bounded error handling.**

Classify:

- `401/403` after auth renewal → auth error;
- `404/410` → not found;
- `408/425/429/5xx` → typed transient/rate error;
- other `4xx` → permanent error;
- `2xx` decode/type/limit failure → invalid payload.

Never include response body text in returned errors.

- [x] **Step 7: Run focused tests.**

```bash
go test ./internal/scraper ./internal/warwick \
  -run 'Canonical|SnapshotSource|Conditional|NotModified|RetryAfter|BodyLimit|Auth' \
  -count=1
go test -race ./internal/warwick ./internal/scraper -count=3
```

- [x] **Step 8: Commit.**

```bash
git add internal/scraper/canonical* internal/warwick internal/domain
git commit -m "feat: fetch bounded canonical Warwick resources"
```

---

### Task 5: Build coordinator, discovery, and transactional outcomes

**Files:**

- Create: `internal/scraper/coordinator.go`
- Create: `internal/scraper/coordinator_test.go`
- Modify: snapshot domain/repository contracts

- [x] **Step 1: Add fake-source/fake-store tests.**

Cover:

1. first catalog success creates version 1 and discovers courses;
2. identical second result is unchanged, advances validation sequence, creates no version;
3. `304` advances validation/freshness and creates no version;
4. changed course detail discovers sessions;
5. one omitted child remains enabled;
6. second successful changed omission disables it;
7. reappearance re-enables it;
8. failure preserves current snapshot and validation sequence;
9. oversized payload is invalid;
10. unknown kind causes no network request;
11. stale generation commit becomes `ErrLeaseLost`;
12. canceled parent context releases or safely expires lease;
13. host permit is released before canonicalization/DB commit;
14. host observation occurs exactly once;
15. error text excludes body/cookie fixtures.

- [x] **Step 2: Define narrow interfaces.**

```go
type Source interface {
    Fetch(context.Context, domain.ScrapeTarget) (warwick.SnapshotFetchResult, error)
}

type Store interface {
    Commit(context.Context, db.CommitInput) (db.CommitResult, error)
    ReleaseLease(context.Context, db.ReleaseLeaseRequest) error
}

type HostObserver interface {
    Observe(context.Context, HostObservation) error
}
```

The coordinator does not claim targets and does not acquire host permits. The scheduler/dispatcher passes a claimed target and an already-authorized attempt.

- [x] **Step 3: Implement `RunClaimed`.**

```go
func (c *Coordinator) RunClaimed(
    ctx context.Context,
    target domain.ScrapeTarget,
) (RunResult, error)
```

Flow:

1. verify non-zero lease generation;
2. create fetch child context;
3. call source;
4. measure typed fetch result and release host permit in caller;
5. canonicalize successful data;
6. compare hash with target current hash;
7. calculate schedule/outcome;
8. discover child targets for changed parent payloads;
9. commit exactly once;
10. emit metrics from compact result.

- [x] **Step 4: Implement discovery.**

Catalog seeds course details with non-secret attributes:

```json
{"course_name":"Math 101","course_status":"active"}
```

Course detail seeds session details:

```json
{"session_status":"active"}
```

Keep operational `missing_count` in a column, not inside attributes.

- [x] **Step 5: Handle not-found targets.**

A `404/410` child target should participate in parent-aware retirement. Do not repeatedly fetch a permanently missing finished session every minute. Define deterministic behavior and tests.

- [x] **Step 6: Run tests.**

```bash
go test ./internal/scraper -run 'Coordinator|Discovery|LeaseLost|NotModified' -count=1
go test -race ./internal/scraper -count=5
```

- [x] **Step 7: Commit.**

```bash
git add internal/scraper/coordinator* internal/domain internal/db
git commit -m "feat: coordinate canonical snapshot outcomes"
```

---
### Task 6: Add the scheduler, bounded dispatch, and refresh coalescing

**Files:**

- Create: `internal/scraper/scheduler.go`
- Create: `internal/scraper/scheduler_test.go`
- Modify: host controller and repository interfaces

- [x] **Step 1: Add deterministic scheduler tests.**

Inject clock, timer factory, repository, host controller, coordinator, and worker launcher. Assert:

- global active attempts never exceed configured limit;
- host permits enforce Warwick concurrency across scheduler instances;
- scheduler claims at most `available_slots * prefetch_factor`;
- no idle claimed batch of 50 waits behind concurrency 2;
- target order follows `next_run_at, id`;
- host pause prevents new claim/dispatch;
- permit retry uses context-aware timer;
- cancellation stops new claims;
- unstarted claimed targets are released with generation check;
- active requests receive cancellation and bounded drain;
- scheduler waits for active goroutines before returning;
- worker crash leaves target/permit reclaimable after TTL;
- `RunDue(ctx, 50)` attempts at most 50 targets;
- `RemainingDue` is queried after execution;
- no `time.Sleep` exists in the production run loop.

- [x] **Step 2: Define scheduler results.**

```go
type TickResult struct {
    Claimed      int `json:"claimed"`
    Attempted    int `json:"attempted"`
    Succeeded    int `json:"succeeded"`
    Changed      int `json:"changed"`
    Failed       int `json:"failed"`
    Canceled     int `json:"canceled"`
    RemainingDue int `json:"remaining_due"`
}
```

- [x] **Step 3: Implement bounded dispatch.**

Pseudo-flow:

```text
while capacity and budget remain:
    claim small due batch
    for each claimed target:
        acquire cluster-wide host permit
        if permit unavailable:
            wait/retry without exceeding lease budget
        dispatch one coordinator attempt
        release permit immediately after typed fetch
        collect terminal result
```

Do not let worker goroutines claim additional targets.

- [x] **Step 4: Handle long permit waits.**

If expected permit wait approaches target lease expiry:

- renew target lease with matching generation; or
- release the claim and move `next_run_at` to the permit retry time.

Prefer release/reschedule over long idle lease renewal unless measurement shows excessive churn.

- [x] **Step 5: Implement `RefreshNow`.**

```go
type SnapshotRefresher interface {
    RefreshNow(context.Context, domain.TargetRef) error
    SetDueNow(context.Context, domain.TargetRef) error
}
```

Flow:

1. ensure target exists/seed it;
2. read baseline `validation_seq`;
3. set due now;
4. try `ClaimOne`;
5. if claimed, execute through the same permit/dispatcher path;
6. if another worker owns it, poll until `validation_seq > baseline`;
7. unchanged/304 counts as successful refresh;
8. return caller timeout without issuing duplicate work.

- [x] **Step 6: Add daily maintenance.**

Run pruning once per UTC day with an injected clock. Multiple instances may run it safely because batched deletes are idempotent; optionally use a PostgreSQL advisory lock to reduce duplicate maintenance work, but do not make scraper correctness depend on that lock.

- [x] **Step 7: Verify race behavior.**

```bash
go test ./internal/scraper -run 'Scheduler|RunDue|RefreshNow|Cancellation|Prefetch' -count=1
go test -race ./internal/scraper -count=10
```

- [x] **Step 8: Commit.**

```bash
git add internal/scraper internal/db/snapshot_repository.go
git commit -m "feat: dispatch bounded snapshot scrapes"
```

---

### Task 7: Add the PostgreSQL-backed teacher provider

**Files:**

- Create: `internal/service/snapshot_provider.go`
- Create: `internal/service/snapshot_provider_test.go`
- Modify: `internal/service/teacher.go`
- Modify: `internal/api/teacher_handlers.go`
- Modify: mocks/test doubles

- [x] **Step 1: Add provider tests.**

Assert:

- current catalog/detail/session/profile payloads decode correctly;
- missing snapshot calls `RefreshNow` once and reads again;
- concurrent cold misses coalesce by `validation_seq`;
- a refresh that returns unchanged satisfies cold wait;
- overdue but valid snapshot is served with stale metadata;
- expired snapshot returns `ErrSnapshotExpired`;
- old content version recently validated by `304` remains valid;
- malformed stored JSON returns a wrapped decode error and never calls Warwick directly;
- full parent-aware target identity is used;
- report session fetch receives course ID and session ID;
- request-level `source=live` is rejected when snapshot mode is active;
- system-wide live rollback still works.

- [x] **Step 2: Split read/write dependencies.**

```go
type TeacherDataProvider interface {
    GetCourses(context.Context) ([]domain.CourseSummary, error)
    GetCourseCatalog(context.Context) ([]domain.CourseSummary, error)
    GetCourseDetail(context.Context, string) (*domain.CourseDetail, error)
    GetCourseDetailWithName(context.Context, string, string) (*domain.CourseDetail, error)
    GetSessionDetail(context.Context, string, string) (*domain.SessionDetail, error)
    FetchStudentProfiles(context.Context) ([]domain.StudentProfile, error)
}

type CheckinWriter interface {
    ToggleCheckin(context.Context, string, string, string, bool) error
}

type TeacherService struct {
    reader            TeacherDataProvider
    sessions          domain.SessionFetcher
    checkins          CheckinWriter
    refresher         SnapshotRefresher
    reportConcurrency int
}
```

All dependencies must be non-nil. Add a no-op refresher only for explicit live rollback mode and tests unrelated to snapshot refresh.

- [x] **Step 3: Implement snapshot provider.**

```go
type SnapshotReader interface {
    Current(context.Context, domain.TargetRef) (domain.Snapshot, error)
    Metadata(context.Context, domain.TargetRef, time.Time) (domain.SnapshotMetadata, error)
    AnyOverdue(context.Context, []domain.TargetRef, time.Time) (bool, error)
}

type SnapshotProvider struct {
    store     SnapshotReader
    refresher SnapshotRefresher
    clock     func() time.Time
}
```

Decode only after expiry checks. Include kind/ref in wrapped decode errors but no payload excerpt.

- [x] **Step 4: Add compatible response headers.**

Implement freshness headers from section 9 without changing response JSON.

- [x] **Step 5: Update report semantics.**

Reports read session snapshots. Keep partial output for missing/expired sessions, mark stale when overdue, and never persist reports.

- [x] **Step 6: Run tests.**

```bash
go test ./internal/service ./internal/api \
  -run 'Snapshot|Teacher|Attendance|Dashboard|Freshness' -count=1
```

- [x] **Step 7: Commit.**

```bash
git add internal/service internal/api internal/domain
git commit -m "feat: serve teacher reads from validated snapshots"
```

---

### Task 8: Preserve check-in correctness and add reconciliation

**Files:**

- Modify teacher service/handlers/domain response
- Modify `useCheckins.js`
- Add service/API/frontend tests

- [x] **Step 1: Add failure-first tests.**

Assert:

1. Warwick write failure does not change snapshot storage and emits no snapshot event;
2. Warwick write success calls `SetDueNow` and `RefreshNow`;
3. changed refresh publishes only committed event;
4. unchanged refresh that already reflects desired state is not pending;
5. validation that still reflects old state returns pending and schedules another refresh;
6. refresh failure still returns write success with pending true;
7. two concurrent toggles cannot produce a stale-generation commit;
8. no direct JSON patch occurs;
9. optimistic UI eventually reconciles from REST/event/polling.

- [x] **Step 2: Extend response compatibly.**

Use `SnapshotRefreshPending` from section 10.

- [x] **Step 3: Implement bounded reconciliation.**

Use a 10-second child timeout. Do not hold the original HTTP request indefinitely. If upstream propagation is delayed, schedule a short follow-up and let polling repair.

- [x] **Step 4: Keep repair polling.**

If pending, retain 10-second polling until:

- a committed relevant version arrives; or
- REST returns the desired state; or
- the existing polling lifecycle ends.

- [x] **Step 5: Run tests.**

```bash
go test ./internal/service ./internal/api \
  -run 'Toggle.*Snapshot|Snapshot.*Toggle|Reconcile' -count=1
cd web && npm test -- --run useCheckins
```

- [x] **Step 6: Commit.**

```bash
git add internal/service internal/api internal/domain web/src
git commit -m "feat: reconcile snapshots after check-in writes"
```

---

### Task 9: Generalize event fan-out and bridge PostgreSQL notifications

**Files:**

- Create event hub/listener files and tests
- Modify room manager and WebSocket API

- [x] **Step 1: Add event-hub tests.**

Cover:

- two subscribers receive an event;
- unsubscribe removes exactly one subscriber;
- slow subscriber cannot block publisher;
- dropped events increment a counter;
- close closes subscriptions once;
- publish after close is safe according to documented behavior;
- existing room event names/shapes remain unchanged.

- [x] **Step 2: Extract shared `EventHub`.**

Keep bounded publish/subscriber buffers. Use one owner goroutine for subscriber map mutation.

- [x] **Step 3: Add PostgreSQL listener.**

- dedicated connection;
- `LISTEN` committed before initial state query;
- `WaitForNotification(ctx)` loop;
- reject payload over 8 KiB before JSON decode;
- decode only `SnapshotMetadata`;
- reconnect with bounded exponential backoff;
- reconcile current versions after reconnect;
- stop immediately on context cancellation.

- [x] **Step 4: Add WebSocket sync and dedupe.**

Subscribe before state read. Send room sync and snapshot sync. Drop buffered events at or below sync version.

Use a single connection writer and existing write timeout. Preserve connection caps and read limits.

- [x] **Step 5: Add listener-gap test.**

Simulate:

1. listener connected;
2. disconnect listener;
3. commit version N+1;
4. reconnect listener;
5. reconciliation observes N+1 and publishes one repair event/state update.

- [x] **Step 6: Run race tests.**

```bash
go test ./internal/service ./internal/api \
  -run 'EventHub|Notification|WebSocket|StateSync|Reconnect' -count=1
go test -race ./internal/service ./internal/api -count=5
```

- [x] **Step 7: Commit.**

```bash
git add internal/service internal/api
git commit -m "feat: broadcast committed snapshot versions"
```

---

### Task 10: Wire configuration, runtime, internal endpoints, and metrics

**Files:**

- Create scraper handlers/tests
- Modify app config/bootstrap/runtime/routes/metrics/env

- [x] **Step 1: Add configuration tests.**

Cover:

- defaults;
- malformed values;
- hard bounds;
- relational lease/fetch/grace constraint;
- body limit hard cap;
- invalid trace sample rate;
- serverless + enabled + empty trigger token fails;
- transport ceiling lower than configured concurrency fails or is raised explicitly;
- snapshot reads enabled without repository/refresher wiring fails fast.

- [x] **Step 2: Add protected handlers.**

`POST /api/internal/scraper/tick`:

- bearer token only;
- 50-second default child context or shorter platform-safe value;
- calls `RunDue(TickLimit)`;
- returns compact `TickResult`;
- no target list or student data.

`GET /api/internal/scraper/status`:

- aggregate counts;
- oldest validation age by kind;
- expired current count;
- due/leased/failed counts;
- current host rate/concurrency/pause;
- active/expired permit counts;
- no payloads or identifiers.

- [x] **Step 3: Wire dependencies.**

Bootstrap order:

1. validate config;
2. create/reuse shared transport and session pool;
3. create snapshot repository;
4. seed host state and root targets;
5. create source;
6. create host controller;
7. create coordinator;
8. create scheduler/refresher;
9. create snapshot provider and live provider;
10. choose read provider by flag;
11. keep live check-in writer;
12. create shared event hub;
13. create notification listener;
14. add scheduler/listener to always-on runtime only when appropriate;
15. expose internal handlers;
16. close listener/hub cleanly.

A missing catalog at startup is allowed. The first read performs bounded cold refresh.

- [x] **Step 4: Add metrics.**

Implement section 12 with fixed label sets. Add tests that fail if a target/resource label is registered.

- [x] **Step 5: Add runtime tests.**

Cover:

- always-on scheduler starts only when enabled and not serverless;
- listener starts whenever snapshot events are needed;
- serverless mode does not spawn sleeping scheduler;
- shutdown order cancels scheduler/listener before closing DB pool;
- no goroutine leak after repeated start/stop tests.

- [x] **Step 6: Run tests.**

```bash
go test ./internal/app ./internal/api ./internal/metrics \
  -run 'Scraper|Snapshot|Runtime|Metric|Config' -count=1
go test -race ./internal/app ./internal/api -count=3
```

- [x] **Step 7: Commit.**

```bash
git add internal/app internal/api internal/metrics .env.example
git commit -m "feat: wire production snapshot runtime"
```

---

### Task 11: Make frontend hooks version-aware

**Files:**

- Create `useSnapshotEvents.js` and tests
- Modify WebSocket/courses/sessions/check-ins/attendance hooks

- [x] **Step 1: Add failing event-routing tests.**

Assert:

- catalog commit refetches courses only;
- course commit refetches sessions only for the open course;
- session commit refetches check-ins only for the open session;
- relevant session commit refetches the open course attendance report;
- unrelated resources do nothing;
- duplicate or lower version does nothing;
- state sync initializes version map without payload storage;
- buffered event equal to sync version does nothing;
- reconnect retains existing full REST repair behavior;
- silent refetch keeps current content rendered.

- [x] **Step 2: Dispatch browser event.**

```js
export const SNAPSHOT_COMMITTED_EVENT = 'snapshot-committed';

if (data.SnapshotCommitted !== undefined) {
  window.dispatchEvent(new CustomEvent(
    SNAPSHOT_COMMITTED_EVENT,
    { detail: data.SnapshotCommitted }
  ));
}
```

- [x] **Step 3: Maintain a resource version map.**

Key by:

```text
kind + "\0" + parent_key + "\0" + resource_key
```

On state sync, keep the maximum version. On event, ignore versions less than or equal to the current value, then update and dispatch.

Do not copy snapshot payloads into Zustand.

- [x] **Step 4: Implement filtered hook.**

```js
export function useSnapshotEvents(predicate, callback) {
  const predicateRef = useRef(predicate);
  const callbackRef = useRef(callback);
  predicateRef.current = predicate;
  callbackRef.current = callback;

  useEffect(() => {
    const handler = (event) => {
      if (predicateRef.current?.(event.detail)) {
        callbackRef.current?.(event.detail);
      }
    };

    window.addEventListener(SNAPSHOT_COMMITTED_EVENT, handler);
    return () => window.removeEventListener(
      SNAPSHOT_COMMITTED_EVENT,
      handler
    );
  }, []);
}
```

- [x] **Step 5: Keep polling as repair behavior.**

Do not remove active-session polling in the first rollout. Measure event delivery/drop/reconnect rates before changing it.

- [x] **Step 6: Run frontend suite.**

```bash
cd web
npm test -- --run
npm run lint
npm run build
```

- [x] **Step 7: Commit.**

```bash
git add web/src
git commit -m "feat: refetch views on new snapshot versions"
```

---

### Task 12: Add end-to-end contracts, guards, performance checks, and rollout

**Files:**

- Create snapshot integration test
- Modify live compatibility test
- Modify storage guard, README, architecture docs
- Add benchmark/load fixtures where repository conventions allow

- [x] **Step 1: Add full integration test.**

Use `httptest.Server` for Warwick and `TEST_DATABASE_URL` for PostgreSQL:

1. seed host state and root targets;
2. scrape catalog and discover courses;
3. scrape course and discover sessions;
4. scrape session and create version 1;
5. read through `TeacherService` with Warwick fixture disabled;
6. return `304`, assert validation sequence advances and version remains 1;
7. advance content, scrape, assert version 2;
8. assert `SnapshotCommitted` after commit;
9. force stale generation, assert commit rejected;
10. run two scheduler instances, assert aggregate host concurrency/rate;
11. return `429 Retry-After`, assert host pause and no overwrite;
12. return transient timeout, assert bounded retry schedule;
13. advance clock beyond max serve age since last validation, assert REST `503`;
14. toggle check-in, assert Warwick-first behavior and reconciliation;
15. disconnect/reconnect notification listener, assert version reconciliation;
16. assert no database field contains fixture cookie/credential values.

- [x] **Step 2: Preserve live rollback integration test.**

Explicitly name the contract:

```text
SNAPSHOT_READS_ENABLED=false
```

Live client behavior remains the upstream writer/source and rollback path.

- [x] **Step 3: Update storage-boundary guard.**

Continue rejecting:

```text
session_checkins
attendance_reports
ASP.NET_SessionId in migrations/repositories
raw_response_body
raw_html
object_storage
S3 bucket clients
Cookie or Authorization columns
```

Add checks that:

- WebSocket code cannot serialize `Snapshot.Payload`;
- Prometheus labels do not include resource/student identifiers;
- logs do not include cookie fixtures;
- snapshot tables have no credential columns;
- raw body fields do not exist;
- profile avatars are not persisted as bytes/files.

- [x] **Step 4: Add performance diagnostics.**

Measure, without hardcoding unrealistic pass thresholds:

- scheduler dispatch allocations;
- canonicalization allocations by kind;
- permit acquisition p50/p95 under expected instance count;
- claim query plan at realistic target count;
- commit transaction duration;
- JSONB size distribution;
- DB pool wait duration;
- HTTP connection reuse.

Document representative numbers and environment.

- [x] **Step 5: Rewrite documentation.**

README must state:

- teacher reads are eventually consistent within policy bounds;
- freshness is based on last successful validation;
- `304` refreshes freshness without a new version;
- PostgreSQL is the only durable store;
- raw responses/cookies are not persisted;
- host rate/concurrency are cluster-wide;
- WebSocket announces committed versions only;
- QR rooms keep existing TTL ownership;
- reports are derived and not stored;
- serverless requires protected tick trigger;
- status endpoint usage;
- rollback with `SNAPSHOT_READS_ENABLED=false`.

Mark the prior live-only design as superseded for teacher-read storage while retaining it as migration history.

- [x] **Step 6: Run complete verification.**

```bash
go test ./... -count=1
go test -race ./internal/scraper ./internal/db ./internal/service \
  ./internal/api ./internal/warwick -count=1
TEST_DATABASE_URL="$TEST_DATABASE_URL" \
  go test ./internal/db ./internal/integration -count=1 -v
./scripts/verify-no-upstream-cache.sh
go vet ./...
cd web
npm test -- --run
npm run lint
npm run build
```

Also run vulnerability/dependency checks according to repository policy.

- [ ] **Step 7: Deploy write-only mode.**

```env
SCRAPER_ENABLED=true
SNAPSHOT_READS_ENABLED=false
```

Wait until status shows:

```text
catalog current:             1
profiles current:            1
active course coverage:      100%
known session coverage:      100%
expired current snapshots:   0
stale generation commits:    0
active permit leaks:         0 after TTL window
```

Observe for at least one full maximum active-session interval. Confirm no repeated `429`, auth storm, permit leak, DB pool exhaustion, or notification queue issue.

- [ ] **Step 8: Enable snapshot reads.**

```env
SNAPSHOT_READS_ENABLED=true
```

Verify:

- courses;
- sessions;
- check-ins;
- reports;
- dashboard;
- toggles;
- QR rooms;
- favourites;
- saved views;
- serverless tick behavior if applicable.

Monitor:

- validation age;
- stale and `503` rates;
- scrape outcomes;
- host rate/concurrency;
- lease lost count;
- permit count;
- DB size and pool waits;
- WebSocket reconnect/drop count;
- polling reconciliation time.

- [ ] **Step 9: Exercise rollback.**

```env
SNAPSHOT_READS_ENABLED=false
```

Confirm reads immediately return to live Warwick while scraper writes remain safe. Do not drop migration 009 data for operational rollback.

- [x] **Step 10: Commit.**

```bash
git add internal/integration scripts README.md docs
git commit -m "docs: ship production PostgreSQL snapshot scraper"
```

---

## 15. Completion checklist

### Architecture and storage

- [x] PostgreSQL is the only durable snapshot/admission store.
- [x] No raw responses, screenshots, avatars, cookies, credentials, or auth headers are persisted.
- [x] Migration 009 is reversible.
- [x] Migration 008's removed tables remain absent.
- [x] Target identity includes host, kind, parent, and resource key.
- [x] JSONB contains typed canonical domain payloads only.
- [x] Current snapshot pointer/version/hash constraints are enforced.

### Concurrency and leases

- [x] Target claims use `FOR UPDATE SKIP LOCKED` and deterministic ordering.
- [x] Claims increment `lease_generation`.
- [x] Commits fence stale generations.
- [x] Cluster-wide host permits enforce aggregate rate/concurrency.
- [x] Permits expire after worker crash.
- [x] Permit acquisition and attempt completion are idempotent per target generation.
- [x] Scheduler prefetch is bounded by available slots.
- [x] Workers do not claim recursively or create unbounded pools.
- [x] Shutdown releases unstarted claims and cancels active work.

### Freshness and correctness

- [x] Freshness uses `last_validated_at`.
- [x] Changed, unchanged, and `304` advance `validation_seq`.
- [x] `304` creates no new content version.
- [x] Cold refresh waits on validation sequence, not version.
- [x] Failed fetch never overwrites current content.
- [x] Last-good data expires only after maximum serve age since validation.
- [x] Conditional requests are enabled only for complete representations.
- [x] Retry ownership is centralized and no amplification exists.
- [x] Child removal requires two successful changed-parent omissions.

### HTTP and security

- [x] One shared transport is reused.
- [x] Body/header/time limits are enforced.
- [x] Auth renewal is bounded and coalesced.
- [x] Error bodies are not logged or persisted.
- [x] Redirect/destination policy remains enforced.
- [x] Internal tick/status token is never logged.
- [x] Status output contains aggregates only.

### Product behavior

- [x] Reports use snapshots and are not stored.
- [x] Check-in writes go to Warwick first.
- [x] Reconciliation never patches JSON directly.
- [x] QR-room behavior is unchanged.
- [x] WebSocket events contain metadata only.
- [x] Frontend ignores duplicate/out-of-order versions.
- [x] Polling remains as repair behavior during rollout.
- [x] Live rollback mode remains tested.

### Verification and operations

- [x] Unit, integration, race, vet, frontend test, lint, and build pass.
- [x] PostgreSQL contention tests pass with independent connections.
- [x] Query plans use intended indexes at realistic scale.
- [x] Pruning is batched and preserves current/latest versions.
- [x] Metrics have bounded labels.
- [x] No goroutine, lease, permit, or DB connection leaks are observed.
- [ ] Write-only rollout reaches complete coverage before read cutover.
- [ ] Snapshot read rollback is exercised in production-like staging.

---

## 16. Expected trade-offs

| Pillar | Gain | Cost | Mitigation |
| --- | --- | --- | --- |
| Read latency | Teacher reads and reports avoid repeated Warwick round trips. | Data is not request-time live. | Adaptive intervals, active-session minimum, post-toggle reconciliation, freshness headers. |
| Correctness | Typed canonical replacement and fenced commits prevent stale overwrites. | More coordination state and tests. | One PostgreSQL system, explicit generations, deterministic integration tests. |
| Multi-instance safety | DB host permits enforce aggregate limits. | One short DB transaction per upstream request. | Warwick starts at 1 RPS; measure before introducing host-owner sharding. |
| Durability | Current/recent versions survive restart. | JSONB stores student-related data and grows. | Changed-only versions, batched retention, least-privilege DB, no raw bodies. |
| Availability | Last-good snapshots survive upstream outages. | Overdue data may be served temporarily. | Explicit stale state, maximum serve age, `503` after expiry. |
| Event latency | WebSocket announces committed versions quickly. | `LISTEN/NOTIFY` is not a durable payload queue. | Durable DB state, reconnect reconciliation, version dedupe, REST polling repair. |
| Operational complexity | Leases, permits, listener, and serverless tick support restarts and scaling. | More moving parts than live reads. | Feature flags, protected status, bounded components, documented rollback. |
| Check-in UX | Warwick remains source of truth and UI reconciles quickly. | Upstream may be eventually consistent after a write. | Pending flag, short follow-up scheduling, event/REST/polling repair. |

---

## 17. Future work intentionally deferred

Do not include these in the first snapshot release unless measurements prove they are required:

- stable host-owner scheduler sharding to remove DB permit transactions at high RPS;
- durable PostgreSQL event outbox/replay beyond state reconciliation;
- partitioning `scrape_runs` or `scrape_snapshots`;
- API conditional GET using snapshot ETags;
- reducing active polling frequency;
- removing the live rollback provider;
- migrating the WebSocket package if not already done;
- broader crawler/robots/browser infrastructure;
- extracting snapshot payloads into normalized per-student relational tables.

---

## 18. Reference notes

Implementation reviewers should verify behavior against the current official documentation for:

- Go 1.26 and the latest supported patch release;
- `net/http.Transport` connection reuse and limits;
- PostgreSQL `FOR UPDATE SKIP LOCKED` queue semantics;
- PostgreSQL `LISTEN` initialization ordering and `NOTIFY` transaction behavior;
- `pgx/v5` dedicated notification connections;
- the maintained WebSocket module if a package migration is separately approved.

Official references:

- [Go 1.26 release notes](https://go.dev/doc/go1.26)
- [Go release history](https://go.dev/doc/devel/release)
- [`net/http` package documentation](https://pkg.go.dev/net/http)
- [PostgreSQL 16 `SELECT` locking clauses](https://www.postgresql.org/docs/16/sql-select.html)
- [PostgreSQL 16 `LISTEN`](https://www.postgresql.org/docs/16/sql-listen.html)
- [PostgreSQL 16 `NOTIFY`](https://www.postgresql.org/docs/16/sql-notify.html)
- [`pgx/v5` package documentation](https://pkg.go.dev/github.com/jackc/pgx/v5)
- [`github.com/coder/websocket`](https://pkg.go.dev/github.com/coder/websocket)

The architectural source of truth remains this plan plus the repository's accepted ADR and tests. External documentation explains primitives; repository tests define the product contract.
