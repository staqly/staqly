-- +goose Up
-- Every workspace has one Default collection that cannot be archived or
-- deleted. An existing collection named "Default" becomes it; the server
-- creates it on start for workspaces that have none.
ALTER TABLE collections ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0;
UPDATE collections SET is_default = 1, archived_at = NULL
WHERE name = 'Default' COLLATE NOCASE;
CREATE UNIQUE INDEX collections_default ON collections (workspace_id) WHERE is_default = 1;

-- +goose Down
DROP INDEX collections_default;
ALTER TABLE collections DROP COLUMN is_default;
