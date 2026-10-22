package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Cloud accounts: sign-in with Google, workspaces, members and invitations (app/accounts.go).

const loginCookie = "staqly_login"

func init() {
	RegisterPublicRoutes(func(s *Server, r chi.Router) {
		r.Get("/auth/google/start", s.googleStart)
		r.Get("/auth/google/callback", s.googleCallback)
		r.Get("/invitations/lookup", s.lookupInvitation)
		r.Post("/auth/email/start", s.emailStart)
		r.Post("/auth/email/verify", s.emailVerify)
		r.Get("/auth/email/dev-code", s.emailDevCode)
		r.Post("/auth/passkey/start", s.passkeySignInStart)
		r.Post("/auth/passkey/finish", s.passkeySignInFinish)
	})
	RegisterAuthedRoutes(func(s *Server, r chi.Router) {
		r.Get("/workspaces", s.listMyWorkspaces)
		r.Post("/session/workspace", s.switchWorkspace)
		r.Patch("/account", s.renameMe)
		r.Post("/account/workspaces", s.createWorkspace)
		r.Get("/account/sign-in-methods", s.listSignInMethods)
		r.Post("/account/sign-in-methods/email/start", s.linkEmailStart)
		r.Post("/account/sign-in-methods/email/verify", s.linkEmailVerify)
		r.Post("/account/sign-in-methods/email/change/current/start", s.emailChangeStart)
		r.Post("/account/sign-in-methods/email/change/current/verify", s.emailChangeVerifyCurrent)
		r.Post("/account/sign-in-methods/email/change/new/start", s.emailChangeStartNew)
		r.Post("/account/sign-in-methods/email/change/new/verify", s.emailChangeVerifyNew)
		r.Post("/account/sign-in-methods/passkey/start", s.passkeyAddStart)
		r.Post("/account/sign-in-methods/passkey/finish", s.passkeyAddFinish)
		r.Patch("/account/sign-in-methods/passkey/{id}", s.renamePasskey)
		r.Post("/account/sign-in-methods/google", s.linkGoogle)
		r.Delete("/account/sign-in-methods/{provider}/{subject}", s.removeSignInMethod)
		r.Get("/members", s.listMembers)
		r.Patch("/members/{user_id}", s.updateMember)
		r.Delete("/members/{user_id}", s.removeMember)
		r.Get("/invitations", s.listInvitations)
		r.Post("/invitations", s.createInvitation)
		r.Post("/invitations/accept", s.acceptInvitation)
		r.Post("/invitations/{id}/resend", s.resendInvitation)
		r.Delete("/invitations/{id}", s.revokeInvitation)
	})
}

func (s *Server) setLoginCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: loginCookie, Value: value, Path: "/", MaxAge: maxAge, HttpOnly: true,
		Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
}

// googleStart sends the browser to Google; the sealed state rides in an HttpOnly cookie.
func (s *Server) googleStart(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	authURL, sealed, err := s.svc.StartGoogleLogin(r.Context(), q.Get("next"), q.Get("invite"), q.Get("setup_token"))
	if err != nil {
		loginRedirect(w, r, err)
		return
	}
	s.setLoginCookie(w, sealed, int(app.LoginCookieTTL.Seconds()))
	redirect(w, authURL)
}

// googleCallback finishes the sign-in and lands on the dashboard (only ever a path on it).
func (s *Server) googleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sealed := ""
	if c, err := r.Cookie(loginCookie); err == nil {
		sealed = c.Value
	}
	s.setLoginCookie(w, "", -1) // single use, whatever happens next
	if q.Get("error") != "" {
		loginRedirect(w, r, &app.Error{Code: "google_cancelled", Status: 401, Message: "the sign-in at Google was cancelled"})
		return
	}
	res, err := s.svc.CompleteGoogleLogin(r.Context(), sealed, q.Get("state"), q.Get("code"), r.UserAgent())
	if err != nil {
		loginRedirect(w, r, err)
		return
	}
	if res.Session.Token != "" { // link mode keeps the current session
		s.setCookie(w, res.Session)
	}
	redirect(w, app.SafeNext(res.Next))
}

// codeSendAllowed: email codes are limited per client IP too (per address: app/email_codes.go).
func (s *Server) codeSendAllowed(w http.ResponseWriter, r *http.Request) bool {
	if s.codes.allow(s.clientIP(r)) {
		return true
	}
	w.Header().Set("Retry-After", "3600")
	writeErr(w, r, &app.Error{Code: "rate_limited", Status: 429, Message: "too many codes requested from this address: try again later"})
	return false
}

