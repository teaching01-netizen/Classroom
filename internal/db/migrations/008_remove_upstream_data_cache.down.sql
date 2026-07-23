CREATE TABLE IF NOT EXISTS session_checkins (
    session_id TEXT NOT NULL,
    student_id TEXT NOT NULL,
    student_name TEXT NOT NULL,
    nickname TEXT NOT NULL DEFAULT '',
    school TEXT NOT NULL DEFAULT '',
    checked_in BOOLEAN NOT NULL DEFAULT FALSE,
    toggled_at TIMESTAMPTZ,
    refreshed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    session_date DATE,
    last_warwick_sync_at TIMESTAMPTZ,
    PRIMARY KEY (session_id, student_id)
);

CREATE INDEX IF NOT EXISTS idx_session_checkins_sync
    ON session_checkins (session_id, last_warwick_sync_at DESC);

CREATE TABLE IF NOT EXISTS attendance_reports (
    course_id TEXT PRIMARY KEY,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    threshold INTEGER NOT NULL,
    duration_ms BIGINT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_attendance_reports_computed
    ON attendance_reports (computed_at DESC);
