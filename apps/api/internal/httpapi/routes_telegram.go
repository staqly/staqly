package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func init() {
	RegisterAuthedRoutes(func(s *Server, r chi.Router) {
		r.Post("/channels/telegram/connect", s.startTelegramConnect)
		r.Get("/channels/telegram/connect/{code}", s.getTelegramConnect)
	})
}

// startTelegramConnect: a code to send in the chat as /connect CODE, for the server's Telegram bot.
func (s *Server) startTelegramConnect(w http.ResponseWriter, r *http.Request) {
	var in struct {
		GroupID string `json:"group_id"`
	}
	if r.ContentLength > 0 && !decode(w, r, &in) {
		return
	}
	out, err := s.svc.StartTelegramConnect(r.Context(), principal(r), in.GroupID)
	respond(w, r, 201, out, err)
}

func (s *Server) getTelegramConnect(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetTelegramConnect(r.Context(), principal(r), chi.URLParam(r, "code"))
	respond(w, r, 200, out, err)
}
