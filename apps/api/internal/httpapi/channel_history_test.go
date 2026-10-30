package httpapi_test

import (
	"context"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// A removed channel leaves the channel lists, but the posts it published still show where they went; what it
// never published goes with it.
func TestRemovedChannelKeepsWhatItPublished(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.collection("History")
	ch := e.customChannel("Gone")
	targets := []map[string]any{{"channel_id": ch.ID}}

	published := e.post(c.ID, map[string]any{"targets": targets})
	e.do(nil, "POST", "/v1/posts/"+published.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	e.do(nil, "POST", "/v1/posts/"+published.ID+"/targets/"+ch.ID+"/result", e.key, map[string]any{"status": "published"}, 200, nil)
	draft := e.post(c.ID, map[string]any{"targets": targets})

	e.do(nil, "DELETE", "/v1/channels/"+ch.ID, e.admin, nil, 204, nil)
	e.do(nil, "GET", "/v1/channels/"+ch.ID, e.key, nil, 404, nil)
	var list struct{ Data []app.Channel }
	e.do(nil, "GET", "/v1/channels", e.key, nil, 200, &list)
	for _, x := range list.Data {
		if x.ID == ch.ID {
			t.Fatalf("removed channel still listed")
		}
	}

	var got app.Post
	e.do(nil, "GET", "/v1/posts/"+published.ID, e.key, nil, 200, &got)
	if len(got.Targets) != 1 || got.Targets[0].ChannelName != "Gone" || got.Targets[0].Provider != "custom" || got.Targets[0].Status != "published" {
		t.Fatalf("published post's target: %+v", got.Targets)
	}
	e.do(nil, "GET", "/v1/posts/"+draft.ID, e.key, nil, 200, &got)
	if len(got.Targets) != 0 {
		t.Fatalf("draft kept the removed channel: %+v", got.Targets)
	}
	// Its credentials are gone with it.
	var creds string
	if err := e.svc.DB.QueryRow(`SELECT credentials FROM channels WHERE id = ?`, ch.ID).Scan(&creds); err != nil || creds != "" {
		t.Fatalf("credentials: %q %v", creds, err)
	}
}
