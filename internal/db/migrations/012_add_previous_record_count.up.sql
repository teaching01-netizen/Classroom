-- Add previous_record_count to scrape_targets for suspicious change detection
ALTER TABLE scrape_targets
    ADD COLUMN previous_record_count INTEGER NOT NULL DEFAULT 0;
