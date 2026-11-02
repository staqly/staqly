-- name: GetPublishingAllowanceCharge :one
SELECT COUNT(*) FROM usage_events WHERE workspace_id = ? AND ref = ? AND provider IN ('x', 'linkedin', 'linkedin_video');

-- name: GetPublishingAllowanceUnits :one
SELECT CAST(COALESCE(SUM(units), 0) AS INTEGER) FROM usage_events WHERE workspace_id = ? AND ref = ? AND provider = ?;

-- name: InsertPublishingAllowanceReservation :exec
INSERT INTO publishing_allowance_reservations (workspace_id, ref, credit_draws) VALUES (?, ?, ?);

-- name: GetPublishingAllowanceReservation :one
SELECT credit_draws FROM publishing_allowance_reservations WHERE workspace_id = ? AND ref = ?;

-- name: DeletePublishingAllowanceReservation :exec
DELETE FROM publishing_allowance_reservations WHERE workspace_id = ? AND ref = ?;

-- name: RefundPublishingAllowanceCharge :exec
DELETE FROM usage_events WHERE workspace_id = ? AND ref = ? AND provider IN ('x', 'linkedin', 'linkedin_video');

-- name: RefundPublishingCreditGrant :exec
UPDATE credit_grants SET used = CASE WHEN used > sqlc.arg(units) THEN used - sqlc.arg(units) ELSE 0 END
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: GetPublishingCreditGrantUsed :one
SELECT used FROM credit_grants WHERE workspace_id = ? AND id = ?;
