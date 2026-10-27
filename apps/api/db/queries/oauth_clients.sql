-- name: GetOAuthClient :one
SELECT credentials FROM oauth_clients WHERE provider = ? AND server = ? AND redirect_uri = ?;

-- name: SaveOAuthClient :exec
INSERT INTO oauth_clients (provider, server, redirect_uri, credentials, created_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (provider, server, redirect_uri) DO UPDATE SET credentials = excluded.credentials, created_at = excluded.created_at;
