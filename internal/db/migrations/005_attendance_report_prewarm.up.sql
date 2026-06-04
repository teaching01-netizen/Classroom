-- Phase 1: pre-warm infrastructure for the attendance report
-- (1) Stamp each session_checkins row with the last time it was refreshed
--     from Warwick. Used by Phase 2 (SessionPreWarmer) to identify stale
--     sessions and by Phase 3 (DB-backed report source) to decide whether
--     to trigger an async re-pre-warm.
-- (2) New attendance_reports table holds the last computed report per course.
--     Phase 4 (ReportPersister) writes here asynchronously after each compute.
--     Phase 5 (ReportHydrator) reads here on server boot to warm the in-memory
--     cache before the HTTP listener accepts traffic.

ALTER TABLE session_checkins
  ADD COLUMN IF NOT EXISTS last_warwick_sync_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_session_checkins_sync
  ON session_checkins (session_id, last_warwick_sync_at DESC);

CREATE TABLE IF NOT EXISTS attendance_reports (
    course_id   TEXT PRIMARY KEY,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    threshold   INTEGER     NOT NULL,
    duration_ms BIGINT      NOT NULL,
    payload     JSONB       NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_attendance_reports_computed
  ON attendance_reports (computed_at DESC);
