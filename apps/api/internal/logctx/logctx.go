// Package logctx carries a request- or worker-scoped *slog.Logger in a
// context, plus the helpers to build it: W3C traceparent parsing, CF-Ray
// validation and LOG_LEVEL / LOG_FORMAT parsing. Logs stay plain log/slog
// (JSON lines by default) so any shipper — Cloudflare Logpush, Loki,
// Datadog — can index them; there is no OpenTelemetry dependency.
package logctx

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

type loggerKey struct{}
type traceKey struct{}

// NewContext returns ctx carrying l.
func NewContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}

// Lookup returns the logger stored in ctx, if any.
func Lookup(ctx context.Context) (*slog.Logger, bool) {
	if ctx == nil {
		return nil, false
	}
	l, ok := ctx.Value(loggerKey{}).(*slog.Logger)
	return l, ok && l != nil
}

// FromContext returns the logger stored in ctx, or slog.Default().
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := Lookup(ctx); ok {
		return l
	}
	return slog.Default()
}

// With returns ctx whose logger has args added (key/value pairs or slog.Attr).
func With(ctx context.Context, args ...any) context.Context {
	return NewContext(ctx, FromContext(ctx).With(args...))
}

// WithTrace records the request's trace id (for error responses).
func WithTrace(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceKey{}, traceID)
}

// TraceID returns the trace id recorded with WithTrace ("" if none).
func TraceID(ctx context.Context) string {
	v, _ := ctx.Value(traceKey{}).(string)
	return v
}

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// ParseTraceparent extracts trace-id and parent-id (span) from a W3C
// traceparent header ("00-<32 hex>-<16 hex>-<2 hex>"). Invalid headers —
// wrong shape, uppercase, all-zero ids, version ff — return ok=false.
func ParseTraceparent(h string) (traceID, spanID string, ok bool) {
	h = strings.TrimSpace(h)
	if len(h) < 55 || len(h) > 512 {
		return "", "", false
	}
	parts := strings.Split(h, "-")
	if len(parts) < 4 {
		return "", "", false
	}
	ver, tid, sid, flags := parts[0], parts[1], parts[2], parts[3]
	if len(ver) != 2 || !isLowerHex(ver) || ver == "ff" || len(tid) != 32 || !isLowerHex(tid) ||
		len(sid) != 16 || !isLowerHex(sid) || len(flags) != 2 || !isLowerHex(flags) {
		return "", "", false
	}
	if ver == "00" && len(parts) != 4 { // version 00 has exactly four fields
		return "", "", false
	}
	if tid == strings.Repeat("0", 32) || sid == strings.Repeat("0", 16) {
		return "", "", false
	}
	return tid, sid, true
}

// ValidCFRay accepts a Cloudflare ray id ("8f2c1a2b3c4d5e6f-AMS"): letters,
// digits and dashes, at most 64 characters.
func ValidCFRay(v string) bool {
	if v == "" || len(v) > 64 {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-') {
			return false
		}
	}
	return true
}

// ParseLevel parses LOG_LEVEL (debug|info|warn|error, case-insensitive;
// empty = info).
func ParseLevel(v string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error (got %q)", v)
}

// ParseFormat parses LOG_FORMAT (json|text, case-insensitive; empty = def).
func ParseFormat(v, def string) (string, error) {
	switch f := strings.ToLower(strings.TrimSpace(v)); f {
	case "":
		return def, nil
	case "json", "text":
		return f, nil
	}
	return "", fmt.Errorf("LOG_FORMAT must be json or text (got %q)", v)
}

// New builds a logger writing format ("json" or "text") at level to w.
func New(w io.Writer, format string, level slog.Leveler) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	if format == "text" {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

// Base adds the attributes every line carries.
func Base(l *slog.Logger, version, mode string) *slog.Logger {
	return l.With("service", "staqly-api", "version", version, "mode", mode)
}
