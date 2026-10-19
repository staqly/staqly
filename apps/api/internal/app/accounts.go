package app

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Cloud accounts (docs/accounts.md). People sign in with Google (OpenID Connect: state, nonce and PKCE,
// a verified ID token, Google's immutable `sub` as the identity, never the email address). An account is
// the billing and ownership boundary with one owner; its workspaces hold the content. A person's role in
// a workspace maps onto the existing scopes here, in one place, so REST, MCP and the dashboard keep a
// single authorization path (Principal.Has / require). Local mode never reaches any of this.

const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleEditor = "editor"
	RoleViewer = "viewer"
)

// Roles in order of power: a person may only grant or change roles up to their own.
var Roles = []string{RoleViewer, RoleEditor, RoleAdmin, RoleOwner}

func roleRank(role string) int { return slices.Index(Roles, role) }

// RoleScopes is the whole permission model: what each workspace role may do, as scopes.
//   - viewer: read only;
//   - editor: write, schedule and publish content, use the connected channels; no members, keys or channels;
//   - admin and owner: everything in the workspace (admin covers every scope); what tells them apart is
//     who may change owners (owners only), and account-level actions belong to the account owner.
var RoleScopes = map[string][]string{
	RoleViewer: {ScopeCollectionsRead, ScopePostsRead, ScopeMediaRead, ScopeEventsRead, ScopeChannelsRead},
	RoleEditor: {ScopeCollectionsRead, ScopeCollectionsWrite, ScopePostsRead, ScopePostsWrite, ScopePostsDelete,
		ScopePostsPublish, ScopeMediaRead, ScopeMediaWrite, ScopeEventsRead, ScopeChannelsRead, ScopeSchedulesManage},
	RoleAdmin: {ScopeAdmin},
	RoleOwner: {ScopeAdmin},
}

// invitationTTL: how long an invitation link works (Resend issues a new link and a new week).
const invitationTTL = 7 * 24 * time.Hour

// GoogleConfig: the OAuth client registered at Google (docs/accounts.md, "Google setup").
type GoogleConfig struct {
	ClientID     string
	ClientSecret string
	// RedirectURL: <dashboard>/api/v1/auth/google/callback, registered at Google.
	RedirectURL string
	// Issuer: https://accounts.google.com; tests point it at a fake provider.
	Issuer string
}

// AccountsEnabled: cloud mode where people sign in with Google or an email code (accounts, members, roles,
// invitations).
func (s *Service) AccountsEnabled() bool {
	return s.Cfg.Mode == ModeCloud && (s.Cfg.Google != nil || s.Cfg.EmailSignIn)
}

// GoogleSignIn: the "Continue with Google" button works here.
func (s *Service) GoogleSignIn() bool { return s.Cfg.Mode == ModeCloud && s.Cfg.Google != nil }

// EmailSignIn: the "Continue with email" form works here.
func (s *Service) EmailSignIn() bool { return s.Cfg.Mode == ModeCloud && s.Cfg.EmailSignIn }

// LegacySignIn: the sign-in from before accounts (setup token, admin API key, recovery key) still works:
// cloud mode without accounts, or AUTH_DEV_SIGNIN for development and tests.
func (s *Service) LegacySignIn() bool {
	return s.Cfg.Mode == ModeCloud && (!s.AccountsEnabled() || s.Cfg.DevSignIn)
}

func accountsOnly(s *Service) error {
	if !s.AccountsEnabled() {
		return &Error{Code: "accounts_disabled", Status: 409, Message: "accounts, members and invitations need cloud mode with Google or email sign-in"}
	}
	return nil
}

func googleOnly(s *Service) error {
	if !s.GoogleSignIn() {
		return &Error{Code: "google_disabled", Status: 409, Message: "sign-in with Google is not set up on this server (GOOGLE_CLIENT_ID)"}
	}
	return nil
}

func forbidden(msg string) *Error { return &Error{Code: "forbidden", Status: 403, Message: msg} }

// ---- sign-in with Google ----

type googleOIDC struct {
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config
}

type oidcCache struct {
	mu     sync.Mutex
	client *googleOIDC
}

// google discovers the provider once (its keys rotate; go-oidc refreshes them itself).
func (s *Service) google(ctx context.Context) (*googleOIDC, error) {
	s.oidc.mu.Lock()
	defer s.oidc.mu.Unlock()
	if s.oidc.client != nil {
		return s.oidc.client, nil
	}
	g := s.Cfg.Google
	issuer := g.Issuer
	if issuer == "" {
		issuer = "https://accounts.google.com"
	}
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(dctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("google sign-in is unavailable: %w", err)
	}
	s.oidc.client = &googleOIDC{
		verifier: provider.Verifier(&oidc.Config{ClientID: g.ClientID}),
		oauth: oauth2.Config{ClientID: g.ClientID, ClientSecret: g.ClientSecret, RedirectURL: g.RedirectURL,
			Endpoint: provider.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "email", "profile"}},
	}
	return s.oidc.client, nil
}

// loginState travels sealed (AES-GCM, the server's secret key) in an HttpOnly cookie between the start and
// the callback: nothing is stored server-side, and a callback without the browser that started it fails.
type loginState struct {
	State      string `json:"s"`
	Nonce      string `json:"n"`
	Verifier   string `json:"v"`
	Next       string `json:"x,omitempty"`
	Invite     string `json:"i,omitempty"`
	SetupToken string `json:"t,omitempty"`
	// Link: the signed-in person adding Google to their sign-in methods (no new session).
	Link    string `json:"l,omitempty"`
	Expires int64  `json:"e"`
}

// LoginCookieTTL: the sign-in at Google has this long to come back.
const LoginCookieTTL = 10 * time.Minute

// SafeNext keeps a redirect on this dashboard: a relative path, never another host (no open redirect).
func SafeNext(next string) string {
	if next == "" || len(next) > 512 || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") ||
		strings.ContainsAny(next, "\\\r\n") {
		return "/schedule"
	}
	if u, err := url.Parse(next); err != nil || u.Scheme != "" || u.Host != "" {
		return "/schedule"
	}
	return next
}

// StartGoogleLogin returns the Google URL to send the browser to and the sealed state for its cookie.
func (s *Service) StartGoogleLogin(ctx context.Context, next, invite, setupToken string) (string, string, error) {
	return s.startGoogle(ctx, loginState{Next: SafeNext(next), Invite: strings.TrimSpace(invite), SetupToken: setupToken})
}

