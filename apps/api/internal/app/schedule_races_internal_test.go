package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

func TestScheduleFailureDoesNotOverwriteConcurrentDisable(t *testing.T) {
	s, p, post := publishTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sc, err := s.CreateSchedule(ctx, p, ScheduleInput{Collection: &post.CollectionID, Name: ptrTo("Failure race"), Kind: ptrTo("recurring"), Rule: &ScheduleRule{EveryMinutes: 10}})
	if err != nil {
		t.Fatal(err)
	}
	now := s.Now()
	paused, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	s.Now = func() time.Time {
		if first.CompareAndSwap(false, true) {
			close(paused)
			select {
			case <-resume:
			case <-ctx.Done():
			}
		}
		return now
	}
	go func() {
		s.scheduleFailed(ctx, sc.ID, errors.New("endpoint failed"))
		close(done)
	}()
	<-paused
	_, err = s.UpdateSchedule(ctx, p, sc.ID, ScheduleInput{Enabled: ptrTo(false)})
	close(resume)
	<-done
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSchedule(ctx, p, sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.NextRunAt != nil {
		t.Fatalf("failure callback reenabled the disabled schedule: %+v", got)
	}
}

func TestScheduleChangeWhileEndpointRunsWins(t *testing.T) {
	for _, action := range []string{"disable", "reschedule", "delete"} {
		t.Run(action, func(t *testing.T) {
			s, p, post := publishTest(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			s.Cfg.Webhook = WebhookConfig{AllowPrivate: true, AllowLoopback: true}
			now := s.Now()
			s.Now = func() time.Time { return now } // same timestamp: changed values still invalidate the old run
			s.Cfg.Mode = ModeCloud
			if _, err := s.W.SetWorkspacePlan(ctx, db.SetWorkspacePlanParams{Plan: "starter", UpdatedAt: s.now(), ID: p.WorkspaceID}); err != nil {
				t.Fatal(err)
			}
			quotaBefore, err := s.Q.GetUsage(ctx, db.GetUsageParams{WorkspaceID: p.WorkspaceID, Period: s.period(), Metric: LimitPostsPerMonth})
			if err != nil && !isNoRows(err) {
				t.Fatal(err)
			}
			started, resume := make(chan struct{}), make(chan struct{})
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				select {
				case <-resume:
				case <-r.Context().Done():
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"posts":[{"title":"Fetched","content":"From the old schedule"}]}`))
			}))
			defer endpoint.Close()
			col, err := s.CreateCollection(ctx, p, CollectionInput{Name: ptrTo("Endpoint")})
			if err != nil {
				t.Fatal(err)
			}
			sc, err := s.CreateSchedule(ctx, p, ScheduleInput{Collection: &col.ID, Name: ptrTo("Original"), Kind: ptrTo("recurring"),
				Rule: &ScheduleRule{EveryMinutes: 10}, Source: ptrTo("endpoint"), EndpointURL: &endpoint.URL,
				Channels: &[]string{post.Targets[0].ChannelID}})
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- s.runSchedule(ctx, sc.ID) }()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("endpoint not called")
			}
			var changed Schedule
			switch action {
			case "disable":
				changed, err = s.UpdateSchedule(ctx, p, sc.ID, ScheduleInput{Enabled: ptrTo(false)})
			case "reschedule":
				changed, err = s.UpdateSchedule(ctx, p, sc.ID, ScheduleInput{Rule: &ScheduleRule{EveryMinutes: 60}})
			case "delete":
				err = s.DeleteSchedule(ctx, p, sc.ID)
			}
			close(resume)
			if err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if action != "delete" {
				got, err := s.GetSchedule(ctx, p, sc.ID)
				if err != nil {
					t.Fatal(err)
				}
				if got.Enabled != changed.Enabled || (got.NextRunAt == nil) != (changed.NextRunAt == nil) ||
					(got.NextRunAt != nil && *got.NextRunAt != *changed.NextRunAt) {
					t.Errorf("old run overwrote schedule: got enabled=%v next=%v, want enabled=%v next=%v", got.Enabled, got.NextRunAt, changed.Enabled, changed.NextRunAt)
				}
			}
			posts, err := s.ListPosts(ctx, p, PostFilter{CollectionID: col.ID})
			if err != nil {
				t.Fatal(err)
			}
			if len(posts.Data) != 0 {
				t.Errorf("stale endpoint response created %d drafts after the schedule changed", len(posts.Data))
			}
			used, err := s.Q.GetUsage(ctx, db.GetUsageParams{WorkspaceID: p.WorkspaceID, Period: s.period(), Metric: LimitPostsPerMonth})
			if (err != nil && !isNoRows(err)) || used != quotaBefore {
				t.Errorf("stale endpoint response consumed post quota: before=%d after=%d err=%v", quotaBefore, used, err)
			}
			for _, p := range posts.Data {
				if p.Status != "draft" {
					t.Errorf("stale schedule published %s: %s", p.ID, p.Status)
				}
			}
		})
	}
}
