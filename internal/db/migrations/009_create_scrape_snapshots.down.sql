DROP TABLE IF EXISTS scrape_host_permits;
ALTER TABLE scrape_targets
    DROP CONSTRAINT IF EXISTS scrape_targets_current_snapshot_fk;
DROP TABLE IF EXISTS scrape_snapshots;
DROP TABLE IF EXISTS scrape_runs;
DROP TABLE IF EXISTS scrape_targets;
DROP TABLE IF EXISTS scrape_host_state;
