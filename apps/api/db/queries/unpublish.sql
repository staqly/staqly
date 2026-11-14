-- name: SetPostUnpublished :exec
UPDATE posts SET unpublished_at = @unpublished_at, unpublished_by = @unpublished_by, updated_at = @updated_at
WHERE workspace_id = @workspace_id AND id = @id;

-- name: SetTargetRemoval :exec
UPDATE post_targets SET removal = @removal, removal_error = @removal_error, removed_at = @removed_at, updated_at = @updated_at
WHERE post_id = @post_id AND channel_id = @channel_id;
