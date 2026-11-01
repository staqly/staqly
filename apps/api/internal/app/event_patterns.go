package app

import (
	"context"
	"path"
	"slices"
	"strings"
)

// knownEventTypes are the event types staqly emits itself (docs: api/events).
var knownEventTypes = map[string]bool{
	"post.created": true, "post.updated": true, "post.scheduled": true, "post.unscheduled": true, "post.deleted": true,
	"post.publish_requested": true, "post.target_published": true, "post.target_failed": true,
	"post.reply_published": true, "post.reply_failed": true,
	"post.published": true, "post.failed": true, "post.purged": true,
	"post.unpublished": true, "post.target_removed": true, "post.target_remove_failed": true,
	"post.preview_shared": true, "post.responses_analyzed": true, "post.feedback_added": true,
	"channel.connected": true, "channel.updated": true, "channel.needs_reconnect": true, "channel.disconnected": true,
	"channel_link.created": true, "channel_link.revoked": true,
	"schedule.created": true, "schedule.updated": true, "schedule.deleted": true,
	"schedule.ran": true, "schedule.posts_requested": true, "schedule.failed": true,
	"collection.created": true, "collection.updated": true, "collection.deleted": true, "collection.archived": true,
	"webhook.created": true, "webhook.updated": true, "webhook.deleted": true, "webhook.test": true,
	"alert.created": true, "alert.updated": true, "alert.deleted": true,
	"workspace.created": true, "workspace.updated": true, "workspace.plan_changed": true, "workspace.credits_added": true, "workspace.account_attached": true, "workspace.trial_review": true,
	"workspace.export_requested": true, "workspace.exported": true,
	"member.updated": true, "member.removed": true,
	"invitation.created": true, "invitation.resent": true, "invitation.revoked": true, "invitation.accepted": true,
	"retention.events_pruned": true,
	"api_key.created":         true, "api_key.rotated": true, "api_key.revoked": true,
	"session.signed_in": true, "session.recovered": true, "session.revoked": true,
	"recovery_key.rotated": true, "access_token.rotated": true, "device_link.created": true, "device.connected": true,
	"sign_in_method.added": true, "sign_in_method.changed": true, "sign_in_method.removed": true,
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
var auditFamilies = map[string]bool{"api_key": true, "session": true, "recovery_key": true, "access_token": true, "device": true, "device_link": true,
	"sign_in_method": true}

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
// subscriptions: "*", globs inside a known family ("post.*",
// "post.*_requested") and known event types. A typo like "post.publised" or
// "posts.*" would otherwise never match and fail silently.
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
		fam, rest, ok := strings.Cut(e, ".")
		if !ok || rest == "" || !eventFamilies[fam] {
			return invalid("events", "unknown event pattern "+e+`; use "*", a family glob such as "post.*", or an event type; families: `+strings.Join(sortedKeys(eventFamilies), ", "))
		}
		if strings.ContainsAny(e, "*?[") {
			if _, err := path.Match(e, ""); err != nil {
				return invalid("events", "invalid event pattern "+e)
			}

			continue
		}
		// Agents can't open the docs: name the family's types so a typo is fixed in one call.
		var known []string
		for t := range knownEventTypes {
			if strings.HasPrefix(t, fam+".") {
				known = append(known, t)
			}
		}
		slices.Sort(known)

		return invalid("events", "unknown event pattern "+e+`; use "*", "`+fam+`.*" or one of: `+strings.Join(known, ", "))
	}

	return nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)

	return out
}
