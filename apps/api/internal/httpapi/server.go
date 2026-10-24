// Package httpapi exposes the domain services as a versioned REST API.
package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/logctx"
	"github.com/staqly/staqly/apps/api/internal/store"
)

const sessionCookie = "staqly_session"

type Config struct {
	CookieSecure bool
	TrustProxy   bool // take the client IP from X-Forwarded-For (only behind your own proxy)
	// RelayToken: the embedded dashboard's secret (web.RelayHeader); requests bearing it name the browser in
	// X-Forwarded-For, which the dashboard set itself. "" = no embedded dashboard.
	RelayToken  string
	OpenAPISpec []byte
	MCP         http.Handler // mounted at /mcp behind API-key auth
	Version     string
	// CORSOrigins may call the API from a browser with an API key (cloud mode
	// only, e.g. the docs playground). Never with cookies.
	CORSOrigins []string
}

type Server struct {
	svc      *app.Service
	log      *slog.Logger
	cfg      Config
	signups  *rateLimiter
	codes    *rateLimiter // email codes sent per client IP (sign-in and linking)
	passkeys *rateLimiter // passkey sign-in ceremonies per client IP
	tests    *tokenBucket // POST .../test endpoints (outbound requests on demand), per principal
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
// /livez). Used by the OpenAPI contract test.
func RegisterOuterMiddleware(f func(s *Server) func(http.Handler) http.Handler) {
	middlewares = append([]func(s *Server) func(http.Handler) http.Handler{f}, middlewares...)
}

// Svc exposes the domain service to route registrars in other files.
func (s *Server) Svc() *app.Service { return s.svc }

// Log exposes the logger to route registrars.
func (s *Server) Log() *slog.Logger { return s.log }

func New(svc *app.Service, log *slog.Logger, cfg Config) http.Handler {
	s := &Server{svc: svc, log: log, cfg: cfg, signups: newRateLimiter(5, time.Hour), codes: newRateLimiter(20, time.Hour),
		passkeys: newRateLimiter(30, 10*time.Minute), tests: newTokenBucket(10, 5, 10_000)}
	r := chi.NewRouter()
	r.Use(middleware.RequestID, s.logRequests, recoverer, securityHeaders, cors(cfg.CORSOrigins))
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
		// JSON answers gzipped for clients that ask (browsers through the dashboard, the SDKs): the
		// collection list is ~1.2 KB per collection of mostly repeated action definitions and shrinks ~25x.
		// Only application/json: downloads (zip, NDJSON) and /mcp stream as they are.
		r.Use(middleware.Compress(5, "application/json"))
		r.Use(allowContentType("application/json", "multipart/form-data"))
		r.Use(s.authenticate(true))

		r.Get("/setup", s.setupStatus)
		r.Get("/plans", s.listPlans)
		r.Post("/workspaces", s.setup)
		r.Post("/sessions/recover", s.recover)
		r.Post("/sessions/api-key", s.signInWithAPIKey)
		r.Post("/device-links/redeem", s.redeemDeviceLink)
		for _, f := range publicRoutes {
			f(s, r)
		}

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth, s.readOnlyWhenSuspended, s.idempotency)
			r.Get("/session", s.currentSession)
			r.Delete("/session", s.logout)
			r.Post("/session/onboarded", s.finishOnboarding)
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
			r.Get("/collections/{id}/posts", s.listPosts)

			r.Get("/posts", s.listPosts)
			r.Post("/posts", s.createPost)
			r.Post("/posts/delete", s.deletePosts)
			r.Get("/tags", s.listTags)
			r.Get("/work", s.listWork)
			r.Get("/agents", s.listAgents)
			r.Delete("/agents/{id}", s.forgetAgent)
			r.Get("/posts/{id}", s.getPost)
			r.Patch("/posts/{id}", s.updatePost)
			r.Delete("/posts/{id}", s.deletePost)
			r.Post("/posts/{id}/schedule", s.schedulePost)
			r.Post("/posts/{id}/publish", s.publishPost)
			r.Post("/posts/{id}/cancel", s.cancelPost)
			r.Post("/posts/{id}/duplicate", s.duplicatePost)
			r.Get("/posts/{id}/events", s.postEvents)
			r.Get("/posts/{id}/preview", s.previewPost)
			r.Post("/posts/{id}/targets/{channel}/result", s.reportTargetResult)
			r.Post("/posts/{id}/targets/{channel}/retry", s.retryTarget)

			r.Get("/providers", s.listProviders)
			r.Put("/providers/{id}/app", s.setProviderApp)
			r.Delete("/providers/{id}/app", s.deleteProviderApp)
			r.Post("/providers/{id}/check", s.checkProvider)
			r.Get("/channels", s.listChannels)
			r.Post("/channels", s.connectChannel)
			r.Get("/channels/oauth/{provider}/start", s.oauthStart)
			r.Get("/channels/oauth/{provider}/callback", s.oauthCallback)
			r.Get("/pending-connections/{id}", s.getPendingConnection)
			r.Post("/pending-connections/{id}", s.finishPendingConnection)
			r.Get("/channels/{id}", s.getChannel)
			r.Patch("/channels/{id}", s.updateChannel)
			r.Delete("/channels/{id}", s.deleteChannel)
			r.Post("/channels/{id}/test", s.testChannel)
			r.Get("/channel-groups", s.listChannelGroups)
			r.Post("/channel-groups", s.createChannelGroup)
			r.Patch("/channel-groups/{id}", s.updateChannelGroup)
			r.Delete("/channel-groups/{id}", s.deleteChannelGroup)

			r.Get("/schedules", s.listSchedules)
			r.Post("/schedules", s.createSchedule)
			r.Get("/schedules/{id}", s.getSchedule)
			r.Patch("/schedules/{id}", s.updateSchedule)
			r.Delete("/schedules/{id}", s.deleteSchedule)
			r.Post("/schedules/{id}/run", s.runSchedule)

			r.Get("/jobs", s.listJobs)
			r.Get("/jobs/stats", s.jobStats)
			r.Get("/jobs/{id}", s.getJob)
			r.Post("/jobs/{id}/retry", s.retryJob)
			r.Post("/jobs/{id}/cancel", s.cancelJob)

			r.Get("/media", s.listMedia)
			r.Post("/media", s.uploadMedia)
			r.Post("/media/delete", s.deleteMediaMany)
			r.Post("/media/bulk", s.bulkUpdateMedia)
			r.Get("/media-folders", s.listMediaFolders)
			r.Post("/media-folders", s.createMediaFolder)
			r.Patch("/media-folders/{id}", s.updateMediaFolder)
			r.Delete("/media-folders/{id}", s.deleteMediaFolder)
			r.Get("/media/{id}", s.getMedia)
			r.Get("/media/{id}/info", s.getMediaInfo)
			r.Get("/media/{id}/usage", s.mediaUsage)
			r.Patch("/media/{id}", s.updateMedia)
			r.Delete("/media/{id}", s.deleteMedia)
			r.Post("/media/{id}/restore", s.restoreMedia)
			r.Post("/media/{id}/keep", s.keepMedia)

			r.Get("/events", s.listEvents)

			r.Get("/webhooks", s.listWebhooks)
			r.Post("/webhooks", s.createWebhook)
			r.Get("/webhooks/{id}", s.getWebhook)
			r.Patch("/webhooks/{id}", s.updateWebhook)
			r.Delete("/webhooks/{id}", s.deleteWebhook)
			r.Post("/webhooks/{id}/test", s.testWebhook)

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

// recoverer turns a panic into the JSON 500 every other failure gets. The panic and its stack are logged
// by obs (inside, with the request's logger); http.ErrAbortHandler keeps aborting the response.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				writeErr(w, r, &app.Error{Code: "internal", Status: 500, Message: "internal server error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
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
		ctx = app.WithClientIP(ctx, s.clientIP(r))
		path := logctx.RedactPath(r.URL.Path)
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
				tok = strings.TrimSpace(tok)
				p, local, err := s.svc.AuthenticateLocalToken(ctx, tok)
				if local && err != nil {
					writeErr(w, r, &app.Error{Code: "unauthorized", Status: 401,
						Message: "invalid access token: print the current one with `staqly serve token` or `docker exec staqly server token`"})
					return
				}
				if !local {
					if p, err = s.svc.AuthenticateAPIKey(ctx, tok); err != nil {
						if errors.Is(err, app.ErrLocalAPIKey) {
							writeErr(w, r, app.ErrLocalAPIKey) // the Community Edition: the access token only
							return
						}
						writeErr(w, r, &app.Error{Code: "unauthorized", Status: 401, Message: "invalid or revoked API key"})
						return
					}
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

// Local mode has no keyless access any more: every request carries the access token (or a session made
// with it, CSRF-checked like any session), which a web page on another origin or a DNS-rebinding page can't
// attach, so the old peer, Host and Origin checks guarded nothing a credential doesn't. What stays as
// defense in depth: the API listens on 127.0.0.1 by default (the image publishes it on 127.0.0.1 only),
// cookies are HttpOnly and SameSite=Lax, and /metrics (localHost below) stays this machine's.

var defaultLocalHosts = []string{"localhost", "127.0.0.1", "::1", "api", "host.docker.internal"}

func (s *Server) localHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if strings.HasSuffix(host, ".localhost") {
		return true
	}
	for _, h := range defaultLocalHosts {
		if host == strings.ToLower(h) {
			return true
		}
	}
	return false
}

// Changes a suspended workspace may still make: sign out, secure itself,
// connect a device to read, export or delete its data, and pay for a plan; and the account-level moves that
// don't change it (switch to another workspace, make a new one), so a read-only workspace never traps anyone.
var allowedWhileSuspended = []*regexp.Regexp{
	regexp.MustCompile(`^POST /v1/session/workspace$`),
	regexp.MustCompile(`^POST /v1/account/workspaces$`),
	regexp.MustCompile(`^DELETE /v1/sessions?(/[^/]+)?$`),
	regexp.MustCompile(`^POST /v1/device-links$`),
	regexp.MustCompile(`^POST /v1/recovery-key/rotate$`),
	regexp.MustCompile(`^DELETE /v1/api-keys/[^/]+$`),
	regexp.MustCompile(`^POST /v1/workspace/export$`),
	regexp.MustCompile(`^DELETE /v1/workspace$`),
	regexp.MustCompile(`^POST /v1/posts/[^/]+/purge$`),
	regexp.MustCompile(`^POST /v1/billing/(checkout|portal|sync|promotion-code)$`),
}

// readOnlyWhenSuspended: an unpaid workspace (trial or plan ended) keeps reading and exporting,
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
			w.Header().Set("WWW-Authenticate", `Bearer realm="staqly"`)
			if s.svc.Cfg.Mode == app.ModeLocal {
				writeErr(w, r, app.ErrNoLocalToken)
				return
			}
			writeErr(w, r, app.ErrUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// staleReservation: an Idempotency-Key reserved this long ago and never completed belongs to a request
// that died with its process (a finished or failed one completes or releases it); a retry takes it over.
// JSON requests finish well within it (uploads, the long ones, skip idempotency).
const staleReservation = 10 * time.Minute

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
		hash := requestHash(r, body)
		p := principal(r)
		ctx := r.Context()
		ids := db.GetIdempotencyKeyParams{WorkspaceID: p.WorkspaceID, PrincipalID: p.ID, Key: key}
		n, err := s.svc.W.ReserveIdempotencyKey(ctx, db.ReserveIdempotencyKeyParams{WorkspaceID: p.WorkspaceID,
			PrincipalID: p.ID, Key: key, RequestHash: hash, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		if err != nil {
			writeErr(w, r, err)
			return
		}
		if n == 0 && !s.takeOverStale(w, r, ids, hash, body) {
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
		if secret || cw.overflow || cw.status >= 500 || cw.status == statusClientClosed ||
			cw.status == 409 && strings.Contains(string(cw.buf), "concurrently") ||
			cw.status == http.StatusTooManyRequests || cw.status == http.StatusPaymentRequired {
			// Secret-bearing, too large to replay, a transient failure, the client hung up (writeErr's 499),
			// or an answer that changes with time (429: wait and retry; 402: until the plan is upgraded):
			// release, so a retry with the same key runs again.
			return
		}
		if err := s.svc.W.CompleteIdempotencyKey(bg, db.CompleteIdempotencyKeyParams{StatusCode: sqlInt(cw.status),
			ResponseBody: cw.buf, WorkspaceID: p.WorkspaceID, PrincipalID: p.ID, Key: key}); err == nil {
			completed = true
		}
	})
}

// takeOverStale answers a request whose Idempotency-Key is already reserved: a replay, a mismatch or
// "in progress". It returns true (nothing written) when the reservation is a stale one of the same request
// (staleReservation) and this request now holds it: the caller runs the handler.
func (s *Server) takeOverStale(w http.ResponseWriter, r *http.Request, ids db.GetIdempotencyKeyParams, hash string, body []byte) bool {
	prev, err := s.svc.Q.GetIdempotencyKey(r.Context(), ids)
	switch {
	case err != nil:
		writeErr(w, r, err)
	case prev.RequestHash != hash && prev.RequestHash != legacyRequestHash(r, body):
		writeErr(w, r, &app.Error{Code: "idempotency_mismatch", Status: 422, Message: "Idempotency-Key reused with a different request"})
	case !prev.StatusCode.Valid:
		// The UPDATE only matches a reservation still stale and unfinished: of two retries racing, one wins.
		now := time.Now().UTC()
		n, err := s.svc.W.TakeOverIdempotencyKey(r.Context(), db.TakeOverIdempotencyKeyParams{CreatedAt: now.Format(time.RFC3339Nano),
			WorkspaceID: ids.WorkspaceID, PrincipalID: ids.PrincipalID, Key: ids.Key,
			StaleBefore: now.Add(-staleReservation).Format(time.RFC3339Nano)})
		if err != nil {
			writeErr(w, r, err)
			return false
		}
		if n == 1 {
			return true
		}
		writeErr(w, r, &app.Error{Code: "idempotency_in_progress", Status: 409, Message: "a request with this Idempotency-Key is in progress"})
	default:
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Idempotent-Replayed", "true")
		w.WriteHeader(int(prev.StatusCode.Int64))
		w.Write(prev.ResponseBody)
	}
	return false
}

// allowTest rate-limits the webhook/channel test endpoints per principal:
// each call makes an outbound request, so they must not become a cheap way
// to hammer a third party (or the network the server sits in).
func (s *Server) allowTest(w http.ResponseWriter, r *http.Request) bool {
	return s.allowOutbound(w, r, s.tests, "test requests")
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
	if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
		// The client hung up (e.g. gave up on a long-poll wait): nobody reads
		// an answer and nothing failed here, so no 500 and no error log.
		w.WriteHeader(statusClientClosed)
		return
	}
	var de *app.Error
	if !errors.As(err, &de) {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			de = &app.Error{Code: "too_large", Status: 413, Message: "request body too large"}
		} else if errors.Is(err, store.ErrNUL) {
			// PostgreSQL text cannot hold NUL: a client error, not a failure.
			de = &app.Error{Code: "validation_failed", Status: 422, Message: err.Error()}
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

// statusClientClosed is logged (never seen by a client) when the client closed
// the connection before the answer; nginx's convention.
const statusClientClosed = 499

// allowContentType answers 415 with the usual JSON error body when a request
// with a body has another Content-Type (chi's AllowContentType sends none).
func allowContentType(types ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength == 0 {
				next.ServeHTTP(w, r)
				return
			}
			ct, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";")
			if slices.Contains(types, strings.ToLower(strings.TrimSpace(ct))) {
				next.ServeHTTP(w, r)
				return
			}
			writeErr(w, r, &app.Error{Code: "unsupported_media_type", Status: http.StatusUnsupportedMediaType,
				Message: "Content-Type must be one of " + strings.Join(types, ", ")})
		})
	}
}

// decode reads a JSON body (max 2 MiB) rejecting unknown fields.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil {
		// One JSON value only: `{"a":1} {"a":2}` or `{...}garbage` is not a valid body.
		if _, terr := dec.Token(); terr != io.EOF {
			var mbe *http.MaxBytesError
			if err = errors.New("unexpected data after the JSON value"); errors.As(terr, &mbe) {
				err = terr
			}
		}
	}
	if err != nil {
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
