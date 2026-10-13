package db

import "context"

// DueDeliveries returns pending webhook deliveries that are due, at most
// perWorkspace per workspace (oldest first), so one workspace's backlog can't
// crowd the others out of a batch. Hand-written because sqlc's SQLite parser
// does not resolve window-function columns in a subquery.
// ponytail: the window scans every due row per poll; fine for thousands of
// due rows, switch to a per-workspace cursor if backlogs get much larger.
func (q *Queries) DueDeliveries(ctx context.Context, now string, perWorkspace, limit int) ([]WebhookDelivery, error) {
	rows, err := q.db.QueryContext(ctx, `
SELECT id, workspace_id, webhook_id, event_id, event_type, status, attempts, next_attempt_at,
       last_status_code, last_error, last_response, last_attempt_at, delivered_at, created_at
FROM (
    SELECT d.*, ROW_NUMBER() OVER (PARTITION BY d.workspace_id ORDER BY d.next_attempt_at) AS rn
    FROM webhook_deliveries d
    WHERE d.status = 'pending' AND d.next_attempt_at <= ?
)
WHERE rn <= ?
ORDER BY next_attempt_at
LIMIT ?`, now, perWorkspace, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []WebhookDelivery
	for rows.Next() {
		var i WebhookDelivery
		if err := rows.Scan(&i.ID, &i.WorkspaceID, &i.WebhookID, &i.EventID, &i.EventType, &i.Status, &i.Attempts,
			&i.NextAttemptAt, &i.LastStatusCode, &i.LastError, &i.LastResponse, &i.LastAttemptAt, &i.DeliveredAt,
			&i.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}
