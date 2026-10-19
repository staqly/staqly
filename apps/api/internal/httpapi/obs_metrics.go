package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/logctx"
	"github.com/staqly/staqly/apps/api/internal/metrics"
)

// (status labels are closed enums, so no cap is needed)
//// collection, so the long tail is folded into "other".

func (o *obs) metrics(w http.ResponseWriter, r *http.Request) {
	if !o.metricsAllowed(r) {
		writeErr(w, r, app.ErrNotFound)
		return
	}
	var b bytes.Buffer
	o.requests.Write(&b)
	o.duration.Write(&b)
	o.limited.Write(&b)
	app.WriteMetrics(&b)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	up := 1.0
	if err := o.writeDBGauges(ctx, &b); err != nil {
		up = 0
		logctx.FromContext(r.Context()).Warn("metrics: database gauges failed", "err", err)
	}
	metrics.WriteGauge(&b, "staqly_metrics_db_up", "1 if the database gauges were collected on this scrape.", metrics.Sample{Value: up})
	metrics.WriteGauge(&b, "staqly_build_info", "Build and mode information.",
		metrics.Sample{Labels: []string{"version", o.s.cfg.Version, "mode", o.mode()}, Value: 1})
	metrics.WriteRuntime(&b, processStart)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(200)
	if r.Method != http.MethodHead {
		w.Write(b.Bytes())
	}
}

func (o *obs) mode() string {
	if o.s.svc.Cfg.Mode == app.ModeCloud {
		return app.ModeCloud
	}
	return app.ModeLocal
}

type statusCount struct {
	status string
	n      int64
}

func samples(rows []statusCount, known ...string) []metrics.Sample {
	seen := map[string]bool{}
	var out []metrics.Sample
	for _, r := range rows {
		seen[r.status] = true
		out = append(out, metrics.Sample{Labels: []string{"status", r.status}, Value: float64(r.n)})
	}
	for _, k := range known { // keep series stable at zero
		if !seen[k] {
			out = append(out, metrics.Sample{Labels: []string{"status", k}, Value: 0})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Labels[1] < out[j].Labels[1] })
	return out
}

func (o *obs) writeDBGauges(ctx context.Context, b *bytes.Buffer) error {
	q := o.s.svc.Q
	dl, err := q.ObsDeliveryCounts(ctx)
	if err != nil {
		return err
	}
	posts, err := q.ObsPostCounts(ctx)
	if err != nil {
		return err
	}
	chans, err := q.ObsChannelCounts(ctx)
	if err != nil {
		return err
	}
	jobs, err := q.ObsJobCounts(ctx)
	if err != nil {
		return err
	}
	oldest, err := q.ObsOldestPendingDelivery(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	due, err := q.ObsDueDeliveries(ctx, now.Format("2006-01-02T15:04:05.000000Z"))
	if err != nil {
		return err
	}

	var rows []statusCount
	for _, r := range dl {
		rows = append(rows, statusCount{r.Status, r.N})
	}
	metrics.WriteGauge(b, "staqly_webhook_deliveries", "Webhook deliveries in the outbox by status.",
		samples(rows, "pending", "succeeded", "failed")...)

	rows = rows[:0]
	for _, r := range posts {
		rows = append(rows, statusCount{r.Status, r.N})
	}
	metrics.WriteGauge(b, "staqly_posts", "Posts by status (all workspaces).",
		samples(rows, "draft", "scheduled", "publishing", "published", "failed")...)

	rows = rows[:0]
	for _, r := range chans {
		rows = append(rows, statusCount{r.Status, r.N})
	}
	metrics.WriteGauge(b, "staqly_channels", "Connected channels by status.", samples(rows, "connected", "needs_reconnect", "disabled")...)

	rows = rows[:0]
	for _, r := range jobs {
		rows = append(rows, statusCount{r.Status, r.N})
	}
	metrics.WriteGauge(b, "staqly_jobs", "Jobs in the queue by status.", samples(rows, "queued", "running", "done", "failed", "cancelled")...)

	lag := 0.0
	if oldest != "" {
		if t, err := time.Parse(time.RFC3339Nano, oldest); err == nil {
			lag = max(0, now.Sub(t).Seconds())
		}
	}
	metrics.WriteGauge(b, "staqly_webhook_outbox_oldest_pending_seconds",
		"Age of the oldest pending webhook delivery (0 when none).", metrics.Sample{Value: lag})
	metrics.WriteGauge(b, "staqly_webhook_outbox_due", "Pending deliveries whose next attempt is due now.",
		metrics.Sample{Value: float64(due)})
	return nil
}
