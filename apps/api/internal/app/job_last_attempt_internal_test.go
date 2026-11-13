package app

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// lastAttempt says, for every job kind, what its last failed attempt leaves behind (jobFailed): "settles" kinds
// are checked below, the others say why nothing needs settling. A new kind needs a line; the test fails until
// it has one.
var lastAttempt = map[string]string{
	"publish_post":    "settles",
	"publish_target":  "settles",
	"publish_reply":   "settles",
	"remove_target":   "settles",
	"run_schedule":    "settles",
	"repeat_post":     "settles",
	"refresh_channel": "settles",
	"target_timeout":  "settles",
	"ai_run":          "settles elsewhere: the request fails and its credits come back (the AI tests)",

	"repost":            "nothing: a repost that never went through is logged; the post is out",
	"notify_people":     "nothing: the event stays in the activity log",
	"notify_person":     "nothing: the event stays in the activity log",
	"sync_seats":        "nothing: it never reaches its last failure before a week of hourly tries (workJob)",
	"sync_entitlements": "nothing: it never reaches its last failure before a week of hourly tries (workJob)",
	"sync_billing":      "nothing: the next Stripe event or the reconcile sweep reads the subscription again",
	"read_responses":    "nothing: the next read is queued by the schedule of reads",
	"check_target":      "nothing: the next check looks again",
	"analyze_responses": "nothing: the analysis is asked for again with the next responses",
	"sync_events":       "nothing: the next sync reads the platform again",
	"welcome_email":     "nothing: an email that could not be sent is not sent late",
}

