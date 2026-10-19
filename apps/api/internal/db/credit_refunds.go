package db

import "context"

// LowerCreditGrant takes posts back from a bought grant (a refund or a dispute): units drops to at most
// units, never below what was already used and never up (a late, smaller refund changes nothing).
// Hand-written next to the sqlc queries of credits.sql; returns the rows changed.
func (q *Queries) LowerCreditGrant(ctx context.Context, source string, units int64) (int64, error) {
	res, err := q.db.ExecContext(ctx, `
UPDATE credit_grants SET units = CASE WHEN used > ? THEN used ELSE ? END
WHERE source = ? AND units > used AND units > ?`, units, units, source, units)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
