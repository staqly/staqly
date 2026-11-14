-- name: CreateTelegramCode :exec
INSERT INTO telegram_connect_codes (code, workspace_id, created_by, actor_name, group_id, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetTelegramCode :one
SELECT * FROM telegram_connect_codes WHERE workspace_id = ? AND code = ?;

-- name: FindTelegramCode :one
-- The code a /connect names, still open (not expired, not used).
SELECT * FROM telegram_connect_codes WHERE code = ? AND expires_at > ? AND channel_id = '';

-- name: FinishTelegramCode :exec
UPDATE telegram_connect_codes SET channel_id = ?, error = ? WHERE code = ?;

-- name: DeleteOldTelegramCodes :exec
DELETE FROM telegram_connect_codes WHERE expires_at < ?;
