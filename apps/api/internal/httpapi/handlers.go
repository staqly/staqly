package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

func sqlInt(v int) sql.NullInt64 { return sql.NullInt64{Int64: int64(v), Valid: true} }

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.DB.PingContext(r.Context()); err != nil {
		writeJSON(w, 503, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok", "version": s.cfg.Version})
}

func (s *Server) setCookie(w http.ResponseWriter, ns app.NewSession) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: ns.Token, Path: "/", Expires: ns.ExpiresAt,
		HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
}

// ---- setup & sessions ----

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	ok, err := s.svc.Initialized(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	cloud := s.svc.Cfg.Mode == app.ModeCloud
	out := map[string]any{"initialized": ok, "mode": s.svc.Cfg.Mode,
		"signup_open": s.svc.SignupOpen(), "setup_token_required": cloud && !s.svc.SignupOpen(),
		// Paid plans can be bought here (Stripe configured); the sign-up page words its pricing by it.
		"billing_enabled": s.svc.BillingEnabled(),
		// Cloud accounts: the sign-in page shows the methods this server has (Google, an email code), and
		// the sign-in from before accounts (API key, recovery key) only where it still works.
		"google_sign_in": s.svc.GoogleSignIn(), "email_sign_in": s.svc.EmailSignIn(), "legacy_sign_in": s.svc.LegacySignIn(),
		"passkey_sign_in": s.svc.PasskeySignIn(),
		// The dashboard shows it in snippets; read here so a pre-built web
		// image needs no build-time URL.
		"public_api_url": strings.TrimRight(s.svc.Cfg.PublicURL, "/")}
	if ph := s.svc.Cfg.PostHog; ph.Key != "" {
		// The dashboard's page views, web vitals and errors go to the same PostHog project (cookieless). The
		// project token only sends events; nothing that reads data ever reaches the browser.
		out["analytics"] = map[string]any{"key": ph.Key, "host": ph.Host, "environment": ph.Environment, "release": s.cfg.Version}
	}
	writeJSON(w, 200, out)
}

func (s *Server) listPlans(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"data": s.svc.LivePlans(r.Context())})
}

func (s *Server) getUsage(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetUsage(r.Context(), principal(r))
	respond(w, r, 200, out, err)
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name       string `json:"name"`
		SetupToken string `json:"setup_token"`
	}
	if !decode(w, r, &in) {
		return
	}
	if s.svc.Cfg.Mode == app.ModeCloud && !s.signups.allow(s.clientIP(r)) {
		w.Header().Set("Retry-After", "3600")
		writeErr(w, r, &app.Error{Code: "rate_limited", Status: 429, Message: "too many workspaces created from this address; try again later"})
		return
	}
	res, err := s.svc.Setup(r.Context(), in.Name, in.SetupToken, r.UserAgent())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.setCookie(w, res.Session)
	writeJSON(w, 201, map[string]any{"workspace": res.Workspace, "api_key": res.APIKey, "recovery_key": res.RecoveryKey,
		"csrf_token": res.Session.CSRFToken})
}

func (s *Server) recover(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RecoveryKey string `json:"recovery_key"`
	}
	if !decode(w, r, &in) {
		return
	}
	res, err := s.svc.Recover(r.Context(), in.RecoveryKey, r.UserAgent())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.setCookie(w, res.Session)
	writeJSON(w, 201, map[string]any{"csrf_token": res.Session.CSRFToken, "api_key": res.APIKey, "recovery_key": res.RecoveryKey})
}

// signInWithAPIKey: the cloud dashboard's sign-in. The key travels in the body
// (an Authorization header would authenticate the request itself).
func (s *Server) signInWithAPIKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		APIKey string `json:"api_key"`
	}
	if !decode(w, r, &in) {
		return
	}
	ns, err := s.svc.SignInWithAPIKey(r.Context(), in.APIKey, r.UserAgent())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.setCookie(w, ns)
	writeJSON(w, 201, map[string]any{"csrf_token": ns.CSRFToken})
}

