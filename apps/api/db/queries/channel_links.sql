-- name: CreateChannelLink :exec
INSERT INTO channel_links (id, workspace_id, token_hash, note, providers, group_id, max_uses, created_by, created_by_name, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListChannelLinks :many
SELECT * FROM channel_links WHERE workspace_id = ? ORDER BY created_at DESC LIMIT 100;

-- name: GetChannelLinkByHash :one
SELECT * FROM channel_links WHERE token_hash = ?;

-- name: GetChannelLink :one
SELECT * FROM channel_links WHERE workspace_id = ? AND id = ?;

-- name: RevokeChannelLink :execrows
UPDATE channel_links SET revoked_at = @now WHERE workspace_id = @workspace_id AND id = @id AND revoked_at IS NULL;

-- name: UseChannelLink :execrows
-- Takes one use: only while the link is open, so two people racing for the last use can't both get it.
UPDATE channel_links SET uses = uses + 1
WHERE workspace_id = @workspace_id AND id = @id AND revoked_at IS NULL AND expires_at > @now AND uses < max_uses;

-- name: ReleaseChannelLink :exec
-- Gives a use back when connecting failed after it was taken.
UPDATE channel_links SET uses = uses - 1 WHERE workspace_id = ? AND id = ? AND uses > 0;
