package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/staqly/staqly/apps/api/internal/app"
)

// Notification channel management (scope channels:manage).

func init() {
	RegisterAuthedRoutes(func(s *Server, r chi.Router) {
		r.Get("/notification-channels", s.listChannels)
		r.Post("/notification-channels", s.createChannel)
		r.Get("/notification-channels/{id}", s.getChannel)
		r.Patch("/notification-channels/{id}", s.updateChannel)
		r.Delete("/notification-channels/{id}", s.deleteChannel)
		r.Post("/notification-channels/{id}/test", s.testChannel)
		r.Get("/notification-channels/{id}/deliveries", s.channelDeliveries)
	})
}

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListNotificationChannels(r.Context(), principal(r))
	respond(w, 200, map[string]any{"data": out}, err)
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	var in app.ChannelInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateNotificationChannel(r.Context(), principal(r), in)
	respond(w, 201, out, err)
}

func (s *Server) getChannel(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetNotificationChannel(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, 200, out, err)
}

func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request) {
	var in app.ChannelInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateNotificationChannel(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, 200, out, err)
}

func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, s.svc.DeleteNotificationChannel(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) testChannel(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.TestNotificationChannel(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, 200, out, err)
}

func (s *Server) channelDeliveries(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListNotificationDeliveries(r.Context(), principal(r), chi.URLParam(r, "id"), qInt(r, "limit"))
	respond(w, 200, map[string]any{"data": out}, err)
}