func (s *Server) createDeviceLink(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	origin := r.Header.Get("Origin")
	if origin == "" && r.Header.Get("X-Forwarded-Host") != "" {
		origin = "http://" + r.Header.Get("X-Forwarded-Host")
	}
	out, err := s.svc.CreateDeviceLink(r.Context(), principal(r), origin)
	respond(w, r, 201, out, err)
}

func (s *Server) redeemDeviceLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &in) {
		return
	}
	current := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		current = c.Value
	}
	ns, err := s.svc.RedeemDeviceLink(r.Context(), in.Token, r.UserAgent(), current)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.setCookie(w, ns)
	writeJSON(w, 201, map[string]any{"csrf_token": ns.CSRFToken})
}

func (s *Server) currentSession(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	ws, err := s.svc.GetWorkspace(r.Context(), p)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	csrf, _ := r.Context().Value(csrfKey).(string)
	scopes := p.Scopes
	if p.Has(app.ScopeAdmin) {
		scopes = app.AllScopes
	}
	workspaces, err := s.svc.ListMyWorkspaces(r.Context(), p)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	role := p.Role
	if role == "" && p.Has(app.ScopeAdmin) {
		role = app.RoleOwner // local mode, sessions from before accounts, admin keys: everything here
	}
	// defaults: server values the dashboard names ("Default (1 h)" in the collection's publish timeout).
	writeJSON(w, 200, map[string]any{"mode": s.svc.Cfg.Mode, "workspace": ws, "principal": map[string]any{"type": p.Kind, "id": p.ID, "name": p.Name,
		"scopes": scopes}, "csrf_token": csrf,
		// The person (sidebar), their role here, and the workspaces they can switch to. A development
		// server's API key session (no person) keeps the dashboard from before accounts.
		"user": s.svc.CurrentUser(r.Context(), p), "role": role, "guest": p.Guest,
		"accounts_enabled": s.svc.AccountsEnabled() && (p.UserID != "" || !s.svc.LegacySignIn()), "workspaces": workspaces,
		// false sends the dashboard to the welcome wizard once (POST /v1/session/onboarded ends it).
		"onboarded": s.svc.Onboarded(r.Context(), p),
		"defaults":  map[string]any{"processing_timeout_seconds": s.svc.ServerProcessingTimeoutSeconds()}})
}

