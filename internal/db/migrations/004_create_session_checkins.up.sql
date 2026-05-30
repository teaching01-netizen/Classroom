CREATE TABLE IF NOT EXISTS session_checkins (
    session_id   TEXT NOT NULL,
    student_id   TEXT NOT NULL,
    student_name TEXT NOT NULL,
    checked_in   BOOLEAN NOT NULL DEFAULT FALSE,
    toggled_at   TIMESTAMPTZ,          -- non-null = teacher override, refresher skips
    refreshed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    session_date DATE,                 -- enables cache growth filter
    PRIMARY KEY (session_id, student_id)
) WITH (fillfactor = 85);
