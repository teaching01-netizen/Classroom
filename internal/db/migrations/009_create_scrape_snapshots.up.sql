CREATE TABLE scrape_host_state (
    host TEXT PRIMARY KEY,
    baseline_requests_per_second NUMERIC(8,3) NOT NULL
        CHECK (baseline_requests_per_second BETWEEN 0.25 AND 5),
    current_requests_per_second NUMERIC(8,3) NOT NULL
        CHECK (current_requests_per_second BETWEEN 0.25 AND 5),
    burst SMALLINT NOT NULL CHECK (burst BETWEEN 1 AND 5),
    available_tokens NUMERIC(12,6) NOT NULL CHECK (available_tokens >= 0),
    tokens_updated_at TIMESTAMPTZ NOT NULL,
    baseline_concurrency SMALLINT NOT NULL
        CHECK (baseline_concurrency BETWEEN 1 AND 4),
    current_concurrency SMALLINT NOT NULL
        CHECK (current_concurrency BETWEEN 1 AND 4),
    consecutive_429s INTEGER NOT NULL DEFAULT 0 CHECK (consecutive_429s >= 0),
    last_429_at TIMESTAMPTZ,
    healthy_streak INTEGER NOT NULL DEFAULT 0 CHECK (healthy_streak >= 0),
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
    missing_count SMALLINT NOT NULL DEFAULT 0 CHECK (missing_count BETWEEN 0 AND 2),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    current_interval_seconds INTEGER NOT NULL CHECK (current_interval_seconds > 0),
    min_interval_seconds INTEGER NOT NULL CHECK (min_interval_seconds > 0),
    max_interval_seconds INTEGER NOT NULL CHECK (max_interval_seconds >= min_interval_seconds),
    max_serve_age_seconds INTEGER NOT NULL CHECK (max_serve_age_seconds >= max_interval_seconds),
    next_run_at TIMESTAMPTZ NOT NULL,
    last_attempt_at TIMESTAMPTZ,
    last_validated_at TIMESTAMPTZ,
    last_content_change_at TIMESTAMPTZ,
    validation_seq BIGINT NOT NULL DEFAULT 0 CHECK (validation_seq >= 0),
    consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
    recent_changes BOOLEAN[] NOT NULL DEFAULT ARRAY[]::BOOLEAN[],
    etag TEXT,
    last_modified TEXT,
    cache_control TEXT,
    current_snapshot_id BIGINT,
    current_version BIGINT NOT NULL DEFAULT 0 CHECK (current_version >= 0),
    current_content_hash BYTEA,
    lease_owner TEXT,
    lease_generation BIGINT NOT NULL DEFAULT 0 CHECK (lease_generation >= 0),
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
