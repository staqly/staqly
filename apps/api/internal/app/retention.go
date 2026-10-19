package app

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// ---- retention ----

// RetentionConfig is read from the environment on every run:
// RETENTION_EVENTS_DAYS (0 = keep forever, default), RETENTION_DELIVERIES_DAYS
// (default 30, 0 = keep forever), RETENTION_NOTIFICATIONS_DAYS (finished notification outbox
// rows; default 30, 0 = keep forever) and RETENTION_AGENTS_DAYS (agents and API
// connections not seen for that long; default 30, 0 = keep forever).
type RetentionConfig struct {
	EventsDays, DeliveriesDays, NotificationsDays, AgentsDays int
}

func (s *Service) RetentionConfig() RetentionConfig {
	num := func(key string, def int) int {
		v := s.Env(key, "")
		if v == "" {
			return def
		}
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < 0 {
			s.Log.Warn("invalid retention setting, using default", "key", key, "default", def)
			return def
		}
		return n
	}
	return RetentionConfig{
		EventsDays:        num("RETENTION_EVENTS_DAYS", 0),
		DeliveriesDays:    num("RETENTION_DELIVERIES_DAYS", 30),
		NotificationsDays: num("RETENTION_NOTIFICATIONS_DAYS", 30),
		AgentsDays:        num("RETENTION_AGENTS_DAYS", 30),
	}
}

type RetentionReport struct {
	Deliveries    int64 `json:"deliveries"`
	Events        int64 `json:"events"`
	Exports       int64 `json:"exports"`
	Notifications int64 `json:"notifications"`
	Agents        int64 `json:"agents"`
	Media         int64 `json:"media"`
}

const retentionBatch = 1000

