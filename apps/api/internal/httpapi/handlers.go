package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/db"
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
	writeJSON(w, 200, map[string]any{"initialized": ok, "mode": s.svc.Cfg.Mode,
		"signup_open": s.svc.SignupOpen(), "setup_token_required": cloud && !s.svc.SignupOpen(),
		// Paid plans can be bought here (Stripe configured); the sign-up page words its pricing by it.
		"billing_enabled": s.svc.BillingEnabled(),
		// The dashboard shows it in snippets; read here so a pre-built web
		// image needs no build-time URL.
		"public_api_url": strings.TrimRight(s.svc.Cfg.PublicURL, "/")})
}

func (s *Server) listPlans(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"data": app.Plans})
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
	ns, err := s.svc.Recover(r.Context(), in.RecoveryKey, r.UserAgent())
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
	if p.Kind == app.PrincipalSession || p.Kind == app.PrincipalLocal {
		scopes = app.AllScopes
	}
	writeJSON(w, 200, map[string]any{"mode": s.svc.Cfg.Mode, "workspace": ws, "principal": map[string]any{"type": p.Kind, "id": p.ID, "name": p.Name,
		"scopes": scopes}, "csrf_token": csrf})
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
	respond(w, r, 200, map[string]any{"data": out, "available_scopes": app.AllScopes}, err)
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

func (s *Server) getCollection(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.FindCollection(r.Context(), principal(r), chi.URLParam(r, "id"))
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

// ---- items ----

func itemFilter(r *http.Request) app.ItemFilter {
	q := r.URL.Query()
	f := app.ItemFilter{
		CollectionID: q.Get("collection_id"), Status: q.Get("status"), Type: q.Get("type"), Source: q.Get("source"),
		ExternalRef: q.Get("external_ref"), Q: q.Get("q"), CreatedAfter: q.Get("created_after"),
		CreatedBefore: q.Get("created_before"), DecidedAfter: q.Get("decided_after"), DecidedBefore: q.Get("decided_before"),
		Sort: q.Get("sort"), Limit: qInt(r, "limit"), Cursor: q.Get("cursor"),
		Decisions: csv(q.Get("decision")), Runs: csv(q.Get("run")), Tags: csv(q.Get("tag")), RunAction: q.Get("run_action"), Archived: q.Get("archived"),
	}
	if id := chi.URLParam(r, "id"); id != "" {
		f.CollectionID = id
	}
	return f
}

func (s *Server) listItems(w http.ResponseWriter, r *http.Request) {
	f := itemFilter(r)
	if f.CollectionID != "" {
		if _, err := s.svc.GetCollection(r.Context(), principal(r), f.CollectionID); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	out, err := s.svc.ListItems(r.Context(), principal(r), f)
	respond(w, r, 200, out, err)
}

func (s *Server) createItem(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r)
	var in app.CreateItemInput
	// Multipart lets n8n/curl upload a file and create the item in one call:
	// form field "file" plus an optional "item" JSON part with the other fields.
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt == "multipart/form-data" {
		r.Body = http.MaxBytesReader(w, r.Body, s.svc.Cfg.MaxUploadBytes+(1<<20))
		mr, err := r.MultipartReader()
		if err != nil {
			writeErr(w, r, &app.Error{Code: "bad_request", Status: 400, Message: err.Error()})
			return
		}
		// An uploaded file belongs to the item: if anything after the upload
		// fails, delete it again so failed requests don't leak storage quota.
		uploaded := ""
		created := false
		defer func() {
			if uploaded != "" && !created {
				_ = s.svc.DeleteMedia(context.WithoutCancel(ctx), p, uploaded)
			}
		}()
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				writeErr(w, r, err)
				return
			}
			switch part.FormName() {
			case "item":
				if err := jsonUnmarshalStrict(io.LimitReader(part, 2<<20), &in); err != nil {
					writeErr(w, r, &app.Error{Code: "bad_request", Status: 400, Message: "invalid item JSON: " + err.Error()})
					return
				}
			case "file":
				if uploaded != "" {
					writeErr(w, r, &app.Error{Code: "bad_request", Status: 400, Message: "only one file part is allowed"})
					return
				}
				m, err := s.svc.UploadMedia(ctx, p, part.FileName(), part)
				if err != nil {
					writeErr(w, r, err)
					return
				}
				uploaded = m.ID
			}
		}
		if uploaded != "" {
			in.MediaID = &uploaded // the file wins over a media_id in the item part
		}
		out, err := s.svc.CreateItem(ctx, p, chi.URLParam(r, "id"), in)
		created = err == nil
		respond(w, r, 201, out, err)
		return
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateItem(ctx, p, chi.URLParam(r, "id"), in)
	respond(w, r, 201, out, err)
}