// StartGoogleLink: a signed-in person adds Google to their sign-in methods (Settings, Account).
func (s *Service) StartGoogleLink(ctx context.Context, p Principal) (string, string, error) {
	if p.Kind != PrincipalSession || p.UserID == "" {
		return "", "", forbidden("only a signed-in person adds a sign-in method")
	}
	return s.startGoogle(ctx, loginState{Next: signInMethodsPath, Link: p.UserID})
}

const signInMethodsPath = "/settings/account"

func (s *Service) startGoogle(ctx context.Context, st loginState) (string, string, error) {
	if err := googleOnly(s); err != nil {
		return "", "", err
	}
	g, err := s.google(ctx)
	if err != nil {
		return "", "", &Error{Code: "google_unavailable", Status: 503, Message: err.Error()}
	}
	st.State, st.Nonce, st.Verifier = randomString(32), randomString(32), oauth2.GenerateVerifier()
	st.Expires = s.Now().Add(LoginCookieTTL).Unix()
	sealed, err := s.seal(st)
	if err != nil {
		return "", "", err
	}
	authURL := g.oauth.AuthCodeURL(st.State, oidc.Nonce(st.Nonce), oauth2.S256ChallengeOption(st.Verifier),
		oauth2.SetAuthURLParam("prompt", "select_account"))
	return authURL, sealed, nil
}

// GoogleIdentity is what a verified ID token says about the person.
type GoogleIdentity struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	Nonce         string `json:"nonce"`
}

// LoginResult: the new session and where the browser goes next.
type LoginResult struct {
	Session NewSession
	Next    string
}

func loginErr(code, msg string) *Error { return &Error{Code: code, Status: 401, Message: msg} }

// CompleteGoogleLogin checks the callback against the sealed state, exchanges the code (with the PKCE
// verifier), verifies the ID token (signature, issuer, audience, expiry, nonce), then signs the person in:
// an invitation they came with is accepted, a returning person lands in their workspace, a new person gets
// an account and a first workspace (when sign-ups are open). Always a fresh session (no fixation).
func (s *Service) CompleteGoogleLogin(ctx context.Context, sealedState, state, code, userAgent string) (LoginResult, error) {
	if err := googleOnly(s); err != nil {
		return LoginResult{}, err
	}
	var st loginState
	if sealedState == "" || s.unseal(sealedState, &st) != nil {
		return LoginResult{}, loginErr("login_expired", "the sign-in took too long or started in another browser; try again")
	}
	if s.Now().Unix() > st.Expires {
		return LoginResult{}, loginErr("login_expired", "the sign-in took too long; try again")
	}
	if state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(st.State)) != 1 {
		return LoginResult{}, loginErr("login_state_mismatch", "this sign-in does not match the one this browser started; try again")
	}
	g, err := s.google(ctx)
	if err != nil {
		return LoginResult{}, &Error{Code: "google_unavailable", Status: 503, Message: err.Error()}
	}
	xctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tok, err := g.oauth.Exchange(xctx, code, oauth2.VerifierOption(st.Verifier))
	if err != nil {
		return LoginResult{}, loginErr("google_failed", "Google did not confirm the sign-in; try again")
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return LoginResult{}, loginErr("google_failed", "Google sent no identity; try again")
	}
	idt, err := g.verifier.Verify(xctx, raw)
	if err != nil {
		return LoginResult{}, loginErr("google_failed", "the identity from Google could not be verified; try again")
	}
	var id GoogleIdentity
	if err := idt.Claims(&id); err != nil || id.Subject == "" {
		return LoginResult{}, loginErr("google_failed", "the identity from Google is incomplete; try again")
	}
	if subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(st.Nonce)) != 1 {
		return LoginResult{}, loginErr("login_state_mismatch", "the identity from Google belongs to another sign-in; try again")
	}
	if !id.EmailVerified || id.Email == "" {
		return LoginResult{}, &Error{Code: "email_unverified", Status: 403, Message: "this Google account has no verified email address"}
	}
	ident := Identity{Provider: ProviderGoogle, Subject: id.Subject, Email: id.Email, Name: id.Name, Picture: id.Picture}
	if st.Link != "" {
		// Link mode: no new session; the outcome shows on the sign-in methods page.
		next := signInMethodsPath + "?linked=google"
		if err := s.linkIdentity(ctx, st.Link, ident); err != nil {
			code := "sign_in_failed"
			var ae *Error
			if errors.As(err, &ae) {
				code = ae.Code
			}
			next = signInMethodsPath + "?error=" + url.QueryEscape(code)
		}
		return LoginResult{Next: next}, nil
	}
	return s.signInIdentity(ctx, ident, st, userAgent)
}

// signInIdentity provisions the person and opens their session, all in one transaction. It is
// provider-agnostic: any verified Identity (Google, an email code) with the sign-in's next path, invitation
// and setup token lands here.
func (s *Service) signInIdentity(ctx context.Context, id Identity, st loginState, userAgent string) (LoginResult, error) {
	var res LoginResult
	err := s.tx(ctx, func(q *db.Queries) error {
		var err error
		res, err = s.signInIdentityTx(ctx, q, id, st, userAgent)
		return err
	})
	return res, err
}

func (s *Service) signInIdentityTx(ctx context.Context, q *db.Queries, id Identity, st loginState, userAgent string) (LoginResult, error) {
	user, err := s.upsertUser(ctx, q, id)
	if err != nil {
		return LoginResult{}, err
	}
	return s.signInUserTx(ctx, q, user, st, userAgent, id.Provider)
}

