-- +goose Up
-- Summaries and auto-tags are a queue on the items themselves: summary_next_at is when the worker should
-- (re)try the item (NULL = not queued: done, given up, or created before this migration).
ALTER TABLE items ADD COLUMN summary_next_at TEXT;
ALTER TABLE items ADD COLUMN summary_attempts INTEGER NOT NULL DEFAULT 0;
CREATE INDEX items_summary_queue ON items (summary_next_at) WHERE summary_next_at IS NOT NULL;

-- +goose Down
DROP INDEX items_summary_queue;
ALTER TABLE items DROP COLUMN summary_attempts;
ALTER TABLE items DROP COLUMN summary_next_at;
