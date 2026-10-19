-- name: GetWorkspaceBilling :one
SELECT id, name, plan, plan_expires_at, stripe_customer_id, stripe_subscription_id, subscription_status
FROM workspaces WHERE id = ?;

-- name: SetStripeCustomer :exec
UPDATE workspaces SET stripe_customer_id = ?, updated_at = ? WHERE id = ?;

-- name: SetSubscription :exec
UPDATE workspaces SET stripe_subscription_id = ?, subscription_status = ?, updated_at = ? WHERE id = ?;

-- name: GetWorkspaceIDByStripeCustomer :one
SELECT id FROM workspaces WHERE stripe_customer_id = ?;

-- name: InsertStripeEvent :execrows
INSERT OR IGNORE INTO stripe_events (id, created_at) VALUES (?, ?);
