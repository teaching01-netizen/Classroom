-- Remove previous_record_count from scrape_targets
ALTER TABLE scrape_targets
    DROP COLUMN previous_record_count;
