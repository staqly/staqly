package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

var youtubeQuotaZone = func() *time.Location {
	z, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		panic(err) // embedded tzdata includes the provider's reset zone
	}
	return z
}()

// publishingCtx bounds the shared app capacity used by publishing, replies and reposts. The underlying
// client retains its SSRF and redirect guards. Connection/refresh calls use the capacity left in reserve.
func (s *Service) publishingCtx(ctx context.Context, workspaceID string) context.Context {
	ctx = s.providerCtx(ctx)
	if s.Cfg.Mode == ModeLocal {
		return ctx
	}
	client := *s.safeClient()
	client.Transport = &publishingCapacityTransport{s: s, workspaceID: workspaceID, next: client.Transport}
	return providers.WithHTTPClient(ctx, &client)
}

type publishingCapacityTransport struct {
	s           *Service
	workspaceID string
	next        http.RoundTripper
}

func (t *publishingCapacityTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// X charges for each statistics read too; cached reports do not make these calls.
	if r.URL.Hostname() == "api.x.com" && r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/2/tweets/") {
		if err := t.s.tx(r.Context(), func(q *db.Queries) error {
			return t.s.useMonthlyQuota(r.Context(), q, t.workspaceID, "x_statistics_reads", func(l PlanLimits) int64 {
				if l.XPostsPerPeriod == Unlimited {
					return Unlimited
				}
				return 2 * l.XPostsPerPeriod
			})
		}); err != nil {
			return nil, err
		}
	}
	metric, cap, zone := "", int64(0), time.UTC
	switch {
	case r.URL.Hostname() == "api.linkedin.com":
		metric, cap = "linkedin_api_calls", t.s.Cfg.LinkedInDailyAPICalls
		if cap == 0 {
			cap = 400 // Development tier: 500 calls/day; keep 100 for connection and token work.
		}
	case r.URL.Hostname() == "www.googleapis.com" && r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/upload/youtube/v3/videos"):
		metric, cap, zone = "youtube_uploads", t.s.Cfg.YouTubeDailyUploads, youtubeQuotaZone
		if cap == 0 {
			cap = 90 // Default: 100 videos.insert calls/day; leave headroom.
		}
	}
	if metric != "" {
		if err := t.s.reservePublishingCapacity(r.Context(), t.workspaceID, metric, cap, zone); err != nil {
			return nil, err
		}
	}
	return t.next.RoundTrip(r)
}

// Count attempts before HTTP, including failures and retries: providers charge those too. The existing
// counters are persistent, workspace-scoped and updated under the same serializable transaction as the
// aggregate check. One workspace gets at most 10% of the operator's daily publishing pool (minimum one).
func (s *Service) reservePublishingCapacity(ctx context.Context, workspaceID, metric string, cap int64, zone *time.Location) error {
	now := s.Now().In(zone)
	period := now.Format("2006-01-02")
	reset := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, zone)
	return s.tx(ctx, func(q *db.Queries) error {
		used, err := q.GetUsage(ctx, db.GetUsageParams{WorkspaceID: workspaceID, Period: period, Metric: metric})
		if err != nil && !isNoRows(err) {
			return err
		}
		// Operator aggregate only; no other workspace's data is returned to the caller.
		totals, err := q.UsageTotals(ctx, period)
		if err != nil {
			return err
		}
		var total int64
		for _, v := range totals {
			if v.Metric == metric {
				total = v.Total
			}
		}
		if used >= max(1, cap/10) || total >= cap {
			return &providers.Failure{Msg: fmt.Sprintf("Daily publishing capacity for %s is reached; try again after %s", metric, reset.UTC().Format(time.RFC3339)), RetryAt: reset}
		}
		_, err = q.IncrementUsage(ctx, db.IncrementUsageParams{WorkspaceID: workspaceID, Period: period, Metric: metric})
		return err
	})
}
