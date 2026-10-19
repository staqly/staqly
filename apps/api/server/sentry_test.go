package server

import (
	"io"
	"log/slog"
	"testing"

	"github.com/getsentry/sentry-go"
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
	log := slog.New(sentryHandler{Handler: slog.NewJSONHandler(io.Discard, nil)}).With("request_id", "r1")
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
