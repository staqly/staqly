package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// movableClock puts the service on a clock the test moves.
func movableClock(s *Service) (advance func(time.Duration)) {
	now := time.Now()
	s.Now = func() time.Time { return now }
	return func(d time.Duration) {
		now = now.Add(d)
		s.Now = func() time.Time { return now }
	}
}

// Every way a send starts (Publish now, the scheduled time, Retry on the post's channel, Retry of the failed job
// in the queue, an attempt after one that died) ends in the same job. For each: the platform takes the post, the
// database refuses its result, and the next attempt records the receipt instead of sending the post again.
func TestNoSendPathPublishesTwiceWhenTheResultWriteFails(t *testing.T) {
	old := settleRetry
	settleRetry = nil
	t.Cleanup(func() { settleRetry = old })
	down := errors.New("platform down")
	paths := []struct {
		name string
		id   string
		errs []error // what the platform answers before the send under test
		// start brings the target to the send under test and says how many attempts are spent on the way.
		start func(t *testing.T, s *Service, p Principal, post Post, ch string, advance func(time.Duration))
	}{
		{"publish now", "sendpath1", nil, func(t *testing.T, s *Service, p Principal, post Post, ch string, _ func(time.Duration)) {
			if _, err := s.PublishPost(t.Context(), p, post.ID); err != nil {
				t.Fatal(err)
			}
		}},
		{"the scheduled time", "sendpath2", nil, func(_ *testing.T, _ *Service, _ Principal, _ Post, _ string, advance func(time.Duration)) {
			advance(2 * time.Hour) // publishTest scheduled it in an hour
		}},
		{"retry of the target", "sendpath3", []error{&providers.Failure{Kind: providers.ErrRejected, Msg: "too long"}},
			func(t *testing.T, s *Service, p Principal, post Post, ch string, _ func(time.Duration)) {
				if _, err := s.PublishPost(t.Context(), p, post.ID); err != nil {
					t.Fatal(err)
				}
				s.RunDueJobsOnce(t.Context())
				if _, err := s.RetryTarget(t.Context(), p, post.ID, ch); err != nil {
					t.Fatalf("setup: retry of the refused target: %v", err)
				}
			}},
		{"retry of the job in the queue", "sendpath4", []error{down, down, down},
			func(t *testing.T, s *Service, p Principal, post Post, ch string, advance func(time.Duration)) {
				if _, err := s.PublishPost(t.Context(), p, post.ID); err != nil {
					t.Fatal(err)
				}
				for range 3 {
					s.RunDueJobsOnce(t.Context())
					advance(time.Hour)
				}
				jobs := jobsByKey(t, s, "publish_target:"+post.ID+":"+ch)
				if len(jobs) != 1 || jobStatus(jobs[0].State) != "failed" {
					t.Fatalf("setup: jobs %+v, want one failed", jobs)
				}
				if _, err := s.RetryJob(t.Context(), p, jobPublicID(jobs[0].ID)); err != nil {
					t.Fatalf("setup: retry of the failed job: %v", err)
				}
			}},
	}
	for _, path := range paths {
		for _, died := range []bool{false, true} {
			name := path.name
			if died {
				name += ", then an attempt that died"
			}
			t.Run(name, func(t *testing.T) {
				s, p, post := publishTest(t)
				ctx := t.Context()
				advance := movableClock(s)
				f, ch := scriptedChannelOn(t, s, p, path.id)
				f.errs = path.errs
				if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &[]PostTargetInput{{ChannelID: ch}}}); err != nil {
					t.Fatal(err)
				}
				path.start(t, s, p, post, ch, advance)
				_, before := f.counts()

				lift := refuseWrites(t, s, `UPDATE ON post_targets WHEN NEW.status = 'published'`)
				s.RunDueJobsOnce(ctx) // the platform takes the post; recording it fails
				got, _ := s.GetPost(ctx, p, post.ID)
				if _, sent := f.counts(); sent != before+1 || targetOf(t, got, ch).Status != "publishing" {
					t.Fatalf("setup: sends %d (before %d), target %+v", sent, before, targetOf(t, got, ch))
				}
				if died {
					// The process running the next attempt died: River's rescuer puts the job back with the
					// attempt spent.
					job := jobsByKey(t, s, "publish_target:"+post.ID+":"+ch)[0]
					stmt := "UPDATE river_job SET attempt = attempt + 1 WHERE id = ?"
					if s.pg {
						stmt = "UPDATE river_job SET attempt = attempt + 1 WHERE id = $1"
					}
					if _, err := s.DB.ExecContext(ctx, stmt, job.ID); err != nil {
						t.Fatal(err)
					}
				}
				lift()
				advance(2 * time.Hour)
				s.RunDueJobsOnce(ctx)

				got, _ = s.GetPost(ctx, p, post.ID)
				tg := targetOf(t, got, ch)
				if _, sent := f.counts(); sent != before+1 {
					t.Fatalf("the platform received the post %d times, want once (target now %s)", sent-before, tg.Status)
				}
				if tg.Status != "published" || tg.ExternalID != "ext" || got.Status != "published" {
					t.Fatalf("target %+v, post %s; want published with the id of the one send", tg, got.Status)
				}
				if n := len(eventsOfType(t, s, p, "post.target_published")); n != 1 {
					t.Fatalf("%d post.target_published events, want 1", n)
				}
			})
		}
	}
}

