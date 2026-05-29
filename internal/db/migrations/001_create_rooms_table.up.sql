DO $$ BEGIN
    CREATE TYPE room_status AS ENUM (
        'idle',
        'running',
        'fetching',
        'warning',
        'auth_expired',
        'stopped'
    );
EXCEPTION
    WHEN duplicate_object THEN null;
END $$;

CREATE TABLE IF NOT EXISTS rooms (
    room_id UUID PRIMARY KEY,
    class_id TEXT NOT NULL,
    name TEXT,
    status room_status NOT NULL DEFAULT 'idle',
    qr_url TEXT,
    expires_at TIMESTAMPTZ,
    last_updated_at TIMESTAMPTZ,
    warning_message TEXT,
    error_message TEXT,
    last_fetch_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
