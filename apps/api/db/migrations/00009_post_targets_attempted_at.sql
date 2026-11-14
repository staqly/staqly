-- +goose Up
-- When a channel's last try ended (published or failed): kept apart from updated_at, which edits move too, so a
-- failed channel shows when it was really tried.
ALTER TABLE post_targets ADD COLUMN attempted_at TEXT;
UPDATE post_targets SET attempted_at = COALESCE(published_at, updated_at) WHERE status IN ('published', 'failed');

-- +goose Down
ALTER TABLE post_targets DROP COLUMN attempted_at;
