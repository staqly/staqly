-- name: GetUserByIdentity :one
SELECT u.* FROM users u JOIN user_identities i ON i.user_id = u.id WHERE i.provider = ? AND i.subject = ?;

-- name: OldestUserByEmail :one
-- The person who has this (lowercased) address; the oldest when an earlier version made two.
SELECT * FROM users WHERE email = ? ORDER BY created_at, id LIMIT 1;

-- name: CreateIdentity :exec
INSERT INTO user_identities (provider, subject, user_id, email, created_at, last_login_at) VALUES (?, ?, ?, ?, ?, ?);

-- name: TouchIdentity :exec
UPDATE user_identities SET email = ?, last_login_at = ? WHERE provider = ? AND subject = ?;

-- name: GetUser :one
SELECT * FROM users WHERE id = ?;

-- name: CreateUser :exec
INSERT INTO users (id, email, name, avatar_url, created_at, updated_at, last_login_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpdateUserProfile :exec
-- The identity provider (Google) is the source of the name, address and picture: refreshed at every sign-in.
UPDATE users SET email = ?, name = ?, avatar_url = ?, updated_at = ?, last_login_at = ? WHERE id = ?;

-- name: SetUserAvatar :exec
-- A picture the person uploaded (a data: URL), or '' to drop it.
UPDATE users SET avatar_url = ?, updated_at = ? WHERE id = ?;

-- name: AddMember :execrows
-- Idempotent: a second accept of the same invitation (or a race) adds nothing.
INSERT INTO workspace_members (workspace_id, user_id, role, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (workspace_id, user_id) DO NOTHING;

-- name: GetMember :one
SELECT * FROM workspace_members WHERE workspace_id = ? AND user_id = ?;

-- name: ListMembers :many
SELECT m.workspace_id, m.user_id, m.role, m.created_at, m.updated_at, u.email, u.name, u.avatar_url
FROM workspace_members m JOIN users u ON u.id = m.user_id
WHERE m.workspace_id = ?
ORDER BY m.created_at, m.user_id;

-- name: UpdateMemberRole :execrows
UPDATE workspace_members SET role = ?, updated_at = ? WHERE workspace_id = ? AND user_id = ?;

-- name: DeleteMember :execrows
DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?;

-- name: CountMembers :one
-- Billed seats: every member, whatever the role (open invitations are not billed until accepted).
SELECT COUNT(*) FROM workspace_members WHERE workspace_id = ?;

-- name: CountOwners :one
SELECT COUNT(*) FROM workspace_members WHERE workspace_id = ? AND role = 'owner';

-- name: ListUserWorkspaces :many
SELECT w.id, w.name, m.role
FROM workspace_members m JOIN workspaces w ON w.id = m.workspace_id
WHERE m.user_id = ?
ORDER BY w.created_at, w.id;

-- name: CreateInvitation :exec
INSERT INTO workspace_invitations (id, workspace_id, email, role, token_hash, invited_by, created_at, updated_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetInvitationByHash :one
SELECT * FROM workspace_invitations WHERE token_hash = ?;

-- name: GetInvitation :one
SELECT * FROM workspace_invitations WHERE workspace_id = ? AND id = ?;

-- name: ListOpenInvitations :many
SELECT * FROM workspace_invitations
WHERE workspace_id = ? AND accepted_at IS NULL AND revoked_at IS NULL
ORDER BY created_at DESC, id;

-- name: RefreshInvitation :execrows
-- Resend: a new token (the old link stops working) and a new expiry.
UPDATE workspace_invitations SET token_hash = ?, expires_at = ?, updated_at = ?
WHERE workspace_id = ? AND id = ? AND accepted_at IS NULL AND revoked_at IS NULL;

-- name: RevokeInvitation :execrows
UPDATE workspace_invitations SET revoked_at = @now, updated_at = @now
WHERE workspace_id = @workspace_id AND id = @id AND accepted_at IS NULL AND revoked_at IS NULL;

-- name: AcceptInvitation :execrows
-- Single use: only one accept wins (concurrent accepts: the other gets 0 rows).
UPDATE workspace_invitations SET accepted_at = @now, accepted_by = @user_id, updated_at = @now
WHERE id = @id AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > @now;

-- name: CreateUserSession :exec
INSERT INTO sessions (id, workspace_id, token_hash, csrf_token, user_agent, created_at, last_seen_at, expires_at, user_id, guest_role)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: SetSessionWorkspace :execrows
UPDATE sessions SET workspace_id = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL;

-- name: RevokeUserSessionsInWorkspace :execrows
-- A member removed from a workspace is signed out of it everywhere.
UPDATE sessions SET revoked_at = @now WHERE workspace_id = @workspace_id AND user_id = @user_id AND revoked_at IS NULL;

-- name: SetAPIKeyCreator :exec
UPDATE api_keys SET created_by_user_id = ? WHERE id = ?;

-- name: RevokeAnonymousSessions :execrows
-- A workspace from before accounts joins one: browser sessions without a person behind them end.
UPDATE sessions SET revoked_at = @now WHERE workspace_id = @workspace_id AND user_id IS NULL AND revoked_at IS NULL;

-- name: AdoptAPIKeys :execrows
-- And its API keys without a creator get the new owner as theirs, so they follow the owner's role.
UPDATE api_keys SET created_by_user_id = @user_id
WHERE workspace_id = @workspace_id AND created_by_user_id IS NULL AND revoked_at IS NULL;

-- name: ExpireAnonymousDeviceLinks :execrows
-- And its unused connect links without a person behind them stop working.
UPDATE device_links SET expires_at = @now WHERE workspace_id = @workspace_id AND user_id = '' AND used_at IS NULL AND expires_at > @now;

-- name: GetUserNotificationPrefs :one
SELECT notification_prefs FROM users WHERE id = ?;

-- name: SetUserNotificationPrefs :exec
UPDATE users SET notification_prefs = ?, updated_at = ? WHERE id = ?;
