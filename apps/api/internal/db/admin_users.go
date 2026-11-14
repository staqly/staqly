package db

import "context"

// Admin panel session revocations, written by hand (no sqlc): revoked_at is set only where it is still NULL,
// so the count is the sessions that were live.

func (q *Queries) RevokeWorkspaceSessions(ctx context.Context, workspaceID, at string) (int64, error) {
	res, err := q.db.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE workspace_id = ? AND revoked_at IS NULL`, at, workspaceID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (q *Queries) RevokeUserSessions(ctx context.Context, userID, at string) (int64, error) {
	res, err := q.db.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, at, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (q *Queries) DeleteUserRow(ctx context.Context, userID string) error {
	_, err := q.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
	return err
}
