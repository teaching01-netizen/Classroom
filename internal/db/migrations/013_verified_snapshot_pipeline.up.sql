-- Verified snapshot pipeline: last-known-good semantics, candidate evidence,
-- completeness manifests, and explicit data quality state.
BEGIN;

-- Data quality state on targets. updated by the commit path; the read path
-- derives verified_stale when the verified snapshot exceeds max_serve_age.
ALTER TABLE scrape_targets
    ADD COLUMN quality_state TEXT NOT NULL DEFAULT 'unavailable';

ALTER TABLE scrape_targets
    ADD CONSTRAINT scrape_targets_quality_state_check
    CHECK (
        quality_state IN (
            'verified_fresh',
            'verified_stale',
            'degraded',
            'unavailable'
        )
    );

ALTER TABLE scrape_targets
    ADD COLUMN last_rejection_code TEXT,
    ADD COLUMN current_parser_version TEXT,
    ADD COLUMN first_missing_at TIMESTAMPTZ;

-- Verification and provenance metadata on published snapshots.
ALTER TABLE scrape_snapshots
    ADD COLUMN verified_at TIMESTAMPTZ,
    ADD COLUMN parser_version TEXT,
    ADD COLUMN schema_version TEXT,
    ADD COLUMN raw_body_hash TEXT,
    ADD COLUMN complete BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN manifest JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN validation_report JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE INDEX scrape_snapshots_target_verified_idx
    ON scrape_snapshots (target_id, verified_at DESC);

-- Quarantined runs are a first-class outcome so suspicious-but-unconfirmed
-- candidates are recorded without touching the published pointer.
ALTER TABLE scrape_runs
    DROP CONSTRAINT IF EXISTS scrape_runs_outcome_check;

ALTER TABLE scrape_runs
    ADD CONSTRAINT scrape_runs_outcome_check
    CHECK (outcome IN (
        'changed',
        'unchanged',
        'not_modified',
        'rate_limited',
        'auth_error',
        'transient_error',
        'not_found',
        'permanent_error',
        'invalid_payload',
        'canceled',
        'quarantined'
    ));

-- Candidate evidence: every parseable fetch is recorded with its disposition
-- so rejected and quarantined results are diagnosable later.
CREATE TABLE scrape_candidates (
    id BIGSERIAL PRIMARY KEY,
    target_id BIGINT NOT NULL
        REFERENCES scrape_targets(id)
        ON DELETE CASCADE,
    lease_generation BIGINT NOT NULL,
    attempt_number INTEGER NOT NULL CHECK (attempt_number > 0),
    fetched_at TIMESTAMPTZ NOT NULL,
    request_id TEXT,
    http_status INTEGER,
    content_type TEXT,
    content_length BIGINT,
    etag TEXT,
    last_modified TEXT,
    raw_body_hash TEXT,
    canonical_hash TEXT,
    parser_version TEXT NOT NULL,
    schema_version TEXT NOT NULL,
    canonicalizer_version TEXT NOT NULL,
    payload JSONB,
    manifest JSONB NOT NULL,
    validation_report JSONB NOT NULL,
    disposition TEXT NOT NULL CHECK (
        disposition IN (
            'accepted',
            'unchanged',
            'needs_confirmation',
            'rejected_transport',
            'rejected_authentication',
            'rejected_parse',
            'rejected_incomplete',
            'rejected_semantic',
            'quarantined_anomaly'
        )
    ),
    rejection_code TEXT,
    confirmation_group UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (target_id, lease_generation, attempt_number),
    CHECK (
        raw_body_hash IS NULL OR length(raw_body_hash) = 64
    ),
    CHECK (
        canonical_hash IS NULL OR length(canonical_hash) = 64
    )
);

CREATE INDEX scrape_candidates_target_created_idx
    ON scrape_candidates (target_id, created_at DESC);

CREATE INDEX scrape_candidates_rejections_idx
    ON scrape_candidates (disposition, created_at DESC)
    WHERE disposition LIKE 'rejected_%'
       OR disposition = 'quarantined_anomaly';

COMMIT;
