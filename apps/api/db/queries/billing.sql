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

-- name: CountOtherAccountTrials :one
-- Other workspaces of the same account whose Stripe trial is running (one trial at a time per account).
SELECT COUNT(*) FROM workspaces
WHERE account_id = (SELECT w.account_id FROM workspaces w WHERE w.id = sqlc.arg(workspace_id))
  AND id <> sqlc.arg(workspace_id) AND subscription_status = 'trialing';

-- name: ListBillingToReconcile :many
-- Workspaces whose plan may be out of date with Stripe (a webhook lost or not yet in): a customer without a
-- subscription on record (a checkout that may have completed), or a plan whose period has ended.
SELECT id, stripe_customer_id FROM workspaces
WHERE stripe_customer_id IS NOT NULL AND stripe_customer_id <> ''
  AND (subscription_status IS NULL OR plan_expires_at < sqlc.arg(now))
ORDER BY id LIMIT 500;
