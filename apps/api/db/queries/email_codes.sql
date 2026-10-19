-- name: CreateEmailCode :exec
INSERT INTO email_codes (id, email, purpose, user_id, code_hash, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: CloseEmailCodes :exec
-- A new code replaces the open one for the same address, purpose and person.
UPDATE email_codes SET used_at = @now WHERE email = @email AND purpose = @purpose AND user_id = @user_id AND used_at IS NULL;

-- name: LatestEmailCode :one
SELECT * FROM email_codes WHERE email = ? AND purpose = ? AND user_id = ? ORDER BY created_at DESC, id DESC LIMIT 1;

-- name: CountEmailCodesSince :one
SELECT COUNT(*) FROM email_codes WHERE email = ? AND created_at > ?;

-- name: SpendEmailCodeAttempt :execrows
-- Every try counts before the comparison, so concurrent guesses cannot exceed the limit.
UPDATE email_codes SET attempts = attempts + 1
WHERE id = @id AND used_at IS NULL AND attempts < @max_attempts AND expires_at > @now;

-- name: UseEmailCode :execrows
UPDATE email_codes SET used_at = @now WHERE id = @id AND used_at IS NULL;

-- name: PurgeEmailCodes :exec
DELETE FROM email_codes WHERE created_at < ?;

-- name: ListUserIdentities :many
SELECT * FROM user_identities WHERE user_id = ? ORDER BY created_at, provider, subject;

-- name: GetIdentity :one
SELECT * FROM user_identities WHERE provider = ? AND subject = ?;

-- name: DeleteIdentity :execrows
DELETE FROM user_identities WHERE provider = ? AND subject = ? AND user_id = ?;

-- name: MarkEmailCodeVerified :execrows
-- Changing the address: the code to the current address is proved and stays open (for a fresh TTL) until
-- the change is made with a code to the new address.
UPDATE email_codes SET verified_at = @now, expires_at = @expires_at WHERE id = @id AND used_at IS NULL AND verified_at IS NULL;

-- name: ChangeIdentitySubject :execrows
UPDATE user_identities SET subject = @new_subject, email = @email
WHERE provider = @provider AND subject = @subject AND user_id = @user_id;

-- name: ChangeUserEmail :exec
-- The person's contact address follows the sign-in address it was.
UPDATE users SET email = @email, updated_at = @now WHERE id = @id AND email = @old_email;
