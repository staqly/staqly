package server

import (
	"io"
	"log/slog"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/posthog/posthog-go"
)

func TestSentryHandlerSendsOnlyErrors(t *testing.T) {
	var got []*sentry.Event
	if err := sentry.Init(sentry.ClientOptions{Dsn: "https://key@sentry.invalid/1",
		BeforeSend: func(ev *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			got = append(got, ev)
			return nil // never leave the process
		}}); err != nil {
		t.Fatal(err)
	}
	log := slog.New(reportHandler{Handler: slog.NewJSONHandler(io.Discard, nil), send: sentrySend}).With("request_id", "r1")
	log.Info("fine")
	log.Error("internal error", "err", "boom", "route", "/v1/items")
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1", len(got))
	}
	ev := got[0]
	if ev.Message != "internal error: boom" || ev.Tags["request_id"] != "r1" || ev.Tags["route"] != "/v1/items" {
		t.Fatalf("event = %q %v", ev.Message, ev.Tags)
	}
}

type fakePostHog struct{ sent []posthog.Message }

func (f *fakePostHog) Enqueue(m posthog.Message) error { f.sent = append(f.sent, m); return nil }

// Error lines become PostHog exceptions of the workspace, with the request's ids and the base properties
// (environment, release); lower levels stay in the log only.
func TestPostHogHandlerSendsOnlyErrors(t *testing.T) {
	f := &fakePostHog{}
	base := posthog.NewProperties().Set("environment", "test").Set("release", "v1")
	log := slog.New(reportHandler{Handler: slog.NewJSONHandler(io.Discard, nil), send: posthogSend(f, base)}).
		With("request_id", "r1", "workspace_id", "ws_1")
	log.Warn("slow")
	log.Error("internal error", "err", "boom", "route", "/v1/posts")
	if len(f.sent) != 1 {
		t.Fatalf("events = %d, want 1", len(f.sent))
	}
	ex := f.sent[0].(posthog.Exception)
	item := ex.ExceptionList[0]
	if ex.DistinctId != "workspace:ws_1" || item.Type != "internal error" || item.Value != "boom" ||
		*ex.ExceptionFingerprint != "internal error|/v1/posts" {
		t.Fatalf("exception = %q %q %q %q", ex.DistinctId, item.Type, item.Value, *ex.ExceptionFingerprint)
	}
	for k, want := range map[string]any{"request_id": "r1", "route": "/v1/posts", "environment": "test", "release": "v1",
		"$process_person_profile": false} {
		if ex.Properties[k] != want {
			t.Errorf("%s = %v, want %v", k, ex.Properties[k], want)
		}
	}
	if item.Stacktrace == nil || len(item.Stacktrace.Frames) == 0 {
		t.Error("no stack trace")
	}
}
