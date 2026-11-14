package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/staqly/staqly/apps/api/internal/app"
)

// A person's own notification settings, and the unsubscribe link in every personal email.

// unsubscribes limits unsubscribe requests per client IP (the token is the credential).
var unsubscribes = newRateLimiter(30, 10*time.Minute)

func init() {
	RegisterAuthedRoutes(func(s *Server, r chi.Router) {
		r.Get("/account/notifications", s.getNotificationPrefs)
		r.Put("/account/notifications", s.setNotificationPrefs)
	})
	// Public: mail clients post here for one-click unsubscribe (RFC 8058, token in the query); the dashboard's
	// unsubscribe page posts the token as JSON.
	RegisterPublicRoutes(func(s *Server, r chi.Router) {
		r.Post("/notifications/unsubscribe", s.unsubscribe)
	})
}

func (s *Server) getNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetNotificationPrefs(r.Context(), principal(r))
	respond(w, r, 200, out, err)
}

func (s *Server) setNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	var in app.NotificationPrefs
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.SetNotificationPrefs(r.Context(), principal(r), in)
	respond(w, r, 200, out, err)
}

func (s *Server) unsubscribe(w http.ResponseWriter, r *http.Request) {
	if !unsubscribes.allow(s.clientIP(r)) {
		writeErr(w, r, &app.Error{Code: "rate_limited", Status: 429, Message: "too many requests: try again later"})
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in)
		token = in.Token
	}
	event, err := s.svc.Unsubscribe(r.Context(), token)
	respond(w, r, 200, map[string]any{"event": event}, err)
}
