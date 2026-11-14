-- name: InsertUsageEvent :execrows
INSERT INTO usage_events (workspace_id, provider, ref, period, units, included, credits, overage, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (workspace_id, provider, ref) DO NOTHING;

-- name: SumIncludedUsage :one
SELECT CAST(COALESCE(SUM(included), 0) AS INTEGER) AS n FROM usage_events
WHERE workspace_id = ? AND provider = ? AND period = ?;

-- name: SumUnitsUsage :one
SELECT CAST(COALESCE(SUM(units), 0) AS INTEGER) AS n FROM usage_events
WHERE workspace_id = ? AND provider = ? AND period = ?;

-- name: ListLiveCreditGrants :many
-- Grants with credits left that haven't expired, oldest first (they are used in that order).
SELECT * FROM credit_grants
WHERE workspace_id = ? AND provider = ? AND expires_at > ? AND used < units
ORDER BY created_at, id;

-- name: UseCreditGrant :exec
UPDATE credit_grants SET used = used + ? WHERE id = ?;

-- name: InsertCreditGrant :execrows
INSERT INTO credit_grants (id, workspace_id, provider, units, used, source, expires_at, created_at)
VALUES (?, ?, ?, ?, 0, ?, ?, ?)
ON CONFLICT (source) DO NOTHING;
