package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// An alert is turned off for an hour (inside NOTIFY_DISABLED_GRACE, 24 h: "re-enabling within the grace
// period delivers them"). Event retention must not delete the alert's still pending delivery with the old event.
func TestEventRetentionKeepsPendingAlertDeliveries(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	s.Getenv = func(k string) string {
		if k == "RETENTION_EVENTS_DAYS" {
			return "1"
		}
		return ""
	}
	events := []string{"post.failed", "post.target_failed"}
	alert, err := s.CreateAlert(ctx, p, AlertInput{Name: ptrTo("Failures"), Type: ChannelSlack,
		Config: map[string]json.RawMessage{"webhook_url": json.RawMessage(`"https://hooks.slack.com/services/T/B/x"`)}, Events: &events})
	if err != nil {
		t.Fatal(err)
	}
	// A failure the alert has not sent yet.
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReportTargetResult(ctx, p, post.ID, post.Targets[0].ChannelID, ResultInput{Status: "failed", Error: "boom"}); err != nil {
		t.Fatal(err)
	}
	pending := func() (n int) {
		if err := s.DB.QueryRow(`SELECT COUNT(*) FROM notification_outbox WHERE channel_id = ? AND status = 'pending'`, alert.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if pending() != 2 {
		t.Fatalf("setup: pending alert rows %d", pending())
	}
	// 25 hours on the alert is turned off; an hour later retention runs.
	at := time.Now().Add(25 * time.Hour)
	s.Now = func() time.Time { return at }
	if _, err := s.UpdateAlert(ctx, p, alert.ID, AlertInput{Enabled: ptrTo(false)}); err != nil {
		t.Fatal(err)
	}
	at = at.Add(time.Hour)
	rep, err := s.RunRetention(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pending() != 2 {
		var all int
		_ = s.DB.QueryRow(`SELECT COUNT(*) FROM notification_outbox WHERE channel_id = ?`, alert.ID).Scan(&all)
		t.Fatalf("retention pruned %d events and with them the alert's pending deliveries: %d pending, %d rows of any status left (want 2 pending)",
			rep.Events, pending(), all)
	}
}