// While a send is at the platform nothing else may start a second one or take the post away: Publish again,
// Retry on the channel, Retry and Cancel of the job, taking the post off the schedule and deleting it are all
// refused, and the one send is recorded.
func TestNothingStartsASecondSendWhileOneIsRunning(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := t.Context()
	f, ch := scriptedChannelOn(t, s, p, "sendrace1")
	f.started, f.hold = make(chan struct{}, 1), make(chan struct{})
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &[]PostTargetInput{{ChannelID: ch}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	job := jobPublicID(jobsByKey(t, s, "publish_target:"+post.ID+":"+ch)[0].ID)
	done := make(chan struct{})
	go func() { s.RunDueJobsOnce(ctx); close(done) }()
	<-f.started // the platform call is on its way

	refused := func(what string, err error) {
		t.Helper()
		var e *Error
		if !errors.As(err, &e) || e.Status != 409 {
			t.Errorf("%s during a running send: %v, want 409", what, err)
		}
	}
	_, err := s.PublishPost(ctx, p, post.ID)
	refused("Publish", err)
	_, err = s.RetryTarget(ctx, p, post.ID, ch)
	refused("Retry of the target", err)
	_, err = s.RetryJob(ctx, p, job)
	refused("Retry of the job", err)
	_, err = s.CancelJob(ctx, p, job)
	refused("Cancel of the job", err)
	_, err = s.CancelPost(ctx, p, post.ID)
	refused("Taking the post off the schedule", err)
	refused("Deleting the post", s.DeletePost(ctx, p, post.ID))
	_, err = s.UpdatePost(ctx, p, post.ID, PostPatch{Content: ptrTo("changed while it goes out")})
	refused("Editing the post", err)

	close(f.hold)
	<-done
	got, err := s.GetPost(ctx, p, post.ID)
	if err != nil {
		t.Fatalf("the post is gone: %v", err)
	}
	if calls, sent := f.counts(); calls != 1 || sent != 1 {
		t.Fatalf("the platform was asked %d times and took the post %d times, want 1 and 1", calls, sent)
	}
	if tg := targetOf(t, got, ch); tg.Status != "published" || got.Status != "published" || got.Content != "later" {
		t.Fatalf("post %s (%q), target %+v; want published as it was written", got.Status, got.Content, tg)
	}
	if jobs := jobsByKey(t, s, "publish_target:"+post.ID+":"+ch); len(jobs) != 1 {
		t.Fatalf("%d publish jobs for the target, want the one that ran", len(jobs))
	}
}

// A channel turned off or disconnected (needs reconnecting) while its job waited in the queue gets nothing: the
// target fails with the reason and the platform is never asked.
func TestChannelTurnedOffBeforeTheSendGetsNothing(t *testing.T) {
	for status, want := range map[string]string{"disabled": "turned off", "needs_reconnect": "needs reconnecting"} {
		t.Run(status, func(t *testing.T) {
			s, p, post := publishTest(t)
			ctx := t.Context()
			f, ch := scriptedChannelOn(t, s, p, "sendoff"+status[:1])
			if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &[]PostTargetInput{{ChannelID: ch}}}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.ExecContext(ctx, `UPDATE channels SET status = ? WHERE id = ?`, status, ch); err != nil {
				t.Fatal(err)
			}
			s.RunDueJobsOnce(ctx)
			got, _ := s.GetPost(ctx, p, post.ID)
			tg := targetOf(t, got, ch)
			if calls, _ := f.counts(); calls != 0 {
				t.Fatalf("the platform was asked %d times for a channel that is %s", calls, status)
			}
			if tg.Status != "failed" || !strings.Contains(tg.Error, want) || got.Status != "failed" {
				t.Fatalf("target %+v, post %s; want failed saying the channel %s", tg, got.Status, want)
			}
		})
	}
}

