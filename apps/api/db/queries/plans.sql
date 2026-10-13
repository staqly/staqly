-- name: GetWorkspacePlan :one
SELECT plan FROM workspaces WHERE id = ?;

-- name: SetWorkspacePlan :execrows
UPDATE workspaces SET plan = ?, updated_at = ? WHERE id = ?;

-- name: ListWorkspaces :many
SELECT id, name, plan, created_at FROM workspaces ORDER BY created_at;

-- name: IncrementUsage :one
INSERT INTO usage_counters (workspace_id, period, metric, count) VALUES (?, ?, ?, 1)
ON CONFLICT (workspace_id, period, metric) DO UPDATE SET count = count + 1
RETURNING count;

-- name: GetUsage :one
SELECT count FROM usage_counters WHERE workspace_id = ? AND period = ? AND metric = ?;

-- name: CountWebhooks :one
SELECT COUNT(*) FROM webhooks WHERE workspace_id = ?;

-- name: CountActiveAPIKeys :one
SELECT COUNT(*) FROM api_keys WHERE workspace_id = ? AND revoked_at IS NULL;

-- name: StorageUsed :one
SELECT CAST(COALESCE(SUM(size_bytes), 0) AS INTEGER) AS used FROM media WHERE workspace_id = ?;
