package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/providers"
)

// scriptedProvider publishes as the script says: each Publish takes the next error (nil = published); past the
// script every call publishes. It counts calls and successful sends. With hold set, a Publish says it started
// and waits there until hold is closed.
type scriptedProvider struct {
	providers.Base
	mu      sync.Mutex
	errs    []error
	calls   int
	sent    int
	started chan struct{}
	hold    chan struct{}
}

func (f *scriptedProvider) Publish(ctx context.Context, _ providers.Credentials, _ providers.PostContent, _ providers.Settings) (providers.Result, error) {
	if f.hold != nil {
		f.started <- struct{}{}
		select {
		case <-f.hold:
		case <-ctx.Done():
			return providers.Result{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return providers.Result{}, err
		}
	}
	f.sent++
	return providers.Result{ExternalID: "ext", URL: "https://example.com/ext"}, nil
}

func (f *scriptedProvider) Remove(context.Context, providers.Credentials, string) error { return nil }

func (f *scriptedProvider) counts() (calls, sent int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.sent
}

// scriptedChannelOn connects a channel on a scriptedProvider registered as id.
func scriptedChannelOn(t *testing.T, s *Service, p Principal, id string) (*scriptedProvider, string) {
	t.Helper()
	f := &scriptedProvider{Base: providers.NewBase(id, id, providers.AuthNone, providers.Limits{MaxChars: 280})}
	providers.Register(f, providers.StatusAvailable)
	t.Cleanup(func() { providers.Unregister(id) })
	e, _ := providers.Get(id)
	ch, err := s.saveChannel(context.Background(), p, e, id, providers.Credentials{}, providers.Profile{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return f, ch.ID
}

// publishOnTwoChannels publishes a post on two scripted channels: the first publishes at once, the second
// follows errs. before (when set) runs right before the post is published.
func publishOnTwoChannels(t *testing.T, idA, idB string, before func(*Service), errs ...error) (*Service, Principal, Post, *scriptedProvider, []string) {
	t.Helper()
	s, p, post := publishTest(t)
	ctx := context.Background()
	_, chA := scriptedChannelOn(t, s, p, idA)
	b, chB := scriptedChannelOn(t, s, p, idB)
	b.errs = errs
	targets := []PostTargetInput{{ChannelID: chA}, {ChannelID: chB}}
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &targets}); err != nil {
		t.Fatal(err)
	}
	if before != nil {
		before(s)
	}
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	return s, p, post, b, []string{chA, chB}
}

// failedOnSecondChannel: the first channel has the post, the second failed for good (its job is discarded).
func failedOnSecondChannel(t *testing.T, idA, idB string) (*Service, Principal, Post, *scriptedProvider, []string) {
	t.Helper()
	down := errors.New("platform down")
	s, p, post, b, chans := publishOnTwoChannels(t, idA, idB, nil, down, down, down)
	later := time.Now()
	for range 3 {
		later = later.Add(time.Hour)
		s.Now = func() time.Time { return later }
		s.RunDueJobsOnce(context.Background())
	}
	got, _ := s.GetPost(context.Background(), p, post.ID)
	if targetOf(t, got, chans[0]).Status != "published" || targetOf(t, got, chans[1]).Status != "failed" {
		t.Fatalf("setup: %s %+v", got.Status, got.Targets)
	}
	return s, p, post, b, chans
}

// Channel A has the post, channel B waits for its retry (a platform outage). The post is unpublished: B's retry
// must not put the post on B afterwards, and the post must not wait for B for good.
func TestUnpublishStopsATargetWaitingForRetry(t *testing.T) {
	s, p, post, b, chans := publishOnTwoChannels(t, "scriptun1", "scriptun2", nil, errors.New("platform down"))
	ctx := context.Background()
	got, _ := s.GetPost(ctx, p, post.ID)
	if targetOf(t, got, chans[0]).Status != "published" || targetOf(t, got, chans[1]).Status != "publishing" {
		t.Fatalf("setup: %s %+v", got.Status, got.Targets)
	}
	if _, err := s.UnpublishPost(ctx, p, post.ID, nil); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Hour)
	s.Now = func() time.Time { return later }
	s.RunDueJobsOnce(ctx)

	got, _ = s.GetPost(ctx, p, post.ID)
	if _, sent := b.counts(); sent != 0 {
		t.Errorf("channel B received the post after it was unpublished (sends: %d)", sent)
	}
	if tg := targetOf(t, got, chans[1]); tg.Status != "failed" || got.Status != "failed" {
		t.Errorf("target B is %q (%s), post %q after the unpublish; want both failed", tg.Status, tg.Error, got.Status)
	}
	if jobs := jobsByKey(t, s, "publish_target:"+post.ID+":"+chans[1]); len(jobs) != 0 {
		t.Errorf("B's publish job is still there: %s", jobs[0].State)
	}
}