// signInUserTx opens a session for a person whose sign-in was verified (`via`: google, email, passkey): the
// invitation is accepted, a returning person lands in their workspace, a new one gets an account.
func (s *Service) signInUserTx(ctx context.Context, q *db.Queries, user db.User, st loginState, userAgent, via string) (LoginResult, error) {
	var err error
	next := SafeNext(st.Next)
	var wsID string
	switch {
	case st.Invite != "":
		if wsID, err = s.acceptInvitation(ctx, q, user, st.Invite); err != nil {
			return LoginResult{}, err
		}
		next = "/schedule"
	default:
		if wsID, err = s.homeWorkspace(ctx, q, user.ID); err != nil {
			return LoginResult{}, err
		}
		if wsID == "" {
			_, accErr := q.GetAccountByOwner(ctx, user.ID)
			if accErr != nil && !isNoRows(accErr) {
				return LoginResult{}, accErr
			}
			if accErr != nil && !s.signupAllowed(st.SetupToken) {
				return LoginResult{}, &Error{Code: "signup_closed", Status: 403,
					Message: "this staqly accepts new people by invitation: ask a workspace admin to invite " + user.Email}
			}
			if wsID, err = s.createAccount(ctx, q, user); err != nil {
				return LoginResult{}, err
			}
		}
	}
	ns, sid, err := s.createUserSession(ctx, q, wsID, user.ID, userAgent)
	if err != nil {
		return LoginResult{}, err
	}
	p := Principal{WorkspaceID: wsID, Kind: PrincipalSession, ID: sid, Name: displayName(user), UserID: user.ID}
	if _, err := s.emit(ctx, q, p, "session.signed_in", "", "", map[string]any{"user_agent": userAgent, "session_id": sid,
		"user_id": user.ID, "via": via}); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Session: ns, Next: next}, nil
}

func (s *Service) signupAllowed(token string) bool {
	if s.Cfg.SetupToken == "" {
		return true
	}
	return token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.Cfg.SetupToken)) == 1
}

func displayName(u db.User) string {
	if u.Name != "" {
		return u.Name
	}
	return u.Email
}

// Identity is a verified sign-in from one provider: ProviderGoogle (subject: Google's `sub`) or
// ProviderEmail (subject: the normalized address the code was sent to).
type Identity struct {
	Provider string
	Subject  string
	Email    string
	Name     string
	Picture  string
}

const (
	ProviderGoogle = "google"
	ProviderEmail  = "email"
)

// userHasAddress: one of the person's sign-in methods proved this address (Google's verified address,
// or an address that received an email code).
func (s *Service) userHasAddress(ctx context.Context, q *db.Queries, user db.User, email string) (bool, error) {
	if strings.EqualFold(user.Email, email) {
		return true, nil
	}
	ids, err := q.ListUserIdentities(ctx, user.ID)
	if err != nil {
		return false, err
	}
	for _, i := range ids {
		if strings.EqualFold(i.Email, email) {
			return true, nil
		}
	}
	return false, nil
}

// upsertUser finds the person by (provider, subject), never by email, and refreshes their profile.
// A new identity always creates a new person: linking a second provider to an existing person is an
// explicit, signed-in action (LinkEmail, Google's link mode), so an email address alone never merges two people.
func (s *Service) upsertUser(ctx context.Context, q *db.Queries, id Identity) (db.User, error) {
	now := s.now()
	email := strings.ToLower(strings.TrimSpace(id.Email))
	name, picture := truncateRunes(strings.TrimSpace(id.Name), 200), id.Picture
	if len(picture) > 1000 || !strings.HasPrefix(picture, "https://") {
		picture = ""
	}
	u, err := q.GetUserByIdentity(ctx, db.GetUserByIdentityParams{Provider: id.Provider, Subject: id.Subject})
	switch {
	case isNoRows(err):
		u = db.User{ID: newID("usr"), Email: email, Name: name, AvatarUrl: picture, CreatedAt: now, UpdatedAt: now, LastLoginAt: nullStr(now)}
		if err := q.CreateUser(ctx, db.CreateUserParams{ID: u.ID, Email: email, Name: name, AvatarUrl: picture,
			CreatedAt: now, UpdatedAt: now, LastLoginAt: nullStr(now)}); err != nil {
			return db.User{}, err
		}
		return u, q.CreateIdentity(ctx, db.CreateIdentityParams{Provider: id.Provider, Subject: id.Subject, UserID: u.ID, Email: email,
			CreatedAt: now, LastLoginAt: nullStr(now)})
	case err != nil:
		return db.User{}, err
	}
	// Providers without a name or picture (an email code) keep what the person already has.
	if name == "" {
		name = u.Name
	}
	if picture == "" && id.Provider != ProviderGoogle {
		picture = u.AvatarUrl
	}
	if err := q.UpdateUserProfile(ctx, db.UpdateUserProfileParams{Email: email, Name: name, AvatarUrl: picture, UpdatedAt: now,
		LastLoginAt: nullStr(now), ID: u.ID}); err != nil {
		return db.User{}, err
	}
	if err := q.TouchIdentity(ctx, db.TouchIdentityParams{Email: email, LastLoginAt: nullStr(now), Provider: id.Provider, Subject: id.Subject}); err != nil {
		return db.User{}, err
	}
	u.Email, u.Name, u.AvatarUrl = email, name, picture
	return u, nil
}

// homeWorkspace: where a returning person lands (their first workspace), "" when they have none.
func (s *Service) homeWorkspace(ctx context.Context, q *db.Queries, userID string) (string, error) {
	rows, err := q.ListUserWorkspaces(ctx, userID)
	if err != nil || len(rows) == 0 {
		return "", err
	}
	return rows[0].ID, nil
}

// createAccount: a new person's account and first workspace, created together (they own both).
func (s *Service) createAccount(ctx context.Context, q *db.Queries, user db.User) (string, error) {
	now := s.now()
	acc, err := q.GetAccountByOwner(ctx, user.ID) // an owner who deleted every workspace keeps the account
	if isNoRows(err) {
		acc = db.Account{ID: newID("acc"), OwnerUserID: user.ID, Name: displayName(user), CreatedAt: now, UpdatedAt: now}
		err = q.CreateAccount(ctx, db.CreateAccountParams{ID: acc.ID, OwnerUserID: user.ID, Name: acc.Name, CreatedAt: now, UpdatedAt: now})
	}
	if err != nil {
		return "", err
	}
	ws, err := s.createCloudWorkspace(ctx, q, "My workspace", acc.ID)
	if err != nil {
		return "", err
	}
	if _, err := q.AddMember(ctx, db.AddMemberParams{WorkspaceID: ws.ID, UserID: user.ID, Role: RoleOwner, CreatedAt: now, UpdatedAt: now}); err != nil {
		return "", err
	}
	return ws.ID, nil
}

