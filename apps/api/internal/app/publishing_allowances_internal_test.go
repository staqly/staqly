package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

type allowanceProvider struct {
	providers.Base
	mu      sync.Mutex
	calls   int
	errs    []error
	entered chan struct{}
	release chan struct{}
}

func (f *allowanceProvider) Publish(ctx context.Context, _ providers.Credentials, post providers.PostContent, _ providers.Settings) (providers.Result, error) {
	f.mu.Lock()
	f.calls++
	var err error
	if len(f.errs) > 0 {
		err, f.errs = f.errs[0], f.errs[1:]
	}
	f.mu.Unlock()
	if f.entered != nil {
		f.entered <- struct{}{}
		select {
		case <-f.release:
		case <-ctx.Done():
			return providers.Result{}, ctx.Err()
		}
	}
	if err != nil {
		return providers.Result{}, err
	}
	return providers.Result{ExternalID: post.PostID, URL: "https://example.com/" + post.PostID}, nil
}

func allowanceFixture(t *testing.T, provider string, f *allowanceProvider) (*Service, Principal, []Post, string, int64) {
	t.Helper()
	s, p, initial := publishTest(t)
	ctx := context.Background()
	original, ok := providers.Get(provider)
	if !ok {
		t.Fatalf("missing provider %s", provider)
	}
	f.Base = providers.NewBase(provider, provider, providers.AuthNone, providers.Limits{MaxChars: 280})
	providers.Register(f, providers.StatusAvailable)
	t.Cleanup(func() { providers.Register(original.Provider, original.Status) })
	entry, _ := providers.Get(provider)
	channel, err := s.saveChannel(ctx, p, entry, "Allowance", providers.Credentials{}, providers.Profile{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	var posts []Post
	for range 2 {
		post, err := s.CreatePost(ctx, p, PostInput{Title: "Allowance", Collection: initial.CollectionID, Content: "One post",
			Targets: []PostTargetInput{{ChannelID: channel.ID}}})
		if err != nil {
			t.Fatal(err)
		}
		post, err = s.PublishPost(ctx, p, post.ID)
		if err != nil {
			t.Fatal(err)
		}
		posts = append(posts, post)
	}
	s.Cfg.Mode = ModeCloud
	if _, err := s.W.SetWorkspacePlan(ctx, db.SetWorkspacePlanParams{Plan: "starter", UpdatedAt: s.now(), ID: p.WorkspaceID}); err != nil {
		t.Fatal(err)
	}
	limit := int64(50)
	metric := ProviderX
	if isLinkedIn(provider) {
		limit, metric = 100, MeterLinkedIn
	}
	if _, err := s.W.InsertUsageEvent(ctx, db.InsertUsageEventParams{WorkspaceID: p.WorkspaceID, Provider: metric, Ref: "earlier",
		Period: s.period(), Units: limit - 1, Included: limit - 1, CreatedAt: s.now()}); err != nil {
		t.Fatal(err)
	}
	return s, p, posts, metric, limit
}

func TestPublishingAllowanceConcurrentWorkers(t *testing.T) {
	for _, provider := range []string{"x", "linkedin", "linkedin_page"} {
		t.Run(provider, func(t *testing.T) {
			f := &allowanceProvider{entered: make(chan struct{}, 2), release: make(chan struct{})}
			s, p, posts, metric, limit := allowanceFixture(t, provider, f)
			// Separate services model separate worker processes sharing only persistent state.
			other := New(s.DB, s.Log, s.Cfg)
			other.Now = s.Now
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			first, second := make(chan error, 1), make(chan error, 1)
			channelID := posts[0].Targets[0].ChannelID
			go func() { first <- s.publishTarget(ctx, posts[0].ID, channelID) }()
			select {
			case <-f.entered:
			case <-ctx.Done():
				t.Fatal("first provider call did not start")
			}
			go func() { second <- other.publishTarget(ctx, posts[1].ID, channelID) }()
			var secondErr error
			secondFinished := false
			select {
			case secondErr = <-second:
				secondFinished = true
			case <-f.entered: // Before the fix both calls spent the last remaining allowance.
			case <-ctx.Done():
				t.Fatal("second worker did not decide while the first provider call was blocked")
			}
			close(f.release)
			if err := <-first; err != nil {
				t.Fatal(err)
			}
			if !secondFinished {
				secondErr = <-second
			}
			if secondErr != nil {
				t.Fatal(secondErr)
			}
			firstPost, err := s.GetPost(ctx, p, posts[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			secondPost, err := s.GetPost(ctx, p, posts[1].ID)
			if err != nil {
				t.Fatal(err)
			}
			used, err := s.Q.SumUnitsUsage(ctx, db.SumUnitsUsageParams{WorkspaceID: p.WorkspaceID, Provider: metric, Period: s.period()})
			if err != nil {
				t.Fatal(err)
			}
			if firstPost.Status != "published" || secondPost.Status != "failed" || f.calls != 1 || used != limit {
				t.Fatalf("statuses %s/%s, provider calls %d, usage %d; want published/failed, 1 call, usage %d",
					firstPost.Status, secondPost.Status, f.calls, used, limit)
			}
		})
	}
}

func TestPublishingAllowanceRefundsDefiniteRejection(t *testing.T) {
	for _, provider := range []string{"x", "linkedin"} {
		t.Run(provider, func(t *testing.T) {
			f := &allowanceProvider{errs: []error{&providers.Failure{Kind: providers.ErrRejected, Msg: "invalid post"}}}
			s, p, posts, metric, limit := allowanceFixture(t, provider, f)
			ctx := context.Background()
			channelID := posts[0].Targets[0].ChannelID
			if err := s.publishTarget(ctx, posts[0].ID, channelID); err != nil {
				t.Fatal(err)
			}
			used, err := s.Q.SumUnitsUsage(ctx, db.SumUnitsUsageParams{WorkspaceID: p.WorkspaceID, Provider: metric, Period: s.period()})
			if err != nil || used != limit-1 {
				t.Fatalf("definite rejection consumed usage: %d, %v", used, err)
			}
			if err := s.publishTarget(ctx, posts[1].ID, channelID); err != nil {
				t.Fatal(err)
			}
			got, err := s.GetPost(ctx, p, posts[1].ID)
			if err != nil || got.Status != "published" || f.calls != 2 {
				t.Fatalf("refunded allowance unavailable: status %s, calls %d, %v", got.Status, f.calls, err)
			}
		})
	}
}

func TestPublishingAllowanceRetryKeepsOneCharge(t *testing.T) {
	for _, provider := range []string{"x", "linkedin"} {
		t.Run(provider, func(t *testing.T) {
			ambiguous := errors.New("connection lost after upload")
			f := &allowanceProvider{errs: []error{ambiguous}}
			s, p, posts, metric, limit := allowanceFixture(t, provider, f)
			ctx := context.Background()
			channelID := posts[0].Targets[0].ChannelID
			if err := s.publishTarget(ctx, posts[0].ID, channelID); !errors.Is(err, ambiguous) {
				t.Fatalf("first attempt: %v", err)
			}
			used, err := s.Q.SumUnitsUsage(ctx, db.SumUnitsUsageParams{WorkspaceID: p.WorkspaceID, Provider: metric, Period: s.period()})
			if err != nil || used != limit {
				t.Fatalf("ambiguous attempt lost its reservation: usage %d, want %d, %v", used, limit, err)
			}
			// A restarted worker in the next month must honor the original reservation.
			other := New(s.DB, s.Log, s.Cfg)
			nextMonth := s.monthEnd().Add(time.Minute)
			other.Now = func() time.Time { return nextMonth }
			if err := other.publishTarget(ctx, posts[0].ID, channelID); err != nil {
				t.Fatal(err)
			}
			got, err := s.GetPost(ctx, p, posts[0].ID)
			if err != nil || got.Status != "published" || f.calls != 2 {
				t.Fatalf("retry: status %s, calls %d, %v", got.Status, f.calls, err)
			}
			used, err = s.Q.SumUnitsUsage(ctx, db.SumUnitsUsageParams{WorkspaceID: p.WorkspaceID, Provider: metric, Period: s.period()})
			if err != nil || used != limit {
				t.Fatalf("retry charged twice: usage %d, want %d, %v", used, limit, err)
			}
			used, err = s.Q.SumUnitsUsage(ctx, db.SumUnitsUsageParams{WorkspaceID: p.WorkspaceID, Provider: metric, Period: other.period()})
			if err != nil || used != 0 {
				t.Fatalf("retry moved its charge into the new month: usage %d, %v", used, err)
			}
		})
	}
}

func TestPublishingAllowanceCancelledScheduleDoesNotCharge(t *testing.T) {
	f := &allowanceProvider{}
	s, p, posts, metric, limit := allowanceFixture(t, "x", f)
	ctx := context.Background()
	at := s.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	post, err := s.CreatePost(ctx, p, PostInput{Title: "Cancelled", Collection: posts[0].CollectionID, Content: "Cancel me",
		Status: "scheduled", PublishAt: &at, Targets: []PostTargetInput{{ChannelID: posts[0].Targets[0].ChannelID}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	// A stale queue job must see cancellation before it can reserve or call the provider.
	if err := s.runPublishPost(ctx, p.WorkspaceID, post.ID); err != nil {
		t.Fatal(err)
	}
	used, err := s.Q.SumUnitsUsage(ctx, db.SumUnitsUsageParams{WorkspaceID: p.WorkspaceID, Provider: metric, Period: s.period()})
	if err != nil || used != limit-1 || f.calls != 0 {
		t.Fatalf("cancelled schedule consumed allowance: usage %d, calls %d, %v", used, f.calls, err)
	}
}

func TestPublishingAllowanceLaterRejectionKeepsAmbiguousCharge(t *testing.T) {
	f := &allowanceProvider{errs: []error{errors.New("lost response"), &providers.Failure{Kind: providers.ErrRejected, Msg: "duplicate"}}}
	s, p, posts, metric, limit := allowanceFixture(t, "x", f)
	ctx := context.Background()
	channelID := posts[0].Targets[0].ChannelID
	if err := s.publishTarget(ctx, posts[0].ID, channelID); err == nil {
		t.Fatal("expected ambiguous first outcome")
	}
	if err := s.publishTarget(ctx, posts[0].ID, channelID); err != nil {
		t.Fatal(err)
	}
	used, err := s.Q.SumUnitsUsage(ctx, db.SumUnitsUsageParams{WorkspaceID: p.WorkspaceID, Provider: metric, Period: s.period()})
	if err != nil || used != limit {
		t.Fatalf("later rejection refunded an earlier ambiguous publication: usage %d, want %d, %v", used, limit, err)
	}
}

func TestPublishingAllowanceRefundsPurchasedCredits(t *testing.T) {
	f := &allowanceProvider{errs: []error{&providers.Failure{Kind: providers.ErrRejected, Msg: "invalid post"}}}
	s, p, posts, _, _ := allowanceFixture(t, "x", f)
	ctx := context.Background()
	if _, err := s.W.InsertUsageEvent(ctx, db.InsertUsageEventParams{WorkspaceID: p.WorkspaceID, Provider: ProviderX, Ref: "last-included",
		Period: s.period(), Units: 1, Included: 1, CreatedAt: s.now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.W.InsertCreditGrant(ctx, db.InsertCreditGrantParams{ID: "purchased", WorkspaceID: p.WorkspaceID, Provider: ProviderX,
		Units: 2, Source: "test-purchase", ExpiresAt: formatTime(s.monthEnd()), CreatedAt: s.now()}); err != nil {
		t.Fatal(err)
	}
	channelID := posts[0].Targets[0].ChannelID
	for i, post := range posts {
		if err := s.publishTarget(ctx, post.ID, channelID); err != nil {
			t.Fatal(err)
		}
		x, err := s.xPosts(ctx, s.Q, p.WorkspaceID)
		if err != nil || x.CreditsLeft != int64(2-i) || x.IncludedLeft != 0 {
			t.Fatalf("attempt %d: purchased credits not preserved/accounted: %+v, %v", i, x, err)
		}
	}
}

func TestPublishingAllowanceRetryCannotIncreaseReservedCost(t *testing.T) {
	for _, provider := range []string{"x", "linkedin"} {
		t.Run(provider, func(t *testing.T) {
			s, p, posts, metric, limit := allowanceFixture(t, provider, &allowanceProvider{})
			ctx := context.Background()
			ref := "post:" + posts[0].ID + ":" + posts[0].Targets[0].ChannelID
			if fresh, err := s.reservePublishingAllowance(ctx, p.WorkspaceID, ref, provider, 1, false); err != nil || !fresh {
				t.Fatalf("initial reservation: fresh %v, %v", fresh, err)
			}
			// An edited retry must not turn a plain post into an unreserved live-link post or video.
			units, video := int64(XLinkPostWeight), false
			if provider == "linkedin" {
				units, video = 1, true
			}
			_, err := s.reservePublishingAllowance(ctx, p.WorkspaceID, ref, provider, units, video)
			var conflict *Error
			if !errors.As(err, &conflict) || conflict.Status != 409 {
				t.Fatalf("increased cost reused original reservation: %v", err)
			}
			used, err := s.Q.SumUnitsUsage(ctx, db.SumUnitsUsageParams{WorkspaceID: p.WorkspaceID, Provider: metric, Period: s.period()})
			if err != nil || used != limit {
				t.Fatalf("refusing edited retry changed original charge: %d, %v", used, err)
			}
		})
	}
}
