package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/providers"
)

// fakeRemovable publishes (with thread replies) and removes as its script says: each Remove takes the next
// error (nil = removed); past the script every call removes.
type fakeRemovable struct {
	providers.Base
	mu      sync.Mutex
	n       int
	removed []string
	errs    []error
}

func (f *fakeRemovable) Publish(context.Context, providers.Credentials, providers.PostContent, providers.Settings) (providers.Result, error) {
	return providers.Result{ExternalID: "post", URL: "https://example.com/post"}, nil
}

func (f *fakeRemovable) Reply(_ context.Context, _ providers.Credentials, _ string, _ providers.PostContent, _ providers.Settings) (providers.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	return providers.Result{ExternalID: "reply" + strconv.Itoa(f.n)}, nil
}

func (f *fakeRemovable) Remove(_ context.Context, _ providers.Credentials, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return err
		}
	}
	f.removed = append(f.removed, id)
	return nil
}

func (f *fakeRemovable) removedIDs() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.removed, ",")
}

// publishedOn: a post published on a fresh fakeRemovable channel (id), with an optional thread of two parts.
func publishedOn(t *testing.T, ids ...string) (*Service, Principal, Post, map[string]*fakeRemovable, []string) {
	t.Helper()
	s, p, post := publishTest(t)
	ctx := context.Background()
	fakes, chans := map[string]*fakeRemovable{}, []string{}
	var targets []PostTargetInput
	for _, id := range ids {
		f := &fakeRemovable{Base: providers.NewBase(id, id, providers.AuthNone, providers.Limits{MaxChars: 280})}
		providers.Register(f, providers.StatusAvailable)
		t.Cleanup(func() { providers.Unregister(id) })
		e, _ := providers.Get(id)
		ch, err := s.saveChannel(ctx, p, e, id, providers.Credentials{}, providers.Profile{}, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		fakes[id], chans = f, append(chans, ch.ID)
		targets = append(targets, PostTargetInput{ChannelID: ch.ID})
	}
	thread := []ThreadPart{{Content: "two"}, {Content: "three"}}
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &targets, Thread: &thread}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	got, _ := s.GetPost(ctx, p, post.ID)
	if got.Status != "published" {
		t.Fatalf("post %s %+v", got.Status, got.Targets)
	}
	return s, p, got, fakes, chans
}

