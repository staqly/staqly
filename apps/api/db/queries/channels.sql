-- name: CreateChannel :exec
INSERT INTO channels (id, workspace_id, provider, name, handle, avatar_url, profile_url, status, credentials, settings, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetChannel :one
SELECT * FROM channels WHERE workspace_id = ? AND id = ?;

-- name: GetChannelByID :one
SELECT * FROM channels WHERE id = ?;

-- name: ListChannels :many
SELECT * FROM channels WHERE workspace_id = ? ORDER BY provider, name COLLATE NOCASE, id;

-- name: ChannelsByIDs :many
SELECT * FROM channels WHERE workspace_id = ? AND id IN (SELECT value FROM json_each(@ids));

-- name: UpdateChannel :exec
UPDATE channels SET name = ?, handle = ?, avatar_url = ?, profile_url = ?, status = ?, credentials = ?, settings = ?, last_error = ?, updated_at = ?
WHERE workspace_id = ? AND id = ?;

-- name: SetChannelHealth :exec
UPDATE channels SET status = ?, last_error = ?, updated_at = ? WHERE id = ?;

-- name: SetChannelCredentials :exec
UPDATE channels SET credentials = ?, updated_at = ? WHERE id = ?;

-- name: DeleteChannel :execrows
DELETE FROM channels WHERE workspace_id = ? AND id = ?;

-- name: CountChannels :one
SELECT COUNT(*) FROM channels WHERE workspace_id = ?;

-- name: ExportAllChannels :many
SELECT id, workspace_id, provider, name, handle, avatar_url, profile_url, status, settings, last_error, created_at, updated_at
FROM channels WHERE workspace_id = ? ORDER BY created_at, id;

-- name: GetProviderApp :one
SELECT * FROM provider_apps WHERE workspace_id = ? AND provider = ?;

-- name: ListProviderApps :many
SELECT provider FROM provider_apps WHERE workspace_id = ?;

-- name: UpsertProviderApp :exec
INSERT INTO provider_apps (workspace_id, provider, credentials, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT (workspace_id, provider) DO UPDATE SET credentials = excluded.credentials, updated_at = excluded.updated_at;

-- name: DeleteProviderApp :execrows
DELETE FROM provider_apps WHERE workspace_id = ? AND provider = ?;
