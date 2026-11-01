-- +goose Up
-- Taking a published post down again: when and by whom the post was unpublished, and per channel what happened
-- to its copy there ('removed', 'manual' = the person deletes it on the platform, 'failed' with the reason).
ALTER TABLE posts ADD COLUMN unpublished_at TEXT;
ALTER TABLE posts ADD COLUMN unpublished_by TEXT NOT NULL DEFAULT '';
ALTER TABLE post_targets ADD COLUMN removal TEXT NOT NULL DEFAULT '';
ALTER TABLE post_targets ADD COLUMN removal_error TEXT NOT NULL DEFAULT '';
ALTER TABLE post_targets ADD COLUMN removed_at TEXT;

-- +goose Down
ALTER TABLE post_targets DROP COLUMN removed_at;
ALTER TABLE post_targets DROP COLUMN removal_error;
ALTER TABLE post_targets DROP COLUMN removal;
ALTER TABLE posts DROP COLUMN unpublished_by;
ALTER TABLE posts DROP COLUMN unpublished_at;
