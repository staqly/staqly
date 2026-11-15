-- +goose Up
-- Chargebacks of a workspace's payments (charge.dispute.*): Stripe's latest status of each, so GET /v1/billing can
-- say a payment is disputed, and the owners and admins hear once per dispute (the first time it is seen).
CREATE TABLE billing_disputes (
  id           TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  status       TEXT NOT NULL,
  updated_at   TEXT NOT NULL
);
CREATE INDEX billing_disputes_workspace ON billing_disputes (workspace_id);

-- +goose Down
DROP TABLE billing_disputes;
