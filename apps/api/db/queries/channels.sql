-- name: CreateChannel :exec
INSERT INTO channels (id, workspace_id, provider, name, handle, avatar_url, profile_url, status, credentials, settings, group_id, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

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

-- name: ChannelActivity :many
-- Per channel of a workspace, from its targets: the last published one (time and post), the next scheduled
-- one and how many went out since @since. One query for the whole list, each figure an index lookup per
-- channel (post_targets_channel_published), not a pass over every post of the workspace.
SELECT c.id AS channel_id,
  CAST(COALESCE((SELECT MAX(t.published_at) FROM post_targets t WHERE t.channel_id = c.id AND t.status = 'published'), '') AS TEXT) AS last_published_at,
  CAST(COALESCE((SELECT t.post_id FROM post_targets t WHERE t.channel_id = c.id AND t.status = 'published'
    ORDER BY t.published_at DESC LIMIT 1), '') AS TEXT) AS last_post_id,
  CAST(COALESCE((SELECT MIN(p.publish_at) FROM post_targets t JOIN posts p ON p.id = t.post_id
    WHERE t.channel_id = c.id AND t.status = 'pending' AND p.status = 'scheduled'), '') AS TEXT) AS next_publish_at,
  CAST((SELECT COUNT(*) FROM post_targets t WHERE t.channel_id = c.id AND t.status = 'published' AND t.published_at >= @since) AS INTEGER) AS published_since
FROM channels c
WHERE c.workspace_id = @workspace_id;

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

-- name: SetChannelComments :exec
UPDATE channels SET comments = ? WHERE id = ?;