func (s *Server) getItem(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetItem(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) updateItem(w http.ResponseWriter, r *http.Request) {
	var in app.UpdateItemInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateItem(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, r, 200, out, err)
}

func (s *Server) deleteItem(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.DeleteItem(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) availableActions(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.AvailableActions(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) runAction(w http.ResponseWriter, r *http.Request) {
	var in app.ActionInput
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	out, err := s.svc.RunAction(r.Context(), principal(r), chi.URLParam(r, "id"), chi.URLParam(r, "action"), in)
	respond(w, r, 200, out, err)
}

func (s *Server) bulkAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs    []string `json:"ids"`
		Action string   `json:"action"`
		app.ActionInput
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.BulkAction(r.Context(), principal(r), in.IDs, in.Action, in.ActionInput)
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) itemEvents(w http.ResponseWriter, r *http.Request) {
	if _, err := s.svc.GetItem(r.Context(), principal(r), chi.URLParam(r, "id")); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := s.svc.ListEvents(r.Context(), principal(r), app.EventFilter{ItemID: chi.URLParam(r, "id"),
		Before: r.URL.Query().Get("cursor"), Limit: qInt(r, "limit")})
	respond(w, r, 200, out, err)
}

// waitItem long-polls until the item is decided (leaves "pending") or the
// timeout (default 30 s, max 60 s) passes; it always returns the current item.
func (s *Server) waitItem(w http.ResponseWriter, r *http.Request) {
	timeout := 30 * time.Second
	if v := qInt(r, "timeout"); v > 0 {
		timeout = min(time.Duration(v)*time.Second, 60*time.Second)
	}
	out, err := s.svc.WaitForDecision(r.Context(), principal(r), chi.URLParam(r, "id"), timeout)
	respond(w, r, 200, out, err)
}

func (s *Server) reportResult(w http.ResponseWriter, r *http.Request) {
	var in app.ResultInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.ReportResult(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, r, 200, out, err)
}

func (s *Server) itemVersions(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListItemVersions(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, map[string]any{"data": out}, err)
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

func serveMediaFile(w http.ResponseWriter, r *http.Request, f *os.File, m db.Medium) {
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
	respondEmpty(w, r, s.svc.DeleteMedia(r.Context(), principal(r), chi.URLParam(r, "id")))
}

// ---- events ----

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := s.svc.ListEvents(r.Context(), principal(r), app.EventFilter{ItemID: q.Get("item_id"),
		CollectionID: q.Get("collection_id"), Type: q.Get("type"), ActorType: q.Get("actor_type"),
		ActorID: q.Get("actor_id"), Agents: q["agent"], TypePrefix: q.Get("type_prefix"), From: q.Get("from"), To: q.Get("to"),
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
	out, err := s.svc.ListDeliveries(r.Context(), principal(r), q.Get("webhook_id"), q.Get("status"), q.Get("item_id"), qInt(r, "limit"))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) retryDelivery(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.RetryDelivery(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 202, out, err)
}

// ---- watchers ----

func (s *Server) listWatchers(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListWatchers(r.Context(), principal(r))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) createWatcher(w http.ResponseWriter, r *http.Request) {
	var in app.WatcherInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateWatcher(r.Context(), principal(r), in)
	respond(w, r, 201, out, err)
}

func (s *Server) getWatcher(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetWatcher(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) updateWatcher(w http.ResponseWriter, r *http.Request) {
	var in app.WatcherInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateWatcher(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, r, 200, out, err)
}

func (s *Server) deleteWatcher(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.DeleteWatcher(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) runWatcher(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.RunWatcherNow(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
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

func jsonUnmarshalStrict(r io.Reader, v any) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// csv splits "approved,rejected" into its values; empty means no filter.
func csv(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (s *Server) listTags(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListTags(r.Context(), principal(r))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) getFlow(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetFlow(r.Context(), principal(r), r.URL.Query().Get("collection_id"))
	respond(w, r, 200, out, err)
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
	created := r.Method == http.MethodPost &&
		(path == "/v1/items" || strings.HasPrefix(path, "/v1/collections/") && strings.HasSuffix(path, "/items"))
	svc.SeenAgent(r.Context(), PrincipalFrom(r.Context()), "api", name, version, created)
	return app.WithAgent(r.Context(), "api", name)
}

func (s *Server) summarizeItem(w http.ResponseWriter, r *http.Request) {
	var in app.SummarizeInput
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	if !s.allowSummary(w, r) {
		return
	}
	out, err := s.svc.Summarize(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, r, 200, out, err)
}

func (s *Server) unarchiveItem(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.Unarchive(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}