// A workspace that became read-only (its plan ended) after a post was scheduled publishes nothing at the
// scheduled time: the platform is not asked, the post fails with the reason and post.failed tells people. The
// same holds for a target already queued when the plan ended.
func TestScheduledPostOnAWorkspaceThatBecameReadOnly(t *testing.T) {
	for _, when := range []string{"before the scheduled time", "between the scheduled time and the send"} {
		t.Run(when, func(t *testing.T) {
			s, p, post := publishTest(t)
			ctx := t.Context()
			advance := movableClock(s)
			f, ch := scriptedChannelOn(t, s, p, "sendro"+when[:2])
			if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &[]PostTargetInput{{ChannelID: ch}}}); err != nil {
				t.Fatal(err)
			}
			s.Cfg.Mode = ModeCloud
			setPlan := func(until time.Time, readOnly bool) {
				t.Helper()
				if _, err := s.W.SetWorkspacePlan(ctx, db.SetWorkspacePlanParams{Plan: "starter", PlanExpiresAt: nullStr(formatTime(until)), UpdatedAt: s.now(), ID: p.WorkspaceID}); err != nil {
					t.Fatal(err)
				}
				if err := s.CheckWritable(ctx, p.WorkspaceID); errors.Is(err, ErrSuspended) != readOnly {
					t.Fatalf("setup: read-only %v, want %v (%v)", !readOnly, readOnly, err)
				}
			}
			setPlan(s.Now().Add(24*time.Hour), false) // paid when the post was scheduled
			advance(2 * time.Hour)
			if when == "before the scheduled time" {
				setPlan(s.Now().Add(-time.Minute), true)
			} else {
				// The scheduled run started the publish; the plan ends before the worker reaches the target.
				if err := s.runPublishPost(ctx, p.WorkspaceID, post.ID); err != nil {
					t.Fatal(err)
				}
				if row, _ := s.Q.GetPostByID(ctx, post.ID); row.Status != "publishing" {
					t.Fatalf("setup: the post is %s, want publishing", row.Status)
				}
				setPlan(s.Now().Add(-time.Minute), true)
			}
			s.RunDueJobsOnce(ctx)

			if calls, _ := f.counts(); calls != 0 {
				t.Fatalf("a read-only workspace asked the platform %d times", calls)
			}
			row, err := s.Q.GetPostByID(ctx, post.ID)
			if err != nil {
				t.Fatal(err)
			}
			failed := eventsOfType(t, s, p, "post.failed")
			if row.Status != "failed" || len(failed) != 1 {
				t.Fatalf("the post is %s with %d post.failed events, want failed and 1", row.Status, len(failed))
			}
			target, _ := s.Q.GetTarget(ctx, db.GetTargetParams{PostID: post.ID, ChannelID: ch})
			if why := "this workspace is read-only"; !strings.Contains(failed[0].data+target.Error, why) {
				t.Fatalf("neither the event (%s) nor the target (%q) says why: %q", failed[0].data, target.Error, why)
			}
			if target.Status == "published" || target.Status == "publishing" {
				t.Fatalf("the target is %s", target.Status)
			}
		})
	}
}

