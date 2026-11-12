-- +goose Up
-- Why the workspace's subscription ended, when Stripe says a payment failed (cancellation_details.reason
-- payment_failed, or the status unpaid): 'payment_failed'; NULL otherwise and for a live subscription. GET
-- /v1/billing reports it (ended_for), so a read-only workspace is told the truth about why.
ALTER TABLE workspaces ADD COLUMN subscription_ended_for TEXT;

-- +goose Down
ALTER TABLE workspaces DROP COLUMN subscription_ended_for;
