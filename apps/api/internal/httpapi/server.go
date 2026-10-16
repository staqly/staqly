// Package httpapi exposes the domain services as a versioned REST API.
package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/logctx"
)

const sessionCookie = "staqly_session"

type Config struct {
	CookieSecure bool
	TrustProxy   bool // take the client IP from X-Forwarded-For (only behind your own proxy)
	OpenAPISpec  []byte
	MCP          http.Handler // mounted at /mcp behind API-key auth
	Version      string
	// CORSOrigins may call the API from a browser with an API key (cloud mode
	// only, e.g. the docs playground). Never with cookies.
	CORSOrigins []string
}

type Server struct {
	svc     *app.Service
	log     *slog.Logger
	cfg     Config
	signups *rateLimiter
	tests   *tokenBucket // POST .../test endpoints (outbound requests on demand), per principal
	// summaries: POST /v1/items/{id}/summarize (a model call on demand), per principal
	summaries *tokenBucket
	dash      dashboardIPs
}

// Route registrars let feature packages add endpoints from their own files
// (call from init()). Authenticated routes run after auth + idempotency;
// public routes (e.g. chat-app callbacks that verify their own signatures)
// sit under /v1 without authentication. Middlewares wrap the whole router.
var (
	authedRoutes []func(s *Server, r chi.Router)
	publicRoutes []func(s *Server, r chi.Router)
	middlewares  []func(s *Server) func(http.Handler) http.Handler
)

func RegisterAuthedRoutes(f func(s *Server, r chi.Router)) { authedRoutes = append(authedRoutes, f) }
func RegisterPublicRoutes(f func(s *Server, r chi.Router)) { publicRoutes = append(publicRoutes, f) }
func RegisterMiddleware(f func(s *Server) func(http.Handler) http.Handler) {
	middlewares = append(middlewares, f)
}

// RegisterOuterMiddleware adds a middleware in front of every other
// registered one (it also sees requests that those answer themselves, like
// /livez or /v1/decide/{token}). Used by the OpenAPI contract test.
func RegisterOuterMiddleware(f func(s *Server) func(http.Handler) http.Handler) {
	middlewares = append([]func(s *Server) func(http.Handler) http.Handler{f}, middlewares...)
}

// Svc exposes the domain service to route registrars in other files.
func (s *Server) Svc() *app.Service { return s.svc }

// Log exposes the logger to route registrars.
func (s *Server) Log() *slog.Logger { return s.log }

