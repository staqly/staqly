-- name: CreateDeviceLink :exec
INSERT INTO device_links (id, workspace_id, token_hash, created_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?);

-- name: GetDeviceLinkByHash :one
SELECT * FROM device_links WHERE token_hash = ?;

-- name: UseDeviceLink :execrows
-- Single use: only the first redeemer flips used_at.
UPDATE device_links SET used_at = @now WHERE id = @id AND used_at IS NULL AND expires_at > @now;

-- name: PurgeDeviceLinks :exec
DELETE FROM device_links WHERE expires_at < ?;
