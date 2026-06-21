-- Add nickname and school columns to session_checkins so the DB cache
-- returns full student data on first visit (not just after Warwick live sync).
ALTER TABLE session_checkins ADD COLUMN IF NOT EXISTS nickname TEXT NOT NULL DEFAULT '';
ALTER TABLE session_checkins ADD COLUMN IF NOT EXISTS school TEXT NOT NULL DEFAULT '';
