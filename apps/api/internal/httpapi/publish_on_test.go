package httpapi_test

import (
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// POST /v1/posts/{id}/schedule takes publish_on instead of publish_at: a channel whose platform says when it goes
// live (Kick) is accepted, one that can't is a 422, and both fields at once too. The catalog says which can.
func TestSchedulePublishOnRoute(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Streams")
	ch := e.customChannel("Worker")
	kick := e.customChannel("Kick")
	if _, err := e.svc.DB.Exec(`UPDATE channels SET provider = 'kick' WHERE id = ?`, kick.ID); err != nil {
		t.Fatal(err)
	}
	post := e.post(c.ID, map[string]any{"targets": []map[string]any{{"channel_id": ch.ID}}})
	path := "/v1/posts/" + post.ID + "/schedule"
	e.do(nil, "POST", path, e.key, map[string]any{"publish_on": map[string]any{"event": "live", "channel_id": ch.ID}}, 422, nil)
	e.do(nil, "POST", path, e.key, map[string]any{"publish_at": future(time.Hour), "publish_on": map[string]any{"event": "live", "channel_id": kick.ID}}, 422, nil)
	var got app.Post
	e.do(nil, "POST", path, e.key, map[string]any{"publish_on": map[string]any{"event": "live", "channel_id": kick.ID}}, 200, &got)
	if got.Status != "scheduled" || got.PublishAt != nil || got.PublishOn == nil || got.PublishOn.ChannelID != kick.ID {
		t.Fatalf("scheduled for the stream: %+v", got)
	}
	var providers struct{ Data []app.ProviderInfo }
	e.do(nil, "GET", "/v1/providers", e.key, nil, 200, &providers)
	for _, p := range providers.Data {
		if p.LiveTrigger != (p.ID == "kick") {
			t.Fatalf("%s live_trigger %v", p.ID, p.LiveTrigger)
		}
	}
}
