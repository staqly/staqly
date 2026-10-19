-- +goose Up
-- The processing timeout (how long an item may stay processing before it fails) moves from the workspace
-- to the collection: an item's own processing_timeout_seconds wins, then the collection's, then the server's
-- PROCESSING_TIMEOUT. workspaces.processing_timeout_seconds stays in the schema (migrations are append-only)
-- but is no longer read or exposed.
ALTER TABLE collections ADD COLUMN processing_timeout_seconds INTEGER;

-- +goose Down
ALTER TABLE collections DROP COLUMN processing_timeout_seconds;
