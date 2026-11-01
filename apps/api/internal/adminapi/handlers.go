package adminapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/staqly/staqly/apps/api/internal/app"
)

func (s *server) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, s.log, &app.Error{Code: "invalid_json", Status: 400, Message: "the body must be a JSON object"})
		return false
	}
	return true
}

// reply writes v as 200 JSON, or the error.
func (s *server) reply(w http.ResponseWriter, v any, err error) {
	if err != nil {
		writeErr(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *server) done(w http.ResponseWriter, err error) {
	if err != nil {
		writeErr(w, s.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) limit(w http.ResponseWriter, r *http.Request) (int, bool) {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return 0, true
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		writeErr(w, s.log, &app.Error{Code: "validation_failed", Status: 422, Message: "limit: must be a number", Fields: map[string]string{"limit": "must be a number"}})
		return 0, false
	}
	return n, true
}

func (s *server) authCode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	// Only an allowlisted address can see the service's own errors (cooldown, hourly cap); everyone else gets the same 204.
	s.done(w, s.svc.StartAdminSignIn(r.Context(), in.Email))
}

func (s *server) authVerify(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Code string }
	if !s.decode(w, r, &in) {
		return
	}
	a, token, exp, err := s.svc.VerifyAdminSignIn(r.Context(), in.Email, in.Code, r.UserAgent())
	s.startSession(w, a, token, exp, err)
}

func (s *server) authToken(w http.ResponseWriter, r *http.Request) {
	var in struct{ Token string }
	if !s.decode(w, r, &in) {
		return
	}
	a, token, exp, err := s.svc.VerifyAdminLocalToken(r.Context(), in.Token, r.UserAgent())
	s.startSession(w, a, token, exp, err)
}

func (s *server) authMethods(w http.ResponseWriter, r *http.Request) {
	code, tok := s.svc.AdminAuthMethods()
	writeJSON(w, http.StatusOK, map[string]bool{"email_code": code, "access_token": tok})
}

func (s *server) listAdmins(w http.ResponseWriter, r *http.Request) {
	v, err := s.svc.AdminListAdmins(r.Context())
	s.reply(w, map[string]any{"items": v}, err)
}

func (s *server) addAdmin(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Name string }
	if !s.decode(w, r, &in) {
		return
	}
	v, err := s.svc.AdminAddAdmin(r.Context(), adminOf(r), in.Email, in.Name)
	if err == nil {
		writeJSON(w, http.StatusCreated, v)
		return
	}
	writeErr(w, s.log, err)
}

func (s *server) removeAdmin(w http.ResponseWriter, r *http.Request) {
	email, err := url.PathUnescape(chi.URLParam(r, "email")) // a%40b.c arrives escaped
	if err != nil {
		email = ""
	}
	s.done(w, s.svc.AdminRemoveAdmin(r.Context(), adminOf(r), email))
}

