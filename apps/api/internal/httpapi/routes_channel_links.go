package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/staqly/staqly/apps/api/internal/app"
)

// Connect links: the team makes a link (channels:manage); someone without access to Staqly opens it on the
// dashboard's /add-channel page and connects a channel. The public routes take the token in the path (redacted in
// logs) and set no session: the token is the only credential, the OAuth state carries the link (signed).

// connectLinkCookie remembers the link's token for the OAuth round trip, so the callback can land the browser back
// on the link's page (HttpOnly, 10 minutes, like the state cookie next to it).
const connectLinkCookie = "staqly_connect_link"

// connectLinks limits the public link routes per client IP (opening the page, connecting, starting a sign-in).
var connectLinks = newRateLimiter(60, 10*time.Minute)

func init() {
	RegisterAuthedRoutes(func(s *Server, r chi.Router) {
		r.Get("/channel-links", s.listChannelLinks)
		r.Post("/channel-links", s.createChannelLink)
		r.Delete("/channel-links/{id}", s.revokeChannelLink)
	})
	RegisterPublicRoutes(func(s *Server, r chi.Router) {
		r.Get("/connect-links/oauth/{provider}/callback", s.connectLinkOAuthCallback)
		r.Get("/connect-links/{token}", s.openConnectLink)
		r.Post("/connect-links/{token}/channels", s.connectViaLink)
		r.Get("/connect-links/{token}/oauth/{provider}/start", s.connectLinkOAuthStart)
		r.Get("/connect-links/{token}/pending/{id}", s.pendingViaLink)
		r.Post("/connect-links/{token}/pending/{id}", s.finishPendingViaLink)
	})
}

func (s *Server) listChannelLinks(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListChannelLinks(r.Context(), principal(r))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) createChannelLink(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	var in app.ChannelLinkInput
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	origin := r.Header.Get("Origin")
	if origin == "" && r.Header.Get("X-Forwarded-Host") != "" {
		origin = "http://" + r.Header.Get("X-Forwarded-Host")
	}
	out, err := s.svc.CreateChannelLink(r.Context(), principal(r), in, origin)
	respond(w, r, 201, out, err)
}

func (s *Server) revokeChannelLink(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.RevokeChannelLink(r.Context(), principal(r), chi.URLParam(r, "id")))
}

// connectLinkLimited answers 429 when this address opened or tried links too often.
func (s *Server) connectLinkLimited(w http.ResponseWriter, r *http.Request) bool {
	if connectLinks.allow(s.clientIP(r)) {
		return false
	}
	w.Header().Set("Retry-After", "600")
	writeErr(w, r, &app.Error{Code: "rate_limited", Status: 429, Message: "too many tries from this address: try again later"})
	return true
}

func (s *Server) openConnectLink(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	w.Header().Set("Cache-Control", "no-store")
	if s.connectLinkLimited(w, r) {
		return
	}
	out, err := s.svc.OpenChannelLink(r.Context(), chi.URLParam(r, "token"))
	respond(w, r, 200, out, err)
}

func (s *Server) connectViaLink(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	if s.connectLinkLimited(w, r) {
		return
	}
	var in app.ChannelInput
	if !decode(w, r, &in) {
		return
	}
	ch, err := s.svc.ConnectChannelViaLink(r.Context(), chi.URLParam(r, "token"), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	// The customer learns which account was connected, nothing else about the workspace.
	writeJSON(w, 201, map[string]any{"name": ch.Name, "handle": ch.Handle, "provider": ch.Provider})
}

func (s *Server) connectLinkOAuthStart(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	if s.connectLinkLimited(w, r) {
		return
	}
	provider, token := chi.URLParam(r, "provider"), chi.URLParam(r, "token")
	u, err := s.svc.StartOAuthViaLink(r.Context(), token, provider, s.oauthRedirect(provider), oauthExtra(r.URL.Query()))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if parsed, perr := url.Parse(u); perr == nil {
		// Pinned to this browser like the team's own sign-ins; the token cookie brings it back to the link's page.
		http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: parsed.Query().Get("state"), Path: "/", MaxAge: 600,
			HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
		http.SetCookie(w, &http.Cookie{Name: connectLinkCookie, Value: token, Path: "/", MaxAge: 600,
			HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	}
	writeJSON(w, 200, map[string]any{"url": u})
}

// connectLinkOAuthCallback finishes a customer's sign-in and lands the browser back on the link's page with the
// result (the dashboard forwards the platform's answer here when the state belongs to a link).
func (s *Server) connectLinkOAuthCallback(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	q := r.URL.Query()
	token := ""
	if c, err := r.Cookie(connectLinkCookie); err == nil {
		token = c.Value
	}
	land := func(params url.Values) {
		for _, name := range []string{oauthCookie, connectLinkCookie} {
			http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
		}
		// The token goes back in the fragment, as the link was sent: never in a server log or a Referer.
		dest := s.svc.WebURL() + "/add-channel?" + params.Encode()
		if token != "" && !strings.ContainsAny(token, "#/?& ") {
			dest += "#" + token
		}
		http.Redirect(w, r, dest, http.StatusFound)
	}
	if e := q.Get("error"); e != "" {
		land(url.Values{"error": {firstNonEmptyStr(q.Get("error_description"), "The platform refused the connection ("+e+")")}})
		return
	}
	if c, err := r.Cookie(oauthCookie); err != nil || c.Value == "" || c.Value != q.Get("state") {
		land(url.Values{"error": {"The connection did not start in this browser; open the link again"}})
		return
	}
	ch, workspace, err := s.svc.CompleteOAuthViaLink(r.Context(), provider, q.Get("code"), q.Get("state"), s.oauthRedirect(provider), oauthExtra(q))
	if err != nil {
		var ae *app.Error
		if errors.As(err, &ae) {
			land(url.Values{"error": {ae.Message}})
			return
		}
		land(url.Values{"error": {"The connection failed; try again"}})
		return
	}
	if ch.Pending != nil {
		// The sign-in may publish as several accounts: the link's page asks which ones.
		land(url.Values{"pending": {ch.Pending.ID}, "workspace": {workspace}})
		return
	}
	land(url.Values{"connected": {firstNonEmptyStr(ch.Channel.Handle, ch.Channel.Name)}, "workspace": {workspace}})
}

func (s *Server) pendingViaLink(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	if s.connectLinkLimited(w, r) {
		return
	}
	out, err := s.svc.GetPendingConnectionViaLink(r.Context(), chi.URLParam(r, "token"), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) finishPendingViaLink(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	if s.connectLinkLimited(w, r) {
		return
	}
	var in pendingChoice
	if !decode(w, r, &in) {
		return
	}
	chs, err := s.svc.FinishPendingConnectionViaLink(r.Context(), chi.URLParam(r, "token"), chi.URLParam(r, "id"), in.AccountIDs)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	// The customer learns which accounts were connected, nothing else about the workspace.
	out := make([]map[string]any, len(chs))
	for i, ch := range chs {
		out[i] = map[string]any{"name": ch.Name, "handle": ch.Handle, "provider": ch.Provider}
	}
	writeJSON(w, 201, map[string]any{"data": out})
}

func init() {
	// The client metadata of a platform the server is its own OAuth client for (Bluesky): the platform fetches it.
	RegisterPublicRoutes(func(s *Server, r chi.Router) {
		r.Get("/oauth/{provider}/client-metadata.json", func(w http.ResponseWriter, r *http.Request) {
			m, err := s.svc.ClientMetadata(chi.URLParam(r, "provider"))
			if err != nil {
				writeErr(w, r, err)
				return
			}
			writeJSON(w, 200, m)
		})
	})
}
