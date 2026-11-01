// Package adminapi is the operator panel's REST API (/v1/admin/...): its own sign-in (mailed code, 12 h cookie
// session), CSRF header on every unsafe method, no-store everywhere. Everything else lives in internal/app.
package adminapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/staqly/staqly/apps/api/internal/app"
)

const (
	cookieName   = "staqly_admin"
	maxBodyBytes = 64 << 10
)

// Config: CookieSecure marks the session cookie Secure (https); TrustProxy reads the client address from X-Forwarded-For.
type Config struct{ CookieSecure, TrustProxy bool }

type server struct {
	svc  *app.Service
	log  *slog.Logger
	cfg  Config
	auth *limiter
}

type ctxKey struct{}

func adminOf(r *http.Request) app.Admin { a, _ := r.Context().Value(ctxKey{}).(app.Admin); return a }

// New returns the handler; paths are /v1/admin/... (the listener strips any prefix before calling it).
func New(svc *app.Service, log *slog.Logger, cfg Config) http.Handler {
	s := &server{svc: svc, log: log, cfg: cfg, auth: &limiter{max: 20, window: 10 * time.Minute, hits: map[string][]time.Time{}}}
	r := chi.NewRouter()
	r.Use(s.base)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, s.log, &app.Error{Code: "not_found", Status: 404, Message: "no such route"})
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, s.log, &app.Error{Code: "method_not_allowed", Status: 405, Message: "method not allowed"})
	})
	r.Route("/v1/admin", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(s.limitAuth)
			r.Post("/auth/code", s.authCode)
			r.Post("/auth/verify", s.authVerify)
			r.Post("/auth/token", s.authToken)
		})
		r.Get("/auth/methods", s.authMethods)
		r.Group(func(r chi.Router) {
			r.Use(s.requireAdmin)
			r.Post("/auth/sign-out", s.signOut)
			r.Get("/me", s.me)
			r.Get("/overview", s.overview)
			r.Get("/workspaces", s.listWorkspaces)
			r.Get("/workspaces/{id}", s.getWorkspace)
			r.Get("/workspaces/{id}/payments", s.workspacePayments)
			r.Get("/users", s.listUsers)
			r.Get("/users/{id}", s.getUser)
			r.Get("/payments", s.payments)
			r.Get("/promotion-codes", s.listPromos)
			r.Post("/promotion-codes", s.createPromo)
			r.Post("/promotion-codes/{id}/deactivate", s.deactivatePromo)
			r.Post("/workspaces/{id}/plan", s.setPlan)
			r.Post("/workspaces/{id}/trial-review/clear", s.clearTrial)
			r.Post("/workspaces/{id}/attach", s.attach)
			r.Post("/workspaces/{id}/sign-in-link", s.signInLink)
			r.Post("/workspaces/{id}/sessions/revoke", s.revokeWorkspace)
			r.Post("/users/{id}/sessions/revoke", s.revokeUser)
			r.Post("/workspaces/{id}/billing/sync", s.billingSync)
			r.Post("/workspaces/{id}/billing/cancel", s.billingCancel)
			r.Delete("/workspaces/{id}", s.deleteWorkspace)
			r.Delete("/users/{id}", s.deleteUser)
			r.Get("/audit", s.audit)
			r.Get("/admins", s.listAdmins)
			r.Post("/admins", s.addAdmin)
			r.Delete("/admins/{email}", s.removeAdmin)
		})
	})
	return r
}

// base: 404 when the panel is off, no-store, the client address for the service's limits, JSON-only bodies.
func (s *server) base(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !s.svc.AdminEnabled() {
			writeErr(w, s.log, &app.Error{Code: "not_found", Status: 404, Message: "no such route"})
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.ContentLength != 0 {
			ct := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
			if ct != "application/json" {
				writeErr(w, s.log, &app.Error{Code: "unsupported_media_type", Status: 415, Message: "send application/json"})
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r.WithContext(app.WithClientIP(r.Context(), s.clientIP(r))))
	})
}

func (s *server) limitAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.auth.allow(s.clientIP(r)) {
			writeErr(w, s.log, &app.Error{Code: "rate_limited", Status: 429, Message: "too many attempts: try again later"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil || c.Value == "" {
			writeErr(w, s.log, app.ErrUnauthorized)
			return
		}
		a, err := s.svc.AuthenticateAdmin(r.Context(), c.Value)
		if err != nil {
			writeErr(w, s.log, err)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions &&
			subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(a.CSRF)) != 1 {
			writeErr(w, s.log, &app.Error{Code: "csrf_failed", Status: 403, Message: "missing or wrong X-CSRF-Token"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, a)))
	})
}

// clientIP: the TCP peer, or with TrustProxy the last X-Forwarded-For hop (the one our own proxy appended; the first is the client's to forge).
func (s *server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
			parts := strings.Split(xff[len(xff)-1], ",")
			if ip, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1])); err == nil {
				return ipKey(ip)
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ipKey(ip)
	}
	return host
}

func ipKey(ip netip.Addr) string {
	ip = ip.Unmap()
	if ip.Is6() {
		p, _ := ip.Prefix(64)
		return p.String()
	}
	return ip.String()
}

// limiter: sliding window per key. shortcut: in memory, per process, keys only pruned on their own hits; fine for one admin listener.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.hits) > 10_000 {
		clear(l.hits)
	}
	recent := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			recent = append(recent, t)
		}
	}
	if len(recent) >= l.max {
		l.hits[key] = recent
		return false
	}
	l.hits[key] = append(recent, now)
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, log *slog.Logger, err error) {
	var ae *app.Error
	if !errors.As(err, &ae) {
		log.Error("admin api", "err", err)
		ae = &app.Error{Code: "internal", Status: 500, Message: "internal error"}
	}
	body := map[string]any{"code": ae.Code, "message": ae.Message}
	if len(ae.Fields) > 0 {
		body["fields"] = ae.Fields
	}
	if len(ae.Details) > 0 {
		body["details"] = ae.Details
	}
	writeJSON(w, ae.Status, map[string]any{"error": body})
}