func (s *server) startSession(w http.ResponseWriter, a app.Admin, token string, exp time.Time, err error) {
	if err != nil {
		writeErr(w, s.log, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", Expires: exp, HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]string{"email": a.Email, "csrf_token": a.CSRF})
}

func (s *server) signOut(w http.ResponseWriter, r *http.Request) {
	err := s.svc.SignOutAdmin(r.Context(), adminOf(r))
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", Expires: time.Unix(0, 0), MaxAge: -1, HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteStrictMode})
	s.done(w, err)
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	a := adminOf(r)
	writeJSON(w, http.StatusOK, map[string]string{"email": a.Email, "csrf_token": a.CSRF})
}

func (s *server) overview(w http.ResponseWriter, r *http.Request) {
	v, err := s.svc.AdminOverview(r.Context())
	s.reply(w, v, err)
}

func (s *server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	n, ok := s.limit(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	v, err := s.svc.AdminListWorkspaces(r.Context(), app.AdminWorkspaceFilter{Q: q.Get("q"), Plan: q.Get("plan"), Status: q.Get("status"), Cursor: q.Get("cursor"), Limit: n})
	s.reply(w, v, err)
}

func (s *server) getWorkspace(w http.ResponseWriter, r *http.Request) {
	v, err := s.svc.AdminWorkspace(r.Context(), chi.URLParam(r, "id"))
	s.reply(w, v, err)
}

func (s *server) payments(w http.ResponseWriter, r *http.Request) {
	s.paymentList(w, r, r.URL.Query().Get("workspace_id"))
}

func (s *server) workspacePayments(w http.ResponseWriter, r *http.Request) {
	s.paymentList(w, r, chi.URLParam(r, "id"))
}

func (s *server) paymentList(w http.ResponseWriter, r *http.Request, ws string) {
	n, ok := s.limit(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	v, err := s.svc.AdminListPayments(r.Context(), q.Get("status"), ws, q.Get("cursor"), n)
	s.reply(w, v, err)
}

func (s *server) listUsers(w http.ResponseWriter, r *http.Request) {
	n, ok := s.limit(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	v, err := s.svc.AdminListUsers(r.Context(), q.Get("q"), q.Get("cursor"), n)
	s.reply(w, v, err)
}

func (s *server) getUser(w http.ResponseWriter, r *http.Request) {
	v, err := s.svc.AdminUser(r.Context(), chi.URLParam(r, "id"))
	s.reply(w, v, err)
}

func (s *server) listPromos(w http.ResponseWriter, r *http.Request) {
	n, ok := s.limit(w, r)
	if !ok {
		return
	}
	v, err := s.svc.AdminListPromotionCodes(r.Context(), r.URL.Query().Get("cursor"), n)
	s.reply(w, v, err)
}

func (s *server) createPromo(w http.ResponseWriter, r *http.Request) {
	var in app.AdminPromoInput
	if !s.decode(w, r, &in) {
		return
	}
	v, err := s.svc.AdminCreatePromotionCode(r.Context(), adminOf(r), in)
	if err == nil {
		writeJSON(w, http.StatusCreated, v)
		return
	}
	writeErr(w, s.log, err)
}

func (s *server) deactivatePromo(w http.ResponseWriter, r *http.Request) {
	v, err := s.svc.AdminDeactivatePromotionCode(r.Context(), adminOf(r), chi.URLParam(r, "id"))
	s.reply(w, v, err)
}

func (s *server) setPlan(w http.ResponseWriter, r *http.Request) {
	var in struct{ Plan, Until string }
	if !s.decode(w, r, &in) {
		return
	}
	s.done(w, s.svc.AdminSetPlan(r.Context(), adminOf(r), chi.URLParam(r, "id"), in.Plan, in.Until))
}

func (s *server) clearTrial(w http.ResponseWriter, r *http.Request) {
	s.done(w, s.svc.AdminClearTrialReview(r.Context(), adminOf(r), chi.URLParam(r, "id")))
}

func (s *server) attach(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email string }
	if !s.decode(w, r, &in) {
		return
	}
	inv, err := s.svc.AdminAttach(r.Context(), adminOf(r), chi.URLParam(r, "id"), in.Email)
	if err != nil {
		writeErr(w, s.log, err)
		return
	}
	emailed := inv.Emailed != nil && *inv.Emailed
	writeJSON(w, http.StatusOK, map[string]any{"link": inv.Link, "expires_at": inv.ExpiresAt, "emailed": emailed})
}

func (s *server) signInLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email, Role string
		Hours       int
	}
	if !s.decode(w, r, &in) {
		return
	}
	v, err := s.svc.AdminSignInLink(r.Context(), adminOf(r), chi.URLParam(r, "id"), app.AdminLinkRequest{Email: in.Email, Role: in.Role, Hours: in.Hours})
	s.reply(w, v, err)
}

func (s *server) revokeWorkspace(w http.ResponseWriter, r *http.Request) {
	n, err := s.svc.AdminRevokeWorkspaceSessions(r.Context(), adminOf(r), chi.URLParam(r, "id"))
	s.reply(w, map[string]int64{"revoked": n}, err)
}

func (s *server) revokeUser(w http.ResponseWriter, r *http.Request) {
	n, err := s.svc.AdminRevokeUserSessions(r.Context(), adminOf(r), chi.URLParam(r, "id"))
	s.reply(w, map[string]int64{"revoked": n}, err)
}

func (s *server) billingSync(w http.ResponseWriter, r *http.Request) {
	s.done(w, s.svc.AdminSyncBilling(r.Context(), adminOf(r), chi.URLParam(r, "id")))
}

func (s *server) billingCancel(w http.ResponseWriter, r *http.Request) {
	s.done(w, s.svc.AdminCancelSubscription(r.Context(), adminOf(r), chi.URLParam(r, "id")))
}

func (s *server) deleteWorkspace(w http.ResponseWriter, r *http.Request) {
	var in struct{ Confirm string }
	if !s.decode(w, r, &in) {
		return
	}
	s.done(w, s.svc.AdminDeleteWorkspace(r.Context(), adminOf(r), chi.URLParam(r, "id"), in.Confirm))
}

func (s *server) deleteUser(w http.ResponseWriter, r *http.Request) {
	var in struct{ Confirm string }
	if !s.decode(w, r, &in) {
		return
	}
	s.done(w, s.svc.AdminDeleteUser(r.Context(), adminOf(r), chi.URLParam(r, "id"), in.Confirm))
}

func (s *server) audit(w http.ResponseWriter, r *http.Request) {
	n, ok := s.limit(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	v, err := s.svc.AdminListAudit(r.Context(), q.Get("q"), q.Get("workspace_id"), q.Get("cursor"), n)
	s.reply(w, v, err)
}