func (s *Server) emailStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &in) || !s.codeSendAllowed(w, r) {
		return
	}
	out, err := s.svc.StartEmailSignIn(r.Context(), in.Email)
	respond(w, r, 202, out, err)
}

func (s *Server) emailVerify(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	var in struct {
		Email      string `json:"email"`
		Code       string `json:"code"`
		Next       string `json:"next"`
		Invite     string `json:"invite"`
		SetupToken string `json:"setup_token"`
	}
	if !decode(w, r, &in) {
		return
	}
	res, err := s.svc.VerifyEmailSignIn(r.Context(), in.Email, in.Code, in.Next, in.Invite, in.SetupToken, r.UserAgent())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.setCookie(w, res.Session)
	writeJSON(w, 201, map[string]any{"csrf_token": res.Session.CSRFToken, "next": app.SafeNext(res.Next)})
}

// emailDevCode: the last code sent to an address, for tests and the dev stack (AUTH_DEV_SIGNIN only;
// a 404 everywhere else, like a route that does not exist).
func (s *Server) emailDevCode(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	code, ok := s.svc.DevEmailCode(r.URL.Query().Get("email"))
	if !ok {
		writeErr(w, r, app.ErrNotFound)
		return
	}
	writeJSON(w, 200, map[string]string{"code": code})
}

func (s *Server) listSignInMethods(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListSignInMethods(r.Context(), principal(r))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) linkEmailStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &in) || !s.codeSendAllowed(w, r) {
		return
	}
	out, err := s.svc.StartEmailLink(r.Context(), principal(r), in.Email)
	respond(w, r, 202, out, err)
}

func (s *Server) linkEmailVerify(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.VerifyEmailLink(r.Context(), principal(r), in.Email, in.Code)
	respond(w, r, 200, map[string]any{"data": out}, err)
}

// ---- changing the email sign-in address (app/email_change.go) ----

type emailChangeIn struct {
	Email    string `json:"email"`
	NewEmail string `json:"new_email"`
	Code     string `json:"code"`
}

func (s *Server) emailChangeStart(w http.ResponseWriter, r *http.Request) {
	var in emailChangeIn
	if !decode(w, r, &in) || !s.codeSendAllowed(w, r) {
		return
	}
	out, err := s.svc.StartEmailChange(r.Context(), principal(r), in.Email)
	respond(w, r, 202, out, err)
}

func (s *Server) emailChangeVerifyCurrent(w http.ResponseWriter, r *http.Request) {
	var in emailChangeIn
	if !decode(w, r, &in) {
		return
	}
	respondEmpty(w, r, s.svc.VerifyEmailChangeCurrent(r.Context(), principal(r), in.Email, in.Code))
}

func (s *Server) emailChangeStartNew(w http.ResponseWriter, r *http.Request) {
	var in emailChangeIn
	if !decode(w, r, &in) || !s.codeSendAllowed(w, r) {
		return
	}
	out, err := s.svc.StartEmailChangeNew(r.Context(), principal(r), in.Email, in.NewEmail)
	respond(w, r, 202, out, err)
}

func (s *Server) emailChangeVerifyNew(w http.ResponseWriter, r *http.Request) {
	var in emailChangeIn
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.VerifyEmailChangeNew(r.Context(), principal(r), in.Email, in.NewEmail, in.Code)
	respond(w, r, 200, map[string]any{"data": out}, err)
}

// ---- passkeys (app/passkeys.go) ----

// passkeyAllowed: passkey sign-in ceremonies are limited per client IP, like email codes.
func (s *Server) passkeyAllowed(w http.ResponseWriter, r *http.Request) bool {
	if s.passkeys.allow(s.clientIP(r)) {
		return true
	}
	w.Header().Set("Retry-After", "600")
	writeErr(w, r, &app.Error{Code: "rate_limited", Status: 429, Message: "too many passkey sign-ins from this address: try again later"})
	return false
}

func (s *Server) passkeySignInStart(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	if !s.passkeyAllowed(w, r) {
		return
	}
	out, err := s.svc.BeginPasskeySignIn(r.Context())
	respond(w, r, 200, out, err)
}

