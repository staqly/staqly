package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

// publishTest: a local workspace with a custom channel and a post scheduled in an hour on it.
func publishTest(t testing.TB) (*Service, Principal, Post) {
	t.Helper()
	dir := t.TempDir()
	database, err := storetest.Open(t, dir+"/p.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	s := New(database, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{UploadDir: dir + "/uploads"})
	ctx := context.Background()
	if err := s.EnsureLocalWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := s.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	col, err := s.CreateCollection(ctx, p, CollectionInput{Name: ptrTo("Races")})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.ConnectChannel(ctx, p, ChannelInput{Provider: "custom", Name: "Worker"})
	if err != nil {
		t.Fatal(err)
	}
	at := s.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	post, err := s.CreatePost(ctx, p, PostInput{Collection: col.ID, Content: "later", Status: "scheduled", PublishAt: &at,
		Targets: []PostTargetInput{{ChannelID: ch.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	return s, p, post
}

// The scheduled time comes while the post is being taken off the schedule: the publish_post run read
// "scheduled" before the cancel committed, and must not publish the post the cancel made a draft.
func TestScheduledRunAfterConcurrentCancelDoesNotPublish(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	later := time.Now().Add(2 * time.Hour)
	s.Now = func() time.Time { return later }

	inTx, release, cancelled := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		cancelled <- s.tx(ctx, func(q *db.Queries) error { // what CancelPost writes, held open
			if err := q.SetPostStatus(ctx, db.SetPostStatusParams{Status: "draft", PublishedAt: nullStr(""), UpdatedAt: s.now(), ID: post.ID}); err != nil {
				return err
			}
			close(inTx)
			<-release
			return nil
		})
	}()
	<-inTx
	ran := make(chan error, 1)
	go func() { ran <- s.runPublishPost(ctx, p.WorkspaceID, post.ID) }()
	time.Sleep(150 * time.Millisecond) // the run has read the post and waits for the write
	close(release)
	if err := <-cancelled; err != nil {
		t.Fatal(err)
	}
	if err := <-ran; err != nil {
		t.Fatal(err)
	}
	row, err := s.Q.GetPostByID(ctx, post.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "draft" {
		t.Fatalf("post %s after a cancel raced the scheduled run, want draft", row.Status)
	}
}

func ptrTo[T any](v T) *T { return &v }

// fakeRotating is an OAuth platform that rotates refresh tokens like X: each one works once.
type fakeRotating struct {
	providers.Base
	mu       sync.Mutex
	current  string
	refreshs int
}

func (f *fakeRotating) Refresh(_ context.Context, _ providers.App, c providers.Credentials) (providers.Credentials, error) {
	time.Sleep(20 * time.Millisecond) // the platform's round trip
	f.mu.Lock()
	defer f.mu.Unlock()
	if c["refresh_token"] != f.current {
		return nil, &providers.Failure{Kind: providers.ErrReauth, Msg: "invalid_grant"}
	}
	f.refreshs++
	f.current = fmt.Sprintf("r%d", f.refreshs)
	return providers.Credentials{"access_token": "a" + f.current, "refresh_token": f.current,
		"expires_at": time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)}, nil
}

// Several publishes on one channel find its token about to run out at once: one refresh renews it and the
// others use what it saved. Refreshing again with the refresh token the first one used up would fail and
// mark the channel needs_reconnect.
func TestConcurrentTokenRefreshesRefreshOnce(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := context.Background()
	f := &fakeRotating{Base: providers.NewBase("fakerotating", "Fake Rotating", providers.AuthOAuth2, providers.Limits{MaxChars: 280}), current: "r0"}
	providers.Register(f, providers.StatusAvailable)
	t.Cleanup(func() { providers.Unregister(f.ID()) })
	s.Getenv = func(k string) string {
		if k == "PROVIDER_FAKEROTATING_CLIENT_ID" {
			return "client"
		}
		return ""
	}
	e, _ := providers.Get(f.ID())
	ch, err := s.saveChannel(ctx, p, e, "Rotating", providers.Credentials{"access_token": "a0", "refresh_token": "r0",
		"expires_at": time.Now().Add(time.Minute).UTC().Format(time.RFC3339)}, providers.Profile{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	row, err := s.Q.GetChannelByID(ctx, ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	var creds providers.Credentials
	if err := s.unseal(row.Credentials, &creds); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.refreshIfDue(ctx, row, e, creds); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("refresh: %v", err)
	}
	after, _ := s.Q.GetChannelByID(ctx, ch.ID)
	if after.Status != "connected" || f.refreshs != 1 {
		t.Fatalf("channel %s after %d refreshes, want connected after 1", after.Status, f.refreshs)
	}
}

// A schedule run's transactions may run twice on PostgreSQL (serialization retries): the posts it reports
// as published are counted once.
func TestScheduleRunCountsPostsOnceAcrossTxReruns(t *testing.T) {
	s, p, scheduled := publishTest(t)
	ctx := context.Background()
	draft, err := s.CreatePost(ctx, p, PostInput{Collection: scheduled.CollectionID, Content: "next up",
		Targets: []PostTargetInput{{ChannelID: scheduled.Targets[0].ChannelID}}})
	if err != nil {
		t.Fatal(err)
	}
	at := s.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	sc, err := s.CreateSchedule(ctx, p, ScheduleInput{Collection: &draft.CollectionID, Name: ptrTo("Daily"), Kind: ptrTo("once"), RunAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	txRehearse = true
	defer func() { txRehearse = false }()
	got, err := s.RunScheduleNow(ctx, p, sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastResult != "published 1" {
		t.Fatalf("last result %q, want published 1", got.LastResult)
	}
}

// Accounts picked after one sign-in (Tumblr blogs, Slack channels) share its refresh token and expiry, so
// their refresh jobs come due at the same moment. On a platform that rotates refresh tokens the two refreshes
// must not both spend the one token: the second would fail and its channel need reconnecting.
func TestSiblingChannelsRefreshingTogetherStayConnected(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := context.Background()
	f := &fakeRotating{Base: providers.NewBase("fakesiblings", "Fake Siblings", providers.AuthOAuth2, providers.Limits{MaxChars: 280}), current: "r0"}
	providers.Register(f, providers.StatusAvailable)
	t.Cleanup(func() { providers.Unregister(f.ID()) })
	s.Getenv = func(k string) string {
		if k == "PROVIDER_FAKESIBLINGS_CLIENT_ID" {
			return "client"
		}
		return ""
	}
	e, _ := providers.Get(f.ID())
	creds := providers.Credentials{"access_token": "a0", "refresh_token": "r0", "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
	var ids []string
	for _, name := range []string{"Blog one", "Blog two"} {
		ch, err := s.saveChannel(ctx, p, e, name, creds, providers.Profile{Handle: name}, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, ch.ID)
	}
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.refreshChannel(ctx, id); err != nil {
				t.Errorf("refresh: %v", err)
			}
		}()
	}
	wg.Wait()
	for _, id := range ids {
		row, err := s.Q.GetChannelByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if row.Status != "connected" {
			t.Fatalf("channel %s after its sibling refreshed at the same time: %s (%s)", row.Name, row.Status, row.LastError)
		}
	}
}
