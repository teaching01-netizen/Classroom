-- Lifecycle fields on scrape_targets
ALTER TABLE scrape_targets
    ADD COLUMN lifecycle_state TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN last_seen_parent_version BIGINT,
    ADD COLUMN consecutive_missing_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN tombstoned_at TIMESTAMPTZ,
    ADD COLUMN reactivated_at TIMESTAMPTZ;

ALTER TABLE scrape_targets
    ADD CONSTRAINT scrape_targets_lifecycle_state_check
    CHECK (lifecycle_state IN ('active', 'missing', 'tombstoned'));

CREATE INDEX scrape_targets_lifecycle_idx
    ON scrape_targets (lifecycle_state)
    WHERE lifecycle_state != 'active';

-- Canonicalization version on scrape_targets
ALTER TABLE scrape_targets
    ADD COLUMN current_canonicalization_version INTEGER;

-- Canonicalization and validation version on scrape_snapshots
ALTER TABLE scrape_snapshots
    ADD COLUMN canonicalization_version INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN validation_version INTEGER NOT NULL DEFAULT 1;

-- Mutation idempotency table
CREATE TABLE checkin_mutation_requests (
    idempotency_key TEXT PRIMARY KEY,
    course_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    student_id TEXT NOT NULL,
    desired_checked_in BOOLEAN NOT NULL,
    expected_snapshot_version BIGINT,
    status TEXT NOT NULL,
    upstream_attempted BOOLEAN NOT NULL DEFAULT FALSE,
    response JSONB,
    error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Durable snapshot event outbox
CREATE TABLE snapshot_commit_events (
    sequence BIGSERIAL PRIMARY KEY,
    snapshot_id UUID NOT NULL,
    target_id UUID NOT NULL,
    snapshot_version BIGINT NOT NULL,
    target_kind TEXT NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (target_id, snapshot_version)
);

CREATE INDEX snapshot_commit_events_target_idx
    ON snapshot_commit_events (target_id, snapshot_version);

-- Listener checkpoint table
CREATE TABLE snapshot_listener_checkpoints (
    consumer_name TEXT PRIMARY KEY,
    last_sequence BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Priority field on scrape_targets
ALTER TABLE scrape_targets
    ADD COLUMN priority SMALLINT NOT NULL DEFAULT 50;

CREATE INDEX scrape_targets_priority_idx
    ON scrape_targets (priority, next_run_at)
    WHERE enabled = TRUE;
