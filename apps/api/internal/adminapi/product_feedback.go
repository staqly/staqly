package adminapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func (s *server) listProductFeedback(w http.ResponseWriter, r *http.Request) {
	page, err := s.svc.AdminListProductFeedback(r.Context(), adminOf(r), r.URL.Query().Get("cursor"))
	s.reply(w, page, err)
}
func (s *server) setProductFeedbackStatus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	s.done(w, s.svc.AdminSetProductFeedbackStatus(r.Context(), adminOf(r), chi.URLParam(r, "id"), in.Status))
}
func (s *server) productFeedbackAttachment(w http.ResponseWriter, r *http.Request) {
	f, ct, err := s.svc.AdminOpenProductFeedbackAttachment(r.Context(), adminOf(r), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, s.log, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", `attachment; filename="screenshot"`)
	http.ServeContent(w, r, "screenshot", time.Time{}, f)
}
