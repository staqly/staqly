-- name: CreateDeviceLink :exec
INSERT INTO device_links (id, workspace_id, token_hash, created_by, created_at, expires_at, user_id, max_uses, guest_role)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDeviceLinkByHash :one
SELECT * FROM device_links WHERE token_hash = ?;

-- name: UseDeviceLink :execrows
-- max_uses redeemers (0: any number until it expires); used_at is the last use.
UPDATE device_links SET used_at = @now, uses = uses + 1
WHERE id = @id AND (max_uses = 0 OR uses < max_uses) AND expires_at > @now;

-- name: PurgeDeviceLinks :exec
DELETE FROM device_links WHERE expires_at < ?;
