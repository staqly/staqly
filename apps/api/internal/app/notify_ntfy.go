package app

import (
	"context"
	"fmt"
	"strings"
)

// ntfy: JSON publish to the server root. Approve / Reject are ntfy "http"
// actions that POST to the one-tap decision link (no confirmation page), and
// tapping the notification opens the item in the dashboard.

func (s *Service) sendNtfy(ctx context.Context, ch *notifyChannel, msg *notifyMessage) error {
	body := map[string]any{"topic": ch.cfg.Topic, "title": truncateRunes(msg.title(), 200), "tags": []string{"inbox_tray"}}
	if ch.cfg.Priority > 0 {
		body["priority"] = ch.cfg.Priority
	}
	var b strings.Builder
	if msg.Headline != "" {
		b.WriteString(msg.Headline)
	}
	for i, e := range msg.Entries {
		if i == 10 {
			fmt.Fprintf(&b, "\n...and %d more", len(msg.Entries)-10)
			break
		}
		if len(msg.Entries) > 1 {
			fmt.Fprintf(&b, "#%d %s — %s\n", i+1, truncateRunes(e.Title, 100), eventLabel(e))
			continue
		}
		b.WriteString(entryMeta(e))
		if p := truncateRunes(e.Preview, 600); p != "" {
			b.WriteString("\n" + p)
		}
		if e.Note != "" {
			b.WriteString("\n" + truncateRunes(e.Note, 200))
		}
	}
	body["message"] = strings.TrimSpace(b.String())
	if len(msg.Entries) == 1 {
		e := msg.Entries[0]
		if e.WebURL != "" {
			body["click"] = e.WebURL
		}
		if e.ApproveURL != "" {
			body["actions"] = []map[string]any{
				{"action": "http", "label": "Approve", "url": e.ApproveURL, "method": "POST", "clear": true},
				{"action": "http", "label": "Reject", "url": e.RejectURL, "method": "POST", "clear": true},
				{"action": "view", "label": "Reject with reason", "url": e.RejectURL},
			}
		}
	} else if len(msg.Entries) > 1 {
		body["click"] = s.webURL() + "/inbox"
	}
	hdr := map[string]string{}
	if ch.cfg.AccessToken != "" {
		hdr["Authorization"] = "Bearer " + ch.cfg.AccessToken
	}
	return s.postJSON(ctx, ch.cfg.ServerURL, hdr, body, nil)
}
