package app

import (
	"io"

	"github.com/staqly/staqly/apps/api/internal/metrics"
)

// Process-wide event counters, written by /metrics. Labels are enums only:
// never workspace ids, URLs or content.
var (
	metricPublishes = metrics.NewCounterVec("staqly_publish_attempts_total",
		"Publish attempts by provider and outcome (published, retry, failed).", "provider", "result")
	metricJobs = metrics.NewCounterVec("staqly_jobs_total",
		"Job runs by kind and outcome (done, retry, failed).", "kind", "result")
	metricWebhookAttempts = metrics.NewCounterVec("staqly_webhook_attempts_total",
		"Webhook delivery attempts by outcome (succeeded, retry, failed).", "result")
	metricWebhookDuration = metrics.NewHistogramVec("staqly_webhook_attempt_duration_seconds",
		"Webhook delivery attempt duration.", metrics.DefBuckets)
	metricNotifications = metrics.NewCounterVec("staqly_notifications_total",
		"Alert messages sent by channel type and result (ok, error).", "channel_type", "result")
)

// WriteMetrics writes the event counters in the Prometheus text format.
func WriteMetrics(w io.Writer) {
	metricPublishes.Write(w)
	metricJobs.Write(w)
	metricWebhookAttempts.Write(w)
	metricWebhookDuration.Write(w)
	metricNotifications.Write(w)
}
