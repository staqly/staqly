-- +goose Up
-- A removed channel stays as a row (name, platform, picture; no credentials) so the posts it published keep
-- showing where they went. Every list and lookup of channels leaves it out.
ALTER TABLE channels ADD COLUMN deleted_at TEXT;

-- +goose Down
ALTER TABLE channels DROP COLUMN deleted_at;
