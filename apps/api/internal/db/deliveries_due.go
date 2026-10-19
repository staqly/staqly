package db

import "context"

// DueDeliveries returns pending webhook deliveries that are due, at most
// perWorkspace per workspace (oldest first), so one workspace's backlog can't
// crowd the others out of a batch. Hand-written (a correlated LIMIT per
// workspace). Both steps walk the partial index webhook_deliveries_pending:
// the workspaces with due rows, then each one's oldest perWorkspace, so a
// long backlog costs an index-only scan, not a sort of every due row (SQLite,
// 50k due rows: 240 ms per poll with a window over them, 17 ms now; 1k: 2.8
// to 0.6 ms). ponytail: the DISTINCT still reads every pending index entry;
// skip-scan the workspace ids (recursive CTE) if backlogs reach millions.
func (q *Queries) DueDeliveries(ctx context.Context, now string, perWorkspace, limit int) ([]WebhookDelivery, error) {
	rows, err := q.db.QueryContext(ctx, `
SELECT d.id, d.workspace_id, d.webhook_id, d.event_id, d.event_type, d.status, d.attempts, d.next_attempt_at,
       d.last_status_code, d.last_error, d.last_response, d.last_attempt_at, d.delivered_at, d.created_at
FROM (SELECT DISTINCT p.workspace_id FROM webhook_deliveries p WHERE p.status = 'pending' AND p.next_attempt_at <= ?) w
JOIN webhook_deliveries d ON d.id IN (
    SELECT x.id FROM webhook_deliveries x
    WHERE x.workspace_id = w.workspace_id AND x.status = 'pending' AND x.next_attempt_at <= ?
    ORDER BY x.next_attempt_at
    LIMIT ?)
ORDER BY d.next_attempt_at
LIMIT ?`, now, now, perWorkspace, limit)
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