// createCloudWorkspace: a workspace on the free trial with its default collection (accountID "" for the
// sign-up from before accounts). No recovery key: account workspaces sign in with Google.
func (s *Service) createCloudWorkspace(ctx context.Context, q *db.Queries, name, accountID string) (db.Workspace, error) {
	now := s.now()
	ws := db.Workspace{ID: newID("ws"), Name: name, Plan: "pro", RecoveryRotatedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := q.CreateWorkspace(ctx, db.CreateWorkspaceParams{ID: ws.ID, Name: name, RecoveryVerifier: hashSecret(randomString(32)),
		RecoveryRotatedAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
		return db.Workspace{}, err
	}
	// A new cloud workspace: with billing, no plan until its owner starts a subscription (Starter: 7 days free,
	// card required); without billing, the server-side trial.
	planID, ends := s.newWorkspacePlan()
	if _, err := q.SetWorkspacePlan(ctx, db.SetWorkspacePlanParams{Plan: planID,
		PlanExpiresAt: sql.NullString{String: ends, Valid: true}, UpdatedAt: now, ID: ws.ID}); err != nil {
		return db.Workspace{}, err
	}
	if accountID != "" {
		if _, err := q.SetWorkspaceAccount(ctx, db.SetWorkspaceAccountParams{AccountID: nullStr(accountID), UpdatedAt: now, ID: ws.ID}); err != nil {
			return db.Workspace{}, err
		}
		ws.AccountID = nullStr(accountID)
	}
	if err := s.ensureDefaultCollection(ctx, q, ws.ID); err != nil {
		return db.Workspace{}, err
	}
	sys := Principal{WorkspaceID: ws.ID, Kind: PrincipalSystem, ID: "setup", Name: "Setup"}
	if _, err := s.emit(ctx, q, sys, "workspace.created", "", "", map[string]any{"workspace": s.toWorkspace(ws)}); err != nil {
		return db.Workspace{}, err
	}
	return ws, nil
}

func (s *Service) createUserSession(ctx context.Context, q *db.Queries, workspaceID, userID, userAgent string) (NewSession, string, error) {
	now := s.Now().UTC()
	ns := NewSession{Token: randomString(32), CSRFToken: randomString(20), ExpiresAt: now.Add(s.Cfg.SessionTTL)}
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	id := newID("ses")
	err := q.CreateUserSession(ctx, db.CreateUserSessionParams{ID: id, WorkspaceID: workspaceID, TokenHash: hashSecret(ns.Token),
		CsrfToken: ns.CSRFToken, UserAgent: userAgent, CreatedAt: formatTime(now), LastSeenAt: formatTime(now),
		ExpiresAt: formatTime(ns.ExpiresAt), UserID: nullStr(userID)})
	return ns, id, err
}

// userPrincipal: what a signed-in person may do in a workspace, recomputed on every request (a role
// change or a removal takes effect at once). The account owner is an owner in every account workspace.
func (s *Service) userPrincipal(ctx context.Context, q *db.Queries, userID, workspaceID string) (Principal, error) {
	user, err := q.GetUser(ctx, userID)
	if err != nil {
		return Principal{}, ErrUnauthorized
	}
	ws, err := q.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return Principal{}, ErrUnauthorized
	}
	role := ""
	if m, err := q.GetMember(ctx, db.GetMemberParams{WorkspaceID: workspaceID, UserID: userID}); err == nil {
		role = m.Role
	} else if !isNoRows(err) {
		return Principal{}, err
	}
	owner := false
	if ws.AccountID.Valid {
		if acc, err := q.GetAccount(ctx, ws.AccountID.String); err == nil && acc.OwnerUserID == userID {
			owner, role = true, RoleOwner
		}
	}
	if role == "" {
		return Principal{}, ErrUnauthorized
	}
	return Principal{WorkspaceID: workspaceID, Name: displayName(user), UserID: userID, Role: role, AccountOwner: owner,
		Scopes: RoleScopes[role]}, nil
}

// ---- who am I, workspaces ----

// User is the signed-in person (or, in local mode, the person at this computer).
type User struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

// CurrentUser: the person behind a principal, for the sidebar. Local mode: the computer's user name.
func (s *Service) CurrentUser(ctx context.Context, p Principal) User {
	if p.UserID != "" {
		if u, err := s.Q.GetUser(ctx, p.UserID); err == nil {
			return User{ID: u.ID, Name: displayName(u), Email: u.Email, AvatarURL: u.AvatarUrl}
		}
	}
	if s.Cfg.Mode == ModeLocal {
		return User{Name: s.localUserName()}
	}
	return User{Name: p.Name}
}

// MyWorkspace is a workspace the person can open, with their role in it.
type MyWorkspace struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Current bool   `json:"current"`
}

// ListMyWorkspaces: the workspaces a signed-in person belongs to; others see just their own.
func (s *Service) ListMyWorkspaces(ctx context.Context, p Principal) ([]MyWorkspace, error) {
	if p.WorkspaceID == "" {
		return nil, ErrUnauthorized
	}
	if p.UserID == "" {
		ws, err := s.GetWorkspace(ctx, p)
		if err != nil {
			return nil, err
		}
		return []MyWorkspace{{ID: ws.ID, Name: ws.Name, Role: RoleOwner, Current: true}}, nil
	}
	out := []MyWorkspace{}
	seen := map[string]bool{}
	rows, err := s.Q.ListUserWorkspaces(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	if acc, err := s.Q.GetAccountByOwner(ctx, p.UserID); err == nil {
		owned, err := s.Q.ListAccountWorkspaces(ctx, nullStr(acc.ID))
		if err != nil {
			return nil, err
		}
		for _, w := range owned {
			seen[w.ID] = true
			out = append(out, MyWorkspace{ID: w.ID, Name: w.Name, Role: RoleOwner, Current: w.ID == p.WorkspaceID})
		}
	}
	for _, r := range rows {
		if !seen[r.ID] {
			out = append(out, MyWorkspace{ID: r.ID, Name: r.Name, Role: r.Role, Current: r.ID == p.WorkspaceID})
		}
	}
	return out, nil
}

// SwitchWorkspace moves the signed-in person's session to another workspace they belong to.
func (s *Service) SwitchWorkspace(ctx context.Context, p Principal, workspaceID string) error {
	if p.Kind != PrincipalSession || p.UserID == "" {
		return forbidden("only a signed-in person switches workspaces")
	}
	if _, err := s.userPrincipal(ctx, s.Q, p.UserID, workspaceID); err != nil {
		return notFound("workspace") // not a member: the workspace does not exist for them
	}
	return s.tx(ctx, func(q *db.Queries) error {
		n, err := q.SetSessionWorkspace(ctx, db.SetSessionWorkspaceParams{WorkspaceID: workspaceID, ID: p.ID, UserID: nullStr(p.UserID)})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrUnauthorized
		}
		return nil
	})
}

