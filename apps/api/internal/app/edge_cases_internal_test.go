package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/providers"
)

type fakeReplier struct {
	providers.Base
	replies *[]string
}

func (f *fakeReplier) Publish(context.Context, providers.Credentials, providers.PostContent, providers.Settings) (providers.Result, error) {
	return providers.Result{ExternalID: "p1", URL: "https://example.com/p1"}, nil
}

func (f *fakeReplier) Reply(_ context.Context, _ providers.Credentials, parent string, p providers.PostContent, _ providers.Settings) (providers.Result, error) {
	*f.replies = append(*f.replies, parent+" <- "+p.Text)
	return providers.Result{ExternalID: "r" + p.Text, URL: "https://example.com/r"}, nil
}

// Was a bug: a post that failed on one channel is editable; editing its thread rewrites the replies still to come
// under the copy already live on another channel.
func TestCommentsOfAPostLiveOnAChannelStay(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	var got []string
	f := &fakeReplier{Base: providers.NewBase("fakereplier", "Fake Replier", providers.AuthNone, providers.Limits{MaxChars: 280, MaxMedia: 4}), replies: &got}
	providers.Register(f, providers.StatusAvailable)
	t.Cleanup(func() { providers.Unregister(f.ID()) })
	e, _ := providers.Get(f.ID())
	ch, err := s.saveChannel(ctx, p, e, "Replies", providers.Credentials{}, providers.Profile{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	custom := post.Targets[0].ChannelID
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &[]PostTargetInput{{ChannelID: custom}, {ChannelID: ch.ID}},
		Thread: &[]ThreadPart{{Content: "part one", DelaySeconds: 3600}, {Content: "part two", DelaySeconds: 60}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	if _, err := s.ReportTargetResult(ctx, p, post.ID, custom, ResultInput{Status: "failed", Error: "nope"}); err != nil {
		t.Fatal(err)
	}
	// The comments are refused: they are still going out under the copy that is live.
	var conflictErr *Error
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Thread: &[]ThreadPart{{Content: "rewritten for the other channel"}}}); !errors.As(err, &conflictErr) || conflictErr.Status != 409 {
		t.Fatalf("changing the comments of a post live on a channel: %v, want 409", err)
	}
	// The same comments sent back unchanged (a full save from the composer) are fine.
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Thread: &[]ThreadPart{{Content: "part one", DelaySeconds: 3600}, {Content: "part two", DelaySeconds: 60}}}); err != nil {
		t.Fatalf("the unchanged comments: %v", err)
	}
	s.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	s.RunDueJobsOnce(ctx)
	s.Now = func() time.Time { return time.Now().Add(3 * time.Hour) } // part two goes a minute after part one
	s.RunDueJobsOnce(ctx)
	if len(got) != 2 || got[0] != "p1 <- part one" {
		t.Fatalf("the live thread got %q, want the parts it was published with", got)
	}
}

type fakeFlaky struct{ providers.Base }

func (f *fakeFlaky) Refresh(context.Context, providers.App, providers.Credentials) (providers.Credentials, error) {
	return nil, errors.New("Post \"https://api.example.com/oauth2/token\": dial tcp: i/o timeout")
}

// Was a bug: a token refresh that hits a network blip marks the channel needs_reconnect; nothing retries.
func TestTransientRefreshErrorKeepsTheChannel(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := context.Background()
	f := &fakeFlaky{Base: providers.NewBase("fakeflaky", "Fake Flaky", providers.AuthOAuth2, providers.Limits{MaxChars: 280})}
	providers.Register(f, providers.StatusAvailable)
	t.Cleanup(func() { providers.Unregister(f.ID()) })
	s.Getenv = func(k string) string {
		if k == "PROVIDER_FAKEFLAKY_CLIENT_ID" {
			return "client"
		}
		return ""
	}
	e, _ := providers.Get(f.ID())
	ch, err := s.saveChannel(ctx, p, e, "Flaky", providers.Credentials{"access_token": "a0", "refresh_token": "r0",
		"expires_at": time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)}, providers.Profile{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return time.Now().Add(25 * time.Hour) } // the refresh_channel job is due
	s.RunDueJobsOnce(ctx)
	row, _ := s.Q.GetChannelByID(ctx, ch.ID)
	t.Logf("channel %s: %s", row.Status, row.LastError)
	if row.Status == "needs_reconnect" {
		t.Fatalf("one timeout on the token endpoint disconnected the channel (refresh_channel has 3 attempts, used 1)")
	}
}

