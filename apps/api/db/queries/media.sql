-- name: CreateMedia :exec
INSERT INTO media (id, workspace_id, storage_name, original_name, mime_type, size_bytes, sha256, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetMedia :one
SELECT * FROM media WHERE workspace_id = ? AND id = ?;

-- name: DeleteMedia :execrows
DELETE FROM media WHERE workspace_id = ? AND id = ?;

-- name: DeleteUnreferencedMedia :execrows
DELETE FROM media WHERE media.workspace_id = ? AND media.id = ?
  AND NOT EXISTS (SELECT 1 FROM posts WHERE posts.workspace_id = media.workspace_id AND instr(posts.media, '"' || media.id || '"') > 0);
