-- Scrape-time gauges for /metrics. They aggregate across all workspaces on
-- purpose (operator view); no workspace id is ever exported as a label.

-- name: ObsDeliveryCounts :many
SELECT status, COUNT(*) AS n FROM webhook_deliveries GROUP BY status;

-- name: ObsPostCounts :many
-- ponytail: full index scan per scrape; fine up to millions of posts at a 15-60s scrape interval.
SELECT status, COUNT(*) AS n FROM posts GROUP BY status;

-- name: ObsChannelCounts :many
SELECT status, COUNT(*) AS n FROM channels GROUP BY status;

-- name: ObsOldestPendingDelivery :one
SELECT CAST(COALESCE(MIN(created_at), '') AS TEXT) AS oldest FROM webhook_deliveries WHERE status = 'pending';

-- name: ObsDueDeliveries :one
SELECT COUNT(*) FROM webhook_deliveries WHERE status = 'pending' AND next_attempt_at <= ?;