func TestUnpublishRemoves(t *testing.T) {
	s, p, post, fakes, chans := publishedOn(t, "fakeun1")
	ctx := context.Background()
	got, err := s.UnpublishPost(ctx, p, post.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.UnpublishedAt == nil || got.UnpublishedBy == "" || targetOf(t, got, chans[0]).Removal != "removing" {
		t.Fatalf("after the request: %+v", got)
	}
	s.RunDueJobsOnce(ctx)
	got, _ = s.GetPost(ctx, p, post.ID)
	tg := targetOf(t, got, chans[0])
	if tg.Removal != "removed" || tg.RemovedAt == nil || got.Status != "published" {
		t.Fatalf("after the job: %s %+v", got.Status, tg)
	}
	// TestUnpublishReplyOrder: parts 2, 1, then the post.
	if fakes["fakeun1"].removedIDs() != "reply2,reply1,post" {
		t.Fatalf("removed %q", fakes["fakeun1"].removedIDs())
	}
	if tg.Replies[0].Status != "removed" || tg.Replies[1].Status != "removed" {
		t.Fatalf("replies %+v", tg.Replies)
	}
	if !hasEvent(t, s, p, "post.unpublished") || !hasEvent(t, s, p, "post.target_removed") {
		t.Fatal("events missing")
	}
	// Run again: nothing is called twice.
	if err := s.removeTarget(ctx, post.ID, chans[0]); err != nil || fakes["fakeun1"].removedIDs() != "reply2,reply1,post" {
		t.Fatalf("rerun: %v %q", err, fakes["fakeun1"].removedIDs())
	}
}

func hasEvent(t *testing.T, s *Service, p Principal, typ string) bool {
	t.Helper()
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE workspace_id = ? AND type = ?`, p.WorkspaceID, typ).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func TestUnpublishManual(t *testing.T) {
	s, p, post := publishTest(t) // a custom channel
	ctx := context.Background()
	if _, err := s.UnpublishPost(ctx, p, post.ID, nil); !isConflictCode(err, "not_published") {
		t.Fatalf("a scheduled post: %v", err)
	}
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	got, _ := s.GetPost(ctx, p, post.ID)
	if got.Targets[0].Status == "published" {
		got2, err := s.UnpublishPost(ctx, p, post.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		if tg := got2.Targets[0]; tg.Removal != "manual" || tg.RemovalError == "" {
			t.Fatalf("%+v", tg)
		}
	} else { // a custom target waits for its result: report it
		if _, err := s.ReportTargetResult(ctx, p, post.ID, got.Targets[0].ChannelID, ResultInput{Status: "published"}); err != nil {
			t.Fatal(err)
		}
		got2, err := s.UnpublishPost(ctx, p, post.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		if tg := got2.Targets[0]; tg.Removal != "manual" || tg.RemovalError == "" {
			t.Fatalf("%+v", tg)
		}
	}
}

func isConflictCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code && e.Status == 409
}

func TestUnpublishErrManual(t *testing.T) {
	s, p, post, fakes, chans := publishedOn(t, "fakeun2")
	ctx := context.Background()
	fakes["fakeun2"].errs = []error{&providers.Failure{Kind: providers.ErrManual, Msg: "too old"}}
	if _, err := s.UnpublishPost(ctx, p, post.ID, nil); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	got, _ := s.GetPost(ctx, p, post.ID)
	if tg := targetOf(t, got, chans[0]); tg.Removal != "manual" || tg.RemovalError != "too old" {
		t.Fatalf("%+v", tg)
	}
}

func TestUnpublishReauth(t *testing.T) {
	s, p, post, fakes, chans := publishedOn(t, "fakeun3")
	ctx := context.Background()
	fakes["fakeun3"].errs = []error{&providers.Failure{Kind: providers.ErrReauth, Msg: "token revoked"}}
	if _, err := s.UnpublishPost(ctx, p, post.ID, nil); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	got, _ := s.GetPost(ctx, p, post.ID)
	ch, _ := s.Q.GetChannelByID(ctx, chans[0])
	if tg := targetOf(t, got, chans[0]); tg.Removal != "failed" || ch.Status != "needs_reconnect" || !hasEvent(t, s, p, "post.target_remove_failed") {
		t.Fatalf("%+v channel %s", tg, ch.Status)
	}
}

func TestUnpublishRetry(t *testing.T) {
	s, p, post, fakes, chans := publishedOn(t, "fakeun4")
	ctx := context.Background()
	boom := errors.New("503")
	fakes["fakeun4"].errs = []error{boom, boom, boom}
	if _, err := s.UnpublishPost(ctx, p, post.ID, nil); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for _, later := range []time.Duration{0, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute, time.Hour} {
		s.Now = func() time.Time { return start.Add(later) }
		s.RunDueJobsOnce(ctx)
	}
	got, _ := s.GetPost(ctx, p, post.ID)
	if tg := targetOf(t, got, chans[0]); tg.Removal != "failed" || !strings.Contains(tg.RemovalError, "503") {
		t.Fatalf("%+v", tg)
	}
	// Again: the failed target is queued once more and now works.
	if _, err := s.UnpublishPost(ctx, p, post.ID, nil); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	got, _ = s.GetPost(ctx, p, post.ID)
	if tg := targetOf(t, got, chans[0]); tg.Removal != "removed" {
		t.Fatalf("%+v", tg)
	}
}

func TestUnpublishScope(t *testing.T) {
	s, p, post, _, _ := publishedOn(t, "fakeun5")
	p.Kind, p.UserID, p.Scopes = PrincipalAPIKey, "", []string{ScopePostsWrite}
	var e *Error
	if _, err := s.UnpublishPost(context.Background(), p, post.ID, nil); !errors.As(err, &e) || e.Status != 403 {
		t.Fatalf("%v", err)
	}
}

func TestUnpublishChosenChannel(t *testing.T) {
	s, p, post, fakes, chans := publishedOn(t, "fakeun6", "fakeun7")
	ctx := context.Background()
	var e *Error
	if _, err := s.UnpublishPost(ctx, p, post.ID, []string{"chn_nope"}); !errors.As(err, &e) || e.Status != 422 || e.Fields["channel_ids"] == "" {
		t.Fatalf("%v", err)
	}
	if _, err := s.UnpublishPost(ctx, p, post.ID, []string{}); !errors.As(err, &e) || e.Status != 422 || e.Fields["channel_ids"] == "" {
		t.Fatalf("empty list: %v", err)
	}
	if _, err := s.UnpublishPost(ctx, p, post.ID, []string{chans[1]}); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	got, _ := s.GetPost(ctx, p, post.ID)
	if targetOf(t, got, chans[0]).Removal != "" || targetOf(t, got, chans[1]).Removal != "removed" || fakes["fakeun6"].removedIDs() != "" {
		t.Fatalf("%+v", got.Targets)
	}
}

func TestUnpublishedGuards(t *testing.T) {
	s, p, post, _, chans := publishedOn(t, "fakeun8")
	ctx := context.Background()
	if _, err := s.UnpublishPost(ctx, p, post.ID, nil); err != nil {
		t.Fatal(err)
	}
	at := s.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if _, err := s.RetryTarget(ctx, p, post.ID, chans[0]); !isConflictCode(err, "unpublished") {
		t.Fatalf("retry: %v", err)
	}
	if _, err := s.SchedulePost(ctx, p, post.ID, at); !isConflictCode(err, "unpublished") {
		t.Fatalf("schedule: %v", err)
	}
	if _, err := s.PublishPost(ctx, p, post.ID); !isConflictCode(err, "unpublished") {
		t.Fatalf("publish: %v", err)
	}
}

// A reply job that was already running when the post was unpublished must not post (or chain) under a removing target.
func TestUnpublishStopsReplyAndRepeat(t *testing.T) {
	s, p, post, fakes, chans := publishedOn(t, "fakeun9")
	ctx := context.Background()
	if _, err := s.UnpublishPost(ctx, p, post.ID, nil); err != nil {
		t.Fatal(err)
	}
	// Pretend part 1 never went out, then run its reply job late.
	if _, err := s.DB.ExecContext(ctx, `UPDATE post_targets SET replies = '[]' WHERE post_id = ?`, post.ID); err != nil {
		t.Fatal(err)
	}
	before := fakes["fakeun9"].n
	if err := s.publishReply(ctx, post.ID, chans[0], 0, "post"); err != nil || fakes["fakeun9"].n != before {
		t.Fatalf("reply posted under a removing target: %v", err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE posts SET repeat_every = 'weekly' WHERE id = ?`, post.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.runRepeatPost(ctx, p.WorkspaceID, post.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Q.RepeatOfPost(ctx, post.ID); !isNoRows(err) {
		t.Fatalf("an unpublished post repeated: %v", err)
	}
}