func New(svc *app.Service, log *slog.Logger, cfg Config) http.Handler {
	s := &Server{svc: svc, log: log, cfg: cfg, signups: newRateLimiter(5, time.Hour), tests: newTokenBucket(10, 5, 10_000),
		summaries: newTokenBucket(20, 10, 10_000)}
	r := chi.NewRouter()
	r.Use(middleware.RequestID, s.logRequests, middleware.Recoverer, securityHeaders, cors(cfg.CORSOrigins))
	for _, m := range middlewares {
		r.Use(m(s))
	}

	r.Get("/health", s.health)
	r.Get("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(cfg.OpenAPISpec)
	})
	if cfg.MCP != nil {
		r.With(s.authenticate(false), s.requireAuth).Handle("/mcp", cfg.MCP)
	}

	r.Route("/v1", func(r chi.Router) {
		r.Use(middleware.AllowContentType("application/json", "multipart/form-data"))
		r.Use(s.authenticate(true))

		r.Get("/setup", s.setupStatus)
		r.Get("/plans", s.listPlans)
		r.Post("/workspaces", s.setup)
		r.Post("/sessions/recover", s.recover)
		r.Post("/device-links/redeem", s.redeemDeviceLink)
		for _, f := range publicRoutes {
			f(s, r)
		}

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth, s.readOnlyWhenSuspended, s.idempotency)
			r.Get("/session", s.currentSession)
			r.Delete("/session", s.logout)
			r.Get("/sessions", s.listSessions)
			r.Post("/device-links", s.createDeviceLink)
			r.Delete("/sessions/{id}", s.revokeSession)
			r.Get("/workspace", s.getWorkspace)
			r.Get("/workspace/usage", s.getUsage)
			r.Patch("/workspace", s.renameWorkspace)
			r.Post("/recovery-key/rotate", s.rotateRecovery)

			r.Get("/api-keys", s.listKeys)
			r.Post("/api-keys", s.createKey)
			r.Delete("/api-keys/{id}", s.revokeKey)
			r.Post("/api-keys/{id}/rotate", s.rotateKey)

			r.Get("/collections", s.listCollections)
			r.Post("/collections", s.createCollection)
			r.Get("/collections/{id}", s.getCollection)
			r.Patch("/collections/{id}", s.updateCollection)
			r.Delete("/collections/{id}", s.deleteCollection)
			r.Get("/collections/{id}/items", s.listItems)
			r.Post("/collections/{id}/items", s.createItem)

			r.Get("/items", s.listItems)
			r.Get("/tags", s.listTags)
			r.Get("/flow", s.getFlow)
			r.Get("/agents", s.listAgents)
			r.Delete("/agents/{id}", s.forgetAgent)
			r.Post("/items/bulk-actions", s.bulkAction)
			r.Get("/items/{id}", s.getItem)
			r.Patch("/items/{id}", s.updateItem)
			r.Delete("/items/{id}", s.deleteItem)
			r.Get("/items/{id}/actions", s.availableActions)
			r.Post("/items/{id}/actions/{action}", s.runAction)
			r.Get("/items/{id}/events", s.itemEvents)
			r.Get("/items/{id}/wait", s.waitItem)
			r.Post("/items/{id}/result", s.reportResult)
			r.Post("/items/{id}/unarchive", s.unarchiveItem)
			r.Post("/items/{id}/summarize", s.summarizeItem)
			r.Get("/items/{id}/versions", s.itemVersions)

			r.Post("/media", s.uploadMedia)
			r.Get("/media/{id}", s.getMedia)
			r.Delete("/media/{id}", s.deleteMedia)

			r.Get("/events", s.listEvents)

			r.Get("/webhooks", s.listWebhooks)
			r.Post("/webhooks", s.createWebhook)
			r.Get("/webhooks/{id}", s.getWebhook)
			r.Patch("/webhooks/{id}", s.updateWebhook)
			r.Delete("/webhooks/{id}", s.deleteWebhook)
			r.Post("/webhooks/{id}/test", s.testWebhook)

			r.Get("/watchers", s.listWatchers)
			r.Post("/watchers", s.createWatcher)
			r.Get("/watchers/{id}", s.getWatcher)
			r.Patch("/watchers/{id}", s.updateWatcher)
			r.Delete("/watchers/{id}", s.deleteWatcher)
			r.Post("/watchers/{id}/run", s.runWatcher)

			for _, f := range authedRoutes {
				f(s, r)
			}
			r.Get("/webhook-deliveries", s.listDeliveries)
			r.Post("/webhook-deliveries/{id}/retry", s.retryDelivery)
		})
	})
	r.NotFound(func(w http.ResponseWriter, req *http.Request) { writeErr(w, req, app.ErrNotFound) })
	return r
}

// ---- plumbing ----

type ctxKey int

const (
	principalKey ctxKey = iota
	csrfKey
	notLocalKey
)

func principal(r *http.Request) app.Principal {
	p, _ := r.Context().Value(principalKey).(app.Principal)
	return p
}

