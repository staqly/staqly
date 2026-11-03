package httpapi

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// TikTok's webhooks call POST /v1/tiktok/webhook, signed with the app's client secret. Off (404) until the server
// has TikTok's app (PROVIDER_TIKTOK_CLIENT_SECRET).

// maxTikTokWebhookBytes: TikTok's events are small; a larger body is not one.
const maxTikTokWebhookBytes = 1 << 20

func init() {
	RegisterPublicRoutes(func(s *Server, r chi.Router) {
		r.Post("/tiktok/webhook", s.tiktokWebhook)
	})
}

func (s *Server) tiktokWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.svc.TikTokWebhookEnabled() {
		respond(w, r, 0, nil, &app.Error{Code: "not_found", Status: 404, Message: "TikTok webhooks are off on this server"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxTikTokWebhookBytes))
	if err != nil {
		respond(w, r, 0, nil, &app.Error{Code: "payload_too_large", Status: 413, Message: "the event is too large"})
		return
	}
	n, err := s.svc.HandleTikTokWebhook(r.Context(), body, r.Header.Get("TikTok-Signature"))
	respond(w, r, 200, map[string]any{"handled": n}, err)
}
