-- +goose Up
-- The webhook worker's poll (db.DueDeliveries) reads due deliveries per workspace: this index serves it
-- without a scan of every due row, so a long backlog (receivers down for hours) keeps the poll cheap.
CREATE INDEX webhook_deliveries_pending ON webhook_deliveries (workspace_id, next_attempt_at) WHERE status = 'pending';

-- +goose Down
DROP INDEX webhook_deliveries_pending;
