CREATE TABLE IF NOT EXISTS saved_dashboard_views (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    filters JSONB NOT NULL DEFAULT '{}',
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_saved_views_last_used ON saved_dashboard_views(last_used_at DESC);
