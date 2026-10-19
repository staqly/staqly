-- +goose Up
-- Stripe billing (cloud): the workspace's customer and subscription, and the
-- Stripe events already applied (webhooks are retried, apply each once).
ALTER TABLE workspaces ADD COLUMN stripe_customer_id TEXT;
ALTER TABLE workspaces ADD COLUMN stripe_subscription_id TEXT;
ALTER TABLE workspaces ADD COLUMN subscription_status TEXT;
CREATE INDEX workspaces_stripe_customer ON workspaces (stripe_customer_id);
CREATE TABLE stripe_events (
  id         TEXT PRIMARY KEY,
  created_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE stripe_events;
DROP INDEX workspaces_stripe_customer;
ALTER TABLE workspaces DROP COLUMN subscription_status;
ALTER TABLE workspaces DROP COLUMN stripe_subscription_id;
ALTER TABLE workspaces DROP COLUMN stripe_customer_id;
