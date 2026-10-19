package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io/fs"
	"math"
	"net"
	"net/http"
	"net/netip"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	dbfiles "github.com/staqly/staqly/apps/api/db"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/logctx"
	"github.com/staqly/staqly/apps/api/internal/metrics"
)

// Observability and hardening (WP-5): X-Request-Id, /livez, /readyz,
// /metrics, and rate limits. Everything lives in one
// middleware registered on the root router, so it sees every request
// (including the root-level probe paths, which it serves before routing).

func init() {
	RegisterMiddleware(newObs)
	app.RegisterWorker(app.BackgroundWorker{Name: "obs-heartbeat", Run: runHeartbeat})
}

var processStart = time.Now()

const (
	heartbeatEvery = 5 * time.Second
	heartbeatStale = 30 * time.Second
)

// heartbeats maps *app.Service → *atomic.Int64 (unix nanos of the last beat;
// -1 once the background workers have stopped).
var heartbeats sync.Map

func runHeartbeat(ctx context.Context, s *app.Service) {
	v := &atomic.Int64{}
	v.Store(time.Now().UnixNano())
	heartbeats.Store(s, v)
	defer v.Store(-1)
	t := time.NewTicker(heartbeatEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			v.Store(now.UnixNano())
		}
	}
}

type obs struct {
	s         *Server
	requests  *metrics.CounterVec
	duration  *metrics.HistogramVec
	limited   *metrics.CounterVec
	tokenHash []byte // sha256 of METRICS_TOKEN; nil = loopback only

	rlDisabled bool
	ipLimit    *tokenBucket // unauthenticated sensitive endpoints, per client IP
	authLimit  *tokenBucket // everything else under /v1 and /mcp, per credential (cloud only)

	migrationsWant int64
}

func newObs(s *Server) func(http.Handler) http.Handler {
	o := &obs{
		s: s,
		requests: metrics.NewCounterVec("http_requests_total", "HTTP requests by method, route pattern and status.",
			"method", "route", "status"),
		duration: metrics.NewHistogramVec("http_request_duration_seconds", "HTTP request latency by method and route pattern.",
			metrics.DefBuckets, "method", "route"),
		limited:    metrics.NewCounterVec("staqly_rate_limited_total", "Requests rejected with 429 by limiter scope.", "scope"),
		rlDisabled: envBool(s, "RATE_LIMIT_DISABLED"),
	}
	if tok := s.svc.Env("METRICS_TOKEN", ""); tok != "" {
		h := sha256.Sum256([]byte(tok))
		o.tokenHash = h[:]
	}
	perMin := envInt(s, "RATE_LIMIT_PER_MINUTE", 10)
	o.ipLimit = newTokenBucket(perMin, envInt(s, "RATE_LIMIT_BURST", perMin), 100_000)
	o.authLimit = newTokenBucket(envInt(s, "RATE_LIMIT_AUTH_PER_MINUTE", 600), envInt(s, "RATE_LIMIT_AUTH_BURST", 100), 100_000)
	o.migrationsWant = embeddedMigrationVersion()
	return o.handler
}

func envInt(s *Server, key string, def int) int {
	if n, err := strconv.Atoi(s.svc.Env(key, "")); err == nil && n > 0 {
		return n
	}
	return def
}

func envBool(s *Server, key string) bool {
	b, _ := strconv.ParseBool(s.svc.Env(key, ""))
	return b
}

