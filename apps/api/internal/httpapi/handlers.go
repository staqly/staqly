package httpapi

import (
	"database/sql"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/staqly/staqly/apps/api/internal/app"
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
		writeErr(w, err)
		return
	}
	cloud := s.svc.Cfg.Mode == app.ModeCloud
	writeJSON(w, 200, map[string]any{"initialized": ok, "mode": s.svc.Cfg.Mode,
		"signup_open": s.svc.SignupOpen(), "setup_token_required": cloud && !s.svc.SignupOpen()})
}

func (s *Server) listPlans(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"data": app.Plans})
}

func (s *Server) getUsage(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetUsage(r.Context(), principal(r))
	respond(w, 200, out, err)
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
		writeErr(w, &app.Error{Code: "rate_limited", Status: 429, Message: "too many workspaces created from this address; try again later"})
		return
	}
	res, err := s.svc.Setup(r.Context(), in.Name, in.SetupToken, r.UserAgent())
	if err != nil {
		writeErr(w, err)
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
		writeErr(w, err)
		return
	}
	s.setCookie(w, ns)
	writeJSON(w, 201, map[string]any{"csrf_token": ns.CSRFToken})
}

func (s *Server) createDeviceLink(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" && r.Header.Get("X-Forwarded-Host") != "" {
		origin = "http://" + r.Header.Get("X-Forwarded-Host")
	}
	out, err := s.svc.CreateDeviceLink(r.Context(), principal(r), origin)
	respond(w, 201, out, err)
}

func (s *Server) redeemDeviceLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &in) {
		return
	}
	ns, err := s.svc.RedeemDeviceLink(r.Context(), in.Token, r.UserAgent())
	if err != nil {
		writeErr(w, err)
		return
	}
	s.setCookie(w, ns)
	writeJSON(w, 201, map[string]any{"csrf_token": ns.CSRFToken})
}

func (s *Server) currentSession(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	ws, err := s.svc.GetWorkspace(r.Context(), p)
	if err != nil {
		writeErr(w, err)
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
		writeErr(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(204)
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListSessions(r.Context(), principal(r))
	respond(w, 200, map[string]any{"data": out}, err)
}

func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, s.svc.RevokeSession(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) getWorkspace(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetWorkspace(r.Context(), principal(r))
	respond(w, 200, out, err)
}

func (s *Server) renameWorkspace(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.RenameWorkspace(r.Context(), principal(r), in.Name)
	respond(w, 200, out, err)
}

func (s *Server) rotateRecovery(w http.ResponseWriter, r *http.Request) {
	key, err := s.svc.RotateRecoveryKey(r.Context(), principal(r))
	respond(w, 200, map[string]string{"recovery_key": key}, err)
}

// ---- api keys ----

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListAPIKeys(r.Context(), principal(r))
	respond(w, 200, map[string]any{"data": out, "available_scopes": app.AllScopes}, err)
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	var in app.CreateAPIKeyInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateAPIKey(r.Context(), principal(r), in)
	respond(w, 201, out, err)
}

func (s *Server) revokeKey(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, s.svc.RevokeAPIKey(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) rotateKey(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.RotateAPIKey(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, 201, out, err)
}

// ---- collections ----

func (s *Server) listCollections(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListCollections(r.Context(), principal(r), r.URL.Query().Get("include_archived") == "true")
	respond(w, 200, map[string]any{"data": out}, err)
}

func (s *Server) createCollection(w http.ResponseWriter, r *http.Request) {
	var in app.CollectionInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateCollection(r.Context(), principal(r), in)
	respond(w, 201, out, err)
}

func (s *Server) getCollection(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.FindCollection(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, 200, out, err)
}

func (s *Server) updateCollection(w http.ResponseWriter, r *http.Request) {
	var in app.CollectionInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateCollection(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, 200, out, err)
}

func (s *Server) deleteCollection(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, s.svc.DeleteCollection(r.Context(), principal(r), chi.URLParam(r, "id"), r.URL.Query().Get("confirm")))
}

// ---- items ----

func itemFilter(r *http.Request) app.ItemFilter {
	q := r.URL.Query()
	f := app.ItemFilter{
		CollectionID: q.Get("collection_id"), Status: q.Get("status"), Type: q.Get("type"), Source: q.Get("source"),
		ExternalRef: q.Get("external_ref"), Q: q.Get("q"), CreatedAfter: q.Get("created_after"),
		CreatedBefore: q.Get("created_before"), DecidedAfter: q.Get("decided_after"), DecidedBefore: q.Get("decided_before"),
		Sort: q.Get("sort"), Limit: qInt(r, "limit"), Cursor: q.Get("cursor"),
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
			writeErr(w, err)
			return
		}
	}
	out, err := s.svc.ListItems(r.Context(), principal(r), f)
	respond(w, 200, out, err)
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
			writeErr(w, &app.Error{Code: "bad_request", Status: 400, Message: err.Error()})
			return
		}
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				writeErr(w, err)
				return
			}
			switch part.FormName() {
			case "item":
				if err := jsonUnmarshalStrict(io.LimitReader(part, 2<<20), &in); err != nil {
					writeErr(w, &app.Error{Code: "bad_request", Status: 400, Message: "invalid item JSON: " + err.Error()})
					return
				}
			case "file":
				m, err := s.svc.UploadMedia(ctx, p, part.FileName(), part)
				if err != nil {
					writeErr(w, err)
					return
				}
				in.MediaID = &m.ID
			}
		}
	} else if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateItem(ctx, p, chi.URLParam(r, "id"), in)
	respond(w, 201, out, err)
}

