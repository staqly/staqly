-- +goose Up
-- How long an item may stay "processing" before it fails, per workspace (seconds).
-- NULL uses the server default (PROCESSING_TIMEOUT, 1h).
ALTER TABLE workspaces ADD COLUMN processing_timeout_seconds INTEGER;

-- +goose Down
ALTER TABLE workspaces DROP COLUMN processing_timeout_seconds;