func (s *Server) finishOnboarding(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.svc.FinishOnboarding(r.Context(), principal(r), in.Name); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.Logout(r.Context(), principal(r)); err != nil {
		writeErr(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(204)
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListSessions(r.Context(), principal(r))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.RevokeSession(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) getWorkspace(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetWorkspace(r.Context(), principal(r))
	respond(w, r, 200, out, err)
}

func (s *Server) renameWorkspace(w http.ResponseWriter, r *http.Request) {
	var in app.WorkspaceUpdate
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateWorkspace(r.Context(), principal(r), in)
	respond(w, r, 200, out, err)
}

func (s *Server) rotateRecovery(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	key, err := s.svc.RotateRecoveryKey(r.Context(), principal(r))
	respond(w, r, 200, map[string]string{"recovery_key": key}, err)
}

// ---- api keys ----

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListAPIKeys(r.Context(), principal(r))
	respond(w, r, 200, map[string]any{"data": out, "available_scopes": principal(r).GrantableKeyScopes()}, err)
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	var in app.CreateAPIKeyInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateAPIKey(r.Context(), principal(r), in)
	respond(w, r, 201, out, err)
}

func (s *Server) revokeKey(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.RevokeAPIKey(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) rotateKey(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	out, err := s.svc.RotateAPIKey(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 201, out, err)
}

// ---- collections ----

func (s *Server) listCollections(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListCollections(r.Context(), principal(r), r.URL.Query().Get("include_archived") == "true")
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) createCollection(w http.ResponseWriter, r *http.Request) {
	var in app.CollectionInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateCollection(r.Context(), principal(r), in)
	respond(w, r, 201, out, err)
}

// collectionRef is the {id} segment of /v1/collections/{id}/...: an ID or a name. When the
// client escaped characters Go keeps escaped (a name with "/", "&" or "+"), the URL has a
// RawPath, chi routes on it and hands the segment over still escaped, so unescape it here;
// without a RawPath the segment is already decoded and must not be decoded twice.
func collectionRef(r *http.Request) string {
	v := chi.URLParam(r, "id")
	if r.URL.RawPath == "" {
		return v
	}
	if u, err := url.PathUnescape(v); err == nil {
		return u
	}
	return v
}

func (s *Server) getCollection(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.FindCollection(r.Context(), principal(r), collectionRef(r))
	respond(w, r, 200, out, err)
}

func (s *Server) updateCollection(w http.ResponseWriter, r *http.Request) {
	var in app.CollectionInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateCollection(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, r, 200, out, err)
}

func (s *Server) deleteCollection(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.DeleteCollection(r.Context(), principal(r), chi.URLParam(r, "id"), r.URL.Query().Get("confirm")))
}

// ---- media ----

func (s *Server) uploadMedia(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.svc.Cfg.MaxUploadBytes+(1<<20))
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, r, &app.Error{Code: "bad_request", Status: 400, Message: "expected multipart/form-data with a file field"})
		return
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			writeErr(w, r, &app.Error{Code: "bad_request", Status: 400, Message: "missing file field"})
			return
		}
		if part.FormName() == "file" {
			out, err := s.svc.UploadMedia(r.Context(), principal(r), part.FileName(), part)
			respond(w, r, 201, out, err)
			return
		}
	}
}

