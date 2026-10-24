-- +goose Up
-- The workspace's logo: a small PNG or JPEG as a data: URL (the dashboard scales it to 128 px first), '' for
-- staqly's. Kept in the row, so backups and exports carry it without a file of its own.
ALTER TABLE workspaces ADD COLUMN logo TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE workspaces DROP COLUMN logo;