// A retry that is already running when the post is unpublished reads the post first: it does not send.
func TestPublishRunOfAnUnpublishedPostDoesNotSend(t *testing.T) {
	s, p, post, b, chans := failedOnSecondChannel(t, "scriptun7", "scriptun8")
	ctx := context.Background()
	if _, err := s.RetryTarget(ctx, p, post.ID, chans[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE posts SET unpublished_at = ? WHERE id = ?`, s.now(), post.ID); err != nil { // the takedown, past its job cancel
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	got, _ := s.GetPost(ctx, p, post.ID)
	if _, sent := b.counts(); sent != 0 || targetOf(t, got, chans[1]).Status != "failed" {
		t.Errorf("sends %d, target %+v; want no send and a failed target", sent, targetOf(t, got, chans[1]))
	}
}

// B failed for good. After the unpublish "Retry" on the channel answers 409 unpublished; retrying the old job in
// the queue must answer the same and publish nothing.
func TestRetryJobOfAnUnpublishedPost(t *testing.T) {
	s, p, post, b, chans := failedOnSecondChannel(t, "scriptun3", "scriptun4")
	ctx := context.Background()
	if _, err := s.UnpublishPost(ctx, p, post.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RetryTarget(ctx, p, post.ID, chans[1]); !errors.Is(err, errUnpublished) {
		t.Fatalf("RetryTarget after unpublish: %v, want errUnpublished", err)
	}
	page, err := s.ListJobs(ctx, p, JobFilter{Kind: "publish_target", Status: "failed"})
	if err != nil || len(page.Data) != 1 {
		t.Fatalf("failed jobs %+v %v", page.Data, err)
	}
	if _, err := s.RetryJob(ctx, p, page.Data[0].ID); !errors.Is(err, errUnpublished) {
		t.Errorf("RetryJob after unpublish: %v, want errUnpublished", err)
	}
	s.RunDueJobsOnce(ctx)
	if _, sent := b.counts(); sent != 0 {
		t.Errorf("RetryJob published an unpublished post on channel B (sends: %d)", sent)
	}
}

// A schedule that publishes selected posts takes failed ones too: not one that was unpublished.
func TestScheduleDoesNotPublishAnUnpublishedPost(t *testing.T) {
	s, p, post, b, _ := failedOnSecondChannel(t, "scriptun5", "scriptun6")
	ctx := context.Background()
	if _, err := s.UnpublishPost(ctx, p, post.ID, nil); err != nil {
		t.Fatal(err)
	}
	sc, err := s.CreateSchedule(ctx, p, ScheduleInput{Collection: &post.CollectionID, Name: ptrTo("Selected"), Kind: ptrTo("recurring"),
		Rule: &ScheduleRule{EveryMinutes: 10}, Pick: ptrTo("selected"), PostIDs: &[]string{post.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunScheduleNow(ctx, p, sc.ID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	if _, sent := b.counts(); sent != 0 {
		t.Errorf("the schedule published an unpublished post on channel B (sends: %d)", sent)
	}
}

// Retrying the failed job of a scheduled time whose post is no longer scheduled would run and do nothing: it is
// refused instead of answering success.
func TestRetryJobOfAPostNoLongerScheduled(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	rescueDiscard(t, s, "publish_post:"+post.ID)
	if n := s.settleRescuedJobs(ctx); n != 1 {
		t.Fatalf("settled %d", n)
	}
	later := time.Now().Add(2 * time.Hour)
	s.Now = func() time.Time { return later }
	if err := s.jobFailed(ctx, "publish_post", jobArgs{WorkspaceID: p.WorkspaceID, Payload: jobPayload{PostID: post.ID}}, errors.New("database is locked")); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetPost(ctx, p, post.ID); got.Status != "failed" {
		t.Fatalf("setup: post %s", got.Status)
	}
	job := jobsByKey(t, s, "publish_post:"+post.ID)[0]
	var e *Error
	if _, err := s.RetryJob(ctx, p, jobPublicID(job.ID)); !errors.As(err, &e) || e.Status != 409 {
		t.Fatalf("RetryJob of a failed post's scheduled job: %v, want 409", err)
	}
}

// The platform accepted the post, then recording the result failed (here: the database refuses the write of
// status "published"). The job's next attempt records the receipt it kept; it does not send the post again.
func TestFailedSettleDoesNotPublishTwice(t *testing.T) {
	old := settleRetry
	settleRetry = []time.Duration{time.Millisecond}
	t.Cleanup(func() { settleRetry = old })
	var lift func()
	s, p, post, b, chans := publishOnTwoChannels(t, "scriptst1", "scriptst2", func(s *Service) {
		lift = refuseWrites(t, s, `UPDATE ON post_targets WHEN NEW.status = 'published'`)
	})
	ctx := context.Background()
	got, _ := s.GetPost(ctx, p, post.ID)
	if calls, sent := b.counts(); sent != 1 || targetOf(t, got, chans[1]).Status != "publishing" {
		t.Fatalf("setup: calls %d sent %d, target %+v", calls, sent, targetOf(t, got, chans[1]))
	}
	lift() // the database is fine again
	later := time.Now().Add(2 * time.Hour)
	s.Now = func() time.Time { return later }
	s.RunDueJobsOnce(ctx)
	got, _ = s.GetPost(ctx, p, post.ID)
	tg := targetOf(t, got, chans[1])
	if _, sent := b.counts(); sent != 1 {
		t.Fatalf("the platform received the post %d times (target now %q); want once", sent, tg.Status)
	}
	if tg.Status != "published" || tg.ExternalID != "ext" || got.Status != "published" {
		t.Fatalf("after the retry: target %+v, post %s; want published with the first send's id", tg, got.Status)
	}
}

// The result can never be recorded: the job runs out of attempts without sending again, and the target fails
// with the link and a message that says it went out (a person checks it; nothing sends it twice).
func TestSendThatIsNeverRecordedFailsWithItsLink(t *testing.T) {
	old := settleRetry
	settleRetry = nil
	t.Cleanup(func() { settleRetry = old })
	s, p, post, b, chans := publishOnTwoChannels(t, "scriptst3", "scriptst4", func(s *Service) {
		// Only the second channel's result is refused. Its id goes into the condition as text: PostgreSQL takes no
		// subquery there, and the test runs on both databases.
		var second string
		if err := s.DB.QueryRow(`SELECT id FROM channels WHERE provider = 'scriptst4'`).Scan(&second); err != nil {
			t.Fatal(err)
		}
		refuseWrites(t, s, `UPDATE ON post_targets WHEN NEW.status = 'published' AND NEW.channel_id = '`+second+`'`)
	})
	ctx := context.Background()
	later := time.Now()
	for range 3 {
		later = later.Add(time.Hour)
		s.Now = func() time.Time { return later }
		s.RunDueJobsOnce(ctx)
	}
	got, _ := s.GetPost(ctx, p, post.ID)
	tg := targetOf(t, got, chans[1])
	if _, sent := b.counts(); sent != 1 {
		t.Fatalf("the platform received the post %d times; want once", sent)
	}
	if tg.Status != "failed" || tg.ExternalURL != "https://example.com/ext" || got.Status != "failed" {
		t.Fatalf("target %+v, post %s; want failed with the link of what was sent", tg, got.Status)
	}
}

// Cancelling a job a worker claimed a moment ago (the caller still saw it queued) is refused: the run goes on
// and its result is recorded, instead of the target failing while the post goes out.
func TestCancelOfAJobThatJustStartedIsRefused(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	f, ch := scriptedChannelOn(t, s, p, "scriptcn1")
	f.started, f.hold = make(chan struct{}, 1), make(chan struct{})
	targets := []PostTargetInput{{ChannelID: ch}}
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &targets}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	queued := jobsByKey(t, s, "publish_target:"+post.ID+":"+ch)[0] // as CancelJob read it
	done := make(chan struct{})
	go func() { s.RunDueJobsOnce(ctx); close(done) }()
	<-f.started // the platform call is on its way
	var e *Error
	if err := s.cancelQueuedJob(ctx, p, queued); !errors.As(err, &e) || e.Status != 409 {
		t.Errorf("cancel of a running job: %v, want 409", err)
	}
	close(f.hold)
	<-done
	got, _ := s.GetPost(ctx, p, post.ID)
	if tg := targetOf(t, got, ch); tg.Status != "published" {
		t.Fatalf("the post went out, the target says %q (%s)", tg.Status, tg.Error)
	}
}
