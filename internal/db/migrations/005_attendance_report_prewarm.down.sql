-- Rollback for 005_attendance_report_prewarm.

DROP TABLE IF EXISTS attendance_reports;

DROP INDEX IF EXISTS idx_session_checkins_sync;

ALTER TABLE session_checkins
  DROP COLUMN IF EXISTS last_warwick_sync_at;
