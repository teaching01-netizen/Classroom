-- Priority field
DROP INDEX IF EXISTS scrape_targets_priority_idx;
ALTER TABLE scrape_targets DROP COLUMN IF EXISTS priority;

-- Listener checkpoints
DROP TABLE IF EXISTS snapshot_listener_checkpoints;

-- Snapshot commit events outbox
DROP INDEX IF EXISTS snapshot_commit_events_target_idx;
DROP TABLE IF EXISTS snapshot_commit_events;

-- Mutation idempotency
DROP TABLE IF EXISTS checkin_mutation_requests;

-- Snapshots: canonicalization and validation version
ALTER TABLE scrape_snapshots DROP COLUMN IF EXISTS validation_version;
ALTER TABLE scrape_snapshots DROP COLUMN IF EXISTS canonicalization_version;

-- Targets: canonicalization version
ALTER TABLE scrape_targets DROP COLUMN IF EXISTS current_canonicalization_version;

-- Targets: lifecycle
DROP INDEX IF EXISTS scrape_targets_lifecycle_idx;
ALTER TABLE scrape_targets
    DROP CONSTRAINT IF EXISTS scrape_targets_lifecycle_state_check;
ALTER TABLE scrape_targets DROP COLUMN IF EXISTS reactivated_at;
ALTER TABLE scrape_targets DROP COLUMN IF EXISTS tombstoned_at;
ALTER TABLE scrape_targets DROP COLUMN IF EXISTS consecutive_missing_count;
ALTER TABLE scrape_targets DROP COLUMN IF EXISTS last_seen_parent_version;
ALTER TABLE scrape_targets DROP COLUMN IF EXISTS lifecycle_state;
