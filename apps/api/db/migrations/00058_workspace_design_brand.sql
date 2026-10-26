-- +goose Up
-- The design editor's brand kit (colours, fonts, logo files from the library), as JSON: kept in the workspace row,
-- so backups and exports carry it.
ALTER TABLE workspaces ADD COLUMN design_brand TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE workspaces DROP COLUMN design_brand;
