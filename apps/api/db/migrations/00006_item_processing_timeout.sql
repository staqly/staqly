-- +goose Up
-- An item can wait longer (or shorter) than its workspace's processing timeout, in seconds.
-- NULL uses the workspace's value.
ALTER TABLE items ADD COLUMN processing_timeout_seconds INTEGER;

-- +goose Down
ALTER TABLE items DROP COLUMN processing_timeout_seconds;