// PrincipalFrom exposes the authenticated principal to other packages (MCP).
func PrincipalFrom(ctx context.Context) app.Principal {
	p, _ := ctx.Value(principalKey).(app.Principal)
	return p
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// cors lets the listed browser origins call the API with an API key
// (Authorization: Bearer stk_...), e.g. the API playground on docs.staqly.ai.
// No Access-Control-Allow-Credentials: cookies are never sent cross-origin, so
// sessions (and local mode, which has none) stay same-origin only.
func cors(origins []string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range origins {
		allowed[strings.TrimRight(o, "/")] = true
	}
	return func(next http.Handler) http.Handler {
		if len(allowed) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if !allowed[origin] {
				next.ServeHTTP(w, r)
				return
			}
			h := w.Header()
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Expose-Headers", "X-Request-Id, Retry-After, Location, Content-Disposition, Idempotent-Replayed")
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// logPath is the request path as it may appear in logs: decision-link
// tokens are credentials and are replaced by the route placeholder.
func logPath(p string) string {
	if strings.HasPrefix(p, "/v1/decide/") {
		return "/v1/decide/{token}"
	}
	return p
}

// logRequests puts a request-scoped logger into the context (request_id,
// trace_id/span_id from a valid W3C traceparent, cf_ray, method, path;
// authenticate adds workspace_id and principal) and writes one http_request
// access line per request. Query strings, headers and bodies are never logged.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		r, info := withObsInfo(r)
		start := time.Now()
		attrs := []any{"request_id", middleware.GetReqID(r.Context())}
		ctx := r.Context()
		if tid, sid, ok := logctx.ParseTraceparent(r.Header.Get("traceparent")); ok {
			attrs = append(attrs, "trace_id", tid, "span_id", sid)
			ctx = logctx.WithTrace(ctx, tid)
		}
		if ray := r.Header.Get("CF-Ray"); logctx.ValidCFRay(ray) {
			attrs = append(attrs, "cf_ray", ray)
		}
		path := logPath(r.URL.Path)
		attrs = append(attrs, "method", r.Method, "path", path)
		l := s.log.With(attrs...)
		r = r.WithContext(logctx.NewContext(ctx, l))
		next.ServeHTTP(ww, r)

		status := ww.Status()
		if status == 0 {
			status = 200
		}
		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case status == http.StatusTooManyRequests:
			level = slog.LevelWarn
		}
		if level == slog.LevelInfo && probePath(path) && path != "/metrics" {
			return // /health, /livez, /readyz: polled constantly
		}
		ua := r.UserAgent()
		if len(ua) > 200 {
			ua = ua[:200]
		}
		l.Log(r.Context(), level, "http_request", append([]any{"status", status,
			"duration_ms", time.Since(start).Milliseconds(), "bytes", ww.BytesWritten(), "ip", s.clientIP(r),
			"user_agent", ua}, info.logAttrs(r)...)...)
	})
}

// authenticate resolves a bearer API key or (if allowCookie) a session cookie.
// Cookie-authenticated unsafe requests must carry the session's CSRF token.
func (s *Server) authenticate(allowCookie bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if h := r.Header.Get("Authorization"); h != "" {
				tok, ok := strings.CutPrefix(h, "Bearer ")
				if !ok {
					writeErr(w, r, app.ErrUnauthorized)
					return
				}
				p, err := s.svc.AuthenticateAPIKey(ctx, strings.TrimSpace(tok))
				if err != nil {
					writeErr(w, r, &app.Error{Code: "unauthorized", Status: 401, Message: "invalid or revoked API key"})
					return
				}
				ctx = context.WithValue(ctx, principalKey, p)
			} else if c, err := r.Cookie(sessionCookie); err == nil && allowCookie {
				p, csrf, err := s.svc.AuthenticateSession(ctx, c.Value)
				if err == nil {
					safe := r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions
					if !safe && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrf)) != 1 {
						writeErr(w, r, &app.Error{Code: "csrf_failed", Status: 403, Message: "missing or invalid X-CSRF-Token header"})
						return
					}
					ctx = context.WithValue(context.WithValue(ctx, principalKey, p), csrfKey, csrf)
				}
			}
			if s.svc.Cfg.Mode == app.ModeLocal && PrincipalFrom(ctx).WorkspaceID == "" {
				// Local mode: no credentials, but only for requests that provably
				// come from this machine and not from a web page on another origin.
				// Other devices (e.g. a phone on the LAN) need a session from a
				// "connect a device" QR link instead.
				if err := s.checkLocal(r); err != nil {
					ctx = context.WithValue(ctx, notLocalKey, err.Error())
				} else if p, err := s.svc.LocalPrincipal(ctx); err == nil {
					ctx = context.WithValue(ctx, principalKey, p)
				}
			}
			if p := PrincipalFrom(ctx); p.WorkspaceID != "" {
				ctx = logctx.With(ctx, "workspace_id", p.WorkspaceID, "principal", principalLabel(p))
			}
			notePrincipal(ctx, PrincipalFrom(ctx))
			ctx = seenAgent(s.svc, r.WithContext(ctx))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// principalLabel identifies the caller in logs without any secret:
// "api_key:key_...", "session", "local", "system:<id>".
func principalLabel(p app.Principal) string {
	switch p.Kind {
	case app.PrincipalAPIKey, app.PrincipalSystem:
		return p.Kind + ":" + p.ID
	}
	return p.Kind
}

var defaultLocalHosts = []string{"localhost", "127.0.0.1", "::1", "api", "host.docker.internal"}

func (s *Server) localHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if strings.HasSuffix(host, ".localhost") {
		return true
	}
	for _, h := range append(defaultLocalHosts, s.svc.Cfg.LocalHosts...) {
		if host == strings.ToLower(h) {
			return true
		}
	}
	return false
}

