package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// ---- retention ----

// RetentionConfig is read from the environment on every run:
// RETENTION_EVENTS_DAYS (0 = keep forever, default), RETENTION_DELIVERIES_DAYS
// (default 30, 0 = keep forever), RETENTION_ARCHIVED_ITEMS_DAYS (0 = never,
// default) and RETENTION_NOTIFICATIONS_DAYS (finished notification outbox
// rows; default 30, 0 = keep forever) and RETENTION_AGENTS_DAYS (agents and API
// connections not seen for that long; default 30, 0 = keep forever).
type RetentionConfig struct {
	EventsDays, DeliveriesDays, ArchivedItemsDays, NotificationsDays, AgentsDays int
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
		ArchivedItemsDays: num("RETENTION_ARCHIVED_ITEMS_DAYS", 0),
		NotificationsDays: num("RETENTION_NOTIFICATIONS_DAYS", 30),
		AgentsDays:        num("RETENTION_AGENTS_DAYS", 30),
	}
}

type RetentionReport struct {
	Deliveries    int64 `json:"deliveries"`
	ArchivedItems int64 `json:"archived_items"`
	Events        int64 `json:"events"`
	Exports       int64 `json:"exports"`
	Notifications int64 `json:"notifications"`
	Agents        int64 `json:"agents"`
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

		// 2. Archived items: deleted like DeleteItem (versions cascade, events
		// stay as history), with one summary event per batch instead of one
		// item.deleted per item.
		if cfg.ArchivedItemsDays > 0 {
			for {
				var ids []string
				err := s.tx(ctx, func(q *db.Queries) error {
					var err error
					ids, err = q.PrunableArchivedItems(ctx, db.PrunableArchivedItemsParams{WorkspaceID: ws,
						UpdatedAt: cutoff(cfg.ArchivedItemsDays), Limit: retentionBatch})
					if err != nil || len(ids) == 0 {
						return err
					}
					for _, id := range ids {
						if _, err := q.DeleteItemByID(ctx, db.DeleteItemByIDParams{WorkspaceID: ws, ID: id}); err != nil {
							return err
						}
					}
					_, err = s.emit(ctx, q, sys, "retention.items_pruned", "", "", map[string]any{
						"count": len(ids), "older_than_days": cfg.ArchivedItemsDays, "item_ids": ids})
					return err
				})
				if err != nil {
					return rep, err
				}
				rep.ArchivedItems += int64(len(ids))
				if len(ids) < retentionBatch {
					break
				}
			}
		}

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
			s.logger(ctx).Info("retention run", "deliveries", rep.Deliveries, "archived_items", rep.ArchivedItems,
				"events", rep.Events, "exports", rep.Exports, "notifications", rep.Notifications, "agents", rep.Agents)
		}
	}})
}

// ---- GDPR: erase a workspace, purge an item ----

// DeleteWorkspace erases the caller's workspace and everything in it (cloud
// only, dashboard session only, confirmed by typing the workspace name). All
// rows cascade from workspaces; media files and export ZIPs are removed from
// disk after the commit. Sessions and keys go with the workspace.
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
	var files, exports []string
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
		if files, err = q.ListWorkspaceMediaFiles(ctx, p.WorkspaceID); err != nil {
			return err
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
		_, err = q.DeleteWorkspaceRow(ctx, p.WorkspaceID)
		return err
	})
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := s.removeMediaFiles(f); err != nil {
			s.logger(ctx).Warn("media file not removed after workspace deletion", "err", err)
		}
	}
	for _, id := range exports {
		s.removeExportFile(id)
	}
	s.logger(ctx).Info("workspace deleted", "workspace_id", p.WorkspaceID, "media_files", len(files))
	return nil
}

type PurgeResult struct {
	ItemID        string `json:"item_id"`
	EventsDeleted int64  `json:"events_deleted"`
	MediaDeleted  bool   `json:"media_deleted"`
}

// PurgeItem deletes an item together with its history: versions, every event
// about it (and their webhook deliveries, even pending ones), its media if no
// other item uses it, and cached idempotent responses that contain it. Events
// can't be edited (immutability trigger), so personal data in them is removed
// by deleting them; one content-free item.purged event records that it happened.
func (s *Service) PurgeItem(ctx context.Context, p Principal, id string) (PurgeResult, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return PurgeResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute) // see DeleteWorkspace
	defer cancel()
	res := PurgeResult{ItemID: id}
	var mediaFile string
	err := s.tx(ctx, func(q *db.Queries) error {
		it, err := q.GetItem(ctx, db.GetItemParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("item")
		}
		if err != nil {
			return err
		}
		ref := db.DeleteDeliveriesForItemEventsParams{WorkspaceID: p.WorkspaceID, ItemID: nullStr(id)}
		if _, err := q.DeleteDeliveriesForItemEvents(ctx, ref); err != nil {
			return err
		}
		if res.EventsDeleted, err = q.DeleteItemEvents(ctx, db.DeleteItemEventsParams(ref)); err != nil {
			return err
		}
		if _, err := q.DeleteItemByID(ctx, db.DeleteItemByIDParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		if it.MediaID.Valid {
			n, err := q.CountMediaReferences(ctx, db.CountMediaReferencesParams{WorkspaceID: p.WorkspaceID, MediaID: it.MediaID})
			if err != nil {
				return err
			}
			if n == 0 {
				m, err := q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: it.MediaID.String})
				if err == nil {
					if _, err := q.DeleteMedia(ctx, db.DeleteMediaParams{WorkspaceID: p.WorkspaceID, ID: m.ID}); err != nil {
						return err
					}
					mediaFile, res.MediaDeleted = m.StorageName, true
				}
			}
		}
		if err := q.DeleteIdempotencyResponsesMentioning(ctx, db.DeleteIdempotencyResponsesMentioningParams{
			WorkspaceID: p.WorkspaceID, Needle: id}); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "item.purged", it.CollectionID, id, map[string]any{
			"item_id": id, "collection_id": it.CollectionID, "events_deleted": res.EventsDeleted, "media_deleted": res.MediaDeleted})
		return err
	})
	if err != nil {
		return PurgeResult{}, err
	}
	if mediaFile != "" {
		if err := s.removeMediaFiles(mediaFile); err != nil {
			s.logger(ctx).Warn("media file not removed after purge", "item_id", id, "err", err)
		}
	}
	// Finished or in-progress exports may contain the item: expire them now
	// (a running build notices at the end and deletes its ZIP).
	if ids, err := s.Q.ReadyWorkspaceExports(ctx, p.WorkspaceID); err == nil {
		for _, x := range ids {
			s.removeExportFile(x)
			_ = s.W.MarkWorkspaceExportExpired(ctx, x)
		}
	}
	return res, nil
}
