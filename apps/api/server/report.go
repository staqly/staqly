package server

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/posthog/posthog-go"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// reportHandler sends error-level log records to an error tracker (Sentry with SENTRY_DSN, PostHog with
// POSTHOG_KEY) and passes every record on. Only the message and log attributes are sent (no request
// bodies, headers or query strings); log lines never carry secrets.
type reportHandler struct {
	slog.Handler
	attrs []slog.Attr
	send  func(r slog.Record, report errorReport)
}

// errorReport is one error log line, split for the trackers: the err attribute, the attributes worth
// filtering on (tags) and the rest (extra).
type errorReport struct {
	err         string
	tags, extra map[string]string
}

func (h reportHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		rep := errorReport{tags: map[string]string{}, extra: map[string]string{}}
		add := func(a slog.Attr) bool {
			v := a.Value.Resolve().String()
			switch a.Key {
			case "err":
				rep.err = v
			case "request_id", "trace_id", "route", "workspace_id", "worker", "mode", "version":
				rep.tags[a.Key] = v
			default:
				rep.extra[a.Key] = v
			}
			return true
		}
		for _, a := range h.attrs {
			add(a)
		}
		r.Attrs(add)
		h.send(r, rep)
	}
	return h.Handler.Handle(ctx, r)
}

func (h reportHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return reportHandler{h.Handler.WithAttrs(as), append(slices.Clone(h.attrs), as...), h.send}
}

// ponytail: groups are flattened into the reports by key only.
func (h reportHandler) WithGroup(name string) slog.Handler {
	return reportHandler{h.Handler.WithGroup(name), h.attrs, h.send}
}

func sentrySend(r slog.Record, rep errorReport) {
	ev := sentry.NewEvent()
	ev.Level = sentry.LevelError
	ev.Message = r.Message
	if rep.err != "" {
		ev.Message += ": " + rep.err
	}
	for k, v := range rep.tags {
		ev.Tags[k] = v
	}
	extra := sentry.Context{}
	for k, v := range rep.extra {
		extra[k] = v
	}
	ev.Contexts["log"] = extra
	ev.Fingerprint = []string{r.Message, fmt.Sprint(ev.Tags["route"])}
	sentry.CaptureEvent(ev)
}

// posthogSend reports to PostHog error tracking: the log message is the issue's title, the error its
// description, grouped by message and route like Sentry. Exceptions are personless (a workspace's, or the
// server's when no workspace is involved).
func posthogSend(c posthog.EnqueueClient, base posthog.Properties) func(slog.Record, errorReport) {
	return func(r slog.Record, rep errorReport) {
		distinct := "staqly-api"
		if ws := rep.tags["workspace_id"]; ws != "" {
			distinct = "workspace:" + ws
		}
		ex := posthog.NewDefaultException(r.Time, distinct, r.Message, cmp.Or(rep.err, r.Message))
		fp := r.Message + "|" + rep.tags["route"]
		ex.ExceptionFingerprint = &fp
		// Exceptions don't get the client's DefaultEventProperties: base goes on them here.
		ex.Properties = posthog.NewProperties().Merge(base).Set("$process_person_profile", false)
		for _, m := range []map[string]string{rep.extra, rep.tags} {
			for k, v := range m {
				ex.Properties.Set(k, v)
			}
		}
		_ = c.Enqueue(ex)
	}
}

// newPostHog makes the client for POSTHOG_KEY: events batched in memory and sent in the background, at most
// 5 s to send what is left at shutdown. Its own complaints (PostHog unreachable) go to log as warnings, never
// back into error tracking.
func newPostHog(cfg app.PostHogConfig, base posthog.Properties, log slog.Handler) (posthog.Client, error) {
	return posthog.NewWithConfig(cfg.Key, posthog.Config{
		Endpoint:               cfg.Host,
		ShutdownTimeout:        5 * time.Second,
		Logger:                 posthog.StdLogger(slog.NewLogLogger(log, slog.LevelWarn), false),
		DefaultEventProperties: base,
	})
}