func (s *Server) getItem(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetItem(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, 200, out, err)
}

func (s *Server) updateItem(w http.ResponseWriter, r *http.Request) {
	var in app.UpdateItemInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateItem(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, 200, out, err)
}

func (s *Server) deleteItem(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, s.svc.DeleteItem(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) availableActions(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.AvailableActions(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, 200, map[string]any{"data": out}, err)
}

func (s *Server) runAction(w http.ResponseWriter, r *http.Request) {
	var in app.ActionInput
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	out, err := s.svc.RunAction(r.Context(), principal(r), chi.URLParam(r, "id"), chi.URLParam(r, "action"), in)
	respond(w, 200, out, err)
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
	respond(w, 200, map[string]any{"data": out}, err)
}

func (s *Server) itemEvents(w http.ResponseWriter, r *http.Request) {
	if _, err := s.svc.GetItem(r.Context(), principal(r), chi.URLParam(r, "id")); err != nil {
		writeErr(w, err)
		return
	}
	out, err := s.svc.ListEvents(r.Context(), principal(r), app.EventFilter{ItemID: chi.URLParam(r, "id"),
		Before: r.URL.Query().Get("cursor"), Limit: qInt(r, "limit")})
	respond(w, 200, out, err)
}

// waitItem long-polls until the item is decided (leaves "pending") or the
// timeout (default 30 s, max 60 s) passes; it always returns the current item.
func (s *Server) waitItem(w http.ResponseWriter, r *http.Request) {
	timeout := 30 * time.Second
	if v := qInt(r, "timeout"); v > 0 {
		timeout = min(time.Duration(v)*time.Second, 60*time.Second)
	}
	out, err := s.svc.WaitForDecision(r.Context(), principal(r), chi.URLParam(r, "id"), timeout)
	respond(w, 200, out, err)
}

func (s *Server) reportResult(w http.ResponseWriter, r *http.Request) {
	var in app.ResultInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.ReportResult(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, 200, out, err)
}

func (s *Server) itemVersions(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListItemVersions(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, 200, map[string]any{"data": out}, err)
}

// ---- media ----

func (s *Server) uploadMedia(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.svc.Cfg.MaxUploadBytes+(1<<20))
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, &app.Error{Code: "bad_request", Status: 400, Message: "expected multipart/form-data with a file field"})
		return
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			writeErr(w, &app.Error{Code: "bad_request", Status: 400, Message: "missing file field"})
			return
		}
		if part.FormName() == "file" {
			out, err := s.svc.UploadMedia(r.Context(), principal(r), part.FileName(), part)
			respond(w, 201, out, err)
			return
		}
	}
}

