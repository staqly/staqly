-- name: CreatePasskey :exec
INSERT INTO passkeys (id, user_id, credential_id, name, credential, created_at) VALUES (?, ?, ?, ?, ?, ?);

-- name: ListUserPasskeys :many
SELECT * FROM passkeys WHERE user_id = ? ORDER BY created_at, id;

-- name: GetPasskeyByCredential :one
SELECT * FROM passkeys WHERE credential_id = ?;

-- name: UpdatePasskeyCredential :exec
-- After a sign-in: the new sign counter (and flags) and when it was used.
UPDATE passkeys SET credential = @credential, last_used_at = @now WHERE id = @id;

-- name: RenamePasskey :execrows
UPDATE passkeys SET name = @name WHERE id = @id AND user_id = @user_id;

-- name: DeletePasskey :execrows
DELETE FROM passkeys WHERE id = @id AND user_id = @user_id;

-- name: CreatePasskeyChallenge :exec
INSERT INTO passkey_challenges (id, purpose, user_id, session, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?);

-- name: TakePasskeyChallenge :one
-- Single use: the ceremony's challenge is deleted as it is read.
DELETE FROM passkey_challenges WHERE id = @id AND purpose = @purpose AND user_id = @user_id AND expires_at > @now
RETURNING *;

-- name: PurgePasskeyChallenges :exec
DELETE FROM passkey_challenges WHERE created_at < ?;
