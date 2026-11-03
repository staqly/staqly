package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/staqly/staqly/apps/api/internal/app"
)

func init() {
	RegisterAuthedRoutes(func(s *Server, r chi.Router) { r.Post("/feedback", s.createProductFeedback) })
}

func (s *Server) createProductFeedback(w http.ResponseWriter, r *http.Request) {
	if !s.allowOutbound(w, r, s.feedback, "feedback requests") {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, app.MaxFeedbackAttachmentBytes+(64<<10))
	if err := r.ParseMultipartForm(app.MaxFeedbackAttachmentBytes + (64 << 10)); err != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			writeErr(w, r, &app.Error{Code: "payload_too_large", Status: 413, Message: "screenshot must be at most 5 MB"})
		} else {
			writeErr(w, r, &app.Error{Code: "bad_request", Status: 400, Message: "expected multipart/form-data"})
		}
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, _, err := r.FormFile("screenshot")
	if err != nil && !errors.Is(err, http.ErrMissingFile) {
		writeErr(w, r, &app.Error{Code: "bad_request", Status: 400, Message: "invalid screenshot"})
		return
	}
	var id string
	in := app.ProductFeedbackInput{Kind: r.FormValue("kind"), Description: r.FormValue("description"), PagePath: r.FormValue("page_path")}
	if file != nil {
		defer file.Close()
		id, err = s.svc.CreateProductFeedback(r.Context(), principal(r), in, file)
	} else {
		id, err = s.svc.CreateProductFeedback(r.Context(), principal(r), in, nil)
	}
	if err != nil {
		if ae, ok := err.(*app.Error); ok && ae.Status == 429 {
			w.Header().Set("Retry-After", "3600")
		}
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}
