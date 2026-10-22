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

-- name: CountOwnersOtherTrials :one
-- Other workspaces, owned by one of this workspace's owners, whose Stripe trial is running (one trial at a
-- time per person).
SELECT COUNT(*) FROM workspaces w
WHERE w.id <> sqlc.arg(workspace_id) AND w.subscription_status = 'trialing'
  AND EXISTS (SELECT 1 FROM workspace_members m WHERE m.workspace_id = w.id AND m.role = 'owner'
    AND m.user_id IN (SELECT o.user_id FROM workspace_members o WHERE o.workspace_id = sqlc.arg(workspace_id) AND o.role = 'owner'));

-- name: ListBillingToReconcile :many
-- Workspaces whose plan may be out of date with Stripe (a webhook lost or not yet in): a customer without a
-- subscription on record (a checkout that may have completed), or a plan whose period has ended. A live
-- subscription past its period comes first (a renewal webhook lost: someone paying is read-only), then customers
-- without one, then ended subscriptions, which match for good and would otherwise fill every round.
SELECT id, stripe_customer_id FROM workspaces
WHERE stripe_customer_id IS NOT NULL AND stripe_customer_id <> ''
  AND (subscription_status IS NULL OR plan_expires_at < sqlc.arg(now))
ORDER BY CASE WHEN subscription_status IS NULL THEN 1 WHEN subscription_status IN ('canceled', 'incomplete_expired') THEN 2 ELSE 0 END, id
LIMIT 500;
