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
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/db"
)

const sessionCookie = "staqly_session"

type Config struct {
	CookieSecure bool
	TrustProxy   bool // take the client IP from X-Forwarded-For (only behind your own proxy)
	OpenAPISpec  []byte
	MCP          http.Handler // mounted at /mcp behind API-key auth
	Version      string
}

type Server struct {
	svc     *app.Service
	log     *slog.Logger
	cfg     Config
	signups *rateLimiter
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

// Svc exposes the domain service to route registrars in other files.
func (s *Server) Svc() *app.Service { return s.svc }

// Log exposes the logger to route registrars.
func (s *Server) Log() *slog.Logger { return s.log }

func New(svc *app.Service, log *slog.Logger, cfg Config) http.Handler {
	s := &Server{svc: svc, log: log, cfg: cfg, signups: newRateLimiter(5, time.Hour)}
	r := chi.NewRouter()
	r.Use(middleware.RequestID, s.logRequests, middleware.Recoverer, securityHeaders)
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
			r.Use(s.requireAuth, s.idempotency)
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
			r.Post("/items/bulk-actions", s.bulkAction)
			r.Get("/items/{id}", s.getItem)
			r.Patch("/items/{id}", s.updateItem)
			r.Delete("/items/{id}", s.deleteItem)
			r.Get("/items/{id}/actions", s.availableActions)
			r.Post("/items/{id}/actions/{action}", s.runAction)
			r.Get("/items/{id}/events", s.itemEvents)
			r.Get("/items/{id}/wait", s.waitItem)
			r.Post("/items/{id}/result", s.reportResult)
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
			r.Get("/rules", s.listRules)
			r.Post("/rules", s.createRule)
			r.Get("/rules/{id}", s.getRule)
			r.Patch("/rules/{id}", s.updateRule)
			r.Delete("/rules/{id}", s.deleteRule)

			r.Get("/watcher-presets", s.listWatcherPresets)
			r.Get("/watchers", s.listWatchers)
			r.Post("/watchers", s.createWatcher)
			r.Post("/watchers/from-preset", s.createWatcherFromPreset)
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
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) { writeErr(w, app.ErrNotFound) })
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

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		r, info := withObsInfo(r)
		start := time.Now()
		next.ServeHTTP(ww, r)
		if r.URL.Path == "/health" {
			return
		}
		// Only the path is logged: query strings and headers may carry secrets.
		s.log.Info("http", append([]any{"method", r.Method, "path", r.URL.Path, "status", ww.Status(),
			"ms", time.Since(start).Milliseconds(), "request_id", middleware.GetReqID(r.Context())}, info.logAttrs(r)...)...)
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
					writeErr(w, app.ErrUnauthorized)
					return
				}
				p, err := s.svc.AuthenticateAPIKey(ctx, strings.TrimSpace(tok))
				if err != nil {
					writeErr(w, &app.Error{Code: "unauthorized", Status: 401, Message: "invalid or revoked API key"})
					return
				}
				ctx = context.WithValue(ctx, principalKey, p)
			} else if c, err := r.Cookie(sessionCookie); err == nil && allowCookie {
				p, csrf, err := s.svc.AuthenticateSession(ctx, c.Value)
				if err == nil {
					safe := r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions
					if !safe && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrf)) != 1 {
						writeErr(w, &app.Error{Code: "csrf_failed", Status: 403, Message: "missing or invalid X-CSRF-Token header"})
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
			notePrincipal(ctx, PrincipalFrom(ctx))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
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
//   - the TCP peer is loopback or a private address (published ports bind to
//     127.0.0.1; Docker forwards from its private bridge network),
//   - the Host header names this machine (defeats DNS rebinding), and
//   - any Origin header is a local origin (defeats cross-site requests from
//     web pages, which cannot forge Origin).
func (s *Server) checkLocal(r *http.Request) error {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	ip, perr := netip.ParseAddr(peer)
	if err != nil || perr != nil || !(ip.Unmap().IsLoopback() || ip.Unmap().IsPrivate()) {
		return errors.New("local mode only accepts connections from this machine")
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if !s.localHost(strings.Trim(host, "[]")) {
		return errors.New("local mode: Host " + r.Host + " is not a local name (add it to LOCAL_ALLOWED_HOSTS)")
	}
	// The dashboard proxy forwards the browser's Host here; a phone reaching the
	// dashboard over the LAN is not local even though the proxy itself is.
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" && s.svc.Cfg.DashboardExposed {
		// Behind an exposed dashboard (Docker Desktop hides the real client
		// address, and Host can be forged) no browser is provably local.
		return errors.New("local mode: the dashboard is open to the network, so this browser needs to be connected (run `make connect` on the computer, or scan a QR code)")
	}
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
		h := fh
		if hh, _, err := net.SplitHostPort(fh); err == nil {
			h = hh
		}
		if !s.localHost(strings.Trim(h, "[]")) {
			return errors.New("local mode: this device is not connected (forwarded host " + fh + "); use Connect a device on your computer")
		}
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil || !s.localHost(u.Hostname()) {
			return errors.New("local mode: cross-origin request from " + o + " refused")
		}
	}
	return nil
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if principal(r).WorkspaceID == "" {
			if msg, ok := r.Context().Value(notLocalKey).(string); ok {
				writeErr(w, &app.Error{Code: "not_local", Status: 403, Message: msg})
				return
			}
			w.Header().Set("WWW-Authenticate", `Bearer realm="staqly"`)
			writeErr(w, app.ErrUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type captureWriter struct {
	http.ResponseWriter
	status int
	buf    []byte
}

func (c *captureWriter) WriteHeader(code int) { c.status = code; c.ResponseWriter.WriteHeader(code) }
func (c *captureWriter) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = 200
	}
	if len(c.buf) < 1<<20 {
		c.buf = append(c.buf, b...)
	}
	return c.ResponseWriter.Write(b)
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
			writeErr(w, &app.Error{Code: "validation_failed", Status: 422, Message: "Idempotency-Key too long"})
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
		if err != nil {
			writeErr(w, &app.Error{Code: "too_large", Status: 413, Message: "request body too large"})
			return
		}
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		sum := sha256.Sum256(append([]byte(r.Method+" "+r.URL.Path+"\n"), body...))
		hash := hex.EncodeToString(sum[:])
		p := principal(r)
		ctx := r.Context()
		ids := db.GetIdempotencyKeyParams{WorkspaceID: p.WorkspaceID, PrincipalID: p.ID, Key: key}
		n, err := s.svc.Q.ReserveIdempotencyKey(ctx, db.ReserveIdempotencyKeyParams{WorkspaceID: p.WorkspaceID,
			PrincipalID: p.ID, Key: key, RequestHash: hash, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		if err != nil {
			writeErr(w, err)
			return
		}
		if n == 0 {
			prev, err := s.svc.Q.GetIdempotencyKey(ctx, db.GetIdempotencyKeyParams(ids))
			switch {
			case err != nil:
				writeErr(w, err)
			case prev.RequestHash != hash:
				writeErr(w, &app.Error{Code: "idempotency_mismatch", Status: 422, Message: "Idempotency-Key reused with a different request"})
			case !prev.StatusCode.Valid:
				writeErr(w, &app.Error{Code: "idempotency_in_progress", Status: 409, Message: "a request with this Idempotency-Key is in progress"})
			default:
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Idempotent-Replayed", "true")
				w.WriteHeader(int(prev.StatusCode.Int64))
				w.Write(prev.ResponseBody)
			}
			return
		}
		cw := &captureWriter{ResponseWriter: w}
		next.ServeHTTP(cw, r)
		if cw.status >= 500 || cw.status == 409 && strings.Contains(string(cw.buf), "concurrently") {
			// Transient failure: let the client retry with the same key.
			_ = s.svc.Q.ReleaseIdempotencyKey(ctx, db.ReleaseIdempotencyKeyParams(ids))
			return
		}
		_ = s.svc.Q.CompleteIdempotencyKey(ctx, db.CompleteIdempotencyKeyParams{StatusCode: sqlInt(cw.status),
			ResponseBody: cw.buf, WorkspaceID: p.WorkspaceID, PrincipalID: p.ID, Key: key})
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	var de *app.Error
	if !errors.As(err, &de) {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			de = &app.Error{Code: "too_large", Status: 413, Message: "request body too large"}
		} else {
			slog.Error("internal error", "err", err)
			de = &app.Error{Code: "internal", Status: 500, Message: "internal server error"}
		}
	}
	body := map[string]any{"error": de}
	if id := w.Header().Get("X-Request-Id"); id != "" {
		body["request_id"] = id
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
			writeErr(w, err)
		} else {
			writeErr(w, &app.Error{Code: "bad_request", Status: 400, Message: "invalid JSON body: " + err.Error()})
		}
		return false
	}
	return true
}

func qInt(r *http.Request, k string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(k))
	return n
}
