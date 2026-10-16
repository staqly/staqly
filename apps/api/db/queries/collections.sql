-- name: CreateCollection :exec
INSERT INTO collections (id, workspace_id, name, description, icon, color, default_view, actions, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: CreateDefaultCollection :exec
-- Idempotent: the partial unique index keeps one Default per workspace.
INSERT INTO collections (id, workspace_id, name, description, default_view, actions, is_default, created_at, updated_at)
SELECT @id, @workspace_id, 'Default', 'Items without a collection of their own.', 'table', @actions, 1, @now, @now
WHERE NOT EXISTS (SELECT 1 FROM collections c WHERE c.workspace_id = @workspace_id AND c.is_default = 1);

-- name: WorkspacesWithoutDefaultCollection :many
SELECT id FROM workspaces w
WHERE NOT EXISTS (SELECT 1 FROM collections c WHERE c.workspace_id = w.id AND c.is_default = 1);

-- name: GetCollection :one
SELECT * FROM collections WHERE workspace_id = ? AND id = ?;

-- name: ListCollections :many
SELECT * FROM collections
WHERE workspace_id = @workspace_id AND (CAST(@include_archived AS INTEGER) = 1 OR archived_at IS NULL)
ORDER BY is_default DESC, name COLLATE NOCASE;

-- name: UpdateCollection :exec
UPDATE collections
SET name = ?, description = ?, icon = ?, color = ?, default_view = ?, actions = ?, archived_at = ?, updated_at = ?
WHERE workspace_id = ? AND id = ?;

-- name: DeleteCollection :execrows
DELETE FROM collections WHERE workspace_id = ? AND id = ?;

-- name: CollectionStatusCounts :many
SELECT collection_id, status, COUNT(*) AS n FROM items WHERE workspace_id = ? GROUP BY collection_id, status;

-- name: CollectionCovers :many
-- Most recent image item with media per collection (gallery cover).
SELECT i.collection_id, i.media_id, i.url FROM items i
WHERE i.workspace_id = ? AND i.type = 'image'
  AND i.created_at = (SELECT MAX(created_at) FROM items j WHERE j.collection_id = i.collection_id AND j.type = 'image');
