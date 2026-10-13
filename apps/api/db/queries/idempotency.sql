-- name: ReserveIdempotencyKey :execrows
INSERT INTO idempotency_keys (workspace_id, principal_id, key, request_hash, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING;

-- name: GetIdempotencyKey :one
SELECT * FROM idempotency_keys WHERE workspace_id = ? AND principal_id = ? AND key = ?;

-- name: CompleteIdempotencyKey :exec
UPDATE idempotency_keys SET status_code = ?, response_body = ?
WHERE workspace_id = ? AND principal_id = ? AND key = ?;

-- name: ReleaseIdempotencyKey :exec
DELETE FROM idempotency_keys WHERE workspace_id = ? AND principal_id = ? AND key = ?;

-- name: PurgeIdempotencyKeys :exec
DELETE FROM idempotency_keys WHERE created_at < ?;