// RunRetention prunes old data once (the daily worker calls it; tests call it
// directly with an overridden Now). Each batch is its own short transaction so
// the single SQLite writer is never held for long.
func (s *Service) RunRetention(ctx context.Context) (RetentionReport, error) {
	cfg := s.RetentionConfig()
	now := s.Now().UTC()
	cutoff := func(days int) string { return formatTime(now.Add(-time.Duration(days) * 24 * time.Hour)) }
	var rep RetentionReport

	// 1. Finished webhook deliveries (pending ones are never pruned).
	if cfg.DeliveriesDays > 0 {
		for {
			n, err := s.W.PruneDeliveries(ctx, db.PruneDeliveriesParams{Cutoff: cutoff(cfg.DeliveriesDays), Lim: retentionBatch})
			if err != nil {
				return rep, err
			}
			rep.Deliveries += n
			if n < retentionBatch {
				break
			}
		}
	}

	// 1b. Notification outbox: pending rows of channels disabled for longer
	// than NOTIFY_DISABLED_GRACE (default 24h) are skipped (re-enabling within
	// the grace period delivers them), then finished rows
	// (sent/skipped/failed) are pruned in batches.
	grace := s.envDuration("NOTIFY_DISABLED_GRACE", 24*time.Hour)
	if _, err := s.W.SkipDisabledChannelOutbox(ctx, nullStr(formatTime(now.Add(-grace)))); err != nil {
		return rep, err
	}
	if cfg.NotificationsDays > 0 {
		for {
			n, err := s.W.PruneNotifyOutbox(ctx, db.PruneNotifyOutboxParams{Cutoff: cutoff(cfg.NotificationsDays), Lim: retentionBatch})
			if err != nil {
				return rep, err
			}
			rep.Notifications += n
			if n < retentionBatch {
				break
			}
		}
	}

	wss, err := s.Q.RetentionWorkspaceIDs(ctx)
	if err != nil {
		return rep, err
	}
	for _, ws := range wss {
		sys := Principal{WorkspaceID: ws, Kind: PrincipalSystem, ID: "retention", Name: "Retention"}

		// 3. Events (opt-in). The table is append-only (UPDATE is blocked by a
		// trigger); DELETE is allowed for exactly this purpose. Finished
		// deliveries go first (FK); events with a pending delivery are kept
		// until it completes.
		if cfg.EventsDays > 0 {
			c := cutoff(cfg.EventsDays)
			var total int64
			for {
				var n int64
				err := s.tx(ctx, func(q *db.Queries) error {
					if _, err := q.DeleteDeliveriesForOldEvents(ctx, db.DeleteDeliveriesForOldEventsParams{WorkspaceID: ws, Cutoff: c}); err != nil {
						return err
					}
					var err error
					n, err = q.PruneEvents(ctx, db.PruneEventsParams{WorkspaceID: ws, Cutoff: c, Lim: retentionBatch})
					return err
				})
				if err != nil {
					return rep, err
				}
				total += n
				if n < retentionBatch {
					break
				}
			}
			if total > 0 {
				if err := s.tx(ctx, func(q *db.Queries) error {
					_, err := s.emit(ctx, q, sys, "retention.events_pruned", "", "", map[string]any{
						"count": total, "older_than_days": cfg.EventsDays, "before": c})
					return err
				}); err != nil {
					return rep, err
				}
			}
			rep.Events += total
		}
	}

	// 4. Bookkeeping rows of old exports (their files are removed at expiry).
	n, err := s.W.DeleteOldWorkspaceExports(ctx, formatTime(now.Add(-7*24*time.Hour)))
	if err != nil {
		return rep, err
	}
	rep.Exports = n

	// 4b. Files in Deleted for longer than MediaTrashDays.
	if rep.Media, err = s.purgeTrashedMedia(ctx, now); err != nil {
		return rep, err
	}

	// 5. Agents and API connections that went quiet (their events keep the history).
	if cfg.AgentsDays > 0 {
		if rep.Agents, err = s.W.PruneAgents(ctx, cutoff(cfg.AgentsDays)); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

func init() {
	RegisterWorker(BackgroundWorker{Name: "retention", Run: func(ctx context.Context, s *Service) {
		// First run shortly after start, then daily.
		wait := time.Minute
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			wait = 24 * time.Hour
			// Refresh planner statistics (sqlite_stat1) so filtered lists pick the selective index.
			if _, err := s.DB.ExecContext(ctx, "PRAGMA optimize"); err != nil && ctx.Err() == nil {
				s.logger(ctx).Warn("pragma optimize failed", "err", err)
			}
			rep, err := s.RunRetention(ctx)
			if err != nil {
				if ctx.Err() == nil {
					s.logger(ctx).Error("retention run failed", "err", err)
				}
				continue
			}
			s.logger(ctx).Info("retention run", "deliveries", rep.Deliveries,
				"events", rep.Events, "exports", rep.Exports, "notifications", rep.Notifications, "agents", rep.Agents)
		}
	}})
}

// ---- GDPR: erase a workspace, purge an item ----

// DeleteWorkspace erases the caller's workspace and everything in it (cloud
// only, dashboard session only, confirmed by typing the workspace name). All
// rows cascade from workspaces; its folder in the store (media, thumbnails,
// projects) and export ZIPs are removed after the commit. Sessions and keys go with the workspace.
func (s *Service) DeleteWorkspace(ctx context.Context, p Principal, confirm string) error {
	if p.WorkspaceID == "" {
		return ErrUnauthorized
	}
	if s.Cfg.Mode != ModeCloud {
		return conflict("local mode has a single workspace and cannot delete it; to remove all data, stop staqly and delete its data: `docker rm -f staqly && docker volume rm staqly-data` for the image, the data directory (default ~/.local/share/staqly) for staqly serve; Settings -> Data & privacy shows the steps")
	}
	if p.Kind != PrincipalSession {
		return &Error{Code: "forbidden", Status: 403, Message: "deleting a workspace requires a signed-in dashboard session (API keys cannot do this)"}
	}
	if err := s.requireAccountOwner(ctx, p); err != nil {
		return err
	}
	// A large workspace takes a while to cascade-delete; a client that gives
	// up must not roll it back half way (or leave files behind).
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
	defer cancel()
	// Stop billing first: a deleted workspace must not be charged again. If
	// Stripe can't be reached, the deletion fails and can be retried.
	if ws, err := s.Q.GetWorkspace(ctx, p.WorkspaceID); err == nil && confirm == ws.Name {
		if err := s.cancelSubscription(ctx, p.WorkspaceID); err != nil {
			return err
		}
	}
	var exports []string
	err := s.tx(ctx, func(q *db.Queries) error {
		ws, err := q.GetWorkspace(ctx, p.WorkspaceID)
		if isNoRows(err) {
			return notFound("workspace")
		}
		if err != nil {
			return err
		}
		if confirm != ws.Name {
			return invalid("confirm", "must be exactly the workspace name")
		}
		if exports, err = q.ListWorkspaceExportIDs(ctx, p.WorkspaceID); err != nil {
			return err
		}
		if err := q.DeleteWorkspaceDeliveries(ctx, p.WorkspaceID); err != nil {
			return err
		}
		if err := q.DeleteInstallationFor(ctx, p.WorkspaceID); err != nil {
			return err
		}
		if err := s.deleteWorkspaceJobs(ctx, q, p.WorkspaceID); err != nil {
			return err
		}
		_, err = q.DeleteWorkspaceRow(ctx, p.WorkspaceID)
		return err
	})
	if err != nil {
		return err
	}
	files, err := s.removeWorkspaceFiles(ctx, p.WorkspaceID)
	if err != nil {
		s.logger(ctx).Warn("files not removed after workspace deletion", "err", err)
	}
	for _, id := range exports {
		s.removeExportFile(id)
	}
	s.logger(ctx).Info("workspace deleted", "workspace_id", p.WorkspaceID, "files", files)
	return nil
}

type PurgeResult struct {
	PostID        string `json:"post_id"`
	EventsDeleted int64  `json:"events_deleted"`
	MediaDeleted  int    `json:"media_deleted"`
}

// PurgePost deletes a post together with its history: every event about it
// (and their webhook deliveries, even pending ones), its media when no other
// post uses them, and cached idempotent responses that contain it. Events
// can't be edited (immutability trigger), so personal data in them is removed
// by deleting them; one content-free post.purged event records that it happened.
func (s *Service) PurgePost(ctx context.Context, p Principal, id string) (PurgeResult, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return PurgeResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute) // see DeleteWorkspace
	defer cancel()
	res := PurgeResult{PostID: id}
	var mediaFiles []string
	err := s.tx(ctx, func(q *db.Queries) error {
		row, err := q.GetPost(ctx, db.GetPostParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("post")
		}
		if err != nil {
			return err
		}
		var media []string
		_ = json.Unmarshal([]byte(row.Media), &media)
		for _, part := range parseThread(row.Thread) { // a comment's files are the post's too
			for _, mid := range part.Media {
				if !slices.Contains(media, mid) {
					media = append(media, mid)
				}
			}
		}
		ref := db.DeleteDeliveriesForPostEventsParams{WorkspaceID: p.WorkspaceID, PostID: nullStr(id)}
		if _, err := q.DeleteDeliveriesForPostEvents(ctx, ref); err != nil {
			return err
		}
		if res.EventsDeleted, err = q.DeletePostEvents(ctx, db.DeletePostEventsParams(ref)); err != nil {
			return err
		}
		if _, err := q.DeletePost(ctx, db.DeletePostParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		for _, mid := range media {
			n, err := q.PostsReferencingMedia(ctx, db.PostsReferencingMediaParams{WorkspaceID: p.WorkspaceID, MediaID: mid})
			if err != nil {
				return err
			}
			if n == 0 {
				if m, err := q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: mid}); err == nil {
					if _, err := q.DeleteMedia(ctx, db.DeleteMediaParams{WorkspaceID: p.WorkspaceID, ID: m.ID}); err != nil {
						return err
					}
					mediaFiles = append(mediaFiles, m.StorageName)
					res.MediaDeleted++
				}
			}
		}
		if err := q.DeleteIdempotencyResponsesMentioning(ctx, db.DeleteIdempotencyResponsesMentioningParams{
			WorkspaceID: p.WorkspaceID, Needle: id}); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "post.purged", row.CollectionID, id, map[string]any{
			"post_id": id, "collection_id": row.CollectionID, "events_deleted": res.EventsDeleted, "media_deleted": res.MediaDeleted})
		return err
	})
	if err != nil {
		return PurgeResult{}, err
	}
	for _, f := range mediaFiles {
		if err := s.removeMediaFiles(ctx, p.WorkspaceID, f); err != nil {
			s.logger(ctx).Warn("media file not removed after purge", "post_id", id, "err", err)
		}
	}
	// Finished or in-progress exports may contain the post: expire them now
	// (a running build notices at the end and deletes its ZIP).
	if ids, err := s.Q.ReadyWorkspaceExports(ctx, p.WorkspaceID); err == nil {
		for _, x := range ids {
			s.removeExportFile(x)
			_ = s.W.MarkWorkspaceExportExpired(ctx, x)
		}
	}
	return res, nil
}
