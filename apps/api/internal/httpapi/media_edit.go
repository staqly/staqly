package httpapi

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// The design editor's endpoints: replace a file with its edit, editable projects, and the AI tools on the
// workspace's own provider key.
func init() {
	RegisterAuthedRoutes(func(s *Server, r chi.Router) {
		r.Post("/media/{id}/replace", s.replaceMedia)

		r.Get("/media-projects", s.listMediaProjects)
		r.Post("/media-projects", s.saveMediaProject)
		r.Get("/media-projects/{id}", s.getMediaProject)
		r.Put("/media-projects/{id}", s.saveMediaProject)
		r.Get("/media-projects/{id}/file", s.getMediaProjectFile)
		r.Delete("/media-projects/{id}", s.deleteMediaProject)

		r.Get("/ai", s.getAI)
		r.Post("/ai/jobs", s.createAIJob)
		r.Get("/ai/jobs/{id}", s.getAIJob)
		r.Delete("/ai/jobs/{id}", s.discardAIJob)
		r.Get("/ai/jobs/{id}/result", s.getAIJobResult)
		r.Post("/ai/jobs/{id}/accept", s.acceptAIJob)

		r.Get("/design/brand", s.getDesignBrand)
		r.Put("/design/brand", s.setDesignBrand)

		r.Get("/stock/photos", s.stockPhotos)
		r.Post("/stock/photos/{id}/use", s.useStockPhoto)
	})
}

// multipartFields reads the small form fields of a multipart body and hands the file field ("file") to
// withFile as it streams; fields after the file are not read (send them first).
func multipartFields(w http.ResponseWriter, r *http.Request, max int64, withFile func(fields map[string]string, name string, file io.Reader) error) error {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	mr, err := r.MultipartReader()
	if err != nil {
		return &app.Error{Code: "bad_request", Status: 400, Message: "expected multipart/form-data with a file field"}
	}
	fields := map[string]string{}
	for {
		part, err := mr.NextPart()
		if err != nil {
			return &app.Error{Code: "bad_request", Status: 400, Message: "missing file field"}
		}
		if part.FormName() == "file" {
			return withFile(fields, part.FileName(), part)
		}
		v, err := io.ReadAll(io.LimitReader(part, 64<<10))
		if err != nil {
			return err
		}
		fields[part.FormName()] = string(v)
	}
}

func (s *Server) replaceMedia(w http.ResponseWriter, r *http.Request) {
	var out app.Media
	err := multipartFields(w, r, s.svc.Cfg.MaxUploadBytes+(1<<20), func(_ map[string]string, name string, f io.Reader) error {
		var err error
		out, err = s.svc.ReplaceMedia(r.Context(), principal(r), chi.URLParam(r, "id"), name, f)
		return err
	})
	respond(w, r, 201, out, err)
}

func (s *Server) listMediaProjects(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListMediaProjects(r.Context(), principal(r), r.URL.Query().Get("media"))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) getMediaProject(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetMediaProject(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) saveMediaProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var out app.MediaProject
	err := multipartFields(w, r, s.svc.Cfg.MaxUploadBytes+(1<<20), func(fields map[string]string, _ string, f io.Reader) error {
		ids := func(field string) []string {
			var out []string
			for _, v := range strings.Split(fields[field], ",") {
				if v = strings.TrimSpace(v); v != "" {
					out = append(out, v)
				}
			}
			return out
		}
		var err error
		out, err = s.svc.SaveMediaProject(r.Context(), principal(r), id, app.MediaProjectInput{Name: fields["name"],
			SourceIDs: ids("source_ids"), OutputID: strings.TrimSpace(fields["output_id"]), PageIDs: ids("page_ids")}, f)
		return err
	})
	status := 201
	if id != "" {
		status = 200
	}
	respond(w, r, status, out, err)
}

