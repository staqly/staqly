package httpapi

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Kick's webhooks call POST /v1/kick/webhook, signed with Kick's key. Off (404) until the server has Kick's app
// (PROVIDER_KICK_CLIENT_ID).

// maxKickWebhookBytes: Kick's events are small; a larger body is not one.
const maxKickWebhookBytes = 1 << 20

func init() {
	RegisterPublicRoutes(func(s *Server, r chi.Router) {
		r.Post("/kick/webhook", s.kickWebhook)
	})
}

func (s *Server) kickWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.svc.KickWebhookEnabled() {
		respond(w, r, 0, nil, &app.Error{Code: "not_found", Status: 404, Message: "Kick webhooks are off on this server"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxKickWebhookBytes))
	if err != nil {
		respond(w, r, 0, nil, &app.Error{Code: "payload_too_large", Status: 413, Message: "the event is too large"})
		return
	}
	n, err := s.svc.HandleKickWebhook(r.Context(), app.KickCall{MessageID: r.Header.Get("Kick-Event-Message-Id"),
		Timestamp: r.Header.Get("Kick-Event-Message-Timestamp"), Signature: r.Header.Get("Kick-Event-Signature"),
		Type: r.Header.Get("Kick-Event-Type")}, body)
	respond(w, r, 200, map[string]any{"handled": n}, err)
}