func (o *obs) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		if id := middleware.GetReqID(r.Context()); id != "" {
			w.Header().Set("X-Request-Id", id)
		}
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		route := ""
		defer func() {
			rec := recover()
			status := ww.Status()
			if rec != nil {
				status = 500 // Recoverer (outer) writes the response
			} else if status == 0 {
				status = 200
			}
			if route == "" {
				route = routePattern(r)
			}
			method := normMethod(r.Method)
			o.requests.Inc(method, route, strconv.Itoa(status))
			o.duration.Observe(time.Since(start).Seconds(), method, route)
			if rec != nil {
				if rec != http.ErrAbortHandler {
					logctx.FromContext(r.Context()).Error("panic", "panic", fmt.Sprint(rec), "stack", string(debug.Stack()))
				}
				panic(rec) // the outer Recoverer writes the 500
			}
		}()
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			switch r.URL.Path {
			case "/livez":
				route = r.URL.Path
				writeJSON(ww, 200, map[string]string{"status": "ok"})
				return
			case "/readyz":
				route = r.URL.Path
				o.readyz(ww, r)
				return
			case "/metrics":
				route = r.URL.Path
				o.metrics(ww, r)
				return
			}
		}
		if !o.allow(ww, r) {
			return
		}
		next.ServeHTTP(ww, r)
	})
}

// routePattern is the chi route pattern (never the raw path, which would
// explode label cardinality and could carry IDs).
func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	return "unmatched"
}

func probePath(p string) bool {
	return p == "/health" || p == "/livez" || p == "/readyz" || p == "/metrics"
}

func normMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return m
	}
	return "OTHER"
}

// ---- rate limiting ----

// sensitiveGroup names unauthenticated endpoints that are limited per client
// IP (credential guessing, signup spam, decision-link abuse).
func sensitiveGroup(r *http.Request) string {
	p := r.URL.Path
	switch {
	case r.Method == http.MethodPost && p == "/v1/sessions/recover":
		return "recover"
	case r.Method == http.MethodPost && p == "/v1/sessions/api-key":
		return "signin"
	case p == "/v1/auth/google/callback": // the start only redirects; the callback calls Google
		return "google"
	case p == "/v1/invitations/lookup" || (r.Method == http.MethodPost && p == "/v1/invitations/accept"):
		return "invite"
	case r.Method == http.MethodPost && (p == "/v1/auth/email/start" || p == "/v1/auth/email/verify"):
		return "email"
	case r.Method == http.MethodPost && p == "/v1/device-links/redeem":
		return "redeem"
	case r.Method == http.MethodPost && p == "/v1/workspaces":
		return "signup"
	}
	return ""
}

// credentialKey identifies the caller for the per-principal limit without a
// DB lookup: one API key or one session is one principal. Requests without
// credentials share a bucket per client IP.
func (o *obs) credentialKey(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		sum := sha256.Sum256([]byte(h))
		return "k:" + hex.EncodeToString(sum[:12])
	}
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		sum := sha256.Sum256([]byte(c.Value))
		return "s:" + hex.EncodeToString(sum[:12])
	}
	return "ip:" + o.s.clientIP(r)
}

