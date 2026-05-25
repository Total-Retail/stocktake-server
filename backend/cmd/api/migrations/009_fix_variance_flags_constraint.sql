-- +goose Up
-- The UNIQUE (session_id, item_no) constraint on variance_flags may be absent
-- on live databases where the original 005_variance migration ran while the
-- sessions table FK reference was invalid (e.g. after the Sprint 1A table
-- rename). Without this constraint the ON CONFLICT upsert in FlagItems()
-- fails with SQLSTATE 42P10.
--
-- Using an exception block makes this idempotent — if the constraint already
-- exists (clean installs) the block silently succeeds.
DO $$
BEGIN
    ALTER TABLE variance_flags
        ADD CONSTRAINT variance_flags_session_id_item_no_key
        UNIQUE (session_id, item_no);
EXCEPTION WHEN duplicate_object THEN
    NULL;  -- already exists, nothing to do
END $$;

-- +goose Down
ALTER TABLE variance_flags
    DROP CONSTRAINT IF EXISTS variance_flags_session_id_item_no_key;
