package app

import (
	"context"
	"path"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// knownEventTypes are the event types staqly emits itself (docs: api/events).
// Custom actions add item.<name>_requested or the event configured on a
// collection action.
var knownEventTypes = map[string]bool{
	"item.created": true, "item.updated": true, "item.deleted": true, "item.approved": true, "item.rejected": true,
	"item.expired": true, "item.status_changed": true,
	"item.completed": true, "item.failed": true, "item.purged": true, "item.tags_changed": true, "item.summarized": true,
	"item.unarchived": true, "item.archived": true,
	"collection.created": true, "collection.updated": true, "collection.deleted": true, "collection.archived": true,
	"watcher.created": true, "watcher.updated": true, "watcher.deleted": true, "watcher.degraded": true,
	"watcher.paused": true, "watcher.recovered": true,
	"webhook.created": true, "webhook.updated": true, "webhook.deleted": true, "webhook.test": true,
	"notification_channel.created": true, "notification_channel.updated": true, "notification_channel.deleted": true,
	"workspace.created": true, "workspace.updated": true, "workspace.plan_changed": true,
	"workspace.export_requested": true, "workspace.exported": true,
	"retention.items_pruned": true, "retention.events_pruned": true,
	"api_key.created": true, "api_key.rotated": true, "api_key.revoked": true,
	"session.signed_in": true, "session.recovered": true, "session.revoked": true,
	"recovery_key.rotated": true, "device_link.created": true, "device.connected": true,
}

// eventFamilies are the prefixes before the first dot of every known type.
var eventFamilies = func() map[string]bool {
	out := map[string]bool{}
	for t := range knownEventTypes {
		out[t[:strings.IndexByte(t, '.')]] = true
	}

	return out
}()

// validateEventPatterns checks webhook and notification channel
// subscriptions: "*", globs inside a known family ("item.*",
// "item.*_requested"), known event types, and custom action events
// (item.<x>_requested or an event configured on one of the workspace's
// collections). A typo like "item.aproved" or "items.*" would otherwise never
// match and fail silently.
func (s *Service) validateEventPatterns(ctx context.Context, workspaceID string, pats []string) error {
	if len(pats) == 0 || len(pats) > 50 {
		return invalid("events", "subscribe to 1-50 event patterns (use \"*\" for all)")
	}
	var custom map[string]bool // loaded lazily, only for unknown exact item events
	for _, e := range pats {
		if e == "*" || knownEventTypes[e] {
			continue
		}
		if e == "" || len(e) > 100 || strings.ContainsAny(e, " /") {
			return invalid("events", "invalid event pattern "+e)
		}
		bad := invalid("events", "unknown event pattern "+e+`; use "*", a family glob such as "item.*", or an event type from the docs`)
		fam, rest, ok := strings.Cut(e, ".")
		if !ok || rest == "" || !eventFamilies[fam] {
			return bad
		}
		if strings.ContainsAny(e, "*?[") {
			if _, err := path.Match(e, ""); err != nil {
				return invalid("events", "invalid event pattern "+e)
			}

			continue
		}
		if fam != "item" || !actionEventRe.MatchString(e) {
			return bad
		}
		if strings.HasSuffix(e, "_requested") {
			continue
		}
		if custom == nil {
			custom = map[string]bool{}
			rows, err := s.Q.ListCollections(ctx, db.ListCollectionsParams{WorkspaceID: workspaceID, IncludeArchived: 1})
			if err != nil {
				return err
			}
			for _, r := range rows {
				for _, a := range toCollection(r).Actions {
					custom[a.Event] = true
				}
			}
		}
		if !custom[e] {
			return bad
		}
	}

	return nil
}