func (o *obs) allow(w http.ResponseWriter, r *http.Request) bool {
	if o.rlDisabled {
		return true
	}
	var ok bool
	var wait time.Duration
	scope := ""
	if g := sensitiveGroup(r); g != "" {
		scope = "ip"
		ok, wait = o.ipLimit.take(g + ":" + o.s.clientIP(r))
	} else if o.s.svc.Cfg.Mode == app.ModeCloud && (strings.HasPrefix(r.URL.Path, "/v1/") || r.URL.Path == "/mcp") {
		// Local mode is single-user on this machine: no per-principal limit.
		scope = "principal"
		ok, wait = o.authLimit.take(o.credentialKey(r))
	} else {
		return true
	}
	if ok {
		return true
	}
	o.limited.Inc(scope)
	secs := int(math.Ceil(wait.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeErr(w, r, &app.Error{Code: "rate_limited", Status: http.StatusTooManyRequests,
		Message: "too many requests; retry after " + strconv.Itoa(secs) + "s"})
	return false
}

// ---- probes ----

func (o *obs) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	checks := map[string]string{}
	ready := true
	fail := func(k, v string) { checks[k] = v; ready = false }

	if err := o.s.svc.DB.PingContext(ctx); err != nil {
		fail("database", "unavailable")
	} else {
		checks["database"] = "ok"
	}
	// ponytail: raw query because goose's table is not part of the sqlc schema.
	var applied *int64
	if err := o.s.svc.DB.QueryRowContext(ctx,
		"SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&applied); err != nil || applied == nil {
		fail("migrations", "unknown")
	} else if *applied < o.migrationsWant {
		fail("migrations", fmt.Sprintf("pending (%d < %d)", *applied, o.migrationsWant))
	} else {
		checks["migrations"] = "ok"
	}
	if v, found := heartbeats.Load(o.s.svc); !found {
		checks["workers"] = "not_started" // e.g. API-only test servers; not fatal
	} else {
		switch ts := v.(*atomic.Int64).Load(); {
		case ts < 0:
			fail("workers", "stopped")
		case time.Since(time.Unix(0, ts)) > heartbeatStale:
			fail("workers", "stale")
		default:
			checks["workers"] = "ok"
		}
	}
	status, code := "ok", 200
	if !ready {
		status, code = "unavailable", 503
	}
	writeJSON(w, code, map[string]any{"status": status, "checks": checks})
}

func embeddedMigrationVersion() int64 {
	entries, err := fs.ReadDir(dbfiles.Migrations, "migrations")
	if err != nil {
		return 0
	}
	var max int64
	for _, e := range entries {
		digits := strings.SplitN(e.Name(), "_", 2)[0]
		if n, err := strconv.ParseInt(digits, 10, 64); err == nil && n > max {
			max = n
		}
	}
	return max
}

// ---- /metrics access ----

// metricsAllowed admits a scrape with the METRICS_TOKEN bearer, or a direct
// request from this machine (loopback peer, local Host, no proxy headers).
// Anything else gets 404 so the endpoint is not discoverable.
func (o *obs) metricsAllowed(r *http.Request) bool {
	if o.tokenHash != nil {
		if tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			sum := sha256.Sum256([]byte(strings.TrimSpace(tok)))
			if subtle.ConstantTimeCompare(sum[:], o.tokenHash) == 1 {
				return true
			}
		}
	}
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	ip, perr := netip.ParseAddr(peer)
	if err != nil || perr != nil || !ip.Unmap().IsLoopback() {
		return false
	}
	// A reverse proxy or the dashboard on this machine also connects from
	// loopback; forwarded requests are never considered local.
	// Presence, not value: an empty header still marks a relayed request.
	for _, h := range []string{"X-Forwarded-For", "X-Forwarded-Host", "Forwarded", "X-Real-Ip", "Origin"} {
		if _, ok := r.Header[h]; ok {
			return false
		}
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return o.s.localHost(strings.Trim(host, "[]")) // defeats DNS rebinding
}

// ---- access-log enrichment (needs the shared-file hooks described in docs) ----

// obsInfo is a mutable per-request record that inner layers fill in so the
// outer access log can include the principal. Create it with withObsInfo in
// logRequests and fill it with notePrincipal in authenticate.
type obsInfo struct {
	mu        sync.Mutex
	principal string
	workspace string
}

type obsInfoKey struct{}

func withObsInfo(r *http.Request) (*http.Request, *obsInfo) {
	info := &obsInfo{}
	return r.WithContext(context.WithValue(r.Context(), obsInfoKey{}, info)), info
}

func notePrincipal(ctx context.Context, p app.Principal) {
	if info, ok := ctx.Value(obsInfoKey{}).(*obsInfo); ok {
		info.mu.Lock()
		info.principal, info.workspace = principalLabel(p), p.WorkspaceID
		info.mu.Unlock()
	}
}

// logAttrs returns route, workspace_id and principal (kind + key id, never a secret) for the access log.
func (info *obsInfo) logAttrs(r *http.Request) []any {
	info.mu.Lock()
	defer info.mu.Unlock()
	out := []any{"route", routePattern(r)}
	if info.workspace != "" {
		out = append(out, "workspace_id", info.workspace, "principal", info.principal)
	}
	return out
}
