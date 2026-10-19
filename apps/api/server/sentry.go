package server

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/getsentry/sentry-go"
)

// sentryHandler sends error-level log records to Sentry and passes every
// record on. Only the message and log attributes are sent (no request
// bodies, headers or query strings); log lines never carry secrets.
type sentryHandler struct {
	slog.Handler
	attrs []slog.Attr
}

func (h sentryHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		ev := sentry.NewEvent()
		ev.Level = sentry.LevelError
		ev.Message = r.Message
		extra := sentry.Context{}
		add := func(a slog.Attr) bool {
			v := a.Value.Resolve().String()
			switch a.Key {
			case "err":
				ev.Message += ": " + v
			case "request_id", "trace_id", "route", "workspace_id", "mode", "version":
				ev.Tags[a.Key] = v
			default:
				extra[a.Key] = v
			}
			return true
		}
		for _, a := range h.attrs {
			add(a)
		}
		r.Attrs(add)
		ev.Contexts["log"] = extra
		ev.Fingerprint = []string{r.Message, fmt.Sprint(ev.Tags["route"])}
		sentry.CaptureEvent(ev)
	}
	return h.Handler.Handle(ctx, r)
}

func (h sentryHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return sentryHandler{h.Handler.WithAttrs(as), append(slices.Clone(h.attrs), as...)}
}

// ponytail: groups are flattened into the Sentry extras by key only.
func (h sentryHandler) WithGroup(name string) slog.Handler {
	return sentryHandler{h.Handler.WithGroup(name), h.attrs}
}
