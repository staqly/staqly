-- name: CreatePreviewLink :exec
INSERT INTO preview_links (id, workspace_id, post_id, token_hash, created_by, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListPreviewLinks :many
SELECT * FROM preview_links WHERE workspace_id = ? AND post_id = ? ORDER BY created_at DESC;

-- name: GetPreviewLinkByHash :one
SELECT * FROM preview_links WHERE token_hash = ?;

-- name: RevokePreviewLink :execrows
UPDATE preview_links SET revoked_at = @now WHERE workspace_id = @workspace_id AND post_id = @post_id AND id = @id AND revoked_at IS NULL;

-- name: CreatePostComment :exec
INSERT INTO post_comments (id, workspace_id, post_id, preview_id, author_name, author_email, body, quote, channel_id,
    created_at, parent_id, part, anchor_start, anchor_end, author_user)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetPostComment :one
SELECT * FROM post_comments WHERE workspace_id = ? AND post_id = ? AND id = ?;

-- name: SetCommentThreadResolved :execrows
UPDATE post_comments SET resolved_at = @resolved_at, resolved_by = @resolved_by
WHERE workspace_id = @workspace_id AND post_id = @post_id AND id = @id AND parent_id = '';

-- name: ListPostComments :many
SELECT * FROM post_comments WHERE workspace_id = @workspace_id AND post_id = @post_id
  -- Keyset: the page after (created_at, id) of the previous one's last row ('' = the first page).
  AND (@after_at = '' OR created_at > @after_at OR (created_at = @after_at AND id > @after_id))
ORDER BY created_at, id
LIMIT @lim;

-- name: CountRecentPreviewComments :one
SELECT COUNT(*) FROM post_comments WHERE preview_id = @preview_id AND created_at > @since;

-- name: DeletePostComment :execrows
-- Removing a thread's first comment removes its replies too.
DELETE FROM post_comments WHERE workspace_id = @workspace_id AND post_id = @post_id AND (id = @id OR parent_id = @id);

-- name: SetTargetMetrics :exec
UPDATE post_targets SET metrics = ?, metrics_at = ? WHERE post_id = ? AND channel_id = ?;
