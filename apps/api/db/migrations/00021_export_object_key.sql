-- +goose Up
-- Where a finished export's archive is on S3. Every builder uploads under a name of its own, and the one that
-- finishes the export (still holding its lease) records it here: a builder that lost its lease and uploads later
-- no longer replaces the archive. '' for exports on disk and for those made before this column (their archive is
-- workspaces/<workspace>/backups/<id>.zip).
ALTER TABLE workspace_exports ADD COLUMN object_key TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE workspace_exports DROP COLUMN object_key;
