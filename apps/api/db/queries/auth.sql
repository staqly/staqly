-- name: CreateInstallation :exec
INSERT INTO installation (id, workspace_id, created_at) VALUES (1, ?, ?);

-- name: GetInstallation :one
SELECT * FROM installation WHERE id = 1;

-- name: CreateWorkspace :exec
INSERT INTO workspaces (id, name, recovery_verifier, recovery_rotated_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetWorkspace :one
SELECT * FROM workspaces WHERE id = ?;

-- name: GetWorkspaceByRecoveryVerifier :one
SELECT * FROM workspaces WHERE recovery_verifier = ?;

-- name: UpdateWorkspaceName :exec
UPDATE workspaces SET name = ?, updated_at = ? WHERE id = ?;

-- name: UpdateWorkspaceTimezone :exec
UPDATE workspaces SET timezone = ?, updated_at = ? WHERE id = ?;

-- name: UpdateWorkspaceLogo :exec
UPDATE workspaces SET logo = ?, updated_at = ? WHERE id = ?;

-- name: UpdateRecoveryVerifier :exec
UPDATE workspaces SET recovery_verifier = ?, recovery_rotated_at = ?, updated_at = ? WHERE id = ?;

-- name: SwapRecoveryVerifier :execrows
-- Recovery is single-use: the old verifier is replaced only if it still matches
-- (two concurrent recoveries with the same key: exactly one wins).
UPDATE workspaces SET recovery_verifier = @new_verifier, recovery_rotated_at = @rotated_at, updated_at = @rotated_at
WHERE id = @id AND recovery_verifier = @old_verifier;

-- name: CreateAPIKey :exec
INSERT INTO api_keys (id, workspace_id, name, prefix, key_hash, scopes, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetAPIKeyByHash :one
SELECT * FROM api_keys WHERE key_hash = ?;

-- name: GetAPIKey :one
SELECT * FROM api_keys WHERE workspace_id = ? AND id = ?;

-- name: ListAPIKeys :many
SELECT * FROM api_keys WHERE workspace_id = ? ORDER BY revoked_at IS NOT NULL, created_at DESC;

-- name: RevokeAPIKey :execrows
UPDATE api_keys SET revoked_at = ? WHERE workspace_id = ? AND id = ? AND revoked_at IS NULL;

-- name: TouchAPIKey :exec
UPDATE api_keys SET last_used_at = ? WHERE id = ?;

-- name: CreateSession :exec
INSERT INTO sessions (id, workspace_id, token_hash, csrf_token, user_agent, created_at, last_seen_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetSessionByHash :one
SELECT * FROM sessions WHERE token_hash = ?;

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = ? WHERE id = ?;

-- name: ListSessions :many
SELECT * FROM sessions
WHERE workspace_id = ? AND revoked_at IS NULL AND expires_at > ?
ORDER BY last_seen_at DESC;

-- name: GetSession :one
SELECT * FROM sessions WHERE workspace_id = ? AND id = ?;

-- name: RevokeSession :execrows
UPDATE sessions SET revoked_at = ? WHERE workspace_id = ? AND id = ? AND revoked_at IS NULL;

-- name: RevokeOtherSessions :execrows
-- Recovery-key use or rotation signs out every other browser.
UPDATE sessions SET revoked_at = @revoked_at
WHERE workspace_id = @workspace_id AND id <> @keep_id AND revoked_at IS NULL;
