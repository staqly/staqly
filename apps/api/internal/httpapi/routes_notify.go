package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/staqly/staqly/apps/api/internal/app"
)

// Alerts: where staqly tells people what happened (scope alerts:manage).

func init() {
	RegisterAuthedRoutes(func(s *Server, r chi.Router) {
		r.Get("/alerts", s.listAlerts)
		r.Post("/alerts", s.createAlert)
		r.Get("/alerts/{id}", s.getAlert)
		r.Patch("/alerts/{id}", s.updateAlert)
		r.Delete("/alerts/{id}", s.deleteAlert)
		r.Post("/alerts/{id}/test", s.testAlert)
		r.Get("/alerts/{id}/deliveries", s.alertDeliveries)
	})
}

func (s *Server) listAlerts(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListAlerts(r.Context(), principal(r))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) createAlert(w http.ResponseWriter, r *http.Request) {
	var in app.AlertInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateAlert(r.Context(), principal(r), in)
	respond(w, r, 201, out, err)
}

func (s *Server) getAlert(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetAlert(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) updateAlert(w http.ResponseWriter, r *http.Request) {
	var in app.AlertInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateAlert(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, r, 200, out, err)
}

func (s *Server) deleteAlert(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.DeleteAlert(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) testAlert(w http.ResponseWriter, r *http.Request) {
	if !s.allowTest(w, r) {
		return
	}
	out, err := s.svc.TestAlert(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) alertDeliveries(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListAlertDeliveries(r.Context(), principal(r), chi.URLParam(r, "id"), qInt(r, "limit"))
	respond(w, r, 200, map[string]any{"data": out}, err)
}
