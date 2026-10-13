package metrics

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Sentry is a minimal fire-and-forget Sentry client using the envelope API,
// so we need no SDK. Events are queued and sent by one goroutine; when the
// queue is full new events are dropped (an error storm must not hurt the API).
//
// Only what the caller passes in tags is sent: no headers, query strings,
// bodies, IPs or user data ever leave the process.
// ponytail: no rate limiting/backoff on Sentry 429 beyond the bounded queue.
type Sentry struct {
	endpoint    string
	auth        string
	release     string
	environment string
	client      *http.Client
	queue       chan []byte
	once        sync.Once
}

// SentryEvent describes one error to report.
type SentryEvent struct {
	Message string            // human-readable summary (must not contain secrets)
	Panic   bool              // reported as an exception with a stack trace
	Stack   []uintptr         // program counters for panics (runtime.Callers)
	Tags    map[string]string // enum-like values: route, method, status, request_id
}

// NewSentry parses a DSN ("https://<public_key>@<host>/<project_id>"). It
// returns nil, nil for an empty DSN, which callers treat as disabled.
func NewSentry(dsn, release, environment string) (*Sentry, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, nil
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User == nil || u.User.Username() == "" {
		return nil, errors.New("invalid SENTRY_DSN")
	}
	path := strings.Trim(u.Path, "/")
	i := strings.LastIndex(path, "/")
	prefix, project := "", path
	if i >= 0 {
		prefix, project = "/"+path[:i], path[i+1:]
	}
	if project == "" {
		return nil, errors.New("invalid SENTRY_DSN: missing project id")
	}
	auth := "Sentry sentry_version=7, sentry_client=staqly-go/1, sentry_key=" + u.User.Username()
	if secret, ok := u.User.Password(); ok && secret != "" {
		auth += ", sentry_secret=" + secret
	}
	if environment == "" {
		environment = "production"
	}
	return &Sentry{
		endpoint:    fmt.Sprintf("%s://%s%s/api/%s/envelope/", u.Scheme, u.Host, prefix, project),
		auth:        auth,
		release:     release,
		environment: environment,
		client:      &http.Client{Timeout: 5 * time.Second},
		queue:       make(chan []byte, 64),
	}, nil
}

// Capture queues an event; it never blocks.
func (s *Sentry) Capture(ev SentryEvent) {
	if s == nil {
		return
	}
	s.once.Do(func() { go s.loop() })
	body, err := s.envelope(ev)
	if err != nil {
		return
	}
	select {
	case s.queue <- body:
	default: // queue full: drop
	}
}

func (s *Sentry) loop() {
	for body := range s.queue {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-sentry-envelope")
			req.Header.Set("X-Sentry-Auth", s.auth)
			if resp, err := s.client.Do(req); err == nil {
				io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
				resp.Body.Close()
			}
		}
		cancel()
	}
}

type sentryFrame struct {
	Function string `json:"function"`
	Filename string `json:"filename"`
	Lineno   int    `json:"lineno"`
	InApp    bool   `json:"in_app"`
}

func (s *Sentry) envelope(ev SentryEvent) ([]byte, error) {
	var idb [16]byte
	_, _ = rand.Read(idb[:])
	id := hex.EncodeToString(idb[:])
	now := time.Now().UTC()
	event := map[string]any{
		"event_id":    id,
		"timestamp":   float64(now.UnixNano()) / 1e9,
		"platform":    "go",
		"level":       "error",
		"logger":      "staqly-api",
		"release":     s.release,
		"environment": s.environment,
		"tags":        ev.Tags,
	}
	if ev.Panic {
		var frames []sentryFrame
		if len(ev.Stack) > 0 {
			it := runtime.CallersFrames(ev.Stack)
			for {
				f, more := it.Next()
				frames = append(frames, sentryFrame{Function: f.Function, Filename: f.File, Lineno: f.Line,
					InApp: strings.Contains(f.Function, "staqly/apps/api")})
				if !more {
					break
				}
			}
			// Sentry expects the oldest frame first.
			for i, j := 0, len(frames)-1; i < j; i, j = i+1, j-1 {
				frames[i], frames[j] = frames[j], frames[i]
			}
		}
		exc := map[string]any{"type": "panic", "value": ev.Message}
		if len(frames) > 0 {
			exc["stacktrace"] = map[string]any{"frames": frames}
		}
		event["exception"] = map[string]any{"values": []any{exc}}
	} else {
		event["message"] = map[string]any{"formatted": ev.Message}
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	hdr, _ := json.Marshal(map[string]any{"event_id": id, "sent_at": now.Format(time.RFC3339Nano)})
	item, _ := json.Marshal(map[string]any{"type": "event", "length": len(payload)})
	b.Write(hdr)
	b.WriteByte('\n')
	b.Write(item)
	b.WriteByte('\n')
	b.Write(payload)
	b.WriteByte('\n')
	return b.Bytes(), nil
}