// CreateAccountWorkspace: only the account owner adds a workspace to their account; they own it.
func (s *Service) CreateAccountWorkspace(ctx context.Context, p Principal, name string) (MyWorkspace, error) {
	if err := accountsOnly(s); err != nil {
		return MyWorkspace{}, err
	}
	if p.Kind != PrincipalSession || p.UserID == "" {
		return MyWorkspace{}, forbidden("only the account owner, signed in, creates workspaces")
	}
	acc, err := s.Q.GetAccountByOwner(ctx, p.UserID)
	if err != nil {
		return MyWorkspace{}, forbidden("only the account owner creates workspaces; ask them to add one")
	}
	name, err = trimmed(name, "name", 100, true)
	if err != nil {
		return MyWorkspace{}, err
	}
	var out MyWorkspace
	err = s.tx(ctx, func(q *db.Queries) error {
		ws, err := s.createCloudWorkspace(ctx, q, name, acc.ID)
		if err != nil {
			return err
		}
		now := s.now()
		if _, err := q.AddMember(ctx, db.AddMemberParams{WorkspaceID: ws.ID, UserID: p.UserID, Role: RoleOwner, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		out = MyWorkspace{ID: ws.ID, Name: ws.Name, Role: RoleOwner}
		return nil
	})
	return out, err
}

// ---- account ----

type Account struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Owner      User          `json:"owner"`
	Workspaces []MyWorkspace `json:"workspaces"`
	CreatedAt  string        `json:"created_at"`
}

// GetAccount: the signed-in person's own account (only owners have one).
func (s *Service) GetAccount(ctx context.Context, p Principal) (Account, error) {
	if err := accountsOnly(s); err != nil {
		return Account{}, err
	}
	if p.UserID == "" {
		return Account{}, notFound("account")
	}
	acc, err := s.Q.GetAccountByOwner(ctx, p.UserID)
	if err != nil {
		return Account{}, notFound("account")
	}
	ws, err := s.Q.ListAccountWorkspaces(ctx, nullStr(acc.ID))
	if err != nil {
		return Account{}, err
	}
	out := Account{ID: acc.ID, Name: acc.Name, Owner: s.CurrentUser(ctx, p), CreatedAt: acc.CreatedAt, Workspaces: []MyWorkspace{}}
	for _, w := range ws {
		out.Workspaces = append(out.Workspaces, MyWorkspace{ID: w.ID, Name: w.Name, Role: RoleOwner, Current: w.ID == p.WorkspaceID})
	}
	return out, nil
}

func (s *Service) RenameAccount(ctx context.Context, p Principal, name string) (Account, error) {
	if _, err := s.GetAccount(ctx, p); err != nil {
		return Account{}, err
	}
	name, err := trimmed(name, "name", 100, true)
	if err != nil {
		return Account{}, err
	}
	acc, _ := s.Q.GetAccountByOwner(ctx, p.UserID)
	if err := s.tx(ctx, func(q *db.Queries) error {
		return q.UpdateAccountName(ctx, db.UpdateAccountNameParams{Name: name, UpdatedAt: s.now(), ID: acc.ID})
	}); err != nil {
		return Account{}, err
	}
	return s.GetAccount(ctx, p)
}

// requireAccountOwner: actions on the account itself (delete a workspace, billing). Workspaces from before
// accounts keep their old rule (an admin dashboard session).
func (s *Service) requireAccountOwner(ctx context.Context, p Principal) error {
	if err := p.require(ScopeAdmin); err != nil {
		return err
	}
	ws, err := s.Q.GetWorkspace(ctx, p.WorkspaceID)
	if err != nil {
		return err
	}
	if !ws.AccountID.Valid {
		return nil
	}
	if p.AccountOwner {
		return nil
	}
	if p.Kind == PrincipalAPIKey && p.UserID != "" {
		if acc, err := s.Q.GetAccount(ctx, ws.AccountID.String); err == nil && acc.OwnerUserID == p.UserID {
			return nil // a key the account owner created
		}
	}
	return forbidden("only the account owner can do this")
}

// ---- members ----

type Member struct {
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
	Role      string `json:"role"`
	// AccountOwner: owns the account; always an owner here, cannot be removed or demoted.
	AccountOwner bool   `json:"account_owner"`
	You          bool   `json:"you"`
	CreatedAt    string `json:"created_at"`
}

// requirePeopleManager: members and invitations are managed by people (never API keys, so a key cannot
// hand out roles), who are admins or owners of this workspace.
func (s *Service) requirePeopleManager(p Principal) error {
	if err := accountsOnly(s); err != nil {
		return err
	}
	if p.Kind != PrincipalSession || p.UserID == "" {
		return forbidden("members and invitations are managed by a signed-in person")
	}
	if roleRank(p.Role) < roleRank(RoleAdmin) {
		return forbidden("only admins and owners manage members")
	}
	return nil
}

// canAssign: owners grant any role; admins up to admin, and never touch owners.
func canAssign(actor Principal, current, next string) error {
	if roleRank(next) < 0 {
		return invalid("role", "use owner, admin, editor or viewer")
	}
	if actor.Role == RoleOwner {
		return nil
	}
	if next == RoleOwner || current == RoleOwner {
		return forbidden("only owners give or change the owner role")
	}
	return nil
}

// accountOwnerOf: the user who owns the workspace's account ("" for a workspace without one). Errors are
// returned, never swallowed: on PostgreSQL a failed read aborts the surrounding transaction.
func (s *Service) accountOwnerOf(ctx context.Context, q *db.Queries, workspaceID string) (string, error) {
	ws, err := q.GetWorkspace(ctx, workspaceID)
	if err != nil || !ws.AccountID.Valid {
		return "", err
	}
	acc, err := q.GetAccount(ctx, ws.AccountID.String)
	if err != nil {
		return "", err
	}
	return acc.OwnerUserID, nil
}

func (s *Service) ListMembers(ctx context.Context, p Principal) ([]Member, error) {
	if err := accountsOnly(s); err != nil {
		return nil, err
	}
	if err := p.require(ScopeAdmin); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListMembers(ctx, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	owner, err := s.accountOwnerOf(ctx, s.Q, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	out := []Member{}
	for _, r := range rows {
		name := r.Name
		if name == "" {
			name = r.Email
		}
		role := r.Role
		if r.UserID == owner {
			role = RoleOwner
		}
		out = append(out, Member{UserID: r.UserID, Name: name, Email: r.Email, AvatarURL: r.AvatarUrl, Role: role,
			AccountOwner: r.UserID == owner, You: r.UserID == p.UserID, CreatedAt: r.CreatedAt})
	}
	return out, nil
}

// errLastOwner: a workspace always keeps an owner who can manage it.
var errLastOwner = &Error{Code: "last_owner", Status: 409, Message: "a workspace needs at least one owner: make someone else an owner first"}

func (s *Service) UpdateMemberRole(ctx context.Context, p Principal, userID, role string) (Member, error) {
	if err := s.requirePeopleManager(p); err != nil {
		return Member{}, err
	}
	err := s.tx(ctx, func(q *db.Queries) error {
		m, err := q.GetMember(ctx, db.GetMemberParams{WorkspaceID: p.WorkspaceID, UserID: userID})
		if isNoRows(err) {
			return notFound("member")
		}
		if err != nil {
			return err
		}
		owner, err := s.accountOwnerOf(ctx, q, p.WorkspaceID)
		if err != nil {
			return err
		}
		if userID == owner && role != RoleOwner {
			return &Error{Code: "account_owner", Status: 409, Message: "the account owner is always an owner of its workspaces"}
		}
		if err := canAssign(p, m.Role, role); err != nil {
			return err
		}
		if m.Role == RoleOwner && role != RoleOwner {
			n, err := q.CountOwners(ctx, p.WorkspaceID)
			if err != nil {
				return err
			}
			if n <= 1 {
				return errLastOwner
			}
		}
		if _, err := q.UpdateMemberRole(ctx, db.UpdateMemberRoleParams{Role: role, UpdatedAt: s.now(), WorkspaceID: p.WorkspaceID, UserID: userID}); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "member.updated", "", "", map[string]any{"user_id": userID, "role": role, "previous_role": m.Role})
		return err
	})
	if err != nil {
		return Member{}, err
	}
	return s.memberOut(ctx, p, userID)
}

func (s *Service) memberOut(ctx context.Context, p Principal, userID string) (Member, error) {
	ms, err := s.ListMembers(ctx, p)
	if err != nil {
		return Member{}, err
	}
	for _, m := range ms {
		if m.UserID == userID {
			return m, nil
		}
	}
	return Member{}, notFound("member")
}

// RemoveMember: admins remove editors and viewers (and other admins), owners anyone; everyone may leave.
// Their sessions in this workspace end at once; the last owner and the account owner stay.
func (s *Service) RemoveMember(ctx context.Context, p Principal, userID string) error {
	self := p.UserID != "" && p.UserID == userID
	if !self {
		if err := s.requirePeopleManager(p); err != nil {
			return err
		}
	} else if err := accountsOnly(s); err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error {
		m, err := q.GetMember(ctx, db.GetMemberParams{WorkspaceID: p.WorkspaceID, UserID: userID})
		if isNoRows(err) {
			return notFound("member")
		}
		if err != nil {
			return err
		}
		owner, err := s.accountOwnerOf(ctx, q, p.WorkspaceID)
		if err != nil {
			return err
		}
		if userID == owner {
			return &Error{Code: "account_owner", Status: 409, Message: "the account owner cannot be removed from the account's workspaces"}
		}
		if !self && m.Role == RoleOwner && p.Role != RoleOwner {
			return forbidden("only owners remove an owner")
		}
		if m.Role == RoleOwner {
			n, err := q.CountOwners(ctx, p.WorkspaceID)
			if err != nil {
				return err
			}
			if n <= 1 {
				return errLastOwner
			}
		}
		if _, err := q.DeleteMember(ctx, db.DeleteMemberParams{WorkspaceID: p.WorkspaceID, UserID: userID}); err != nil {
			return err
		}
		if err := s.queueSeatSync(ctx, q, p.WorkspaceID); err != nil {
			return err
		}
		if _, err := q.RevokeUserSessionsInWorkspace(ctx, db.RevokeUserSessionsInWorkspaceParams{Now: nullStr(s.now()),
			WorkspaceID: p.WorkspaceID, UserID: nullStr(userID)}); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "member.removed", "", "", map[string]any{"user_id": userID, "role": m.Role, "left": self})
		return err
	})
}

