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

-- name: TakeOverIdempotencyKey :execrows
-- A reservation whose request never finished (the process died with it in flight) is taken by the retry.
UPDATE idempotency_keys SET created_at = @created_at
WHERE workspace_id = @workspace_id AND principal_id = @principal_id AND key = @key
  AND status_code IS NULL AND created_at < @stale_before;
