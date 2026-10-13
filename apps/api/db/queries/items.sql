-- name: CreateItem :exec
INSERT INTO items (id, workspace_id, collection_id, title, type, content, url, media_id, metadata, status, source, external_ref, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetItem :one
SELECT * FROM items WHERE workspace_id = ? AND id = ?;

-- name: UpdateItem :execrows
-- Optimistic concurrency: only succeeds if the caller saw the current version.
UPDATE items
SET title = @title, content = @content, url = @url, media_id = @media_id, metadata = @metadata,
    status = @status, source = @source, external_ref = @external_ref, feedback = @feedback,
    decided_at = @decided_at, collection_id = @collection_id,
    version = version + 1, updated_at = @updated_at
WHERE workspace_id = @workspace_id AND id = @id AND version = @version;

-- name: DeleteItem :execrows
DELETE FROM items WHERE workspace_id = ? AND id = ?;

-- name: InsertItemVersion :exec
INSERT INTO item_versions (item_id, version, snapshot, actor, created_at) VALUES (?, ?, ?, ?, ?);

-- name: ListItemVersions :many
SELECT * FROM item_versions WHERE item_id = ? ORDER BY version DESC;

-- name: CountItemsReferencingMedia :one
SELECT COUNT(*) FROM items WHERE media_id = ?;

-- name: SetItemEditable :exec
UPDATE items SET editable = ? WHERE id = ?;

-- name: GetItemEditable :one
SELECT editable FROM items WHERE id = ?;