func (s *Server) getMedia(w http.ResponseWriter, r *http.Request) {
	f, m, err := s.svc.OpenMedia(r.Context(), principal(r), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", m.MimeType)
	h.Set("Cache-Control", "private, max-age=3600")
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
	respondEmpty(w, s.svc.DeleteMedia(r.Context(), principal(r), chi.URLParam(r, "id")))
}

// ---- events ----

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := s.svc.ListEvents(r.Context(), principal(r), app.EventFilter{ItemID: q.Get("item_id"),
		CollectionID: q.Get("collection_id"), Type: q.Get("type"), ActorType: q.Get("actor_type"),
		Before: q.Get("cursor"), Limit: qInt(r, "limit")})
	respond(w, 200, out, err)
}

// ---- webhooks ----

func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListWebhooks(r.Context(), principal(r))
	respond(w, 200, map[string]any{"data": out}, err)
}

func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request) {
	var in app.WebhookInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateWebhook(r.Context(), principal(r), in)
	respond(w, 201, out, err)
}

func (s *Server) getWebhook(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetWebhook(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, 200, out, err)
}

func (s *Server) updateWebhook(w http.ResponseWriter, r *http.Request) {
	var in app.WebhookInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateWebhook(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, 200, out, err)
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, s.svc.DeleteWebhook(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) testWebhook(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.TestWebhook(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, 202, out, err)
}

func (s *Server) listDeliveries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := s.svc.ListDeliveries(r.Context(), principal(r), q.Get("webhook_id"), q.Get("status"), qInt(r, "limit"))
	respond(w, 200, map[string]any{"data": out}, err)
}

func (s *Server) retryDelivery(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.RetryDelivery(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, 202, out, err)
}

// ---- rules ----

func (s *Server) listRules(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListRules(r.Context(), principal(r))
	respond(w, 200, map[string]any{"data": out}, err)
}

func (s *Server) createRule(w http.ResponseWriter, r *http.Request) {
	var in app.RuleInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateRule(r.Context(), principal(r), in)
	respond(w, 201, out, err)
}

func (s *Server) getRule(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetRule(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, 200, out, err)
}

func (s *Server) updateRule(w http.ResponseWriter, r *http.Request) {
	var in app.RuleInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateRule(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, 200, out, err)
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, s.svc.DeleteRule(r.Context(), principal(r), chi.URLParam(r, "id")))
}

// ---- watchers ----

func (s *Server) listWatcherPresets(w http.ResponseWriter, r *http.Request) {
	presets := make([]app.WatcherPreset, len(app.WatcherPresets))
	for i, p := range app.WatcherPresets {
		if p.Params == nil {
			p.Params = []app.PresetParam{} // [] not null for presets without parameters
		}
		presets[i] = p
	}
	writeJSON(w, 200, map[string]any{"data": presets})
}

func (s *Server) listWatchers(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListWatchers(r.Context(), principal(r))
	respond(w, 200, map[string]any{"data": out}, err)
}

func (s *Server) createWatcher(w http.ResponseWriter, r *http.Request) {
	var in app.WatcherInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateWatcher(r.Context(), principal(r), in, "")
	respond(w, 201, out, err)
}

func (s *Server) createWatcherFromPreset(w http.ResponseWriter, r *http.Request) {
	var in app.FromPresetInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateWatcherFromPreset(r.Context(), principal(r), in)
	respond(w, 201, out, err)
}

func (s *Server) getWatcher(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetWatcher(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, 200, out, err)
}

func (s *Server) updateWatcher(w http.ResponseWriter, r *http.Request) {
	var in app.WatcherInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateWatcher(r.Context(), principal(r), chi.URLParam(r, "id"), in)
	respond(w, 200, out, err)
}

func (s *Server) deleteWatcher(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, s.svc.DeleteWatcher(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) runWatcher(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.RunWatcherNow(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, 200, out, err)
}

// ---- helpers ----

func respond(w http.ResponseWriter, status int, v any, err error) {
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, status, v)
}

func respondEmpty(w http.ResponseWriter, err error) {
	if err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(204)
}

func jsonUnmarshalStrict(r io.Reader, v any) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
