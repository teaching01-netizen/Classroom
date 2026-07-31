BEGIN;

DROP TABLE IF EXISTS scrape_candidates;

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
        'canceled'
    ));

DROP INDEX IF EXISTS scrape_snapshots_target_verified_idx;

ALTER TABLE scrape_snapshots
    DROP COLUMN IF EXISTS verified_at,
    DROP COLUMN IF EXISTS parser_version,
    DROP COLUMN IF EXISTS schema_version,
    DROP COLUMN IF EXISTS raw_body_hash,
    DROP COLUMN IF EXISTS complete,
    DROP COLUMN IF EXISTS manifest,
    DROP COLUMN IF EXISTS validation_report;

ALTER TABLE scrape_targets
    DROP CONSTRAINT IF EXISTS scrape_targets_quality_state_check,
    DROP COLUMN IF EXISTS quality_state,
    DROP COLUMN IF EXISTS last_rejection_code,
    DROP COLUMN IF EXISTS current_parser_version,
    DROP COLUMN IF EXISTS first_missing_at;

COMMIT;
