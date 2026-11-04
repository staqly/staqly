-- name: CreateChannel :exec
INSERT INTO channels (id, workspace_id, provider, name, handle, avatar_url, profile_url, status, credentials, settings, group_id, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetChannel :one
SELECT * FROM channels WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL;

-- name: GetChannelByID :one
SELECT * FROM channels WHERE id = ?;

-- name: ListChannels :many
SELECT * FROM channels WHERE workspace_id = ? AND deleted_at IS NULL ORDER BY provider, name COLLATE NOCASE, id;

-- name: ChannelsByIDs :many
SELECT * FROM channels WHERE workspace_id = ? AND deleted_at IS NULL AND id IN (SELECT value FROM json_each(@ids));

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
WHERE c.workspace_id = @workspace_id AND c.deleted_at IS NULL;

-- name: SetChannelHealth :exec
UPDATE channels SET status = ?, last_error = ?, updated_at = ? WHERE id = ?;

-- name: MarkChannelNeedsReconnect :execrows
UPDATE channels SET status = 'needs_reconnect', last_error = ?, updated_at = ? WHERE id = ? AND status NOT IN ('needs_reconnect', 'disabled');

-- name: SetChannelCredentials :exec
UPDATE channels SET credentials = ?, updated_at = ? WHERE id = ?;

-- name: DeleteChannel :execrows
-- Kept as a row for the posts it published (their targets stay); its credentials go.
UPDATE channels SET deleted_at = @deleted_at, credentials = '', status = 'disabled', last_error = '', updated_at = @deleted_at
WHERE workspace_id = @workspace_id AND id = @id AND deleted_at IS NULL;

-- name: DeleteUnpublishedTargetsOfChannel :exec
-- A removed channel takes the targets that never went out with it; the published ones stay as history.
DELETE FROM post_targets WHERE channel_id = ? AND status <> 'published';

-- name: CountChannels :one
SELECT COUNT(*) FROM channels WHERE workspace_id = ? AND deleted_at IS NULL;

-- name: ExportAllChannels :many
SELECT id, workspace_id, provider, name, handle, avatar_url, profile_url, status, settings, last_error, created_at, updated_at
FROM channels WHERE workspace_id = ? AND deleted_at IS NULL ORDER BY created_at, id;

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

-- name: ConnectedMetaChannels :many
-- Every workspace's connected Facebook and Instagram channels: a Meta webhook says whose permission changed only by
-- ids kept in the sealed credentials.
SELECT id, workspace_id, provider, credentials FROM channels
WHERE provider IN ('facebook', 'instagram', 'instagram_business') AND status = 'connected' AND deleted_at IS NULL;

-- name: MetaChannels :many
-- Every workspace's Facebook, Instagram and Threads channels still kept: a Meta deauthorize or data deletion request
-- names the person only by ids kept in the sealed credentials.
SELECT * FROM channels WHERE provider IN ('facebook', 'instagram', 'instagram_business', 'threads') AND deleted_at IS NULL;

-- name: WipeChannelAccount :exec
-- A data deletion request from the platform: the channel keeps its row and name, loses what came from the account.
UPDATE channels SET credentials = '', avatar_url = '', profile_url = '', status = 'needs_reconnect', last_error = @last_error,
    updated_at = @updated_at
WHERE id = @id;

-- name: ConnectedTikTokChannels :many
-- Every workspace's connected TikTok channels: a TikTok webhook names the account only by the open_id kept in the
-- sealed credentials.
SELECT id, workspace_id, credentials FROM channels WHERE provider = 'tiktok' AND status = 'connected' AND deleted_at IS NULL;

-- name: ChannelsByProfileURL :many
-- A platform's channels (not removed) with this profile link: a Kick webhook names the channel by its slug.
SELECT * FROM channels WHERE provider = ? AND profile_url = ? AND deleted_at IS NULL;

-- name: ConnectedChannelsOfProvider :many
SELECT id, workspace_id FROM channels WHERE provider = ? AND status = 'connected' AND deleted_at IS NULL;

-- name: MarkInboundEvent :execrows
-- Records a webhook call by its id; 0 rows when it was handled before.
INSERT INTO inbound_events (provider, id, received_at) VALUES (?, ?, ?) ON CONFLICT (provider, id) DO NOTHING;

-- name: PruneInboundEvents :exec
DELETE FROM inbound_events WHERE received_at < ?;

-- name: AddChannelActivity :exec
INSERT INTO channel_activity (workspace_id, channel_id, kind, count, at) VALUES (?, ?, ?, ?, ?);

-- name: AnalyticsAudience :many
-- The workspace's channel activity in [from, to), per channel, kind and minute (the report's days are local).
SELECT channel_id, kind, substr(at, 1, 16) AS minute, CAST(SUM(count) AS INTEGER) AS n
FROM channel_activity
WHERE workspace_id = sqlc.arg(workspace_id) AND at >= sqlc.arg(from_at) AND at < sqlc.arg(to_at)
GROUP BY channel_id, kind, substr(at, 1, 16);