// A thread's reply went out but its result was refused through every attempt: its job failed with the receipt.
// Retry of that job in the queue records the reply from the receipt and goes on with the thread; the platform
// does not get the reply a second time.
func TestRetryOfAReplyJobThatWasSentOnlyRecordsIt(t *testing.T) {
	old := settleRetry
	settleRetry = nil
	t.Cleanup(func() { settleRetry = old })
	s, p, post := publishTest(t)
	ctx := t.Context()
	advance := movableClock(s)
	var got []string
	f := &fakeReplier{Base: providers.NewBase("replyretry", "Reply Retry", providers.AuthNone, providers.Limits{MaxChars: 280, MaxMedia: 4}), replies: &got}
	providers.Register(f, providers.StatusAvailable)
	t.Cleanup(func() { providers.Unregister(f.ID()) })
	e, _ := providers.Get(f.ID())
	ch, err := s.saveChannel(ctx, p, e, "Replies", providers.Credentials{}, providers.Profile{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &[]PostTargetInput{{ChannelID: ch.ID}},
		Thread: &[]ThreadPart{{Content: "part one", DelaySeconds: 3600}, {Content: "part two"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx) // the post is out; its first reply waits an hour
	lift := refuseWrites(t, s, `UPDATE OF replies ON post_targets`)
	for range 4 { // the reply goes out on the first attempt; no attempt can record it
		advance(2 * time.Hour)
		s.RunDueJobsOnce(ctx)
	}
	lift()
	jobs := jobsByKey(t, s, "publish_reply:"+post.ID+":"+ch.ID+":0")
	if len(got) != 1 || len(jobs) != 1 || jobStatus(jobs[0].State) != "failed" {
		t.Fatalf("setup: replies sent %q, jobs %+v; want one reply and one failed job", got, jobs)
	}
	if _, err := s.RetryJob(ctx, p, jobPublicID(jobs[0].ID)); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	pub, _ := s.GetPost(ctx, p, post.ID)
	r := pub.Targets[0].Replies
	if len(got) != 2 || got[0] != "p1 <- part one" || got[1] != "rpart one <- part two" {
		t.Fatalf("the platform got %q; want part one once, then part two under it", got)
	}
	if len(r) != 2 || r[0].Status != "published" || r[0].ExternalID != "rpart one" || r[1].Status != "published" {
		t.Fatalf("replies %+v; want both published, the first with the id of its one send", r)
	}
	// A second run of a reply that is out (a job queued twice) sends nothing.
	if err := s.publishReply(ctx, post.ID, ch.ID, 0, "p1"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("a published reply was sent again: %q", got)
	}
}

// A schedule's run in a read-only workspace publishes nothing and fails nothing: the run is recorded as skipped,
// the waiting post stays a draft and the next run is queued, so the schedule goes on once the plan is paid.
func TestScheduleRunInAReadOnlyWorkspaceWaits(t *testing.T) {
	s, p, scheduled := publishTest(t)
	ctx := t.Context()
	advance := movableClock(s)
	f, ch := scriptedChannelOn(t, s, p, "schedro")
	post, err := s.CreatePost(ctx, p, PostInput{Title: "Waiting", Collection: scheduled.CollectionID, Content: "for the schedule", Status: "draft",
		Targets: []PostTargetInput{{ChannelID: ch}}}) // a draft without a time of its own: the schedule's to take
	if err != nil {
		t.Fatal(err)
	}
	s.Cfg.Mode = ModeCloud
	setPlan := func(until time.Time, readOnly bool) {
		t.Helper()
		if _, err := s.W.SetWorkspacePlan(ctx, db.SetWorkspacePlanParams{Plan: "starter", PlanExpiresAt: nullStr(formatTime(until)), UpdatedAt: s.now(), ID: p.WorkspaceID}); err != nil {
			t.Fatal(err)
		}
		if err := s.CheckWritable(ctx, p.WorkspaceID); errors.Is(err, ErrSuspended) != readOnly {
			t.Fatalf("setup: read-only %v, want %v (%v)", !readOnly, readOnly, err)
		}
	}
	setPlan(s.Now().Add(24*time.Hour), false)
	sc, err := s.CreateSchedule(ctx, p, ScheduleInput{Collection: &post.CollectionID, Name: ptrTo("Hourly"), Kind: ptrTo("recurring"),
		Rule: &ScheduleRule{EveryMinutes: 60}})
	if err != nil {
		t.Fatal(err)
	}
	setPlan(s.Now().Add(-time.Minute), true)
	advance(61 * time.Minute)
	s.RunDueJobsOnce(ctx)

	got, _ := s.GetPost(ctx, p, post.ID)
	ran, _ := s.GetSchedule(ctx, p, sc.ID)
	failed := 0 // publishTest's own scheduled post fails at its time, as it should; this is about the schedule's draft
	for _, e := range eventsOfType(t, s, p, "post.failed") {
		if e.item == post.ID {
			failed++
		}
	}
	if calls, _ := f.counts(); calls != 0 || got.Status != "draft" || failed != 0 {
		t.Fatalf("in a read-only workspace the platform was asked %d times and the post is %s (%d post.failed events); want 0, draft, 0",
			calls, got.Status, failed)
	}
	if !ran.Enabled || !strings.Contains(ran.LastResult, "plan has ended") || ran.NextRunAt == nil {
		t.Fatalf("the schedule after its run: %+v; want enabled, skipped for the plan, with a next run", ran)
	}
	setPlan(s.Now().Add(24*time.Hour), false) // paid again
	advance(61 * time.Minute)
	s.RunDueJobsOnce(ctx)
	got, _ = s.GetPost(ctx, p, post.ID)
	if _, sent := f.counts(); sent != 1 || got.Status != "published" {
		ran, _ = s.GetSchedule(ctx, p, sc.ID)
		t.Fatalf("after the plan was paid the post is %s and went out %d times; want published, once (the schedule's last run: %q)", got.Status, sent, ran.LastResult)
	}
}
