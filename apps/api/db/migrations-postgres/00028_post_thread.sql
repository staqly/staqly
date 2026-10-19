-- +goose Up
-- Mirrors db/migrations/00028_post_thread.sql (threads: posts.thread, post_targets.replies).
ALTER TABLE posts ADD COLUMN thread TEXT NOT NULL DEFAULT '[]' CHECK (thread IS JSON);
ALTER TABLE post_targets ADD COLUMN replies TEXT NOT NULL DEFAULT '[]' CHECK (replies IS JSON);

-- +goose Down
ALTER TABLE post_targets DROP COLUMN replies;
ALTER TABLE posts DROP COLUMN thread;
