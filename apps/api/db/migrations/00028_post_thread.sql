-- +goose Up
-- Threads: a post's follow-up parts ("comments"), published as replies under it after their delays.
-- posts.thread holds the parts [{content, media, delay_seconds}]; post_targets.replies how each part went
-- on that channel [{index, status, external_id, external_url, error, published_at}].
ALTER TABLE posts ADD COLUMN thread TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(thread));
ALTER TABLE post_targets ADD COLUMN replies TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(replies));

-- +goose Down
ALTER TABLE post_targets DROP COLUMN replies;
ALTER TABLE posts DROP COLUMN thread;
