CREATE TABLE IF NOT EXISTS teacher_favourites (
    course_id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
