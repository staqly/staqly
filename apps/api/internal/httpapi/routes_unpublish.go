package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func init() {
	RegisterAuthedRoutes(func(s *Server, r chi.Router) {
		r.Post("/posts/{id}/unpublish", s.unpublishPost)
	})
}

// unpublishPost takes a published post down from its channels (all, or channel_ids).
func (s *Server) unpublishPost(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ChannelIDs []string `json:"channel_ids"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UnpublishPost(r.Context(), principal(r), chi.URLParam(r, "id"), in.ChannelIDs)
	respond(w, r, 202, out, err)
}
