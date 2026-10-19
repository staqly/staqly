-- name: CreateCollection :exec
INSERT INTO collections (id, workspace_id, name, description, icon, color, default_view, actions, processing_timeout_seconds, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, '[]', ?, ?, ?);

-- name: CreateDefaultCollection :exec
-- Idempotent: the partial unique index keeps one Default per workspace.
INSERT INTO collections (id, workspace_id, name, description, default_view, actions, is_default, created_at, updated_at)
SELECT @id, @workspace_id, 'Default', 'Posts without a collection of their own.', 'table', '[]', 1, @now, @now
WHERE NOT EXISTS (SELECT 1 FROM collections c WHERE c.workspace_id = @workspace_id AND c.is_default = 1);

-- name: WorkspacesWithoutDefaultCollection :many
SELECT id FROM workspaces w
WHERE NOT EXISTS (SELECT 1 FROM collections c WHERE c.workspace_id = w.id AND c.is_default = 1);

-- name: GetCollection :one
SELECT * FROM collections WHERE workspace_id = ? AND id = ?;

-- name: GetCollectionByID :one
SELECT * FROM collections WHERE id = ?;

-- name: ListCollections :many
SELECT * FROM collections
WHERE workspace_id = @workspace_id AND (CAST(@include_archived AS INTEGER) = 1 OR archived_at IS NULL)
ORDER BY is_default DESC, name COLLATE NOCASE;

-- name: UpdateCollection :exec
UPDATE collections
SET name = ?, description = ?, icon = ?, color = ?, default_view = ?, processing_timeout_seconds = ?, archived_at = ?, updated_at = ?
WHERE workspace_id = ? AND id = ?;

-- name: DeleteCollection :execrows
DELETE FROM collections WHERE workspace_id = ? AND id = ?;