// ---- invitations ----

type Invitation struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	InvitedBy string `json:"invited_by"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
	Expired   bool   `json:"expired"`
	// Link: the accept link, only in the response that created or re-sent it (shown once, like a key).
	Link string `json:"link,omitempty"`
	// Emailed: whether staqly sent the link by email (MAIL_SMTP_HOST configured).
	Emailed *bool `json:"emailed,omitempty"`
}

func (s *Service) toInvitation(r db.WorkspaceInvitation) Invitation {
	return Invitation{ID: r.ID, Email: r.Email, Role: r.Role, InvitedBy: r.InvitedBy, CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt,
		Expired: r.ExpiresAt <= s.now()}
}

const invitePrefix = "staqly-invite-"

func (s *Service) inviteLink(token string) string {
	base := strings.TrimRight(s.Cfg.PublicDashboardURL, "/")
	if base == "" {
		base = "http://localhost:" + s.Cfg.DashboardPort
	}
	return base + "/invite?token=" + url.QueryEscape(token)
}

func normalizeEmail(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	a, err := mail.ParseAddress(v)
	if err != nil || a.Address != v || len(v) > 254 {
		return "", invalid("email", "enter an email address such as ana@example.com")
	}
	return v, nil
}

func (s *Service) ListInvitations(ctx context.Context, p Principal) ([]Invitation, error) {
	if err := s.requirePeopleManager(p); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListOpenInvitations(ctx, p.WorkspaceID)
	out := []Invitation{}
	for _, r := range rows {
		out = append(out, s.toInvitation(r))
	}
	return out, err
}

// CreateInvitation: a single-use, expiring link for one address and role (only its hash is stored).
func (s *Service) CreateInvitation(ctx context.Context, p Principal, email, role string) (Invitation, error) {
	if err := s.requirePeopleManager(p); err != nil {
		return Invitation{}, err
	}
	email, err := normalizeEmail(email)
	if err != nil {
		return Invitation{}, err
	}
	if err := canAssign(p, "", role); err != nil {
		return Invitation{}, err
	}
	token := invitePrefix + randomString(32)
	now := s.now()
	inv := db.WorkspaceInvitation{ID: newID("inv"), WorkspaceID: p.WorkspaceID, Email: email, Role: role, InvitedBy: p.Name,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: formatTime(s.Now().Add(invitationTTL))}
	err = s.tx(ctx, func(q *db.Queries) error {
		members, err := q.ListMembers(ctx, p.WorkspaceID)
		if err != nil {
			return err
		}
		for _, m := range members {
			if m.Email == email {
				return &Error{Code: "already_member", Status: 409, Message: email + " is already a member of this workspace"}
			}
		}
		if err := q.CreateInvitation(ctx, db.CreateInvitationParams{ID: inv.ID, WorkspaceID: inv.WorkspaceID, Email: email, Role: role,
			TokenHash: hashSecret(token), InvitedBy: inv.InvitedBy, CreatedAt: now, UpdatedAt: now, ExpiresAt: inv.ExpiresAt}); err != nil {
			if isUniqueViolation(err) {
				return &Error{Code: "already_invited", Status: 409, Message: email + " already has an open invitation: use Resend"}
			}
			return err
		}
		_, err = s.emit(ctx, q, p, "invitation.created", "", "", map[string]any{"invitation_id": inv.ID, "email": email, "role": role})
		return err
	})
	if err != nil {
		return Invitation{}, err
	}
	return s.deliverInvitation(ctx, p, inv, token), nil
}

// deliverInvitation emails the link when mail is configured (after the commit: no network call in a
// transaction) and returns it to the person who invited, so they can also pass it on themselves.
func (s *Service) deliverInvitation(ctx context.Context, p Principal, inv db.WorkspaceInvitation, token string) Invitation {
	out := s.toInvitation(inv)
	out.Link = s.inviteLink(token)
	sent := false
	if s.mailConfigured() {
		ws, _ := s.Q.GetWorkspace(ctx, p.WorkspaceID)
		subject := fmt.Sprintf("%s invited you to %s on staqly", p.Name, ws.Name)
		text := fmt.Sprintf("%s invited you to the workspace %q on staqly as %s.\n\nAccept the invitation (sign in as %s, with Google or an email code):\n%s\n\nThe link works once and expires on %s.\n",
			p.Name, ws.Name, inv.Role, inv.Email, out.Link, inv.ExpiresAt[:10])
		if err := s.sendSystemEmail(ctx, inv.Email, subject, text); err != nil {
			s.logger(ctx).Warn("invitation email not sent", "invitation_id", inv.ID, "err", err)
		} else {
			sent = true
		}
	}
	out.Emailed = &sent
	return out
}

// ResendInvitation issues a new link (the old one stops working) and a new expiry.
func (s *Service) ResendInvitation(ctx context.Context, p Principal, id string) (Invitation, error) {
	if err := s.requirePeopleManager(p); err != nil {
		return Invitation{}, err
	}
	token := invitePrefix + randomString(32)
	var inv db.WorkspaceInvitation
	err := s.tx(ctx, func(q *db.Queries) error {
		cur, err := q.GetInvitation(ctx, db.GetInvitationParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("invitation")
		}
		if err != nil {
			return err
		}
		if err := canAssign(p, "", cur.Role); err != nil {
			return err
		}
		now := s.now()
		exp := formatTime(s.Now().Add(invitationTTL))
		n, err := q.RefreshInvitation(ctx, db.RefreshInvitationParams{TokenHash: hashSecret(token), ExpiresAt: exp, UpdatedAt: now,
			WorkspaceID: p.WorkspaceID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return &Error{Code: "invitation_closed", Status: 409, Message: "this invitation was already accepted or revoked"}
		}
		cur.ExpiresAt, cur.UpdatedAt = exp, now
		inv = cur
		_, err = s.emit(ctx, q, p, "invitation.resent", "", "", map[string]any{"invitation_id": id, "email": cur.Email})
		return err
	})
	if err != nil {
		return Invitation{}, err
	}
	return s.deliverInvitation(ctx, p, inv, token), nil
}

func (s *Service) RevokeInvitation(ctx context.Context, p Principal, id string) error {
	if err := s.requirePeopleManager(p); err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error {
		cur, err := q.GetInvitation(ctx, db.GetInvitationParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("invitation")
		}
		if err != nil {
			return err
		}
		if err := canAssign(p, "", cur.Role); err != nil {
			return err
		}
		n, err := q.RevokeInvitation(ctx, db.RevokeInvitationParams{Now: nullStr(s.now()), WorkspaceID: p.WorkspaceID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return &Error{Code: "invitation_closed", Status: 409, Message: "this invitation was already accepted or revoked"}
		}
		_, err = s.emit(ctx, q, p, "invitation.revoked", "", "", map[string]any{"invitation_id": id, "email": cur.Email})
		return err
	})
}

// InvitationPreview: what the accept page shows before anyone signs in.
type InvitationPreview struct {
	Workspace string `json:"workspace"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	InvitedBy string `json:"invited_by"`
	Status    string `json:"status"` // open, expired, accepted, revoked
	ExpiresAt string `json:"expires_at"`
}

