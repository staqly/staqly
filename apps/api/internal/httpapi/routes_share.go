package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Sharing a post for feedback (preview links, reviewer comments) and a post's numbers per channel.

// previewComments limits reviewer comments per client IP (each link also allows 30 an hour).
var previewComments = newRateLimiter(20, 10*time.Minute)

func init() {
	RegisterAuthedRoutes(func(s *Server, r chi.Router) {
		r.Get("/posts/{id}/metrics", s.postMetrics)
		r.Put("/posts/{id}/targets/{channel}/metrics", s.reportTargetMetrics)
		r.Get("/posts/{id}/previews", s.listPreviewLinks)
		r.Post("/posts/{id}/previews", s.createPreviewLink)
		r.Delete("/posts/{id}/previews/{preview}", s.revokePreviewLink)
		r.Get("/posts/{id}/comments", s.listPostComments)
		r.Post("/posts/{id}/comments", s.addPostComment)
		r.Post("/posts/{id}/comments/{comment}/resolve", s.resolvePostComment(true))
		r.Post("/posts/{id}/comments/{comment}/reopen", s.resolvePostComment(false))
		r.Delete("/posts/{id}/comments/{comment}", s.deletePostComment)
	})
	// The preview page is public: the token in the path is the credential.
	RegisterPublicRoutes(func(s *Server, r chi.Router) {
		r.Get("/previews/{token}", s.openPreview)
		r.Post("/previews/{token}/comments", s.addPreviewComment)
		r.Post("/previews/{token}/comments/{comment}/resolve", s.resolvePreviewComment(true))
		r.Post("/previews/{token}/comments/{comment}/reopen", s.resolvePreviewComment(false))
		r.Get("/previews/{token}/media/{media}", s.previewMedia)
	})
}

func (s *Server) postMetrics(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.PostMetrics(r.Context(), principal(r), chi.URLParam(r, "id"), r.URL.Query().Get("refresh") == "true")
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) reportTargetMetrics(w http.ResponseWriter, r *http.Request) {
	var in providers.Metrics
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.ReportTargetMetrics(r.Context(), principal(r), chi.URLParam(r, "id"), chi.URLParam(r, "channel"), in)
	respond(w, r, 200, out, err)
}

func (s *Server) listPreviewLinks(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListPreviewLinks(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) createPreviewLink(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	var in app.PreviewLinkInput
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	origin := r.Header.Get("Origin")
	if origin == "" && r.Header.Get("X-Forwarded-Host") != "" {
		origin = "http://" + r.Header.Get("X-Forwarded-Host")
	}
	out, err := s.svc.CreatePreviewLink(r.Context(), principal(r), chi.URLParam(r, "id"), in, origin)
	respond(w, r, 201, out, err)
}

func (s *Server) revokePreviewLink(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.RevokePreviewLink(r.Context(), principal(r), chi.URLParam(r, "id"), chi.URLParam(r, "preview")))
}

func (s *Server) listPostComments(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListPostComments(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) addPostComment(w http.ResponseWriter, r *http.Request) {
	var in app.PostCommentInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.AddPostComment(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, r, 201, out, err)
}

func (s *Server) resolvePostComment(resolved bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := s.svc.SetPostCommentResolved(r.Context(), principal(r), chi.URLParam(r, "id"), chi.URLParam(r, "comment"), resolved)
		respond(w, r, 200, out, err)
	}
}

// resolvePreviewComment: a reviewer resolves or reopens a thread (counted with their comments per address).
func (s *Server) resolvePreviewComment(resolved bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !previewComments.allow(s.clientIP(r)) {
			w.Header().Set("Retry-After", "600")
			writeErr(w, r, &app.Error{Code: "rate_limited", Status: 429, Message: "too many changes from this address: try again later"})
			return
		}
		var in app.ResolveInput
		if r.ContentLength != 0 && !decode(w, r, &in) {
			return
		}
		out, err := s.svc.SetPreviewCommentResolved(r.Context(), chi.URLParam(r, "token"), chi.URLParam(r, "comment"), in, resolved)
		respond(w, r, 200, out, err)
	}
}

func (s *Server) deletePostComment(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.DeletePostComment(r.Context(), principal(r), chi.URLParam(r, "id"), chi.URLParam(r, "comment")))
}

func (s *Server) openPreview(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	w.Header().Set("Cache-Control", "no-store")
	out, err := s.svc.OpenPreview(r.Context(), chi.URLParam(r, "token"))
	respond(w, r, 200, out, err)
}

func (s *Server) addPreviewComment(w http.ResponseWriter, r *http.Request) {
	if !previewComments.allow(s.clientIP(r)) {
		w.Header().Set("Retry-After", "600")
		writeErr(w, r, &app.Error{Code: "rate_limited", Status: 429, Message: "too many comments from this address: try again later"})
		return
	}
	var in app.PreviewCommentInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.AddPreviewComment(r.Context(), chi.URLParam(r, "token"), in)
	respond(w, r, 201, out, err)
}

func (s *Server) previewMedia(w http.ResponseWriter, r *http.Request) {
	f, m, err := s.svc.OpenPreviewMedia(r.Context(), chi.URLParam(r, "token"), chi.URLParam(r, "media"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", m.MimeType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	serveMediaFile(w, r, f, m)
}

// redactPreviewToken keeps preview tokens out of request logs: /v1/previews/<token>/... is logged with the token
// replaced.
func redactPreviewToken(path string) string {
	const prefix = "/v1/previews/"
	if !strings.HasPrefix(path, prefix) {
		return path
	}
	rest := path[len(prefix):]
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return prefix + "REDACTED" + rest[i:]
	}
	return prefix + "REDACTED"
}
