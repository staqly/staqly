package app

import (
	"context"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// ArchivePost archives a post (archived true) or brings it back. An archived post stays as it is but is left out of
// the post list and the calendar unless they ask for archived posts. A post on its way out (scheduled, publishing)
// can't be archived: it would go out unseen.
func (s *Service) ArchivePost(ctx context.Context, p Principal, id string, archived bool) (Post, error) {
	if err := p.require(ScopePostsWrite); err != nil {
		return Post{}, err
	}
	err := s.tx(ctx, func(q *db.Queries) error {
		row, err := q.GetPost(ctx, db.GetPostParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("post")
		}
		if err != nil {
			return err
		}
		if archived && (row.Status == "scheduled" || row.Status == "publishing") {
			return conflict("a scheduled or publishing post can't be archived; take it off the schedule first")
		}
		if row.ArchivedAt.Valid == archived {
			return nil
		}
		now := s.now()
		at := ""
		if archived {
			at = now
		}
		_, err = q.SetPostArchived(ctx, db.SetPostArchivedParams{ArchivedAt: nullStr(at), UpdatedAt: now, WorkspaceID: p.WorkspaceID, ID: id})
		return err
	})
	if err != nil {
		return Post{}, err
	}
	return s.getPost(ctx, p, id)
}
