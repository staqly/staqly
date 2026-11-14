-- +goose Up
-- An archived post is left out of the post list and the calendar unless they ask for archived posts too.
ALTER TABLE posts ADD COLUMN archived_at TEXT;

-- +goose Down
ALTER TABLE posts DROP COLUMN archived_at;
