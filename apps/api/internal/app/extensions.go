package app

import (
	"context"
	"log/slog"
	"os"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/logctx"
)

// Extension points. Feature packages (notifications, push, retention, ...)
// register themselves from init() in their own files, so they never need to
// edit the core service files.

// EmitHook runs inside the transaction that records an event (see emit), so
// anything it writes (e.g. a notification outbox row) commits atomically with
// the change. It must not perform network I/O.
type EmitHook func(ctx context.Context, s *Service, q *db.Queries, ev db.Event) error

// BackgroundWorker runs for the lifetime of the server (started by
// RunBackground) and must return when ctx is cancelled.
type BackgroundWorker struct {
	Name string
	Run  func(ctx context.Context, s *Service)
}

var (
	emitHooks         []EmitHook
	backgroundWorkers []BackgroundWorker
)

// RegisterEmitHook adds a hook called for every recorded event. Call from init().
func RegisterEmitHook(h EmitHook) { emitHooks = append(emitHooks, h) }

// RegisterWorker adds a background worker. Call from init().
func RegisterWorker(w BackgroundWorker) { backgroundWorkers = append(backgroundWorkers, w) }

// RunBackground starts the core workers (webhooks, watchers, summaries) and every
// registered worker, and blocks until all of them have stopped after ctx ends.
func (s *Service) RunBackground(ctx context.Context) {
	done := make(chan struct{}, 3+len(backgroundWorkers))
	go func() { s.RunWorker(s.workerContext(ctx, "webhooks")); done <- struct{}{} }()
	go func() { s.RunWatchers(s.workerContext(ctx, "watchers")); done <- struct{}{} }()
	go func() { s.RunSummaries(s.workerContext(ctx, "summaries")); done <- struct{}{} }()
	for _, w := range backgroundWorkers {
		go func(w BackgroundWorker) { w.Run(s.workerContext(ctx, w.Name), s); done <- struct{}{} }(w)
	}
	for range 3 + len(backgroundWorkers) {
		<-done
	}
}

// Env reads feature configuration. Packages use it instead of adding fields to
// Config, e.g. s.Env("SMTP_HOST"). Tests can replace Service.Getenv.
func (s *Service) Env(key, def string) string {
	get := s.Getenv
	if get == nil {
		get = os.Getenv
	}
	if v := get(key); v != "" {
		return v
	}
	return def
}

// logger returns the context's logger (request-scoped: request_id, trace_id,
// workspace_id...; worker-scoped: worker=<name>) or the service logger.
func (s *Service) logger(ctx context.Context) *slog.Logger {
	if l, ok := logctx.Lookup(ctx); ok {
		return l
	}
	return s.Log
}

// workerContext tags everything a background worker logs with worker=<name>.
func (s *Service) workerContext(ctx context.Context, name string) context.Context {
	return logctx.NewContext(ctx, s.Log.With("worker", name))
}
