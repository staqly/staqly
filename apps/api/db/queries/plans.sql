-- name: GetWorkspacePlan :one
SELECT plan, plan_expires_at, entitlements FROM workspaces WHERE id = ?;

-- name: SetWorkspacePlan :execrows
UPDATE workspaces SET plan = ?, plan_expires_at = ?, updated_at = ? WHERE id = ?;

-- name: ListWorkspaces :many
SELECT id, name, plan, plan_expires_at, created_at FROM workspaces ORDER BY created_at;

-- name: IncrementUsage :one
INSERT INTO usage_counters (workspace_id, period, metric, count) VALUES (?, ?, ?, 1)
ON CONFLICT (workspace_id, period, metric) DO UPDATE SET count = usage_counters.count + 1
RETURNING count;

-- name: GetUsage :one
SELECT count FROM usage_counters WHERE workspace_id = ? AND period = ? AND metric = ?;

-- name: CountWebhooks :one
SELECT COUNT(*) FROM webhooks WHERE workspace_id = ?;

-- name: CountActiveAPIKeys :one
SELECT COUNT(*) FROM api_keys WHERE workspace_id = @workspace_id AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > @now);

-- name: StorageUsed :one
-- Library files (Deleted included) and design editor project files.
SELECT CAST((SELECT COALESCE(SUM(size_bytes), 0) FROM media WHERE media.workspace_id = @workspace_id)
  + (SELECT COALESCE(SUM(size_bytes), 0) FROM media_projects WHERE media_projects.workspace_id = @workspace_id) AS INTEGER) AS used;

-- name: UsageTotals :many
SELECT metric, CAST(SUM(count) AS INTEGER) AS total, COUNT(DISTINCT workspace_id) AS workspaces
FROM usage_counters WHERE period = ? GROUP BY metric ORDER BY metric;

-- name: WebhookDeliveryBacklog :one
SELECT CAST(COALESCE(SUM(status = 'pending' AND attempts = 0), 0) AS INTEGER) AS queued,
       CAST(COALESCE(SUM(status = 'pending' AND attempts > 0), 0) AS INTEGER) AS retrying,
       CAST(COALESCE(SUM(status = 'failed'), 0) AS INTEGER) AS failed
FROM webhook_deliveries WHERE workspace_id = ?;

-- name: SetWorkspaceEntitlements :exec
UPDATE workspaces SET entitlements = ?, updated_at = ? WHERE id = ?;
