-- name: CreateWebhook :exec
INSERT INTO webhooks (id, workspace_id, url, description, secret, events, collection_ids, active, timeout_ms, max_attempts, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetWebhook :one
SELECT * FROM webhooks WHERE workspace_id = ? AND id = ?;

-- name: ListWebhooks :many
SELECT * FROM webhooks WHERE workspace_id = ? ORDER BY created_at;

-- name: ListActiveWebhooks :many
SELECT * FROM webhooks WHERE workspace_id = ? AND active = 1;

-- name: UpdateWebhook :exec
UPDATE webhooks
SET url = ?, description = ?, secret = ?, events = ?, collection_ids = ?, active = ?, timeout_ms = ?, max_attempts = ?, updated_at = ?
WHERE workspace_id = ? AND id = ?;

-- name: DeleteWebhook :execrows
DELETE FROM webhooks WHERE workspace_id = ? AND id = ?;

-- name: InsertDelivery :exec
INSERT INTO webhook_deliveries (id, workspace_id, webhook_id, event_id, event_type, next_attempt_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (webhook_id, event_id) DO NOTHING;

-- name: LeaseDelivery :execrows
-- Claims a delivery by pushing next_attempt_at forward; a crash mid-request
-- leaves it pending so it is retried after the lease expires.
UPDATE webhook_deliveries
SET next_attempt_at = @lease_until, attempts = attempts + 1, last_attempt_at = @now
WHERE id = @id AND status = 'pending' AND next_attempt_at = @expected_next;

-- name: FinishDelivery :exec
UPDATE webhook_deliveries
SET status = ?, next_attempt_at = ?, last_status_code = ?, last_error = ?, last_response = ?, delivered_at = ?
WHERE id = ?;

-- name: ListDeliveries :many
SELECT * FROM webhook_deliveries
WHERE workspace_id = @workspace_id
  AND (sqlc.narg(webhook_id) IS NULL OR webhook_id = sqlc.narg(webhook_id))
  AND (sqlc.narg(status) IS NULL OR status = sqlc.narg(status))
ORDER BY created_at DESC
LIMIT @lim;

-- name: GetDelivery :one
SELECT * FROM webhook_deliveries WHERE workspace_id = ? AND id = ?;

-- name: RequeueDelivery :execrows
UPDATE webhook_deliveries
SET status = 'pending', next_attempt_at = ?, attempts = 0, last_error = ''
WHERE workspace_id = ? AND id = ? AND status <> 'pending';
