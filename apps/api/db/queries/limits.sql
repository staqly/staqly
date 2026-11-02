-- name: ResolveLimits :many
-- Every limit with its default, the plan's value and the workspace's (NULL where none is set).
SELECT l.key, l.value, l.description, lp.value AS plan_value, lw.value AS workspace_value, lw.note AS workspace_note
FROM limits l
LEFT JOIN limit_plans lp ON lp.limit_key = l.key AND lp.plan_id = sqlc.arg(plan_id)
LEFT JOIN limit_workspaces lw ON lw.limit_key = l.key AND lw.workspace_id = sqlc.arg(workspace_id)
ORDER BY l.key;

-- name: ListLimits :many
SELECT key, value, description, updated_at FROM limits ORDER BY key;

-- name: ListLimitPlans :many
SELECT limit_key, plan_id, value, updated_at FROM limit_plans ORDER BY limit_key, plan_id;

-- name: SetLimitDefault :execrows
UPDATE limits SET value = ?, updated_at = ? WHERE key = ?;

-- name: UpsertLimitPlan :exec
INSERT INTO limit_plans (limit_key, plan_id, value, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT (limit_key, plan_id) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at;

-- name: DeleteLimitPlan :execrows
DELETE FROM limit_plans WHERE limit_key = ? AND plan_id = ?;

-- name: UpsertLimitWorkspace :exec
INSERT INTO limit_workspaces (limit_key, workspace_id, value, note, updated_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (workspace_id, limit_key) DO UPDATE SET value = excluded.value, note = excluded.note, updated_at = excluded.updated_at;

-- name: DeleteLimitWorkspace :execrows
DELETE FROM limit_workspaces WHERE limit_key = ? AND workspace_id = ?;
