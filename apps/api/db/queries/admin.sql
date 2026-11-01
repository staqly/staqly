-- name: CreateAdminCode :exec
INSERT INTO admin_codes (id, email, code_hash, created_at, expires_at) VALUES (?, ?, ?, ?, ?);

-- name: LatestAdminCode :one
SELECT * FROM admin_codes WHERE email = ? ORDER BY created_at DESC, id DESC LIMIT 1;

-- name: SpendAdminCodeAttempt :execrows
-- Every try counts before the comparison, so concurrent guesses cannot exceed the limit.
UPDATE admin_codes SET attempts = attempts + 1
WHERE id = @id AND used_at IS NULL AND attempts < @max_attempts AND expires_at > @now;

-- name: UseAdminCode :exec
UPDATE admin_codes SET used_at = @used_at WHERE id = @id AND used_at IS NULL;

-- name: CountAdminCodesSince :one
SELECT COUNT(*) FROM admin_codes WHERE email = ? AND created_at > ?;

-- name: PurgeAdminCodes :exec
DELETE FROM admin_codes WHERE created_at < ?;

-- name: CreateAdminSession :exec
INSERT INTO admin_sessions (id, email, token_hash, csrf_token, user_agent, created_at, last_seen_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetAdminSessionByHash :one
SELECT * FROM admin_sessions WHERE token_hash = ?;

-- name: TouchAdminSession :exec
UPDATE admin_sessions SET last_seen_at = @last_seen_at WHERE id = @id;

-- name: RevokeAdminSession :exec
UPDATE admin_sessions SET revoked_at = @revoked_at WHERE id = @id AND revoked_at IS NULL;

-- name: InsertAdminAudit :exec
INSERT INTO admin_audit (id, email, action, workspace_id, target, details, created_at) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListAdminAudit :many
-- Newest first; '' = no filter / no cursor.
SELECT * FROM admin_audit
WHERE (CAST(@workspace_id AS TEXT) = '' OR workspace_id = @workspace_id)
  AND (CAST(@q AS TEXT) = '' OR email LIKE '%' || @q || '%' OR action LIKE '%' || @q || '%' OR target LIKE '%' || @q || '%')
  AND (CAST(@cursor_created_at AS TEXT) = '' OR created_at < @cursor_created_at OR (created_at = @cursor_created_at AND id < @cursor_id))
ORDER BY created_at DESC, id DESC
LIMIT @limit;

-- name: GetAdminByEmail :one
SELECT * FROM admins WHERE email = ?;

-- name: ListAdmins :many
SELECT * FROM admins ORDER BY created_at, id;

-- name: CountAdmins :one
SELECT COUNT(*) FROM admins;

-- name: CreateAdmin :execrows
INSERT OR IGNORE INTO admins (id, email, name, created_at, created_by) VALUES (?, ?, ?, ?, ?);

-- name: DeleteAdmin :execrows
DELETE FROM admins WHERE email = ?;

-- name: TouchAdminLogin :exec
UPDATE admins SET last_login_at = @last_login_at WHERE email = @email;

-- name: RevokeAdminSessionsByEmail :exec
UPDATE admin_sessions SET revoked_at = @revoked_at WHERE email = @email AND revoked_at IS NULL;
