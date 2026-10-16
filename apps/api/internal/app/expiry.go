package app

import (
	"context"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// expireItems moves pending items past their expires_at (set by expires_in
// on creation) to the "expired" status.
func (s *Service) expireItems(ctx context.Context) {
	due, err := s.Q.ExpiredItems(ctx, nullStr(s.now()))
	if err != nil {
		return
	}
	for _, d := range due {
		actor := Principal{WorkspaceID: d.WorkspaceID, Kind: PrincipalSystem, ID: "expiry", Name: "Expiry"}
		err := s.tx(ctx, func(q *db.Queries) error {
			if err := q.ClearItemExpiry(ctx, d.ID); err != nil {
				return err
			}
			_, err := s.mutateItem(ctx, q, actor, d.ID, nil, func(cur *db.Item, _ Collection) (string, map[string]any, error) {
				if cur.Status != "pending" {
					return "", nil, conflict("no longer pending")
				}
				cur.Status, cur.DecidedAt, cur.Feedback = "expired", nullStr(s.now()), "Expired before review"
				return "item.expired", map[string]any{"feedback": cur.Feedback}, nil
			})
			return err
		})
		if err != nil && !isConflict(err) {
			s.logger(ctx).Warn("expire item", "workspace_id", d.WorkspaceID, "item_id", d.ID, "err", err)
		}
	}
}
