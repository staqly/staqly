package httpapi

import (
	"bufio"
	"encoding/json"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/logctx"
)

func init() {
	RegisterAuthedRoutes(func(s *Server, r chi.Router) {
		r.Get("/events/export", s.exportEvents)
		r.Post("/workspace/export", s.requestWorkspaceExport)
		r.Get("/workspace/exports/{id}", s.getWorkspaceExport)
		r.Get("/workspace/exports/{id}/download", s.downloadWorkspaceExport)
		r.Delete("/workspace", s.deleteWorkspace)
		r.Post("/items/{id}/purge", s.purgeItem)
	})
}

// exportEvents streams the activity log as NDJSON or CSV, page by page.
func (s *Server) exportEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	x, err := s.svc.PrepareEventExport(r.Context(), principal(r), app.EventExportFilter{
		Format: q.Get("format"), From: q.Get("from"), To: q.Get("to"), Type: q.Get("type"), TypePrefix: q.Get("type_prefix"),
		ActorType: q.Get("actor_type"), ActorID: q.Get("actor_id"), CollectionID: q.Get("collection_id"), ItemID: q.Get("item_id"),
	})
	if err != nil {
		writeErr(w, r, err)
		return
	}
	name := "staqly-events-" + time.Now().UTC().Format("20060102-150405")
	h := w.Header()
	h.Set("X-Accel-Buffering", "no")
	bw := bufio.NewWriterSize(w, 32<<10)
	var each func(app.Event) error
	var finish func() error
	if x.Format == "csv" {
		h.Set("Content-Type", "text/csv; charset=utf-8")
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".csv"}))
		cw := app.NewEventCSVWriter(bw)
		_ = cw.Write(app.EventCSVHeader)
		each = func(e app.Event) error { return cw.Write(app.EventCSVRecord(e)) }
		finish = func() error { cw.Flush(); return cw.Error() }
	} else {
		h.Set("Content-Type", "application/x-ndjson")
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".ndjson"}))
		enc := json.NewEncoder(bw)
		each = func(e app.Event) error { return enc.Encode(e) }
		finish = func() error { return nil }
	}
	w.WriteHeader(200)
	rc := http.NewResponseController(w)
	n := 0
	err = x.Each(r.Context(), func(e app.Event) error {
		if err := each(e); err != nil {
			return err
		}
		if n++; n%500 == 0 { // push each page to the client
			if err := finish(); err != nil {
				return err
			}
			if err := bw.Flush(); err != nil {
				return err
			}
			_ = rc.Flush()
		}
		return nil
	})
	if err == nil {
		if err = finish(); err == nil {
			err = bw.Flush()
		}
	}
	if err != nil {
		// Headers are gone; the truncated body (no final newline) tells the client.
		logctx.FromContext(r.Context()).Warn("event export aborted", "err", err)
		return
	}
	_ = rc.Flush()
}

func (s *Server) requestWorkspaceExport(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.RequestWorkspaceExport(r.Context(), principal(r))
	respond(w, r, 202, out, err)
}

func (s *Server) getWorkspaceExport(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetWorkspaceExport(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) downloadWorkspaceExport(w http.ResponseWriter, r *http.Request) {
	f, x, err := s.svc.OpenWorkspaceExport(r.Context(), principal(r), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	stamp := strings.NewReplacer("-", "", ":", "", "T", "-").Replace(x.CreatedAt)
	if len(stamp) > 15 {
		stamp = stamp[:15]
	}
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": "staqly-export-" + stamp + ".zip"}))
	t, _ := time.Parse(time.RFC3339Nano, x.CreatedAt)
	http.ServeContent(w, r, "", t, f)
}

func (s *Server) deleteWorkspace(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirm string `json:"confirm"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	if err := s.svc.DeleteWorkspace(r.Context(), principal(r), in.Confirm); err != nil {
		writeErr(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(204)
}

func (s *Server) purgeItem(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.PurgeItem(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}
