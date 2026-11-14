package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

func TestPublishingCapacityIsSharedPersistentAndAtomic(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if err := s.reservePublishingCapacity(ctx, p.WorkspaceID, "linkedin_api_calls", 100, time.UTC); err == nil {
				accepted.Add(1)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 10 {
		t.Fatalf("one workspace accepted %d calls, want 10", accepted.Load())
	}
	// A different Service (another worker/restart) reads the same reservation, not an in-memory counter.
	other := New(s.DB, s.Log, s.Cfg)
	other.Now = s.Now
	if err := other.reservePublishingCapacity(ctx, p.WorkspaceID, "linkedin_api_calls", 100, time.UTC); err == nil {
		t.Fatal("another worker bypassed the workspace share")
	}
	for i := range 10 {
		id := fmt.Sprintf("capacity_%d", i)
		if err := s.W.CreateWorkspace(ctx, db.CreateWorkspaceParams{ID: id, Name: id, RecoveryVerifier: id, RecoveryRotatedAt: s.now(), CreatedAt: s.now(), UpdatedAt: s.now()}); err != nil {
			t.Fatal(err)
		}
		for range 10 {
			err := s.reservePublishingCapacity(ctx, id, "linkedin_api_calls", 100, time.UTC)
			if (i < 9) != (err == nil) {
				t.Fatalf("global pool, workspace %d: %v", i, err)
			}
		}
	}
	now = now.Add(24 * time.Hour)
	if err := s.reservePublishingCapacity(ctx, p.WorkspaceID, "linkedin_api_calls", 100, time.UTC); err != nil {
		t.Fatalf("next day: %v", err)
	}
}

type capacityRoundTripper struct{ calls int }

func (r *capacityRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls++
	return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
}

func TestPublishingCapacityStopsHTTPAndUsesPacificReset(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := context.Background()
	s.Cfg.Mode = ModeCloud
	s.Cfg.YouTubeDailyUploads = 10                       // one upload per workspace
	now := time.Date(2026, 11, 1, 7, 30, 0, 0, time.UTC) // 00:30 PDT, before the DST transition
	s.Now = func() time.Time { return now }
	next := &capacityRoundTripper{}
	tr := &publishingCapacityTransport{s: s, workspaceID: p.WorkspaceID, next: next}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://www.googleapis.com/upload/youtube/v3/videos?uploadType=resumable", nil)
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	_, err := tr.RoundTrip(req)
	var failure *providers.Failure
	if !errors.As(err, &failure) || !failure.RetryAt.Equal(time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)) || next.calls != 1 {
		t.Fatalf("blocked before HTTP, DST reset: %v, calls %d", err, next.calls)
	}
	// The reset must survive the provider and http.Client wrappers so the queue snoozes
	// instead of spending its attempts and failing the post before tomorrow's capacity.
	youtube, _ := providers.Get("youtube")
	_, err = youtube.Publish(providers.WithHTTPClient(ctx, &http.Client{Transport: tr}),
		providers.Credentials{"access_token": "test", "channel_id": "test"},
		providers.PostContent{Media: []providers.MediaFile{{Size: 1, MimeType: "video/mp4"}}},
		providers.Settings{"title": "Capacity test"})
	if !providers.RetryAt(err).Equal(failure.RetryAt) || next.calls != 1 {
		t.Fatalf("YouTube lost capacity retry time: %v, calls %d", err, next.calls)
	}
	now = failure.RetryAt
	if _, err := tr.RoundTrip(req); err != nil || next.calls != 2 {
		t.Fatalf("Pacific midnight reset: %v, calls %d", err, next.calls)
	}
}

func TestXStatisticsStopsHTTPAndResetsMonthly(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := context.Background()
	s.Cfg.Mode = ModeCloud
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	if _, err := s.W.SetWorkspacePlan(ctx, db.SetWorkspacePlanParams{Plan: "starter", UpdatedAt: s.now(), ID: p.WorkspaceID}); err != nil {
		t.Fatal(err)
	}
	next := &capacityRoundTripper{}
	tr := &publishingCapacityTransport{s: s, workspaceID: p.WorkspaceID, next: next}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.x.com/2/tweets/123?tweet.fields=public_metrics", nil)
	for range 100 {
		if _, err := tr.RoundTrip(req); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tr.RoundTrip(req); err == nil || next.calls != 100 {
		t.Fatalf("statistics allowance did not block HTTP: %v, calls %d", err, next.calls)
	}
	now = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if _, err := tr.RoundTrip(req); err != nil || next.calls != 101 {
		t.Fatalf("monthly reset: %v, calls %d", err, next.calls)
	}
}

func TestSuspendedWorkspaceCannotRefreshMetrics(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := context.Background()
	s.Cfg.Mode = ModeCloud
	past := formatTime(s.Now().Add(-24 * time.Hour))
	if _, err := s.W.SetWorkspacePlan(ctx, db.SetWorkspacePlanParams{Plan: "starter", PlanExpiresAt: nullStr(past), UpdatedAt: s.now(), ID: p.WorkspaceID}); err != nil {
		t.Fatal(err)
	}
	// A nil reader would panic if the paid provider path were reached.
	if _, err := s.readMetrics(ctx, p.WorkspaceID, "unused", providers.Entry{}, nil, "123"); !errors.Is(err, ErrSuspended) {
		t.Fatalf("expected suspension before provider call, got %v", err)
	}
}

func TestAssistantReservationUsesEndpointPriceAndContext(t *testing.T) {
	s := &Service{}
	msgs := []AssistantMessage{{Role: "user", Content: "Write a short post"}}
	cheap := s.assistantEstimate(AIEndpoint{Model: "gpt-6-luna"}, msgs, "system", nil)
	expensive := s.assistantEstimate(AIEndpoint{Model: "gpt-6-astra"}, msgs, "system", nil)
	if cheap <= 0 || expensive < 100_000 || expensive <= cheap {
		t.Fatalf("endpoint reservations cheap=%d expensive=%d", cheap, expensive)
	}
	if more := s.assistantEstimate(AIEndpoint{Model: "gpt-6-astra"}, append(msgs, msgs...), "system", nil); more <= expensive {
		t.Fatal("longer context did not increase the reservation")
	}
}
