package httpapi

import (
	"errors"
	"io"
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
	r.Body = http.MaxBytesReader(w, r.Body, app.MaxFeedbackAttachments*app.MaxFeedbackAttachmentBytes+(64<<10))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			writeErr(w, r, &app.Error{Code: "payload_too_large", Status: 413, Message: "attach at most 10 images of up to 5 MB each"})
		} else {
			writeErr(w, r, &app.Error{Code: "bad_request", Status: 400, Message: "expected multipart/form-data"})
		}
		return
	}
	defer r.MultipartForm.RemoveAll()
	headers := r.MultipartForm.File["screenshot"]
	if len(headers) > app.MaxFeedbackAttachments {
		writeErr(w, r, &app.Error{Code: "validation_failed", Status: 422, Message: "attach at most 10 images"})
		return
	}
	files := make([]io.Reader, 0, len(headers))
	for _, header := range headers {
		file, err := header.Open()
		if err != nil {
			writeErr(w, r, &app.Error{Code: "bad_request", Status: 400, Message: "invalid screenshot"})
			return
		}
		defer file.Close()
		files = append(files, file)
	}
	in := app.ProductFeedbackInput{Kind: r.FormValue("kind"), Description: r.FormValue("description"), PagePath: r.FormValue("page_path")}
	id, err := s.svc.CreateProductFeedback(r.Context(), principal(r), in, files)
	if err != nil {
		if ae, ok := err.(*app.Error); ok && ae.Status == 429 {
			w.Header().Set("Retry-After", "3600")
		}
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}
