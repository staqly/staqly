-- +goose Up
-- Free workspaces get a data-only export (no media files); Pro and local installs keep the full ZIP.
ALTER TABLE workspace_exports ADD COLUMN includes_media INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE workspace_exports DROP COLUMN includes_media;