func TestEveryJobKindSaysWhatItsLastFailureLeaves(t *testing.T) {
	for _, kind := range jobKinds {
		if lastAttempt[kind] == "" {
			t.Errorf("job kind %s: say in lastAttempt what its last failed attempt leaves behind (and settle it in jobFailed if something would wait for good)", kind)
		}
	}
	for kind := range lastAttempt {
		if !slices.Contains(jobKinds, kind) {
			t.Errorf("lastAttempt names %s, which is not a job kind any more", kind)
		}
	}
	boom := errors.New("boom: the database is locked")

	// The kinds with nothing to settle change nothing: no event, no job, the post as it was.
	t.Run("nothing to settle", func(t *testing.T) {
		s, p, post := publishTest(t)
		ctx := t.Context()
		state := func() (events, jobs int, status string) {
			t.Helper()
			if err := s.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE workspace_id = ?`, p.WorkspaceID).Scan(&events); err != nil {
				t.Fatal(err)
			}
			if err := s.DB.QueryRow(`SELECT COUNT(*) FROM river_job`).Scan(&jobs); err != nil {
				t.Fatal(err)
			}
			row, err := s.Q.GetPostByID(ctx, post.ID)
			if err != nil {
				t.Fatal(err)
			}
			return events, jobs, row.Status + " " + row.UpdatedAt
		}
		events, jobs, status := state()
		for kind, what := range lastAttempt {
			if !strings.HasPrefix(what, "nothing: ") {
				continue
			}
			a := jobArgs{WorkspaceID: p.WorkspaceID, Payload: jobPayload{PostID: post.ID, ChannelID: post.Targets[0].ChannelID}}
			if err := s.jobFailed(ctx, kind, a, boom); err != nil {
				t.Errorf("%s: %v", kind, err)
			}
			if e, j, st := state(); e != events || j != jobs || st != status {
				t.Errorf("%s is declared to settle nothing, yet events %d -> %d, jobs %d -> %d, post %q -> %q", kind, events, e, jobs, j, status, st)
			}
		}
	})

	settles := map[string]func(t *testing.T, s *Service, p Principal, post Post){
		// The target and its post fail with the cause; people are told once.
		"publish_target": func(t *testing.T, s *Service, p Principal, post Post) {
			ctx := t.Context()
			ch := post.Targets[0].ChannelID
			if err := s.tx(ctx, func(q *db.Queries) error { return s.startPublish(ctx, q, p, p.WorkspaceID, post.ID) }); err != nil {
				t.Fatal(err)
			}
			a := jobArgs{WorkspaceID: p.WorkspaceID, Payload: jobPayload{PostID: post.ID, ChannelID: ch}}
			for range 2 { // settling twice (the rescue sweep after a worker) changes nothing more
				if err := s.jobFailed(ctx, "publish_target", a, boom); err != nil {
					t.Fatal(err)
				}
			}
			got, _ := s.GetPost(ctx, p, post.ID)
			if tg := got.Targets[0]; tg.Status != "failed" || !strings.Contains(tg.Error, "boom") || got.Status != "failed" {
				t.Fatalf("target %+v, post %s; want both failed with the cause", tg, got.Status)
			}
			if n, m := len(eventsOfType(t, s, p, "post.target_failed")), len(eventsOfType(t, s, p, "post.failed")); n != 1 || m != 1 {
				t.Fatalf("%d post.target_failed and %d post.failed events, want 1 and 1", n, m)
			}
		},
		// A scheduled post whose time came fails (one whose time is still ahead is left alone).
		"publish_post": func(t *testing.T, s *Service, p Principal, post Post) {
			ctx := t.Context()
			a := jobArgs{WorkspaceID: p.WorkspaceID, Payload: jobPayload{PostID: post.ID}}
			if err := s.jobFailed(ctx, "publish_post", a, boom); err != nil {
				t.Fatal(err)
			}
			if got, _ := s.GetPost(ctx, p, post.ID); got.Status != "scheduled" {
				t.Fatalf("a post whose time is ahead is %s, want scheduled", got.Status)
			}
			later := time.Now().Add(2 * time.Hour)
			s.Now = func() time.Time { return later }
			if err := s.jobFailed(ctx, "publish_post", a, boom); err != nil {
				t.Fatal(err)
			}
			failed := eventsOfType(t, s, p, "post.failed")
			if got, _ := s.GetPost(ctx, p, post.ID); got.Status != "failed" || len(failed) != 1 || !strings.Contains(failed[0].data, "boom") {
				t.Fatalf("post %s with post.failed events %+v, want failed and one event with the cause", got.Status, failed)
			}
		},
		// The reply fails, the parts after it are skipped, the post stays published.
		"publish_reply": func(t *testing.T, s *Service, p Principal, post Post) {
			ctx := t.Context()
			var sent []string
			f := &fakeReplier{Base: providers.NewBase("lastreply", "Last Reply", providers.AuthNone, providers.Limits{MaxChars: 280, MaxMedia: 4}), replies: &sent}
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
			s.RunDueJobsOnce(ctx)
			a := jobArgs{WorkspaceID: p.WorkspaceID, Payload: jobPayload{PostID: post.ID, ChannelID: ch.ID, Index: 0, ParentID: "p1"}}
			if err := s.jobFailed(ctx, "publish_reply", a, boom); err != nil {
				t.Fatal(err)
			}
			got, _ := s.GetPost(ctx, p, post.ID)
			r := got.Targets[0].Replies
			if got.Status != "published" || len(r) != 2 || r[0].Status != "failed" || !strings.Contains(r[0].Error, "boom") || r[1].Status != "skipped" {
				t.Fatalf("post %s, replies %+v; want published, the first reply failed with the cause, the second skipped", got.Status, r)
			}
			if n := len(eventsOfType(t, s, p, "post.reply_failed")); n != 1 || len(sent) != 0 {
				t.Fatalf("%d post.reply_failed events and %d replies sent, want 1 and 0", n, len(sent))
			}
		},
		// The removal fails with the cause: the post is still on the platform and says so.
		"remove_target": func(t *testing.T, s *Service, p Principal, post Post) {
			ctx := t.Context()
			_, ch := scriptedChannelOn(t, s, p, "lastremove")
			if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &[]PostTargetInput{{ChannelID: ch}}}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
				t.Fatal(err)
			}
			s.RunDueJobsOnce(ctx)
			if _, err := s.DB.ExecContext(ctx, `UPDATE post_targets SET removal = 'removing' WHERE post_id = ? AND channel_id = ?`, post.ID, ch); err != nil {
				t.Fatal(err)
			}
			a := jobArgs{WorkspaceID: p.WorkspaceID, Payload: jobPayload{PostID: post.ID, ChannelID: ch}}
			if err := s.jobFailed(ctx, "remove_target", a, boom); err != nil {
				t.Fatal(err)
			}
			got, _ := s.GetPost(ctx, p, post.ID)
			if tg := got.Targets[0]; tg.Removal != "failed" || !strings.Contains(tg.RemovalError, "boom") || tg.Status != "published" {
				t.Fatalf("target %+v; want the removal failed with the cause and the target still published", tg)
			}
			if n := len(eventsOfType(t, s, p, "post.target_remove_failed")); n != 1 {
				t.Fatalf("%d post.target_remove_failed events, want 1", n)
			}
		},
		// The schedule records the failure and keeps going: its next run is queued.
		"run_schedule": func(t *testing.T, s *Service, p Principal, post Post) {
			ctx := t.Context()
			sc, err := s.CreateSchedule(ctx, p, ScheduleInput{Collection: &post.CollectionID, Name: ptrTo("Hourly"), Kind: ptrTo("recurring"),
				Rule: &ScheduleRule{EveryMinutes: 60}})
			if err != nil {
				t.Fatal(err)
			}
			later := time.Now().Add(61 * time.Minute)
			s.Now = func() time.Time { return later }
			if err := s.jobFailed(ctx, "run_schedule", jobArgs{WorkspaceID: p.WorkspaceID, Payload: jobPayload{ScheduleID: sc.ID}}, boom); err != nil {
				t.Fatal(err)
			}
			got, _ := s.GetSchedule(ctx, p, sc.ID)
			if !got.Enabled || !strings.Contains(got.LastResult, "boom") || got.NextRunAt == nil || *got.NextRunAt != formatTime(later.Add(time.Hour)) {
				t.Fatalf("schedule %+v; want enabled, the cause as its last result and the next run in an hour", got)
			}
			jobs := jobsByKey(t, s, "run_schedule:"+sc.ID)
			if len(jobs) != 1 || jobStatus(jobs[0].State) != "queued" || len(eventsOfType(t, s, p, "schedule.failed")) != 1 {
				t.Fatalf("jobs %+v and %d schedule.failed events, want one queued run and one event", jobs, len(eventsOfType(t, s, p, "schedule.failed")))
			}
		},
		// The chain of repeats ends with an event people can be alerted on.
		"repeat_post": func(t *testing.T, s *Service, p Principal, post Post) {
			ctx := t.Context()
			if err := s.jobFailed(ctx, "repeat_post", jobArgs{WorkspaceID: p.WorkspaceID, Payload: jobPayload{PostID: post.ID}}, boom); err != nil {
				t.Fatal(err)
			}
			stopped := eventsOfType(t, s, p, "post.repeat_stopped")
			if len(stopped) != 1 || stopped[0].item != post.ID || !strings.Contains(stopped[0].data, "boom") {
				t.Fatalf("post.repeat_stopped events %+v, want one for the post with the cause", stopped)
			}
		},
		// Was a bug: a custom target whose timeout job ran out of attempts (its write kept failing) stayed publishing
		// for good. The last failure times the target out, once, and leaves a newer attempt alone.
		"target_timeout": func(t *testing.T, s *Service, p Principal, post Post) {
			ctx := t.Context()
			ch := post.Targets[0].ChannelID
			s.Cfg.ProcessingTimeout = time.Minute
			if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
				t.Fatal(err)
			}
			s.RunDueJobsOnce(ctx) // the target waits for the automation's result
			targetJobs := jobsByKey(t, s, "publish_target:"+post.ID+":"+ch)
			if len(targetJobs) != 1 {
				t.Fatalf("setup: %d publish jobs", len(targetJobs))
			}
			attempt := jobPublicID(targetJobs[0].ID)
			lift := refuseWrites(t, s, `UPDATE ON post_targets WHEN NEW.status = 'failed'`)
			later := time.Now()
			for range 4 { // every attempt of the timeout fails to write
				later = later.Add(time.Hour)
				s.Now = func() time.Time { return later }
				s.RunDueJobsOnce(ctx)
			}
			lift()
			jobs := jobsByKey(t, s, "target_timeout:"+post.ID+":"+ch)
			if got, _ := s.GetPost(ctx, p, post.ID); len(jobs) != 1 || jobStatus(jobs[0].State) != "failed" || got.Targets[0].Status != "publishing" {
				t.Fatalf("setup: timeout jobs %+v, target %s; want one failed job and the target still waiting", jobs, got.Targets[0].Status)
			}
			stale := jobArgs{WorkspaceID: p.WorkspaceID, Payload: jobPayload{PostID: post.ID, ChannelID: ch, AttemptID: "job_0"}}
			if err := s.jobFailed(ctx, "target_timeout", stale, boom); err != nil {
				t.Fatal(err)
			}
			if got, _ := s.GetPost(ctx, p, post.ID); got.Targets[0].Status != "publishing" {
				t.Fatalf("the last failure of an older attempt's timeout settled the target: %s", got.Targets[0].Status)
			}
			a := jobArgs{WorkspaceID: p.WorkspaceID, Payload: jobPayload{PostID: post.ID, ChannelID: ch, AttemptID: attempt}}
			for range 2 { // the database takes the write again; settling twice changes nothing more
				if err := s.jobFailed(ctx, "target_timeout", a, boom); err != nil {
					t.Fatal(err)
				}
			}
			got, _ := s.GetPost(ctx, p, post.ID)
			if tg := got.Targets[0]; tg.Status != "failed" || tg.Error != "No result reported in time" || got.Status != "failed" {
				t.Fatalf("target %+v, post %s; want both failed as by a timeout", tg, got.Status)
			}
			if n, m := len(eventsOfType(t, s, p, "post.target_failed")), len(eventsOfType(t, s, p, "post.failed")); n != 1 || m != 1 {
				t.Fatalf("%d post.target_failed and %d post.failed events, want 1 and 1", n, m)
			}
		},
		// The channel stays connected and the refresh is tried again later (a refused sign-in is handled by the run).
		"refresh_channel": func(t *testing.T, s *Service, p Principal, post Post) {
			ctx := t.Context()
			ch := post.Targets[0].ChannelID
			if err := s.jobFailed(ctx, "refresh_channel", jobArgs{WorkspaceID: p.WorkspaceID, Payload: jobPayload{ChannelID: ch}}, boom); err != nil {
				t.Fatal(err)
			}
			row, _ := s.Q.GetChannelByID(ctx, ch)
			jobs := jobsByKey(t, s, "refresh_channel:"+ch)
			if row.Status != "connected" || len(jobs) != 1 || jobs[0].ScheduledAt.Sub(s.Now().Add(refreshRetryDelay)).Abs() > time.Minute {
				t.Fatalf("channel %s, refresh jobs %+v; want connected and one refresh in %s", row.Status, jobs, refreshRetryDelay)
			}
		},
	}
	for kind, what := range lastAttempt {
		if what != "settles" {
			continue
		}
		check, ok := settles[kind]
		if !ok {
			t.Errorf("%s is declared to settle and has no check here", kind)
			continue
		}
		t.Run(kind, func(t *testing.T) {
			s, p, post := publishTest(t)
			check(t, s, p, post)
		})
	}
}
