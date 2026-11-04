package httpapi

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Whop's webhooks call POST /v1/whop/webhook/{channel}, one webhook per connected Whop channel, signed with its own
// secret (Standard Webhooks headers).

const maxWhopWebhookBytes = 1 << 20

func init() {
	RegisterPublicRoutes(func(s *Server, r chi.Router) {
		r.Post("/whop/webhook/{channel}", s.whopWebhook)
	})
}

func (s *Server) whopWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWhopWebhookBytes))
	if err != nil {
		respond(w, r, 0, nil, &app.Error{Code: "payload_too_large", Status: 413, Message: "the event is too large"})
		return
	}
	n, err := s.svc.HandleWhopWebhook(r.Context(), chi.URLParam(r, "channel"), app.WhopCall{ID: r.Header.Get("webhook-id"),
		Timestamp: r.Header.Get("webhook-timestamp"), Signature: r.Header.Get("webhook-signature")}, body)
	respond(w, r, 200, map[string]any{"handled": n}, err)
}
