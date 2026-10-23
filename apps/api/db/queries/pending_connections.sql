-- name: CreatePendingConnection :exec
INSERT INTO pending_connections (id, workspace_id, provider, credentials, accounts, link_id, group_id, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetPendingConnection :one
SELECT * FROM pending_connections WHERE workspace_id = ? AND id = ? AND expires_at > @now;

-- name: DeletePendingConnection :execrows
DELETE FROM pending_connections WHERE workspace_id = ? AND id = ?;

-- name: DeleteExpiredPendingConnections :exec
DELETE FROM pending_connections WHERE expires_at <= ?;
