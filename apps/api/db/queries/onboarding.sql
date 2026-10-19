-- name: SetUserOnboarded :exec
UPDATE users SET onboarded_at = ?, updated_at = ? WHERE id = ?;

-- name: SetWorkspaceOnboarded :exec
UPDATE workspaces SET onboarded_at = ?, updated_at = ? WHERE id = ?;

-- name: SetUserName :exec
UPDATE users SET name = ?, updated_at = ? WHERE id = ?;

-- name: WorkspaceContentCount :one
-- A local install that already has channels or posts is past the welcome.
SELECT (SELECT COUNT(*) FROM channels c WHERE c.workspace_id = @workspace_id)
     + (SELECT COUNT(*) FROM posts p WHERE p.workspace_id = @workspace_id);
