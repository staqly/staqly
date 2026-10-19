package db

import "context"

// PostponeWebhookDeliveries moves a webhook's pending deliveries due before until to until, without
// counting an attempt (the webhook's circuit breaker is open). Returns the rows moved.
func (q *Queries) PostponeWebhookDeliveries(ctx context.Context, webhookID, until string) (int64, error) {
	res, err := q.db.ExecContext(ctx, `
UPDATE webhook_deliveries SET next_attempt_at = ?
WHERE webhook_id = ? AND status = 'pending' AND next_attempt_at < ?`, until, webhookID, until)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
