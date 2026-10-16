package app

import (
	"io"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/metrics"
)

// Process-wide event counters, written by /metrics. Labels are enums only:
// never workspace ids, URLs or content.
var (
	metricDecisions = metrics.NewCounterVec("staqly_item_decisions_total",
		"Items moved to approved, rejected or expired, by who decided.", "status", "via")
	metricDecisionLatency = metrics.NewHistogramVec("staqly_item_decision_latency_seconds",
		"Time from item creation to its decision.",
		[]float64{1, 10, 60, 300, 900, 3600, 4 * 3600, 24 * 3600, 7 * 24 * 3600}, "status")
	metricWebhookAttempts = metrics.NewCounterVec("staqly_webhook_attempts_total",
		"Webhook delivery attempts by outcome (succeeded, retry, failed).", "result")
	metricWebhookDuration = metrics.NewHistogramVec("staqly_webhook_attempt_duration_seconds",
		"Webhook delivery attempt duration.", metrics.DefBuckets)
	metricWatcherRuns = metrics.NewCounterVec("staqly_watcher_runs_total",
		"Watcher runs by kind and result (ok, error).", "kind", "result")
	metricWatcherItems = metrics.NewCounterVec("staqly_watcher_items_created_total",
		"Items created by watchers.", "kind")
	metricNotifications = metrics.NewCounterVec("staqly_notifications_total",
		"Notification messages sent by channel type and result (ok, error).", "channel_type", "result")
)

// WriteMetrics writes the event counters in the Prometheus text format.
func WriteMetrics(w io.Writer) {
	metricDecisions.Write(w)
	metricDecisionLatency.Write(w)
	metricWebhookAttempts.Write(w)
	metricWebhookDuration.Write(w)
	metricWatcherRuns.Write(w)
	metricWatcherItems.Write(w)
	metricNotifications.Write(w)
}

// recordDecision counts a status change into a final state.
// ponytail: counted inside the transaction, so a rare rollback after this
// point over-counts by one; move to an after-commit hook if that matters.
func recordDecision(p Principal, status, createdAt string) {
	if status != "approved" && status != "rejected" && status != "expired" {
		return
	}
	metricDecisions.Inc(status, decisionVia(p))
	if t, err := time.Parse(timeFormat, createdAt); err == nil {
		metricDecisionLatency.Observe(max(0, time.Since(t).Seconds()), status)
	}
}

// decisionVia names who decided: dashboard (session), local, api_key,
// expiry, link (email/ntfy decide links) or chat (Telegram/Slack/Discord buttons).
func decisionVia(p Principal) string {
	if p.Kind != PrincipalSystem {
		return p.Kind
	}
	switch {
	case p.ID == "expiry":
		return "expiry"
	case p.ID == "processing-timeout":
		return "timeout"
	case strings.HasPrefix(p.ID, "link:"):
		return "link"
	}
	return "chat"
}
