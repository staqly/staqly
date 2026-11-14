package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Local mode's access token (app/local_token.go): the dashboard's agents page shows it and makes a new one.
func init() {
	RegisterAuthedRoutes(func(s *Server, r chi.Router) {
		r.Get("/access-token", s.getAccessToken)
		r.Post("/access-token/rotate", s.rotateAccessToken)
	})
}

func (s *Server) getAccessToken(w http.ResponseWriter, r *http.Request) {
	tok, err := s.svc.LocalAccessToken(principal(r))
	respond(w, r, 200, map[string]string{"token": tok}, err)
}

// rotateAccessToken: a new token; every other browser is signed out, this one stays.
func (s *Server) rotateAccessToken(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	p := principal(r)
	keep := ""
	if p.Kind == app.PrincipalSession {
		keep = p.ID
	}
	tok, revoked, err := s.svc.RotateLocalToken(r.Context(), p, keep)
	respond(w, r, 200, map[string]any{"token": tok, "sessions_revoked": revoked}, err)
}
