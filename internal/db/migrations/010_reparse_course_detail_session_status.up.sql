-- Warwick represents not-started sessions with an empty dStatus. Clear the
-- conditional validators once so existing course-detail snapshots are parsed
-- again and their discovered session targets receive the not-started policy.
UPDATE scrape_targets
SET etag = NULL,
    last_modified = NULL,
    next_run_at = LEAST(next_run_at, NOW()),
    updated_at = NOW()
WHERE kind = 'course_detail';
