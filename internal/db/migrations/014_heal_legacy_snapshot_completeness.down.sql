-- This migration is a one-way data backfill: it heals legacy snapshot rows
-- that migration 013 left incomplete. Reversing it would corrupt data
-- (complete / verified_at are set by the commit path for every snapshot),
-- so the down migration is intentionally a no-op.
BEGIN;

-- no-op: a data backfill is not safely reversible

COMMIT;