func (s *Service) LookupInvitation(ctx context.Context, token string) (InvitationPreview, error) {
	if err := accountsOnly(s); err != nil {
		return InvitationPreview{}, err
	}
	inv, err := s.Q.GetInvitationByHash(ctx, hashSecret(strings.TrimSpace(token)))
	if err != nil {
		return InvitationPreview{}, notFound("invitation")
	}
	ws, err := s.Q.GetWorkspace(ctx, inv.WorkspaceID)
	if err != nil {
		return InvitationPreview{}, notFound("invitation")
	}
	status := "open"
	switch {
	case inv.AcceptedAt.Valid:
		status = "accepted"
	case inv.RevokedAt.Valid:
		status = "revoked"
	case inv.ExpiresAt <= s.now():
		status = "expired"
	}
	return InvitationPreview{Workspace: ws.Name, Email: inv.Email, Role: inv.Role, InvitedBy: inv.InvitedBy, Status: status, ExpiresAt: inv.ExpiresAt}, nil
}

// AcceptInvitation: a signed-in person accepts a link for their own address; their session moves there.
func (s *Service) AcceptInvitation(ctx context.Context, p Principal, token string) (MyWorkspace, error) {
	if err := accountsOnly(s); err != nil {
		return MyWorkspace{}, err
	}
	if p.Kind != PrincipalSession || p.UserID == "" {
		return MyWorkspace{}, forbidden("sign in to accept an invitation")
	}
	var out MyWorkspace
	err := s.tx(ctx, func(q *db.Queries) error {
		user, err := q.GetUser(ctx, p.UserID)
		if err != nil {
			return ErrUnauthorized
		}
		wsID, err := s.acceptInvitation(ctx, q, user, token)
		if err != nil {
			return err
		}
		if _, err := q.SetSessionWorkspace(ctx, db.SetSessionWorkspaceParams{WorkspaceID: wsID, ID: p.ID, UserID: nullStr(p.UserID)}); err != nil {
			return err
		}
		ws, err := q.GetWorkspace(ctx, wsID)
		if err != nil {
			return err
		}
		m, err := q.GetMember(ctx, db.GetMemberParams{WorkspaceID: wsID, UserID: user.ID})
		if err != nil {
			return err
		}
		out = MyWorkspace{ID: ws.ID, Name: ws.Name, Role: m.Role, Current: true}
		return nil
	})
	return out, err
}

