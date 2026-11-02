package httpapi

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Meta's webhooks (Instagram, Facebook Pages, Threads) call /v1/meta/webhook: GET checks the subscription, POST
// brings an event, signed with the app's secret. Off (404) until META_WEBHOOK_VERIFY_TOKEN is set.

// maxMetaWebhookBytes: Meta's events are small; a larger body is not one.
const maxMetaWebhookBytes = 1 << 20

func init() {
	RegisterPublicRoutes(func(s *Server, r chi.Router) {
		r.Get("/meta/webhook", s.verifyMetaWebhook)
		r.Post("/meta/webhook", s.metaWebhook)
	})
}

func (s *Server) verifyMetaWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.svc.MetaWebhookEnabled() {
		respond(w, r, 0, nil, &app.Error{Code: "not_found", Status: 404, Message: "Meta webhooks are off on this server"})
		return
	}
	q := r.URL.Query()
	challenge, err := s.svc.VerifyMetaWebhook(q.Get("hub.mode"), q.Get("hub.verify_token"), q.Get("hub.challenge"))
	if err != nil {
		respond(w, r, 0, nil, err)
		return
	}
	// Meta wants the challenge back as it sent it, as plain text.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, challenge)
}

func (s *Server) metaWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.svc.MetaWebhookEnabled() {
		respond(w, r, 0, nil, &app.Error{Code: "not_found", Status: 404, Message: "Meta webhooks are off on this server"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxMetaWebhookBytes))
	if err != nil {
		respond(w, r, 0, nil, &app.Error{Code: "payload_too_large", Status: 413, Message: "the event is too large"})
		return
	}
	n, err := s.svc.HandleMetaWebhook(r.Context(), body, r.Header.Get("X-Hub-Signature-256"))
	respond(w, r, 200, map[string]any{"queued": n}, err)
}
