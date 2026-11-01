package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// What people answered under a published post.

func init() {
	RegisterAuthedRoutes(func(s *Server, r chi.Router) {
		r.Get("/posts/{id}/responses", s.postResponses)
		r.Post("/posts/{id}/responses/refresh", s.refreshPostResponses)
	})
}

func (s *Server) postResponses(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.PostResponses(r.Context(), principal(r), chi.URLParam(r, "id"), r.URL.Query().Get("channel"))
	respond(w, r, 200, out, err)
}

func (s *Server) refreshPostResponses(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.RefreshPostResponses(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 202, out, err)
}