// checkLocal admits a credential-less request in local mode only if:
//   - the TCP peer is loopback, or inside LOCAL_TRUSTED_PEERS (e.g. the Docker
//     bridge networks: published ports bind to 127.0.0.1 and Docker forwards
//     from its bridge). Other LAN hosts are never trusted by default;
//   - the Host header names this machine (defeats DNS rebinding), and
//   - any Origin header is the dashboard's own origin or listed in
//     LOCAL_ALLOWED_ORIGINS (defeats cross-site requests from web pages, which
//     cannot forge Origin — including pages on other localhost ports).
func (s *Server) checkLocal(r *http.Request) error {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	ip, perr := netip.ParseAddr(peer)
	if err != nil || perr != nil || !s.trustedPeer(ip.Unmap()) {
		return errors.New("local mode only accepts connections from this machine (add trusted networks to LOCAL_TRUSTED_PEERS)")
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if !s.localHost(strings.Trim(host, "[]")) {
		return errors.New("local mode: Host " + r.Host + " is not a local name (add it to LOCAL_ALLOWED_HOSTS)")
	}
	// Requests relayed by the dashboard are never provably local when the
	// dashboard is published on the network (Docker hides the real client and
	// headers can be forged or emptied), so they need a session. Decide this
	// from the TCP peer (the dashboard container) AND from header presence,
	// never from header values alone.
	fwd, proxied := r.Header["X-Forwarded-Host"]
	if s.svc.Cfg.DashboardExposed && (proxied || s.peerIsDashboard(ip.Unmap())) {
		return errors.New("local mode: the dashboard is open to the network, so this browser needs to be connected (on the computer run `staqly serve connect-link`, or `docker exec staqly server connect-link` with Docker, or scan a QR code)")
	}
	if proxied {
		fh := ""
		if len(fwd) > 0 {
			fh = fwd[0]
		}
		h := fh
		if hh, _, err := net.SplitHostPort(fh); err == nil {
			h = hh
		}
		if fh == "" || !s.localHost(strings.Trim(h, "[]")) {
			return errors.New("local mode: this device is not connected (forwarded host " + fh + "); use Connect a device on your computer")
		}
	}
	if origin := r.Header.Get("Origin"); origin != "" { // browsers never send an empty Origin
		if !s.localOrigin(origin) {
			return errors.New("local mode: cross-origin request from " + origin + " refused (add it to LOCAL_ALLOWED_ORIGINS)")
		}
	}
	return nil
}

// trustedPeer: loopback, or an address inside LOCAL_TRUSTED_PEERS.
func (s *Server) trustedPeer(ip netip.Addr) bool {
	if ip.IsLoopback() {
		return true
	}
	for _, p := range s.svc.Cfg.LocalTrustedPeers {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func normOrigin(o string) string {
	u, err := url.Parse(strings.TrimSpace(o))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	return u.Scheme + "://" + host
}

// localOrigin admits the dashboard's origins (localhost / 127.0.0.1 / [::1]
// on DASHBOARD_PORT, the origin of PUBLIC_DASHBOARD_URL) and LOCAL_ALLOWED_ORIGINS.
func (s *Server) localOrigin(origin string) bool {
	o := normOrigin(origin)
	if o == "" {
		return false
	}
	port := s.svc.Cfg.DashboardPort
	if port == "" {
		port = "8080"
	}
	allowed := []string{s.svc.Cfg.PublicDashboardURL}
	for _, scheme := range []string{"http", "https"} {
		for _, h := range []string{"localhost", "127.0.0.1", "[::1]"} {
			allowed = append(allowed, scheme+"://"+h+":"+port)
		}
	}
	allowed = append(allowed, s.svc.Cfg.LocalOrigins...)
	for _, a := range allowed {
		if a != "" && normOrigin(a) == o {
			return true
		}
	}
	return false
}

// dashboardIPs caches the addresses of the dashboard container (DNS name
// DASHBOARD_HOST, default "dashboard": the Compose service) for 30 seconds.
type dashboardIPs struct {
	mu      sync.Mutex
	ips     []netip.Addr
	fetched time.Time
}

func (s *Server) peerIsDashboard(peer netip.Addr) bool {
	s.dash.mu.Lock()
	defer s.dash.mu.Unlock()
	if time.Since(s.dash.fetched) > 30*time.Second {
		s.dash.fetched = time.Now()
		s.dash.ips = nil
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", s.svc.Env("DASHBOARD_HOST", "dashboard")); err == nil {
			for _, a := range addrs {
				s.dash.ips = append(s.dash.ips, a.Unmap())
			}
		}
	}
	for _, a := range s.dash.ips {
		if a == peer {
			return true
		}
	}
	return false
}

// Changes a suspended workspace may still make: sign out, secure itself,
// connect a device to read, export or delete its data, and switch to Free.
var allowedWhileSuspended = []*regexp.Regexp{
	regexp.MustCompile(`^DELETE /v1/sessions?(/[^/]+)?$`),
	regexp.MustCompile(`^POST /v1/device-links$`),
	regexp.MustCompile(`^POST /v1/recovery-key/rotate$`),
	regexp.MustCompile(`^DELETE /v1/api-keys/[^/]+$`),
	regexp.MustCompile(`^POST /v1/workspace/(export|plan)$`),
	regexp.MustCompile(`^DELETE /v1/workspace$`),
	regexp.MustCompile(`^POST /v1/items/[^/]+/purge$`),
	regexp.MustCompile(`^POST /v1/billing/(checkout|portal)$`),
}

// readOnlyWhenSuspended: an unpaid Pro workspace keeps reading and exporting,
// every other change answers 402 plan_expired (MCP does the same per tool).
func (s *Server) readOnlyWhenSuspended(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		req := r.Method + " " + r.URL.Path
		for _, re := range allowedWhileSuspended {
			if re.MatchString(req) {
				next.ServeHTTP(w, r)
				return
			}
		}
		if err := s.svc.CheckWritable(r.Context(), principal(r).WorkspaceID); err != nil {
			writeErr(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if principal(r).WorkspaceID == "" {
			if msg, ok := r.Context().Value(notLocalKey).(string); ok {
				writeErr(w, r, &app.Error{Code: "not_local", Status: 403, Message: msg})
				return
			}
			w.Header().Set("WWW-Authenticate", `Bearer realm="staqly"`)
			writeErr(w, r, app.ErrUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// maxStoredResponse: larger responses are not cached for idempotent replay
// (the key is released instead of storing a truncated body).
const maxStoredResponse = 1 << 20

type captureWriter struct {
	http.ResponseWriter
	status   int
	buf      []byte
	overflow bool
}

func (c *captureWriter) WriteHeader(code int) { c.status = code; c.ResponseWriter.WriteHeader(code) }
func (c *captureWriter) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = 200
	}
	if !c.overflow {
		if len(c.buf)+len(b) > maxStoredResponse {
			c.overflow, c.buf = true, nil
		} else {
			c.buf = append(c.buf, b...)
		}
	}
	return c.ResponseWriter.Write(b)
}

type noStoreKey struct{}

// noStore marks a response as carrying a secret (new API key, recovery key,
// device link, webhook signing secret): it must not be kept in the
// idempotency cache, so the key is released instead of completed.
func noStore(r *http.Request) {
	if f, ok := r.Context().Value(noStoreKey{}).(*bool); ok {
		*f = true
	}
}

// idempotency replays the stored response for a repeated Idempotency-Key on
// unsafe requests. Keys are scoped to the principal and kept for 24h.
func (s *Server) idempotency(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" || r.Method == http.MethodGet || strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			next.ServeHTTP(w, r)
			return
		}
		if len(key) > 200 {
			writeErr(w, r, &app.Error{Code: "validation_failed", Status: 422, Message: "Idempotency-Key too long"})
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
		if err != nil {
			writeErr(w, r, &app.Error{Code: "too_large", Status: 413, Message: "request body too large"})
			return
		}
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		sum := sha256.Sum256(append([]byte(r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery+"\n"), body...))
		hash := hex.EncodeToString(sum[:])
		p := principal(r)
		ctx := r.Context()
		ids := db.GetIdempotencyKeyParams{WorkspaceID: p.WorkspaceID, PrincipalID: p.ID, Key: key}
		n, err := s.svc.W.ReserveIdempotencyKey(ctx, db.ReserveIdempotencyKeyParams{WorkspaceID: p.WorkspaceID,
			PrincipalID: p.ID, Key: key, RequestHash: hash, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		if err != nil {
			writeErr(w, r, err)
			return
		}
		if n == 0 {
			prev, err := s.svc.Q.GetIdempotencyKey(ctx, db.GetIdempotencyKeyParams(ids))
			switch {
			case err != nil:
				writeErr(w, r, err)
			case prev.RequestHash != hash:
				writeErr(w, r, &app.Error{Code: "idempotency_mismatch", Status: 422, Message: "Idempotency-Key reused with a different request"})
			case !prev.StatusCode.Valid:
				writeErr(w, r, &app.Error{Code: "idempotency_in_progress", Status: 409, Message: "a request with this Idempotency-Key is in progress"})
			default:
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Idempotent-Replayed", "true")
				w.WriteHeader(int(prev.StatusCode.Int64))
				w.Write(prev.ResponseBody)
			}
			return
		}
		// The bookkeeping must happen even if the client went away, and the
		// reservation must never outlive a failed or panicking handler (it
		// would block retries with "in progress" for 24 h).
		bg := context.WithoutCancel(ctx)
		completed := false
		defer func() {
			if !completed {
				_ = s.svc.W.ReleaseIdempotencyKey(bg, db.ReleaseIdempotencyKeyParams(ids))
			}
		}()
		secret := false
		cw := &captureWriter{ResponseWriter: w}
		next.ServeHTTP(cw, r.WithContext(context.WithValue(ctx, noStoreKey{}, &secret)))
		if secret || cw.overflow || cw.status >= 500 || cw.status == 409 && strings.Contains(string(cw.buf), "concurrently") {
			// Secret-bearing, too large to replay, or a transient failure:
			// release, so a retry with the same key runs again.
			return
		}
		if err := s.svc.W.CompleteIdempotencyKey(bg, db.CompleteIdempotencyKeyParams{StatusCode: sqlInt(cw.status),
			ResponseBody: cw.buf, WorkspaceID: p.WorkspaceID, PrincipalID: p.ID, Key: key}); err == nil {
			completed = true
		}
	})
}

// allowTest rate-limits the webhook/channel test endpoints per principal:
// each call makes an outbound request, so they must not become a cheap way
// to hammer a third party (or the network the server sits in).
func (s *Server) allowTest(w http.ResponseWriter, r *http.Request) bool {
	return s.allowOutbound(w, r, s.tests, "test requests")
}

// allowSummary rate-limits POST /v1/items/{id}/summarize per principal: each call is a model request the
// operator pays for (hosted models) or waits on (local ones).
func (s *Server) allowSummary(w http.ResponseWriter, r *http.Request) bool {
	return s.allowOutbound(w, r, s.summaries, "summary requests")
}

func (s *Server) allowOutbound(w http.ResponseWriter, r *http.Request, b *tokenBucket, what string) bool {
	p := principal(r)
	ok, wait := b.take(p.WorkspaceID + "/" + p.Kind + ":" + p.ID)
	if ok {
		return true
	}
	secs := max(1, int(wait.Seconds()+0.999))
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeErr(w, r, &app.Error{Code: "rate_limited", Status: http.StatusTooManyRequests,
		Message: "too many " + what + "; retry after " + strconv.Itoa(secs) + "s"})
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr writes the JSON error envelope. Unknown errors become 500 and are
// logged with the request's context logger (request_id, trace_id,
// workspace_id, path), never with headers, query strings or bodies.
func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	var de *app.Error
	if !errors.As(err, &de) {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			de = &app.Error{Code: "too_large", Status: 413, Message: "request body too large"}
		} else if app.IsBusy(err) {
			// The database stayed locked past busy_timeout (another process
			// writing): transient, so tell the client to retry.
			logctx.FromContext(r.Context()).Warn("database busy", "err", err)
			w.Header().Set("Retry-After", "1")
			de = &app.Error{Code: "busy", Status: http.StatusServiceUnavailable, Message: "database is busy; retry shortly"}
		} else {
			logctx.FromContext(r.Context()).Error("internal error", "err", err)
			de = &app.Error{Code: "internal", Status: 500, Message: "internal server error"}
		}
	}
	body := map[string]any{"error": de}
	if id := w.Header().Get("X-Request-Id"); id != "" {
		body["request_id"] = id
	}
	if tid := logctx.TraceID(r.Context()); tid != "" {
		body["trace_id"] = tid
	}
	writeJSON(w, de.Status, body)
}

// decode reads a JSON body (max 2 MiB) rejecting unknown fields.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeErr(w, r, err)
		} else {
			writeErr(w, r, &app.Error{Code: "bad_request", Status: 400, Message: "invalid JSON body: " + err.Error()})
		}
		return false
	}
	return true
}

func qInt(r *http.Request, k string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(k))
	return n
}
