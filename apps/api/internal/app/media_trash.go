package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/staqly/staqly/apps/api/internal/blob"
	"github.com/staqly/staqly/apps/api/internal/db"
)

// MediaTrashDays is how long a deleted file waits in Deleted (restorable) before the retention sweep removes it.
const MediaTrashDays = 30

// TrashMedia moves a file to Deleted: it stays restorable (and counts toward storage) until the sweep removes it.
// A file that a scheduled or publishing post uses is refused (409), so nothing goes out without its media.
// Trashing a file already in Deleted does nothing.
func (s *Service) TrashMedia(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopePostsDelete); err != nil {
		return err
	}
	m, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err != nil {
		return notFound("media")
	}
	if m.DeletedAt.Valid {
		return nil
	}
	return s.tx(ctx, func(q *db.Queries) error { // checked with the write: a post scheduled meanwhile counts
		if n, err := upcomingUses(ctx, q, p.WorkspaceID, id); err != nil {
			return err
		} else if n > 0 {
			return conflict(fmt.Sprintf("%s is used by %d scheduled %s; remove it from %s first", m.OriginalName, n,
				plural(n, "post", "posts"), plural(n, "that post", "those posts")))
		}
		_, err := q.TrashMedia(ctx, db.TrashMediaParams{DeletedAt: nullStr(s.now()), WorkspaceID: p.WorkspaceID, ID: id})
		return err
	})
}

// upcomingUses counts the scheduled or publishing posts that carry the file (in their files or their thread).
func upcomingUses(ctx context.Context, q *db.Queries, ws, id string) (int, error) {
	n, err := q.UpcomingMediaUses(ctx, db.UpcomingMediaUsesParams{WorkspaceID: ws, Quoted: `"` + id + `"`})
	return int(n), err
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// RestoreMedia takes a file out of Deleted, back where it was.
func (s *Service) RestoreMedia(ctx context.Context, p Principal, id string) (Media, error) {
	if err := p.require(ScopePostsDelete); err != nil {
		return Media{}, err
	}
	if _, err := s.W.RestoreMedia(ctx, db.RestoreMediaParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
		return Media{}, err
	}
	m, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err != nil {
		return Media{}, notFound("media")
	}
	return s.toMedia(m), nil
}

// KeepMedia keeps one file of a set of identical ones: posts that use a copy use the kept file instead (in one
// transaction), and the copies go to Deleted. It answers the ids moved to Deleted.
func (s *Service) KeepMedia(ctx context.Context, p Principal, id string) ([]string, error) {
	if err := p.require(ScopePostsDelete); err != nil {
		return nil, err
	}
	if err := p.require(ScopePostsWrite); err != nil {
		return nil, err
	}
	keep, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err != nil || keep.DeletedAt.Valid {
		return nil, notFound("media")
	}
	var trashed []string
	err = s.tx(ctx, func(q *db.Queries) error {
		trashed = []string{} // fn reruns on a PostgreSQL serialization retry
		now := s.now()
		for {
			twin, err := q.OldestTwin(ctx, db.OldestTwinParams{WorkspaceID: p.WorkspaceID, Sha256: keep.Sha256, ID: keep.ID})
			if isNoRows(err) {
				return nil
			} else if err != nil {
				return err
			}
			if err := q.RepointPostMedia(ctx, db.RepointPostMediaParams{FromQuoted: `"` + twin + `"`, ToQuoted: `"` + keep.ID + `"`,
				UpdatedAt: now, WorkspaceID: p.WorkspaceID}); err != nil {
				return err
			}
			if _, err := q.TrashMedia(ctx, db.TrashMediaParams{DeletedAt: nullStr(now), WorkspaceID: p.WorkspaceID, ID: twin}); err != nil {
				return err
			}
			trashed = append(trashed, twin)
		}
	})
	return trashed, err
}

// purgeTrashedMedia removes files that have been in Deleted for longer than MediaTrashDays, for good: the file
// first, then the record (posts referencing it lose it). A file the store can't delete keeps its record, so the
// next run tries again.
// ponytail: file before row, with no lock between: a file restored in the moment between the PurgeableMedia read
// and its row's delete loses its file and record. The ceiling is that window (milliseconds, once per file, 30
// days after it went to Deleted); a conditional delete that commits before the file goes would close it at the
// cost of orphan files when the store fails.
func (s *Service) purgeTrashedMedia(ctx context.Context, now time.Time) (int64, error) {
	cutoff := nullStr(formatTime(now.Add(-MediaTrashDays * 24 * time.Hour)))
	var purged int64
	for {
		rows, err := s.Q.PurgeableMedia(ctx, db.PurgeableMediaParams{Cutoff: cutoff, Lim: retentionBatch})
		if err != nil {
			return purged, err
		}
		done := 0
		for _, r := range rows {
			if err := s.removeMediaFiles(ctx, r.WorkspaceID, r.StorageName); err != nil && !errors.Is(err, blob.ErrNotExist) {
				s.Log.Warn("deleted media file not removed, retrying next run", "media_id", r.ID, "err", err)
				continue
			}
			if err := s.tx(ctx, func(q *db.Queries) error {
				if _, err := q.DeleteMedia(ctx, db.DeleteMediaParams{WorkspaceID: r.WorkspaceID, ID: r.ID}); err != nil {
					return err
				}
				return s.stripMediaFromPosts(ctx, q, r.WorkspaceID, r.ID)
			}); err != nil {
				return purged, err
			}
			purged++
			done++
		}
		// A batch where nothing could go (the store is down) stops the run instead of looping on the same rows.
		if len(rows) < retentionBatch || done == 0 {
			return purged, nil
		}
	}
}