func customChannelFor(t *testing.T, s *Service, p Principal, name string) string {
	t.Helper()
	ch, err := s.ConnectChannel(context.Background(), p, ChannelInput{Provider: "custom", Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return ch.ID
}

// Was a bug: cancelling a queued publish_target job strands the post in "publishing" with no way out.
func TestCancelledTargetJobFailsTheTarget(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListJobs(ctx, p, JobFilter{Kind: "publish_target", Status: "queued"})
	if err != nil || len(page.Data) != 1 {
		t.Fatalf("jobs %v %v", page.Data, err)
	}
	if _, err := s.CancelJob(ctx, p, page.Data[0].ID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	s.Now = func() time.Time { return time.Now().Add(48 * time.Hour) }
	s.RunDueJobsOnce(ctx)
	got, _ := s.GetPost(ctx, p, post.ID)
	t.Logf("post status %s, target status %s", got.Status, got.Targets[0].Status)
	_, e1 := s.RetryTarget(ctx, p, post.ID, got.Targets[0].ChannelID)
	_, e2 := s.RetryJob(ctx, p, page.Data[0].ID)
	_, e3 := s.PublishPost(ctx, p, post.ID)
	_, e4 := s.UpdatePost(ctx, p, post.ID, PostPatch{Content: ptrTo("x")})
	_, e5 := s.SchedulePost(ctx, p, post.ID, time.Now().Add(72*time.Hour).UTC().Format(time.RFC3339))
	t.Logf("retry target: %v\nretry job: %v\npublish: %v\nupdate: %v\nschedule: %v", e1, e2, e3, e4, e5)
	if got.Status == "publishing" && e1 != nil && e2 != nil && e3 != nil && e4 != nil && e5 != nil {
		t.Fatalf("post stuck in publishing with every way out refused")
	}
}

// Was a bug: deleting the channel of the last open target leaves the post publishing forever.
func TestRemovingAChannelSettlesItsPublishingPosts(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	other := customChannelFor(t, s, p, "Other")
	first := post.Targets[0].ChannelID
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &[]PostTargetInput{{ChannelID: first}, {ChannelID: other}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx) // both custom targets now wait for a result
	if _, err := s.ReportTargetResult(ctx, p, post.ID, first, ResultInput{Status: "published", URL: "https://example.com/1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteChannel(ctx, p, other); err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return time.Now().Add(72 * time.Hour) } // past every processing timeout
	s.RunDueJobsOnce(ctx)
	got, _ := s.GetPost(ctx, p, post.ID)
	t.Logf("post %s, targets %d", got.Status, len(got.Targets))
	if got.Status == "publishing" {
		t.Fatalf("post stuck in publishing after its open target's channel was removed")
	}
}

// Was a bug: a scheduled post whose only channel was removed is marked published at its time.
func TestScheduledPostWithoutChannelsFails(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	if err := s.DeleteChannel(ctx, p, post.Targets[0].ChannelID); err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	s.RunDueJobsOnce(ctx)
	got, _ := s.GetPost(ctx, p, post.ID)
	t.Logf("post %s, published_at %v, targets %d", got.Status, got.PublishedAt, len(got.Targets))
	if got.Status == "published" {
		t.Fatalf("a post with no channels was marked published")
	}
}

// Was a bug: cancelling the queued publish_post job leaves the post "scheduled" forever.
func TestCancelledPublishJobUnschedulesThePost(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	page, err := s.ListJobs(ctx, p, JobFilter{Kind: "publish_post", Status: "queued"})
	if err != nil || len(page.Data) != 1 {
		t.Fatalf("jobs %v %v", page.Data, err)
	}
	if _, err := s.CancelJob(ctx, p, page.Data[0].ID); err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return time.Now().Add(48 * time.Hour) }
	s.RunDueJobsOnce(ctx)
	got, _ := s.GetPost(ctx, p, post.ID)
	t.Logf("post %s, publish_at %v (now +48h)", got.Status, *got.PublishAt)
	if got.Status == "scheduled" {
		t.Fatalf("post still scheduled two days after its time with no job")
	}
}

// Was a bug: schedule pick=next, no channels: the oldest draft without channels blocks every later draft.
func TestScheduleNextPassesOverADraftItCantPublish(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	ch := post.Targets[0].ChannelID
	stuck, err := s.CreatePost(ctx, p, PostInput{Collection: post.CollectionID, Content: "no channels yet"})
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return time.Now().Add(time.Second) }
	ready, err := s.CreatePost(ctx, p, PostInput{Collection: post.CollectionID, Content: "ready", Targets: []PostTargetInput{{ChannelID: ch}}})
	if err != nil {
		t.Fatal(err)
	}
	sc, err := s.CreateSchedule(ctx, p, ScheduleInput{Collection: &post.CollectionID, Name: ptrTo("Daily"), Kind: ptrTo("recurring"),
		Rule: &ScheduleRule{EveryMinutes: 60}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		h := time.Duration(i) * 61 * time.Minute
		s.Now = func() time.Time { return time.Now().Add(h) }
		s.RunDueJobsOnce(ctx)
	}
	r, _ := s.GetPost(ctx, p, ready.ID)
	st, _ := s.GetPost(ctx, p, stuck.ID)
	sch, _ := s.GetSchedule(ctx, p, sc.ID)
	t.Logf("stuck %s, ready %s, last_result %q", st.Status, r.Status, sch.LastResult)
	if r.Status == "draft" {
		t.Fatalf("3 runs and the draft with a channel never went out")
	}
}

// F4b: a schedule whose channel was deleted fails every post that needs its channels.
func TestScheduleIgnoresARemovedChannel(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	keep := post.Targets[0].ChannelID
	gone := customChannelFor(t, s, p, "Gone")
	d, err := s.CreatePost(ctx, p, PostInput{Collection: post.CollectionID, Content: "draft"})
	if err != nil {
		t.Fatal(err)
	}
	sc, err := s.CreateSchedule(ctx, p, ScheduleInput{Collection: &post.CollectionID, Name: ptrTo("Daily"), Kind: ptrTo("recurring"),
		Rule: &ScheduleRule{EveryMinutes: 60}, Channels: &[]string{keep, gone}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteChannel(ctx, p, gone); err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return time.Now().Add(61 * time.Minute) }
	s.RunDueJobsOnce(ctx)
	got, _ := s.GetPost(ctx, p, d.ID)
	sch, _ := s.GetSchedule(ctx, p, sc.ID)
	t.Logf("draft %s, schedule channels %v, last_result %q", got.Status, sch.Channels, sch.LastResult)
	if got.Status == "draft" {
		t.Fatalf("draft never published: the schedule still names the removed channel")
	}
}