func (s *Server) getMedia(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Has("w") {
		s.getMediaThumb(w, r)
		return
	}
	f, m, err := s.svc.OpenMedia(r.Context(), principal(r), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", m.MimeType)
	h.Set("Cache-Control", "private, max-age=3600")
	serveMediaFile(w, r, f, m)
}

// getMediaThumb serves ?w=160|320|640|1280: a cached, scaled-down image.
// Media ids never change content, so it is cacheable forever.
func (s *Server) getMediaThumb(w http.ResponseWriter, r *http.Request) {
	width, err := strconv.Atoi(r.URL.Query().Get("w"))
	if err != nil {
		width = -1 // MediaThumb rejects it with the list of allowed widths
	}
	id := chi.URLParam(r, "id")
	th, err := s.svc.MediaThumb(r.Context(), principal(r), id, width)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer th.File.Close()
	h := w.Header()
	h.Set("Content-Type", th.MimeType)
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	h.Set("ETag", `"`+id+"-w"+strconv.Itoa(width)+`"`)
	serveMediaFile(w, r, th.File, th.Media)
}

func serveMediaFile(w http.ResponseWriter, r *http.Request, f io.ReadSeeker, m db.Medium) {
	h := w.Header()
	// Sandbox anything rendered from uploads; only previewable types go inline.
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'; sandbox")
	disp := "attachment"
	previewable := strings.HasPrefix(m.MimeType, "image/") || strings.HasPrefix(m.MimeType, "video/") ||
		strings.HasPrefix(m.MimeType, "audio/") || m.MimeType == "application/pdf" || m.MimeType == "text/plain"
	if previewable && r.URL.Query().Get("download") == "" {
		disp = "inline"
	}
	h.Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": m.OriginalName}))
	t, _ := time.Parse(time.RFC3339Nano, m.CreatedAt)
	http.ServeContent(w, r, "", t, f) // handles Range for video seeking
}

func (s *Server) deleteMedia(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("permanent") == "true" {
		respondEmpty(w, r, s.svc.DeleteMedia(r.Context(), principal(r), chi.URLParam(r, "id")))
		return
	}
	respondEmpty(w, r, s.svc.TrashMedia(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) restoreMedia(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.RestoreMedia(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

// keepMedia keeps this file of a set of identical ones; the copies go to Deleted, their posts use this one.
func (s *Server) keepMedia(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.KeepMedia(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, map[string]any{"deleted": out}, err)
}

// listMedia is the media library: ?q=, kind=image|video|audio, used=used|unused, folder=<id>|root, tag=, sort=,
// offset=, limit=.
func (s *Server) listMedia(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := s.svc.ListMedia(r.Context(), principal(r), app.MediaFilter{Query: q.Get("q"), Kind: q.Get("kind"),
		Used: q.Get("used"), Folder: q.Get("folder"), Tag: q.Get("tag"), Deleted: q.Get("deleted") == "true",
		Duplicates: q.Get("duplicates") == "true", Sort: q.Get("sort"), Offset: qInt(r, "offset"),
		Limit: qInt(r, "limit")})
	respond(w, r, 200, out, err)
}

func (s *Server) listMediaFolders(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListMediaFolders(r.Context(), principal(r))
	respond(w, r, 200, out, err)
}

func (s *Server) createMediaFolder(w http.ResponseWriter, r *http.Request) {
	var in app.MediaFolderInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateMediaFolder(r.Context(), principal(r), in)
	respond(w, r, 201, out, err)
}

func (s *Server) updateMediaFolder(w http.ResponseWriter, r *http.Request) {
	var in app.MediaFolderInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateMediaFolder(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, r, 200, out, err)
}

func (s *Server) deleteMediaFolder(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.DeleteMediaFolder(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) bulkUpdateMedia(w http.ResponseWriter, r *http.Request) {
	var in app.MediaBulk
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.BulkUpdateMedia(r.Context(), principal(r), in)
	respond(w, r, 200, out, err)
}

func (s *Server) getMediaInfo(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetMediaInfo(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) mediaUsage(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.MediaUsage(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) updateMedia(w http.ResponseWriter, r *http.Request) {
	var in app.MediaPatch
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateMedia(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, r, 200, out, err)
}

func (s *Server) deletePosts(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.DeletePosts(r.Context(), principal(r), in.IDs)
	respond(w, r, 200, out, err)
}

func (s *Server) deleteMediaMany(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs       []string `json:"ids"`
		Permanent bool     `json:"permanent"`
	}
	if !decode(w, r, &in) {
		return
	}
	deleted, missing, err := s.svc.DeleteMediaMany(r.Context(), principal(r), in.IDs, in.Permanent)
	respond(w, r, 200, map[string]any{"deleted": deleted, "missing": missing}, err)
}

// ---- events ----

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := s.svc.ListEvents(r.Context(), principal(r), app.EventFilter{PostID: q.Get("post_id"),
		CollectionID: q.Get("collection_id"), ChannelID: q.Get("channel_id"), ScheduleID: q.Get("schedule_id"),
		Type: q.Get("type"), ActorType: q.Get("actor_type"),
		ActorID: q.Get("actor_id"), Agents: q["agent"], Types: q["types"], TypePrefix: q.Get("type_prefix"),
		From: firstOf(q, "since", "from"), To: firstOf(q, "until", "to"),
		Before: q.Get("cursor"), Limit: qInt(r, "limit")})
	respond(w, r, 200, out, err)
}

// ---- webhooks ----

func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListWebhooks(r.Context(), principal(r))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request) {
	noStore(r) // the response carries the signing secret
	var in app.WebhookInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateWebhook(r.Context(), principal(r), in)
	respond(w, r, 201, out, err)
}

func (s *Server) getWebhook(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetWebhook(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) updateWebhook(w http.ResponseWriter, r *http.Request) {
	var in app.WebhookInput
	if !decode(w, r, &in) {
		return
	}
	if in.RotateSecret {
		noStore(r)
	}
	out, err := s.svc.UpdateWebhook(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, r, 200, out, err)
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.DeleteWebhook(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) testWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.allowTest(w, r) {
		return
	}
	out, err := s.svc.TestWebhook(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 202, out, err)
}

func (s *Server) listDeliveries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := s.svc.ListDeliveries(r.Context(), principal(r), q.Get("webhook_id"), q.Get("status"), q.Get("post_id"), qInt(r, "limit"), q.Get("cursor"))
	respond(w, r, 200, out, err)
}

func (s *Server) retryDelivery(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.RetryDelivery(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 202, out, err)
}

// ---- helpers ----

func respond(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, status, v)
}

func respondEmpty(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) listTags(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListTags(r.Context(), principal(r))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListAgents(r.Context(), principal(r))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) forgetAgent(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.ForgetAgent(r.Context(), principal(r), chi.URLParam(r, "id")))
}

// seenAgent records a REST call from an agent (MCP over HTTP records itself with the client's own name) and
// tags the request so its events name the agent. Browsers are people, not agents.
func seenAgent(svc *app.Service, r *http.Request) context.Context {
	path := r.URL.Path
	if !strings.HasPrefix(path, "/v1/") || strings.HasPrefix(r.UserAgent(), "Mozilla/") {
		return r.Context()
	}
	name, version := app.AgentFromUserAgent(r.UserAgent())
	created := r.Method == http.MethodPost && path == "/v1/posts"
	svc.SeenAgent(r.Context(), PrincipalFrom(r.Context()), "api", name, version, created)
	return app.WithAgent(r.Context(), "api", name)
}

// firstOf returns the first non-empty query value among names.
func firstOf(q url.Values, names ...string) string {
	for _, n := range names {
		if v := q.Get(n); v != "" {
			return v
		}
	}
	return ""
}

// ---- posts ----

func postFilter(r *http.Request) app.PostFilter {
	q := r.URL.Query()
	return app.PostFilter{CollectionID: q.Get("collection_id"), Status: q.Get("status"), ChannelID: q.Get("channel_id"), Q: q.Get("q"),
		Tag: q.Get("tag"), PublishAfter: q.Get("publish_after"), PublishBefore: q.Get("publish_before"), Sort: q.Get("sort"),
		HidePast: q.Get("hide_past") == "true", Limit: qInt(r, "limit"), Cursor: q.Get("cursor")}
}

func (s *Server) listPosts(w http.ResponseWriter, r *http.Request) {
	f := postFilter(r)
	if chi.URLParam(r, "id") != "" { // GET /collections/{id}/posts
		f.CollectionID = collectionRef(r)
	}
	if f.CollectionID != "" {
		col, err := s.svc.PostsCollection(r.Context(), principal(r), f.CollectionID)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		f.CollectionID = col.ID
	}
	out, err := s.svc.ListPosts(r.Context(), principal(r), f)
	respond(w, r, 200, out, err)
}

func (s *Server) createPost(w http.ResponseWriter, r *http.Request) {
	var in app.PostInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreatePost(r.Context(), principal(r), in)
	respond(w, r, 201, out, err)
}

func (s *Server) getPost(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetPost(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) updatePost(w http.ResponseWriter, r *http.Request) {
	var in app.PostPatch
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UpdatePost(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, r, 200, out, err)
}

func (s *Server) deletePost(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.DeletePost(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) schedulePost(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PublishAt string `json:"publish_at"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.SchedulePost(r.Context(), principal(r), chi.URLParam(r, "id"), in.PublishAt)
	respond(w, r, 200, out, err)
}

func (s *Server) publishPost(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.PublishPost(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 202, out, err)
}

func (s *Server) cancelPost(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.CancelPost(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) duplicatePost(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.DuplicatePost(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 201, out, err)
}

func (s *Server) postEvents(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListEvents(r.Context(), principal(r), app.EventFilter{PostID: chi.URLParam(r, "id"),
		Before: r.URL.Query().Get("cursor"), Limit: qInt(r, "limit")})
	respond(w, r, 200, out, err)
}

func (s *Server) previewPost(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.PreviewPost(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) reportTargetResult(w http.ResponseWriter, r *http.Request) {
	var in app.ResultInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.ReportTargetResult(r.Context(), principal(r), chi.URLParam(r, "id"), chi.URLParam(r, "channel"), in)
	respond(w, r, 200, out, err)
}

func (s *Server) retryTarget(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.RetryTarget(r.Context(), principal(r), chi.URLParam(r, "id"), chi.URLParam(r, "channel"))
	respond(w, r, 202, out, err)
}

func (s *Server) listWork(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListRequestedWork(r.Context(), principal(r), qInt(r, "limit"))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

// ---- providers & channels ----

func (s *Server) listProviders(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListProviders(r.Context(), principal(r))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) setProviderApp(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	var in providers.App
	if !decode(w, r, &in) {
		return
	}
	respondEmpty(w, r, s.svc.SetProviderApp(r.Context(), principal(r), chi.URLParam(r, "id"), in))
}

func (s *Server) deleteProviderApp(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.DeleteProviderApp(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) checkProvider(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.CheckProvider(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := s.svc.ListChannels(r.Context(), principal(r), app.ChannelFilter{Group: q.Get("group")})
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) connectChannel(w http.ResponseWriter, r *http.Request) {
	noStore(r) // the request carried credentials
	var in app.ChannelInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.ConnectChannel(r.Context(), principal(r), in)
	respond(w, r, 201, out, err)
}

func (s *Server) getChannel(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetChannel(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	var in app.ChannelPatch
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateChannel(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, r, 200, out, err)
}

func (s *Server) listChannelGroups(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListChannelGroups(r.Context(), principal(r))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) createChannelGroup(w http.ResponseWriter, r *http.Request) {
	var in app.ChannelGroupInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateChannelGroup(r.Context(), principal(r), in)
	respond(w, r, 201, out, err)
}

func (s *Server) updateChannelGroup(w http.ResponseWriter, r *http.Request) {
	var in app.ChannelGroupInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateChannelGroup(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, r, 200, out, err)
}

func (s *Server) deleteChannelGroup(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.DeleteChannelGroup(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.DeleteChannel(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) testChannel(w http.ResponseWriter, r *http.Request) {
	if !s.allowTest(w, r) {
		return
	}
	out, err := s.svc.TestChannel(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) channelOptions(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ChannelOptions(r.Context(), principal(r), chi.URLParam(r, "id"), chi.URLParam(r, "key"))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

const oauthCookie = "staqly_oauth_state"

// oauthRedirect is the URL the platform sends the browser back to: the dashboard's callback page, which the
// embedded web server forwards to oauthCallback while it carries a code or state.
func (s *Server) oauthRedirect(provider string) string {
	return s.svc.WebURL() + "/channels/callback/" + provider
}

// oauthExtra passes provider-specific connection parameters through (a Mastodon instance URL, say).
func oauthExtra(q url.Values) map[string]string {
	extra := map[string]string{}
	for k, v := range q {
		if k != "code" && k != "state" && k != "error" && k != "error_description" && len(v) > 0 {
			extra[k] = v[0]
		}
	}
	return extra
}

func (s *Server) oauthStart(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	u, err := s.svc.StartOAuth(r.Context(), principal(r), provider, s.oauthRedirect(provider), oauthExtra(r.URL.Query()))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if parsed, perr := url.Parse(u); perr == nil {
		// The state is also pinned to this browser: the callback must present the same value.
		http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: parsed.Query().Get("state"), Path: "/", MaxAge: 600,
			HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	}
	writeJSON(w, 200, map[string]any{"url": u})
}

// oauthCallback finishes the connection and lands the browser on the dashboard's channels page.
func (s *Server) oauthCallback(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	q := r.URL.Query()
	land := func(params url.Values) {
		http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, s.oauthRedirect(provider)+"?"+params.Encode(), http.StatusFound)
	}
	if e := q.Get("error"); e != "" {
		land(url.Values{"error": {oauthRefusal(q)}})
		return
	}
	if principal(r).WorkspaceID == "" {
		land(url.Values{"error": {"You were signed out of staqly while connecting; sign in and connect again"}})
		return
	}
	if c, err := r.Cookie(oauthCookie); err != nil || c.Value == "" || c.Value != q.Get("state") {
		land(url.Values{"error": {"The connection did not start in this browser; try again"}})
		return
	}
	ch, err := s.svc.CompleteOAuth(r.Context(), principal(r), provider, q.Get("code"), q.Get("state"), s.oauthRedirect(provider), oauthExtra(q))
	if err != nil {
		s.log.WarnContext(r.Context(), "channel connection failed", "provider", provider, "err", err)
		var ae *app.Error
		if errors.As(err, &ae) {
			v := url.Values{"error": {ae.Message}}
			if ae.Code == "trial_already_used" { // the dashboard explains it and offers to pay
				v.Set("code", ae.Code)
			}
			land(v)
			return
		}
		land(url.Values{"error": {"The connection failed; try again"}})
		return
	}
	if ch.Pending != nil {
		// The sign-in may publish as several accounts: the callback page asks which ones.
		land(url.Values{"pending": {ch.Pending.ID}})
		return
	}
	land(url.Values{"channel": {ch.Channel.ID}})
}

// getPendingConnection lists the accounts a sign-in may publish as, for the person to pick from.
func (s *Server) getPendingConnection(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	out, err := s.svc.GetPendingConnection(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

type pendingChoice struct {
	AccountIDs []string `json:"account_ids"`
}

// finishPendingConnection connects the picked accounts, each as its own channel.
func (s *Server) finishPendingConnection(w http.ResponseWriter, r *http.Request) {
	var in pendingChoice
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.FinishPendingConnection(r.Context(), principal(r), chi.URLParam(r, "id"), in.AccountIDs)
	respond(w, r, 201, map[string]any{"data": out}, err)
}

// oauthRefusal says why a platform sent the browser back without a code: the person cancelled (access_denied
// from Google, X, Meta and most others; LinkedIn's user_cancelled_*), or the platform's own reason.
func oauthRefusal(q url.Values) string {
	switch e := q.Get("error"); e {
	case "access_denied", "user_denied", "user_cancelled_login", "user_cancelled_authorize":
		return "You cancelled the connection, so nothing was connected"
	default:
		return firstNonEmptyStr(q.Get("error_description"), "The platform refused the connection ("+e+")")
	}
}

func firstNonEmptyStr(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// ---- schedules ----

func (s *Server) listSchedules(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListSchedules(r.Context(), principal(r), r.URL.Query().Get("collection_id"))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) createSchedule(w http.ResponseWriter, r *http.Request) {
	noStore(r) // the response can carry the endpoint secret
	var in app.ScheduleInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateSchedule(r.Context(), principal(r), in)
	respond(w, r, 201, out, err)
}

func (s *Server) getSchedule(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetSchedule(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) updateSchedule(w http.ResponseWriter, r *http.Request) {
	noStore(r) // the response can carry a new endpoint secret
	var in app.ScheduleInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateSchedule(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, r, 200, out, err)
}

func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.DeleteSchedule(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) runSchedule(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.RunScheduleNow(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 202, out, err)
}

// ---- jobs ----

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := s.svc.ListJobs(r.Context(), principal(r), app.JobFilter{Status: q.Get("status"), Kind: q.Get("kind"), Cursor: q.Get("cursor"), Limit: qInt(r, "limit")})
	respond(w, r, 200, out, err)
}

func (s *Server) jobStats(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.JobStats(r.Context(), principal(r))
	respond(w, r, 200, out, err)
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetJob(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) retryJob(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.RetryJob(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 202, out, err)
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.CancelJob(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}
