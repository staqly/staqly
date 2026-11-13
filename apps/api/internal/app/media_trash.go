package app

import (
	"context"
	"encoding/json"
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
		if _, err := q.TrashMedia(ctx, db.TrashMediaParams{DeletedAt: nullStr(s.now()), WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		return s.emitMedia(ctx, q, p, "media.deleted", map[string]any{"media_ids": []string{id}})
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
	var m db.Medium
	err := s.tx(ctx, func(q *db.Queries) error {
		if _, err := q.RestoreMedia(ctx, db.RestoreMediaParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		var err error
		m, err = q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("media")
		}
		if err != nil {
			return err
		}
		if m.PurgeStartedAt.Valid {
			return conflict("this file is already being permanently deleted")
		}
		return s.emitMedia(ctx, q, p, "media.updated", map[string]any{"media_ids": []string{id}})
	})
	if err != nil {
		return Media{}, err
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
	var trashed []string
	err := s.tx(ctx, func(q *db.Queries) error {
		trashed = []string{} // fn reruns on a PostgreSQL serialization retry
		keep, err := q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) || err == nil && keep.DeletedAt.Valid {
			return notFound("media")
		}
		if err != nil {
			return err
		}
		now := s.now()
		for {
			twin, err := q.OldestTwin(ctx, db.OldestTwinParams{WorkspaceID: p.WorkspaceID, Sha256: keep.Sha256, ID: keep.ID})
			if isNoRows(err) {
				if err := s.dedupePostMedia(ctx, q, p.WorkspaceID, keep.ID, now); err != nil || len(trashed) == 0 {
					return err
				}
				return s.emitMedia(ctx, q, p, "media.deleted", map[string]any{"media_ids": trashed})
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

// dedupePostMedia: a post that carried two copies now names the kept file twice; it keeps it once (in its
// files and in each comment), or it would publish the picture twice and no edit of it would save.
func (s *Service) dedupePostMedia(ctx context.Context, q *db.Queries, ws, id, now string) error {
	rows, err := q.PostsCarryingMedia(ctx, db.PostsCarryingMediaParams{WorkspaceID: ws, Quoted: `"` + id + `"`})
	if err != nil {
		return err
	}
	for _, r := range rows {
		var media []string
		_ = json.Unmarshal([]byte(r.Media), &media)
		parts := parseThread(r.Thread)
		for i := range parts {
			parts[i].Media = uniqueIDs(parts[i].Media)
		}
		if err := q.SetPostMediaLists(ctx, db.SetPostMediaListsParams{Media: mustJSON(uniqueIDs(media)), Thread: mustJSON(parts),
			UpdatedAt: now, WorkspaceID: ws, ID: r.ID}); err != nil {
			return err
		}
	}
	return nil
}

// purgeTrashedMedia claims expired trash before touching storage, so a concurrent restore either wins
// first or is refused. Claims survive storage failures and process crashes; the next sweep retries them.
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
			var claimed int64
			err := s.tx(ctx, func(q *db.Queries) error {
				var err error
				claimed, err = q.ClaimMediaPurge(ctx, db.ClaimMediaPurgeParams{WorkspaceID: r.WorkspaceID, ID: r.ID, Cutoff: cutoff, StartedAt: nullStr(formatTime(now))})
				return err
			})
			if err != nil {
				return purged, err
			}
			if claimed == 0 {
				continue // restored or removed after the batch was read
			}
			if err := s.removeMediaFiles(ctx, r.WorkspaceID, r.StorageName); err != nil && !errors.Is(err, blob.ErrNotExist) {
				s.Log.Warn("deleted media file not removed, retrying next run", "media_id", r.ID, "err", err)
				continue
			}
			var removed int64
			if err := s.tx(ctx, func(q *db.Queries) error {
				var err error
				if removed, err = q.DeleteMedia(ctx, db.DeleteMediaParams{WorkspaceID: r.WorkspaceID, ID: r.ID}); err != nil {
					return err
				}
				return s.stripMediaFromPosts(ctx, q, r.WorkspaceID, r.ID)
			}); err != nil {
				return purged, err
			}
			purged += removed
			done++
		}
		// A batch where nothing could go (the store is down) stops the run instead of looping on the same rows.
		if len(rows) < retentionBatch || done == 0 {
			return purged, nil
		}
	}
}
