package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// One post goes out on channel A and is refused on channel B. The alert's digest holds the three events in
// one message: the failure must be in it, not hidden behind the earlier success of the same post.
func TestAlertDigestKeepsTheFailure(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	b, err := s.ConnectChannel(ctx, p, ChannelInput{Provider: "custom", Name: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	a := post.Targets[0].ChannelID
	targets := []PostTargetInput{{ChannelID: a}, {ChannelID: b.ID}}
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &targets}); err != nil {
		t.Fatal(err)
	}
	events := []string{"post.target_published", "post.target_failed", "post.failed"}
	alert, err := s.CreateAlert(ctx, p, AlertInput{Name: ptrTo("Outcomes"), Type: ChannelSlack,
		Config: map[string]json.RawMessage{"webhook_url": json.RawMessage(`"https://hooks.slack.com/services/T/B/x"`)},
		Events: &events, DigestWindowSeconds: ptrTo(int64(300))})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReportTargetResult(ctx, p, post.ID, a, ResultInput{Status: "published", URL: "https://example.com/1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReportTargetResult(ctx, p, post.ID, b.ID, ResultInput{Status: "failed", Error: "refused by the platform"}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Q.PendingNotifyOutbox(ctx, db.PendingNotifyOutboxParams{ChannelID: alert.ID, NextAttemptAt: "9999", Limit: 50})
	if err != nil || len(rows) != 3 {
		t.Fatalf("setup: pending alert rows %d %v", len(rows), err)
	}
	r, err := s.Q.GetNotifyChannelByID(ctx, alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.loadChannel(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range rows { // what tells two targets' failures of one post apart
		if ev, err := s.Q.GetEvent(ctx, o.EventID); err != nil || o.EventType != "post.failed" && !ev.ChannelID.Valid {
			t.Fatalf("%s has no channel: %v", o.EventType, err)
		}
	}
	msg, skipped := s.buildMessage(ctx, ch, rows)
	said := map[string]string{}
	for _, e := range msg.Entries {
		said[e.EventType] = e.Note
	}
	if len(msg.Entries) != 3 || len(msg.rows) != 3 || len(skipped) != 0 || !strings.Contains(said["post.target_failed"], "refused") ||
		!strings.Contains(said["post.target_published"], "example.com/1") {
		t.Fatalf("3 events became %d entries (%d rows marked sent): %q", len(msg.Entries), len(msg.rows), said)
	}
	// The same event again (a retry that fails the same way) still collapses into its entry.
	msg, _ = s.buildMessage(ctx, ch, append(rows, rows...))
	if len(msg.Entries) != 3 || len(msg.rows) != 6 {
		t.Fatalf("repeated events: %d entries, %d rows", len(msg.Entries), len(msg.rows))
	}
}
