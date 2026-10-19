-- +goose Up
-- The workspace's time zone (an IANA name): post repeats keep their local wall-clock time in it across DST, and
-- a schedule rule without a zone of its own runs in it.
ALTER TABLE workspaces ADD COLUMN timezone TEXT NOT NULL DEFAULT 'UTC';

-- +goose Down
ALTER TABLE workspaces DROP COLUMN timezone;
