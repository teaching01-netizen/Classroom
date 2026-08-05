-- Heal legacy snapshots: migration 013 added `complete` and `verified_at`
-- without a backfill, so every pre-existing snapshot row stayed
-- complete = FALSE / verified_at = NULL forever and the live-export gate
-- rejected them as incomplete. Active targets' current snapshots are
-- re-verified against the target's most recent validation time (NOT the
-- snapshot's ancient fetch time), which also keeps the derived quality
-- state fresh instead of flipping it to verified_stale.
BEGIN;

UPDATE scrape_snapshots AS s
SET complete = TRUE,
    verified_at = COALESCE(t.last_validated_at, s.verified_at)
FROM scrape_targets AS t
WHERE t.current_snapshot_id = s.id
  AND t.lifecycle_state = 'active'
  AND s.complete = FALSE;

COMMIT;