func (s *Server) getMediaProjectFile(w http.ResponseWriter, r *http.Request) {
	f, p, err := s.svc.OpenMediaProject(r.Context(), principal(r), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer f.Close()
	h := w.Header()
	// ponytail: projects from before 2026-10-25 are .gpex files and download under this name too.
	h.Set("Content-Type", "application/json")
	h.Set("Content-Disposition", `attachment; filename="design.json"`)
	h.Set("Cache-Control", "private, no-cache")
	modified, _ := time.Parse(time.RFC3339Nano, p.UpdatedAt)
	http.ServeContent(w, r, "design.json", modified, f)
}

func (s *Server) deleteMediaProject(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.DeleteMediaProject(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) getDesignBrand(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetDesignBrand(r.Context(), principal(r))
	respond(w, r, 200, out, err)
}

func (s *Server) setDesignBrand(w http.ResponseWriter, r *http.Request) {
	var in app.DesignBrand
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.SetDesignBrand(r.Context(), principal(r), in)
	respond(w, r, 200, out, err)
}

func (s *Server) getAI(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetAIStatus(r.Context(), principal(r))
	respond(w, r, 200, out, err)
}

// createAIJob: POST /v1/ai/jobs, multipart/form-data (the operation and its settings, then the image and mask
// files). Answers 202 with the queued job, or 200 with the earlier job a repeated Idempotency-Key or a request
// still running answers (multipart bodies skip the idempotency middleware: the service matches the key itself).
func (s *Server) createAIJob(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2*app.MaxAIImageBytes+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeErr(w, r, &app.Error{Code: "bad_request", Status: 400, Message: "expected multipart/form-data with an operation"})
		return
	}
	defer r.MultipartForm.RemoveAll()
	read := func(name string) ([]byte, error) {
		f, _, err := r.FormFile(name)
		if err == http.ErrMissingFile {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(io.LimitReader(f, app.MaxAIImageBytes+1))
	}
	in := app.AIJobInput{Operation: r.FormValue("operation"), Prompt: r.FormValue("prompt"),
		AspectRatio: r.FormValue("aspect_ratio"), IdempotencyKey: r.Header.Get("Idempotency-Key")}
	for name, dst := range map[string]*int{"width": &in.Width, "height": &in.Height, "duration": &in.Duration,
		"canvas_width": &in.CanvasWidth, "canvas_height": &in.CanvasHeight, "image_x": &in.ImageX, "image_y": &in.ImageY,
		"place_width": &in.PlaceWidth, "place_height": &in.PlaceHeight} {
		if v := r.FormValue(name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				writeErr(w, r, &app.Error{Code: "validation_failed", Status: 422, Message: name + ": must be a whole number", Fields: map[string]string{name: "must be a whole number"}})
				return
			}
			*dst = n
		}
	}
	var err error
	if in.Image, err = read("image"); err != nil {
		writeErr(w, r, err)
		return
	}
	if in.Mask, err = read("mask"); err != nil {
		writeErr(w, r, err)
		return
	}
	job, created, err := s.svc.CreateAIJob(r.Context(), principal(r), in)
	status := 202
	if !created {
		status = 200
	}
	respond(w, r, status, job, err)
}

func (s *Server) getAIJob(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetAIJob(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) discardAIJob(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.DiscardAIJob(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) acceptAIJob(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.AcceptAIJob(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 201, out, err)
}

// getAIJobResult serves the made file for the preview (never cached, sandboxed like library files).
func (s *Server) getAIJobResult(w http.ResponseWriter, r *http.Request) {
	f, ct, err := s.svc.ReadAIJobResult(r.Context(), principal(r), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Cache-Control", "private, no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", time.Time{}, f)
}

func (s *Server) stockPhotos(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	out, err := s.svc.StockPhotos(r.Context(), principal(r), r.URL.Query().Get("query"), page)
	respond(w, r, 200, out, err)
}

func (s *Server) useStockPhoto(w http.ResponseWriter, r *http.Request) {
	err := s.svc.UseStockPhoto(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 204, nil, err)
}
