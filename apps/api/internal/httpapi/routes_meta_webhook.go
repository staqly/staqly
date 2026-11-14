package httpapi

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Meta's webhooks (Instagram, Facebook Pages, Threads) call /v1/meta/webhook: GET checks the subscription, POST
// brings an event, signed with the app's secret. Off (404) until PROVIDER_META_WEBHOOK_VERIFY_TOKEN is set.

// maxMetaWebhookBytes: Meta's events are small; a larger body is not one.
const maxMetaWebhookBytes = 1 << 20

// metaFormPaths: Meta's account callbacks, which post a form (signed_request) instead of JSON.
var metaFormPaths = []string{"/v1/meta/deauthorize", "/v1/meta/data-deletion"}

func init() {
	RegisterPublicRoutes(func(s *Server, r chi.Router) {
		r.Get("/meta/webhook", s.verifyMetaWebhook)
		r.Post("/meta/webhook", s.metaWebhook)
		r.Post("/meta/deauthorize", s.metaDeauthorize)
		r.Post("/meta/data-deletion", s.metaDataDeletion)
		r.Get("/meta/data-deletion/{code}", s.metaDataDeletionStatus)
	})
}

// metaDeauthorize: Meta's Deauthorize callback (someone removed the app from their account).
func (s *Server) metaDeauthorize(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMetaWebhookBytes)
	n, err := s.svc.MetaDeauthorize(r.Context(), r.PostFormValue("signed_request"))
	respond(w, r, 200, map[string]any{"channels": n}, err)
}

// metaDataDeletion: Meta's Data deletion request callback; Meta shows the person the status page and the code.
func (s *Server) metaDataDeletion(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMetaWebhookBytes)
	url, code, err := s.svc.MetaDataDeletion(r.Context(), r.PostFormValue("signed_request"))
	respond(w, r, 200, map[string]any{"url": url, "confirmation_code": code}, err)
}

// metaDataDeletionStatus: the page a deletion request's link opens, in plain words.
func (s *Server) metaDataDeletionStatus(w http.ResponseWriter, r *http.Request) {
	text, err := s.svc.MetaDataDeletionStatus(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		respond(w, r, 0, nil, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, text) // #nosec G705 -- text/plain + nosniff; the text is ours, not the request's
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
	_, _ = io.WriteString(w, challenge) // #nosec G705 -- text/plain + nosniff, echoed only after hub.verify_token matched
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