func (s *Server) passkeySignInFinish(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	var in struct {
		ChallengeID string          `json:"challenge_id"`
		Response    json.RawMessage `json:"response"`
		Next        string          `json:"next"`
	}
	if !decode(w, r, &in) || !s.passkeyAllowed(w, r) {
		return
	}
	res, err := s.svc.FinishPasskeySignIn(r.Context(), in.ChallengeID, in.Response, in.Next, r.UserAgent())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.setCookie(w, res.Session)
	writeJSON(w, 201, map[string]any{"csrf_token": res.Session.CSRFToken, "next": app.SafeNext(res.Next)})
}

func (s *Server) passkeyAddStart(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	out, err := s.svc.BeginPasskeyRegistration(r.Context(), principal(r))
	respond(w, r, 200, out, err)
}

func (s *Server) passkeyAddFinish(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ChallengeID string          `json:"challenge_id"`
		Name        string          `json:"name"`
		Response    json.RawMessage `json:"response"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.FinishPasskeyRegistration(r.Context(), principal(r), in.ChallengeID, in.Name, in.Response)
	respond(w, r, 201, map[string]any{"data": out}, err)
}

func (s *Server) renamePasskey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.RenamePasskey(r.Context(), principal(r), chi.URLParam(r, "id"), in.Name)
	respond(w, r, 200, map[string]any{"data": out}, err)
}

// linkGoogle starts the Google flow in link mode: the dashboard sends the browser to the returned URL.
func (s *Server) linkGoogle(w http.ResponseWriter, r *http.Request) {
	authURL, sealed, err := s.svc.StartGoogleLink(r.Context(), principal(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.setLoginCookie(w, sealed, int(app.LoginCookieTTL.Seconds()))
	writeJSON(w, 200, map[string]string{"url": authURL})
}

func (s *Server) removeSignInMethod(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.RemoveSignInMethod(r.Context(), principal(r), chi.URLParam(r, "provider"), chi.URLParam(r, "subject"))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

// loginRedirect: sign-in errors land on the sign-in page, which explains the code in plain words.
func loginRedirect(w http.ResponseWriter, _ *http.Request, err error) {
	code := "sign_in_failed"
	var ae *app.Error
	if errors.As(err, &ae) {
		code = ae.Code
	}
	redirect(w, "/login?error="+url.QueryEscape(code))
}

// redirect: a bare 302 (no HTML body, unlike http.Redirect).
func redirect(w http.ResponseWriter, location string) {
	w.Header().Set("Location", location)
	w.WriteHeader(http.StatusFound)
}

func (s *Server) lookupInvitation(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	out, err := s.svc.LookupInvitation(r.Context(), r.URL.Query().Get("token"))
	respond(w, r, 200, out, err)
}

func (s *Server) listMyWorkspaces(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListMyWorkspaces(r.Context(), principal(r))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) switchWorkspace(w http.ResponseWriter, r *http.Request) {
	var in struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	respondEmpty(w, r, s.svc.SwitchWorkspace(r.Context(), principal(r), in.WorkspaceID))
}

func (s *Server) renameMe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.RenameMe(r.Context(), principal(r), in.Name)
	respond(w, r, 200, out, err)
}

func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Timezone string `json:"timezone"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateWorkspace(r.Context(), principal(r), in.Name, in.Timezone)
	respond(w, r, 201, out, err)
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListMembers(r.Context(), principal(r))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) updateMember(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role string `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateMemberRole(r.Context(), principal(r), chi.URLParam(r, "user_id"), in.Role)
	respond(w, r, 200, out, err)
}

func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.RemoveMember(r.Context(), principal(r), chi.URLParam(r, "user_id")))
}

func (s *Server) listInvitations(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListInvitations(r.Context(), principal(r))
	respond(w, r, 200, map[string]any{"data": out}, err)
}

func (s *Server) createInvitation(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	var in struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.CreateInvitation(r.Context(), principal(r), in.Email, in.Role)
	respond(w, r, 201, out, err)
}

func (s *Server) resendInvitation(w http.ResponseWriter, r *http.Request) {
	noStore(r)
	out, err := s.svc.ResendInvitation(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.RevokeInvitation(r.Context(), principal(r), chi.URLParam(r, "id")))
}

func (s *Server) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.AcceptInvitation(r.Context(), principal(r), in.Token)
	respond(w, r, 200, out, err)
}
