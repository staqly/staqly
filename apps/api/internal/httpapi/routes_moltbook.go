package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/staqly/staqly/apps/api/internal/app"
)

func init() {
	RegisterAuthedRoutes(func(s *Server, r chi.Router) {
		r.Post("/channels/moltbook/register", s.registerMoltbookAgent)
	})
}

// registerMoltbookAgent registers a new Moltbook agent and connects it; the answer has the link to claim it.
func (s *Server) registerMoltbookAgent(w http.ResponseWriter, r *http.Request) {
	var in app.MoltbookAgentInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.RegisterMoltbookAgent(r.Context(), principal(r), in)
	respond(w, r, 201, out, err)
}
