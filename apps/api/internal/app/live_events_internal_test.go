package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/providers"
)

// dataChannelID is the channel_id an event's data names.
func dataChannelID(t *testing.T, e eventRow) string {
	t.Helper()
	var d struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal([]byte(e.data), &d); err != nil {
		t.Fatal(err)
	}
	return d.ChannelID
}

// Comments read in the background tell open pages once per read that changed something, never for a read that didn't.
func TestReadResponsesEmitsUpdated(t *testing.T) {
	s, p, post, ch, r := readerOn(t, "fakerdlive")
	ctx := context.Background()
	at := time.Now().UTC()
	read := func(want int) {
		t.Helper()
		if err := s.readResponses(ctx, post.ID, ch); err != nil {
			t.Fatal(err)
		}
		got := eventsOfType(t, s, p, "post.responses_updated")
		if len(got) != want {
			t.Fatalf("want %d post.responses_updated, got %d", want, len(got))
		}
		if want > 0 && (got[want-1].item != post.ID || got[want-1].collection != post.CollectionID || dataChannelID(t, got[want-1]) != ch) {
			t.Fatalf("event %+v", got[want-1])
		}
	}
	read(0) // nothing on the platform
	likes := int64(1)
	r.out = []providers.Response{{ID: "9", Text: "a", Likes: &likes, CreatedAt: at}, {ID: "10", Text: "b", CreatedAt: at}}
	read(1) // two new comments: one event
	read(1) // the same again
	more := int64(2)
	r.out[0].Likes = &more
	read(2) // a like more
	r.out[1].Text = "b, edited"
	read(3) // an edit
	r.out = []providers.Response{{ID: "own", Text: "mine", Own: true, CreatedAt: at}}
	read(3) // only the channel's own reply
}

// fakeNumbers is a fakeRemovable that reports the numbers it holds.
type fakeNumbers struct {
	*fakeRemovable
	views int64
}

func (f *fakeNumbers) Metrics(context.Context, providers.Credentials, string) (providers.Metrics, error) {
	v := f.views
	return providers.Metrics{Impressions: &v}, nil
}

// A target's numbers checked in the background tell open pages when they moved, not on every check.
func TestCheckTargetEmitsMetricsUpdated(t *testing.T) {
	s, p, post, fakes, chans := publishedOn(t, "fakenumlive")
	ctx := context.Background()
	f := &fakeNumbers{fakeRemovable: fakes["fakenumlive"], views: 5}
	providers.Unregister("fakenumlive")
	providers.Register(f, providers.StatusAvailable)
	check := func(want int) {
		t.Helper()
		if err := s.checkTarget(ctx, post.ID, chans[0]); err != nil {
			t.Fatal(err)
		}
		got := eventsOfType(t, s, p, "post.metrics_updated")
		if len(got) != want {
			t.Fatalf("want %d post.metrics_updated, got %d", want, len(got))
		}
		if want > 0 && (got[want-1].item != post.ID || dataChannelID(t, got[want-1]) != chans[0]) {
			t.Fatalf("event %+v", got[want-1])
		}
	}
	check(1)
	check(1) // same numbers
	f.views = 6
	check(2)
}

// Numbers a custom target's automation reports are news to open pages too, once.
func TestReportTargetMetricsEmitsUpdated(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	e, _ := providers.Get("custom")
	ch, err := s.saveChannel(ctx, p, e, "mine", providers.Credentials{}, providers.Profile{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	targets := []PostTargetInput{{ChannelID: ch.ID}}
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &targets}); err != nil {
		t.Fatal(err)
	}
	views := int64(3)
	for range 2 {
		if _, err := s.ReportTargetMetrics(ctx, p, post.ID, ch.ID, providers.Metrics{Impressions: &views}); err != nil {
			t.Fatal(err)
		}
	}
	if got := eventsOfType(t, s, p, "post.metrics_updated"); len(got) != 1 || got[0].item != post.ID {
		t.Fatalf("events %+v", got)
	}
}

// Media library changes made anywhere (an agent over MCP, another tab) are events; reusing a twin changes nothing.
func TestMediaChangesEmitEvents(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := t.Context()
	count := func(typ string, want int) {
		t.Helper()
		if got := eventsOfType(t, s, p, typ); len(got) != want {
			t.Fatalf("want %d %s, got %d", want, typ, len(got))
		}
	}
	m, err := s.UploadMedia(ctx, p, "photo.png", feedbackPNG(t), false)
	if err != nil {
		t.Fatal(err)
	}
	count("media.created", 1)
	if got := eventsOfType(t, s, p, "media.created"); got[0].data != `{"media_ids":["`+m.ID+`"]}` {
		t.Fatalf("data %s", got[0].data)
	}
	if _, err := s.UploadMedia(ctx, p, "again.png", feedbackPNG(t), true); err != nil {
		t.Fatal(err)
	}
	count("media.created", 1) // the twin was reused

	name := "renamed.png"
	if _, err := s.UpdateMedia(ctx, p, m.ID, MediaPatch{Name: &name}); err != nil {
		t.Fatal(err)
	}
	count("media.updated", 1)
	folder, err := s.CreateMediaFolder(ctx, p, MediaFolderInput{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	count("media.updated", 2)
	if _, err := s.BulkUpdateMedia(ctx, p, MediaBulk{IDs: []string{m.ID}, FolderID: &folder.ID}); err != nil {
		t.Fatal(err)
	}
	count("media.updated", 3)
	if err := s.DeleteMediaFolder(ctx, p, folder.ID); err != nil {
		t.Fatal(err)
	}
	count("media.updated", 4)

	if err := s.TrashMedia(ctx, p, m.ID); err != nil {
		t.Fatal(err)
	}
	count("media.deleted", 1)
	if _, err := s.RestoreMedia(ctx, p, m.ID); err != nil {
		t.Fatal(err)
	}
	count("media.updated", 5)
	if err := s.DeleteMedia(ctx, p, m.ID); err != nil {
		t.Fatal(err)
	}
	count("media.deleted", 2)
}

// An entitlement sync that changes what the workspace has is an event; one that finds the same set is not.
func TestSyncEntitlementsEmitsWhenChanged(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := t.Context()
	if _, err := s.DB.Exec(`UPDATE workspaces SET entitlements = '[{"lookup_key":"staqly_addon_ai_pack","name":"AI pack"}]' WHERE id = ?`, p.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.runSyncEntitlements(ctx, p.WorkspaceID); err != nil {
			t.Fatal(err)
		}
	}
	if got := eventsOfType(t, s, p, "workspace.entitlements_changed"); len(got) != 1 || got[0].data != `{"features":[]}` {
		t.Fatalf("events %+v", got)
	}
}

// Background refreshes reach an alert channel only when it names the type itself.
func TestQuietEventsNeedTheirOwnSubscription(t *testing.T) {
	for typ := range quietEvents {
		if !knownEventTypes[typ] {
			t.Errorf("%s is quiet but not a known event type", typ)
		}
		for _, pat := range []string{`["*"]`, `["post.*"]`, `["media.*"]`, `["workspace.*"]`} {
			if channelMatches(pat, "", typ, "") {
				t.Errorf("%s matched %s", pat, typ)
			}
		}
		if !channelMatches(`["`+typ+`"]`, "", typ, "") {
			t.Errorf("an alert on %s itself does not get it", typ)
		}
	}
	if !channelMatches(`["*"]`, "", "post.published", "") {
		t.Error("* no longer matches other events")
	}
}
