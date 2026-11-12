package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func queuedScheduleJobs(t *testing.T, s *Service, p Principal) []Job {
	t.Helper()
	page, err := s.ListJobs(context.Background(), p, JobFilter{Kind: "run_schedule", Status: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	return page.Data
}

// Someone cancels the queued run of a recurring schedule (Settings > Queue) to skip it: that run is skipped,
// the one after it is queued, and the schedule keeps going.
func TestCancelledScheduleRunKeepsTheScheduleGoing(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	sc, err := s.CreateSchedule(ctx, p, ScheduleInput{Collection: &post.CollectionID, Name: ptrTo("Every 10"), Kind: ptrTo("recurring"),
		Rule: &ScheduleRule{EveryMinutes: 10}})
	if err != nil {
		t.Fatal(err)
	}
	jobs := queuedScheduleJobs(t, s, p)
	if len(jobs) != 1 {
		t.Fatalf("setup: queued run_schedule jobs %d", len(jobs))
	}
	if _, err := s.CancelJob(ctx, p, jobs[0].ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSchedule(ctx, p, sc.ID)
	if next := queuedScheduleJobs(t, s, p); len(next) != 1 || got.NextRunAt == nil || *got.NextRunAt <= *sc.NextRunAt {
		t.Fatalf("after the cancel: next_run_at %v (was %v), queued runs %+v; want the run after the skipped one", got.NextRunAt, sc.NextRunAt, next)
	}
	s.RunDueJobsOnce(ctx)
	if got, _ = s.GetSchedule(ctx, p, sc.ID); got.LastRunAt != nil {
		t.Fatalf("the skipped run ran at %s", *got.LastRunAt)
	}
	later := time.Now().Add(25 * time.Minute)
	s.Now = func() time.Time { return later }
	s.RunDueJobsOnce(ctx)
	if got, _ = s.GetSchedule(ctx, p, sc.ID); !got.Enabled || got.LastRunAt == nil || len(queuedScheduleJobs(t, s, p)) != 1 {
		t.Fatalf("the schedule stopped: enabled %v, last run %v, queued runs %d", got.Enabled, got.LastRunAt, len(queuedScheduleJobs(t, s, p)))
	}
}

// Cancelling the run of a one-time schedule turns it off: it has no next run to queue.
func TestCancelledOneTimeScheduleRunTurnsItOff(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	at := s.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	sc, err := s.CreateSchedule(ctx, p, ScheduleInput{Collection: &post.CollectionID, Name: ptrTo("Once"), Kind: ptrTo("once"), RunAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelJob(ctx, p, queuedScheduleJobs(t, s, p)[0].ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetSchedule(ctx, p, sc.ID); got.Enabled || got.NextRunAt != nil || len(queuedScheduleJobs(t, s, p)) != 0 {
		t.Fatalf("after the cancel: enabled %v, next %v", got.Enabled, got.NextRunAt)
	}
}

// "Run now" finishes while the automatic run waits on the schedule's endpoint. The automatic run still records
// itself and queues the next one.
func TestRunNowDuringAutomaticRunKeepsTheScheduleGoing(t *testing.T) {
	s, p, post := publishTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s.Cfg.Webhook = WebhookConfig{AllowPrivate: true, AllowLoopback: true}
	var calls atomic.Int32
	started, resume := make(chan struct{}), make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 { // the automatic run waits; the manual one (second call) answers at once
			close(started)
			select {
			case <-resume:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[]}`))
	}))
	defer endpoint.Close()
	col, err := s.CreateCollection(ctx, p, CollectionInput{Name: ptrTo("Endpoint")})
	if err != nil {
		t.Fatal(err)
	}
	sc, err := s.CreateSchedule(ctx, p, ScheduleInput{Collection: &col.ID, Name: ptrTo("Fetch"), Kind: ptrTo("recurring"),
		Rule: &ScheduleRule{EveryMinutes: 10}, Source: ptrTo("endpoint"), EndpointURL: &endpoint.URL,
		Channels: &[]string{post.Targets[0].ChannelID}})
	if err != nil {
		t.Fatal(err)
	}
	var now atomic.Int64
	now.Store(time.Now().Add(11 * time.Minute).UnixNano())
	s.Now = func() time.Time { return time.Unix(0, now.Load()) }
	done := make(chan struct{})
	go func() { s.RunDueJobsOnce(ctx); close(done) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("the automatic run did not call the endpoint")
	}
	now.Add(int64(time.Second))
	if _, err := s.RunScheduleNow(ctx, p, sc.ID); err != nil {
		t.Fatal(err)
	}
	close(resume)
	<-done
	got, err := s.GetSchedule(ctx, p, sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || !got.Enabled {
		t.Fatalf("setup: endpoint calls %d, enabled %v", calls.Load(), got.Enabled)
	}
	if jobs := queuedScheduleJobs(t, s, p); len(jobs) != 1 || got.NextRunAt == nil || *got.NextRunAt <= formatTime(s.Now()) {
		t.Fatalf("after the overlap the enabled schedule has %d queued runs (next_run_at %v, now %s): nothing will run it again",
			len(jobs), got.NextRunAt, formatTime(s.Now()))
	}
}

// A run fails and the record of that failure is refused by the database too: the schedule is left enabled with a
// run time in the past and no job. The minute sweep queues it again.
func TestSweepQueuesAScheduleThatLostItsRun(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	sc, err := s.CreateSchedule(ctx, p, ScheduleInput{Collection: &post.CollectionID, Name: ptrTo("Every 10"), Kind: ptrTo("recurring"),
		Rule: &ScheduleRule{EveryMinutes: 10}})
	if err != nil {
		t.Fatal(err)
	}
	lift := refuseWrites(t, s, `UPDATE ON schedules`)
	later := time.Now().Add(11 * time.Minute)
	s.Now = func() time.Time { return later }
	s.RunDueJobsOnce(ctx) // the run can't record itself, and scheduleFailed can't either
	lift()
	if n := len(queuedScheduleJobs(t, s, p)); n != 0 {
		t.Fatalf("setup: %d queued runs, want the schedule left without one", n)
	}
	if err := s.scheduleFailed(ctx, sc.ID, errors.New("x")); err != nil { // the write works again: its error is no longer dropped
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`DELETE FROM river_job WHERE kind = 'run_schedule'`); err != nil { // lost again
		t.Fatal(err)
	}
	if n := s.sweepSchedules(ctx); n != 0 {
		t.Fatalf("swept %d: a run that is not overdue yet is left alone", n)
	}
	later = later.Add(20 * time.Minute)
	if n := s.sweepSchedules(ctx); n != 1 {
		t.Fatalf("swept %d, want the lost run queued", n)
	}
	if n := s.sweepSchedules(ctx); n != 0 {
		t.Fatalf("swept %d again: its run is queued", n)
	}
	s.RunDueJobsOnce(ctx)
	if got, _ := s.GetSchedule(ctx, p, sc.ID); got.LastRunAt == nil || *got.LastRunAt != formatTime(later) || len(queuedScheduleJobs(t, s, p)) != 1 {
		t.Fatalf("after the sweep: last run %v, queued runs %d", got.LastRunAt, len(queuedScheduleJobs(t, s, p)))
	}
}