var (
	errInvitationInvalid = &Error{Code: "invitation_invalid", Status: 410, Message: "this invitation link is not valid any more: ask for a new one"}
	errInvitationExpired = &Error{Code: "invitation_expired", Status: 410, Message: "this invitation expired: ask for a new one"}
)

// acceptInvitation (in the caller's transaction): single use, for the invited address only, idempotent for
// the person who already accepted it. A workspace from before accounts that gets its first owner this way
// joins that person's account (created for them if they have none), so it gains an account owner.
func (s *Service) acceptInvitation(ctx context.Context, q *db.Queries, user db.User, token string) (string, error) {
	inv, err := q.GetInvitationByHash(ctx, hashSecret(strings.TrimSpace(token)))
	if isNoRows(err) {
		return "", errInvitationInvalid
	}
	if err != nil {
		return "", err
	}
	ok, err := s.userHasAddress(ctx, q, user, inv.Email)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", &Error{Code: "invitation_email_mismatch", Status: 403,
			Message: "this invitation is for " + inv.Email + ": sign in with that address"}
	}
	switch {
	case inv.AcceptedAt.Valid && inv.AcceptedBy.String == user.ID:
		return inv.WorkspaceID, nil // accepted already (a double click, a retry)
	case inv.AcceptedAt.Valid || inv.RevokedAt.Valid:
		return "", errInvitationInvalid
	case inv.ExpiresAt <= s.now():
		return "", errInvitationExpired
	}
	now := s.now()
	n, err := q.AcceptInvitation(ctx, db.AcceptInvitationParams{Now: nullStr(now), UserID: nullStr(user.ID), ID: inv.ID})
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", errInvitationInvalid // accepted concurrently by someone else
	}
	if _, err := q.AddMember(ctx, db.AddMemberParams{WorkspaceID: inv.WorkspaceID, UserID: user.ID, Role: inv.Role, CreatedAt: now, UpdatedAt: now}); err != nil {
		return "", err
	}
	if err := s.queueSeatSync(ctx, q, inv.WorkspaceID); err != nil { // one more seat billed
		return "", err
	}
	m, err := q.GetMember(ctx, db.GetMemberParams{WorkspaceID: inv.WorkspaceID, UserID: user.ID})
	if err != nil {
		return "", err
	}
	if roleRank(inv.Role) > roleRank(m.Role) {
		// Already a member with a smaller role: the invitation raises it (an admin chose that role).
		if _, err := q.UpdateMemberRole(ctx, db.UpdateMemberRoleParams{Role: inv.Role, UpdatedAt: now, WorkspaceID: inv.WorkspaceID, UserID: user.ID}); err != nil {
			return "", err
		}
	}
	ws, err := q.GetWorkspace(ctx, inv.WorkspaceID)
	if err != nil {
		return "", err
	}
	if !ws.AccountID.Valid && inv.Role == RoleOwner {
		acc, err := q.GetAccountByOwner(ctx, user.ID)
		if isNoRows(err) {
			acc = db.Account{ID: newID("acc"), OwnerUserID: user.ID, Name: displayName(user)}
			if err := q.CreateAccount(ctx, db.CreateAccountParams{ID: acc.ID, OwnerUserID: user.ID, Name: acc.Name, CreatedAt: now, UpdatedAt: now}); err != nil {
				return "", err
			}
		} else if err != nil {
			return "", err
		}
		if _, err := q.SetWorkspaceAccount(ctx, db.SetWorkspaceAccountParams{AccountID: nullStr(acc.ID), UpdatedAt: now, ID: ws.ID}); err != nil {
			return "", err
		}
		if err := s.attachedToAccount(ctx, q, ws.ID, acc.ID, user); err != nil {
			return "", err
		}
	}
	p := Principal{WorkspaceID: inv.WorkspaceID, Kind: PrincipalSystem, ID: "invitation", Name: displayName(user), UserID: user.ID}
	if _, err := s.emit(ctx, q, p, "invitation.accepted", "", "", map[string]any{"invitation_id": inv.ID, "user_id": user.ID,
		"email": user.Email, "role": inv.Role}); err != nil {
		return "", err
	}
	return inv.WorkspaceID, nil
}

// isUniqueViolation: SQLite and PostgreSQL both say "unique" in a unique constraint error.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate key")
}

// ErrAccountsMisconfigured explains a cloud start without a way to sign in (config.Load).
var ErrAccountsMisconfigured = errors.New("cloud mode needs a way to sign in: Google (GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET, " +
	"with PUBLIC_DASHBOARD_URL or GOOGLE_REDIRECT_URL), email codes (MAIL_SMTP_HOST and MAIL_FROM), or both; " +
	"for development only, AUTH_DEV_SIGNIN=true")

func (s *Service) workspaceHasAccount(ctx context.Context, workspaceID string) bool {
	ws, err := s.Q.GetWorkspace(ctx, workspaceID)
	return err == nil && ws.AccountID.Valid
}
