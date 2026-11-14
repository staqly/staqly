package db

import "context"

// LowerCreditGrant takes posts back from a bought grant (a refund or a dispute): units drops to units, never up
// (a late, smaller refund changes nothing). It may end below used: such a grant has nothing left (readers want
// used < units), and stays so when a publish that held one of its posts is rejected and gives it back.
// Hand-written next to the sqlc queries of credits.sql; returns the rows changed.
func (q *Queries) LowerCreditGrant(ctx context.Context, source string, units int64) (int64, error) {
	res, err := q.db.ExecContext(ctx, `
UPDATE credit_grants SET units = ? WHERE source = ? AND units > ?`, units, source, units)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
