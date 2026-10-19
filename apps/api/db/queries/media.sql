-- name: CreateMedia :exec
INSERT INTO media (id, workspace_id, storage_name, original_name, mime_type, size_bytes, sha256, created_at, width, height)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetMedia :one
SELECT * FROM media WHERE workspace_id = ? AND id = ?;

-- name: DeleteMedia :execrows
DELETE FROM media WHERE workspace_id = ? AND id = ?;

-- name: DeleteUnreferencedMedia :execrows
DELETE FROM media WHERE media.workspace_id = ? AND media.id = ?
  AND NOT EXISTS (SELECT 1 FROM posts WHERE posts.workspace_id = media.workspace_id AND instr(posts.media, '"' || media.id || '"') > 0);

-- name: UpdateMediaInfo :execrows
UPDATE media SET original_name = @original_name, alt_text = @alt_text WHERE workspace_id = @workspace_id AND id = @id;

-- name: SetMediaSize :execrows
-- A video's size and length, measured by the browser that uploaded it; pictures are measured on upload.
UPDATE media SET width = @width, height = @height, duration_ms = @duration_ms WHERE workspace_id = @workspace_id AND id = @id;

-- name: MediaUsage :many
-- Posts that carry a media id in their files or in a comment of their thread (both are JSON with quoted ids).
SELECT id, collection_id, content, status, publish_at, created_at FROM posts
WHERE workspace_id = @workspace_id AND (instr(media, @quoted) > 0 OR instr(thread, @quoted) > 0)
ORDER BY created_at DESC LIMIT 50;
