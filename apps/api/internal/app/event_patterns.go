package app

import (
	"context"
	"path"
	"strings"
)

// knownEventTypes are the event types staqly emits itself (docs: api/events),
// including the catalog actions' events (regenerate, publish, retry; archive
// emits item.archived).
var knownEventTypes = map[string]bool{
	"item.regenerate_requested": true, "item.publish_requested": true, "item.retry_requested": true,
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

// auditFamilies hold the credential and session audit events (sign-ins with
// IP and browser, key changes). Like GET /v1/events, which shows them to
// admins only, subscriptions get them only when they name the family itself
// ("session.*", "api_key.revoked"), never through "*" or another glob, and
// only an admin may subscribe that way.
var auditFamilies = map[string]bool{"api_key": true, "session": true, "recovery_key": true, "device": true, "device_link": true}

func isAuditEvent(typ string) bool {
	fam, _, _ := strings.Cut(typ, ".")
	return auditFamilies[fam]
}

// eventMatches tells whether a webhook or channel pattern covers an event type.
func eventMatches(pat, typ string) bool {
	if isAuditEvent(typ) {
		fam, _, _ := strings.Cut(pat, ".")
		if !auditFamilies[fam] {
			return false
		}
	}
	if pat == "*" {
		return true
	}
	ok, _ := path.Match(pat, typ)
	return ok
}

// validateEventPatterns checks webhook and notification channel
// subscriptions: "*", globs inside a known family ("item.*",
// "item.*_requested") and known event types, the catalog action events among
// them. A typo like "item.aproved" or "items.*" would otherwise never match and
// fail silently.
func (s *Service) validateEventPatterns(_ context.Context, p Principal, pats []string) error {
	if len(pats) == 0 || len(pats) > 50 {
		return invalid("events", "subscribe to 1-50 event patterns (use \"*\" for all)")
	}
	for _, e := range pats {
		if fam, _, _ := strings.Cut(e, "."); auditFamilies[fam] && !p.Has(ScopeAdmin) {
			return &Error{Code: "forbidden", Status: 403, Message: "subscribing to " + e + " (credential and session audit events) needs the admin scope"}
		}
	}
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

		return bad
	}

	return nil
}
