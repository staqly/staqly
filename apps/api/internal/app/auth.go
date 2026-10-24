package app

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Secrets are 160+ bits of crypto/rand, so a plain SHA-256 is a sufficient
// verifier (no password stretching needed); only hashes are stored.
const (
	apiKeyPrefix   = "stk_"
	recoveryPrefix = "staqly-recovery-"
)

func hashSecret(v string) string {
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:])
}

func newAPIKeySecret() string { return apiKeyPrefix + randomString(32) }

func newRecoveryKey() string {
	raw := strings.ToUpper(randomString(25)) // 40 chars, 200 bits
	parts := make([]string, 0, 8)
	for i := 0; i < len(raw); i += 5 {
		parts = append(parts, raw[i:i+5])
	}
	return recoveryPrefix + strings.Join(parts, "-")
}

func normalizeRecoveryKey(v string) string {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(strings.ToLower(v), recoveryPrefix) {
		return v
	}
	return recoveryPrefix + strings.ToUpper(v[len(recoveryPrefix):])
}

type Workspace struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Plan              string `json:"plan"`
	RecoveryRotatedAt string `json:"recovery_rotated_at"`
	CreatedAt         string `json:"created_at"`
	// Timezone is an IANA name (default UTC): repeats and schedule rules without a zone use it.
	Timezone string `json:"timezone"`
	// Logo is a PNG or JPEG data: URL, '' for staqly's.
	Logo string `json:"logo"`
}

// The processing timeout used to live here; since migration 00024 it is per collection (and per item), and
// workspaces.processing_timeout_seconds is no longer read.
func (s *Service) toWorkspace(w db.Workspace) Workspace {
	tz := w.Timezone
	if tz == "" {
		tz = "UTC"
	}
	return Workspace{ID: w.ID, Name: w.Name, Plan: w.Plan, RecoveryRotatedAt: w.RecoveryRotatedAt, CreatedAt: w.CreatedAt, Timezone: tz, Logo: w.Logo}
}

// WorkspaceUpdate changes the name, the time zone and the logo ('' removes it); nil fields stay as they are.
type WorkspaceUpdate struct {
	Name     *string `json:"name"`
	Timezone *string `json:"timezone"`
	Logo     *string `json:"logo"`
}

type APIKey struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Prefix     string   `json:"prefix"`
	Scopes     []string `json:"scopes"`
	CreatedAt  string   `json:"created_at"`
	LastUsedAt *string  `json:"last_used_at"`
	ExpiresAt  *string  `json:"expires_at"`
	RevokedAt  *string  `json:"revoked_at"`
	// Key is the raw secret, only present in the response that created it.
	Key string `json:"key,omitempty"`
}

func ptr(n sql.NullString) *string {
	if n.Valid {
		return &n.String
	}
	return nil
}

func toAPIKey(k db.ApiKey) APIKey {
	var scopes []string
	_ = json.Unmarshal([]byte(k.Scopes), &scopes)
	return APIKey{
		ID: k.ID, Name: k.Name, Prefix: k.Prefix, Scopes: scopes, CreatedAt: k.CreatedAt,
		LastUsedAt: ptr(k.LastUsedAt), ExpiresAt: ptr(k.ExpiresAt), RevokedAt: ptr(k.RevokedAt),
	}
}

type Session struct {
	ID         string `json:"id"`
	UserAgent  string `json:"user_agent"`
	CreatedAt  string `json:"created_at"`
	LastSeenAt string `json:"last_seen_at"`
	ExpiresAt  string `json:"expires_at"`
	Current    bool   `json:"current"`
}

// NewSession is returned when a browser session is created. Token goes into
// an HttpOnly cookie and is never shown to scripts.
type NewSession struct {
	Token     string
	CSRFToken string
	ExpiresAt time.Time
}

type SetupResult struct {
	Workspace   Workspace  `json:"workspace"`
	APIKey      APIKey     `json:"api_key"`
	RecoveryKey string     `json:"recovery_key"`
	Session     NewSession `json:"-"`
}

func (s *Service) Initialized(ctx context.Context) (bool, error) {
	_, err := s.Q.GetInstallation(ctx)
	if isNoRows(err) {
		return false, nil
	}
	return err == nil, err
}

// SignupOpen reports whether anyone may create a cloud workspace (no
// SETUP_TOKEN configured) or only holders of the token.
func (s *Service) SignupOpen() bool { return s.Cfg.Mode == ModeCloud && s.Cfg.SetupToken == "" }

// Setup creates a new cloud workspace with an admin API key (the dashboard
// sign-in credential), a single-use recovery key and a browser session. Each call creates a separate,
// isolated workspace; if SETUP_TOKEN is set, only token holders may sign up.
// (Local mode has exactly one workspace, created at startup.)
func (s *Service) Setup(ctx context.Context, name, setupToken, userAgent string) (SetupResult, error) {
	if s.Cfg.Mode != ModeCloud {
		return SetupResult{}, conflict("setup is only used in cloud mode; the local workspace is created automatically")
	}
	if !s.LegacySignIn() {
		return SetupResult{}, &Error{Code: "sign_in_with_account", Status: 409, Message: "new workspaces start on the sign-in page: " + s.accountSignInMethods()}
	}
	if s.Cfg.SetupToken != "" && subtle.ConstantTimeCompare([]byte(setupToken), []byte(s.Cfg.SetupToken)) != 1 {
		return SetupResult{}, &Error{Code: "forbidden", Status: 403, Message: "invalid setup token"}
	}
	name, err := trimmed(name, "name", 100, false)
	if err != nil {
		return SetupResult{}, err
	}
	if name == "" {
		name = "My Workspace"
	}
	now := s.now()
	ws := db.Workspace{ID: newID("ws"), Name: name, Plan: s.trialPlan(), RecoveryRotatedAt: now, CreatedAt: now, UpdatedAt: now} // 7-day trial
	recovery := newRecoveryKey()
	var res SetupResult
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := q.CreateWorkspace(ctx, db.CreateWorkspaceParams{
			ID: ws.ID, Name: ws.Name, RecoveryVerifier: hashSecret(recovery),
			RecoveryRotatedAt: now, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		// A new cloud workspace: with billing, no plan until its owner starts a subscription (Starter: 7 days free,
		// card required); without billing, the server-side trial.
		planID, ends := s.newWorkspacePlan()
		if _, err := q.SetWorkspacePlan(ctx, db.SetWorkspacePlanParams{Plan: planID,
			PlanExpiresAt: sql.NullString{String: ends, Valid: true}, UpdatedAt: now, ID: ws.ID}); err != nil {
			return err
		}
		if err := s.ensureDefaultCollection(ctx, q, ws.ID); err != nil {
			return err
		}
		sys := Principal{WorkspaceID: ws.ID, Kind: PrincipalSystem, ID: "setup", Name: "Setup"}
		// No expiry, unlike CreateAPIKey's year: this key is how a workspace from before accounts signs in to the
		// dashboard (the recovery key replaces it when lost). Workspaces with people never get one (Setup is legacy only).
		key, err := s.createKey(ctx, q, sys, "Admin key", AdminKeyScopes, nil)
		if err != nil {
			return err
		}
		sess, err := s.createSession(ctx, q, ws.ID, userAgent)
		if err != nil {
			return err
		}
		if _, err := s.emit(ctx, q, sys, "workspace.created", "", "", map[string]any{"workspace": s.toWorkspace(ws)}); err != nil {
			return err
		}
		res = SetupResult{Workspace: s.toWorkspace(ws), APIKey: key, RecoveryKey: recovery, Session: sess}
		return nil
	})
	return res, err
}

// EnsureLocalWorkspace creates the workspace on first start in local mode
// (no keys, no recovery key: access is limited to this machine instead).
func (s *Service) EnsureLocalWorkspace(ctx context.Context) error {
	if ok, err := s.Initialized(ctx); ok || err != nil {
		return err
	}
	now := s.now()
	ws := db.Workspace{ID: newID("ws"), Name: "Local workspace", Plan: UnlockedPlan.ID, CreatedAt: now}
	err := s.tx(ctx, func(q *db.Queries) error {
		if err := q.CreateInstallation(ctx, db.CreateInstallationParams{WorkspaceID: ws.ID, CreatedAt: now}); err != nil {
			return conflict("workspace already initialized")
		}
		// Unusable verifier; `server recovery-key` issues a real one if the
		// data is later served in cloud mode.
		if err := q.CreateWorkspace(ctx, db.CreateWorkspaceParams{ID: ws.ID, Name: ws.Name,
			RecoveryVerifier: hashSecret(randomString(32)), RecoveryRotatedAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		// This machine's zone: a local workspace's repeats and schedules follow the clock on the wall.
		if err := s.setWorkspaceTimezone(ctx, q, &ws, localZoneName()); err != nil {
			return err
		}
		if err := s.ensureDefaultCollection(ctx, q, ws.ID); err != nil {
			return err
		}
		sys := Principal{WorkspaceID: ws.ID, Kind: PrincipalSystem, ID: "local", Name: "Local setup"}
		_, err := s.emit(ctx, q, sys, "workspace.created", "", "", map[string]any{"workspace": s.toWorkspace(ws)})
		return err
	})
	if isConflict(err) {
		return nil // another process created it concurrently
	}
	return err
}

// LocalPrincipal is local mode's full-access principal: the access token's holder (local_token.go) and
// cmd/mcp-stdio, which opens the database itself.
func (s *Service) LocalPrincipal(ctx context.Context) (Principal, error) {
	inst, err := s.Q.GetInstallation(ctx)
	if err != nil {
		return Principal{}, ErrUnauthorized
	}
	return Principal{WorkspaceID: inst.WorkspaceID, Kind: PrincipalLocal, ID: "local", Name: s.localUserName()}, nil
}

// ResetRecoveryKey issues a new recovery key for a workspace (default: the
// local one) and signs out all of its browser sessions, like a rotation from
// the dashboard. It is only reachable from the server CLI (`server
// recovery-key`), i.e. by whoever controls the host and its data volume.
// It returns the key and the number of sessions revoked.
func (s *Service) ResetRecoveryKey(ctx context.Context, workspaceID string) (string, int64, error) {
	if s.Cfg.Mode == ModeLocal {
		return "", 0, fmt.Errorf("the Community Edition (local mode) has no recovery key: it signs in with its access token; " +
			"print it with `server token`, or make a new one with `server token --rotate` (every browser is signed out)")
	}
	if workspaceID == "" {
		inst, err := s.Q.GetInstallation(ctx)
		if err != nil {
			return "", 0, fmt.Errorf("no local workspace; pass a workspace id (see `server workspaces`)")
		}
		workspaceID = inst.WorkspaceID
	}
	if _, err := s.Q.GetWorkspace(ctx, workspaceID); err != nil {
		return "", 0, fmt.Errorf("workspace %s not found", workspaceID)
	}
	p := Principal{WorkspaceID: workspaceID, Kind: PrincipalSystem, ID: "cli", Name: "Server CLI"}
	return s.replaceRecoveryKey(ctx, p, "", map[string]any{"via": "cli"})
}

// replaceRecoveryKey stores a new recovery key and revokes every session of
// the workspace except keepSessionID ("" = all) in one transaction. Shared by
// the dashboard rotation and the server CLI.
func (s *Service) replaceRecoveryKey(ctx context.Context, p Principal, keepSessionID string, data map[string]any) (string, int64, error) {
	key := newRecoveryKey()
	now := s.now()
	var revoked int64
	err := s.tx(ctx, func(q *db.Queries) error {
		if err := q.UpdateRecoveryVerifier(ctx, db.UpdateRecoveryVerifierParams{
			RecoveryVerifier: hashSecret(key), RecoveryRotatedAt: now, UpdatedAt: now, ID: p.WorkspaceID,
		}); err != nil {
			return err
		}
		var err error
		revoked, err = q.RevokeOtherSessions(ctx, db.RevokeOtherSessionsParams{RevokedAt: nullStr(now), WorkspaceID: p.WorkspaceID, KeepID: keepSessionID})
		if err != nil {
			return err
		}
		if data == nil {
			data = map[string]any{}
		}
		data["sessions_revoked"] = revoked
		_, err = s.emit(ctx, q, p, "recovery_key.rotated", "", "", data)
		return err
	})
	return key, revoked, err
}

func (s *Service) GetWorkspace(ctx context.Context, p Principal) (Workspace, error) {
	if p.WorkspaceID == "" {
		return Workspace{}, ErrUnauthorized
	}
	w, err := s.Q.GetWorkspace(ctx, p.WorkspaceID)
	if err != nil {
		return Workspace{}, err
	}
	out := s.toWorkspace(w)
	if plan, err := s.planFor(ctx, s.Q, p.WorkspaceID); err == nil {
		out.Plan = plan.ID
	}
	return out, nil
}

func (s *Service) UpdateWorkspace(ctx context.Context, p Principal, in WorkspaceUpdate) (Workspace, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return Workspace{}, err
	}
	if in.Name == nil && in.Timezone == nil && in.Logo == nil {
		return Workspace{}, invalid("name", "send a name, a timezone or a logo")
	}
	changes := map[string]any{}
	var name, tz string
	var err error
	if in.Name != nil {
		if name, err = trimmed(*in.Name, "name", 100, true); err != nil {
			return Workspace{}, err
		}
		changes["name"] = name
	}
	if in.Timezone != nil {
		if tz, err = checkTimezone(*in.Timezone); err != nil {
			return Workspace{}, err
		}
		changes["timezone"] = tz
	}
	if in.Logo != nil {
		if err := checkLogo(*in.Logo); err != nil {
			return Workspace{}, err
		}
		changes["logo"] = *in.Logo != "" // whether there is one, never the image
	}
	if err := s.tx(ctx, func(q *db.Queries) error {
		now := s.now()
		if in.Name != nil {
			if err := q.UpdateWorkspaceName(ctx, db.UpdateWorkspaceNameParams{Name: name, UpdatedAt: now, ID: p.WorkspaceID}); err != nil {
				return err
			}
		}
		if in.Timezone != nil {
			if err := q.UpdateWorkspaceTimezone(ctx, db.UpdateWorkspaceTimezoneParams{Timezone: tz, UpdatedAt: now, ID: p.WorkspaceID}); err != nil {
				return err
			}
		}
		if in.Logo != nil {
			if err := q.UpdateWorkspaceLogo(ctx, db.UpdateWorkspaceLogoParams{Logo: *in.Logo, UpdatedAt: now, ID: p.WorkspaceID}); err != nil {
				return err
			}
		}
		_, err := s.emit(ctx, q, p, "workspace.updated", "", "", changes)
		return err
	}); err != nil {
		return Workspace{}, err
	}
	return s.GetWorkspace(ctx, p)
}

// ErrLocalAPIKey: the Community Edition (local mode) takes only its access token, never an API key (not even
// one created while the same data ran in cloud mode).
var ErrLocalAPIKey = &Error{Code: "unauthorized", Status: 401,
	Message: "the Community Edition (local mode) takes no API keys: send its access token instead; print it with `staqly serve token`, or `docker exec staqly server token` for Docker"}

// AuthenticateAPIKey resolves a bearer key to a principal. In local mode every key is refused (ErrLocalAPIKey).
func (s *Service) AuthenticateAPIKey(ctx context.Context, raw string) (Principal, error) {
	if s.Cfg.Mode == ModeLocal {
		return Principal{}, ErrLocalAPIKey
	}
	if !strings.HasPrefix(raw, apiKeyPrefix) {
		return Principal{}, ErrUnauthorized
	}
	k, err := s.Q.GetAPIKeyByHash(ctx, hashSecret(raw))
	if err != nil {
		return Principal{}, ErrUnauthorized
	}
	now := s.now()
	if k.RevokedAt.Valid || (k.ExpiresAt.Valid && k.ExpiresAt.String <= now) {
		return Principal{}, ErrUnauthorized
	}
	// last_used_at is informational: write it at most once a minute per key,
	// so busy agents don't turn every request into a database write.
	if last, err := time.Parse(timeFormat, k.LastUsedAt.String); !k.LastUsedAt.Valid || err != nil || s.Now().Sub(last) >= time.Minute {
		_ = s.W.TouchAPIKey(ctx, db.TouchAPIKeyParams{LastUsedAt: nullStr(now), ID: k.ID})
	}
	var scopes []string
	_ = json.Unmarshal([]byte(k.Scopes), &scopes)
	p := Principal{WorkspaceID: k.WorkspaceID, Kind: PrincipalAPIKey, ID: k.ID, Name: k.Name, Scopes: scopes}
	if k.CreatedByUserID.Valid {
		// A key never does more than the person who created it may do now: a demotion narrows it, a
		// removal from the workspace turns it off.
		up, err := s.userPrincipal(ctx, s.Q, k.CreatedByUserID.String, k.WorkspaceID)
		if err != nil {
			return Principal{}, ErrUnauthorized
		}
		p.Scopes, p.UserID, p.Role = intersectScopes(scopes, up.Scopes), up.UserID, up.Role
	}
	s.trackActive(p)
	return p, nil
}

// intersectScopes: what both scope lists allow (admin in a list covers every scope).
func intersectScopes(a, b []string) []string {
	has := func(l []string, sc string) bool { return slices.Contains(l, sc) || slices.Contains(l, ScopeAdmin) }
	out := []string{}
	for _, sc := range AllScopes {
		if has(a, sc) && has(b, sc) {
			out = append(out, sc)
		}
	}
	return out
}

// AuthenticateSession resolves a session cookie; it also returns the CSRF
// token the caller must echo on unsafe requests.
func (s *Service) AuthenticateSession(ctx context.Context, token string) (Principal, string, error) {
	if token == "" {
		return Principal{}, "", ErrUnauthorized
	}
	sess, err := s.Q.GetSessionByHash(ctx, hashSecret(token))
	if err != nil {
		return Principal{}, "", ErrUnauthorized
	}
	now := s.Now().UTC()
	if sess.RevokedAt.Valid || sess.ExpiresAt <= formatTime(now) {
		return Principal{}, "", ErrUnauthorized
	}
	if last, err := time.Parse(timeFormat, sess.LastSeenAt); err == nil && now.Sub(last) > time.Minute {
		_ = s.W.TouchSession(ctx, db.TouchSessionParams{LastSeenAt: formatTime(now), ID: sess.ID})
	}
	if sess.UserID.Valid {
		// A person signed in with Google: their role in this workspace, read on every request.
		p, err := s.userPrincipal(ctx, s.Q, sess.UserID.String, sess.WorkspaceID)
		if err != nil {
			return Principal{}, "", ErrUnauthorized
		}
		p.Kind, p.ID = PrincipalSession, sess.ID
		s.trackActive(p)
		return p, sess.CsrfToken, nil
	}
	// A session with no person behind it (development API-key sign-in): name what it is, not the page.
	name := "Workspace admin"
	if s.Cfg.Mode == ModeLocal {
		name = s.localUserName() // one person per local install
	}
	return Principal{WorkspaceID: sess.WorkspaceID, Kind: PrincipalSession, ID: sess.ID, Name: name}, sess.CsrfToken, nil
}

func (s *Service) createSession(ctx context.Context, q *db.Queries, workspaceID, userAgent string) (NewSession, error) {
	ns, _, err := s.createSessionID(ctx, q, workspaceID, userAgent)
	return ns, err
}

func (s *Service) createSessionID(ctx context.Context, q *db.Queries, workspaceID, userAgent string) (NewSession, string, error) {
	now := s.Now().UTC()
	ns := NewSession{Token: randomString(32), CSRFToken: randomString(20), ExpiresAt: now.Add(s.Cfg.SessionTTL)}
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	id := newID("ses")
	err := q.CreateSession(ctx, db.CreateSessionParams{
		ID: id, WorkspaceID: workspaceID, TokenHash: hashSecret(ns.Token), CsrfToken: ns.CSRFToken,
		UserAgent: userAgent, CreatedAt: formatTime(now), LastSeenAt: formatTime(now), ExpiresAt: formatTime(ns.ExpiresAt),
	})
	return ns, id, err
}

// SignInWithAPIKey exchanges an admin API key for a browser session (the
// cloud dashboard's sign-in; in local mode, the access token). Agent keys without the admin scope never open
// the dashboard. Other sessions stay signed in.
func (s *Service) SignInWithAPIKey(ctx context.Context, rawKey, userAgent string) (NewSession, error) {
	if s.Cfg.Mode != ModeCloud {
		return s.SignInWithLocalToken(ctx, rawKey, userAgent) // local mode: the access token is its key
	}
	p, err := s.AuthenticateAPIKey(ctx, strings.TrimSpace(rawKey))
	if err != nil {
		return NewSession{}, &Error{Code: "unauthorized", Status: 401, Message: "invalid, revoked or expired API key"}
	}
	if p.UserID != "" || s.workspaceHasPeople(ctx, s.Q, p.WorkspaceID) {
		return NewSession{}, s.signInWithAccount()
	}
	if !p.Has(ScopeAdmin) {
		return NewSession{}, &Error{Code: "forbidden", Status: 403,
			Message: "this API key cannot sign in: only keys with the admin scope open the dashboard (agent keys stay API-only)"}
	}
	var ns NewSession
	err = s.tx(ctx, func(q *db.Queries) error {
		var err error
		var id string
		if ns, id, err = s.createSessionID(ctx, q, p.WorkspaceID, userAgent); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "session.signed_in", "", "", map[string]any{"user_agent": userAgent, "session_id": id,
			"api_key_id": p.ID})
		return err
	})
	return ns, err
}

// RecoverResult is what a recovery returns: a session, a new admin API key
// and the next recovery key (both shown once).
type RecoverResult struct {
	Session     NewSession
	APIKey      APIKey
	RecoveryKey string
}

// Recover is the "lost your key?" path: the recovery key buys a new browser
// session, a new admin API key and a new recovery key, all in one transaction.
// The recovery key is single-use (it is rotated out). On plans with a key limit
// (Starter, Plus) the old admin keys are revoked, since the new one takes their slot;
// other keys keep working. Every other session is signed out (whoever used the key may be
// locking out an intruder). The recovery key is never accepted anywhere else.
func (s *Service) Recover(ctx context.Context, recoveryKey, userAgent string) (RecoverResult, error) {
	if s.Cfg.Mode == ModeLocal {
		return RecoverResult{}, errLocalNoRecovery()
	}
	// Brute force: 200-bit keys, and the route is rate-limited per IP (sensitiveGroup).
	invalidKey := &Error{Code: "unauthorized", Status: 401, Message: "invalid recovery key (each recovery key works once)"}
	oldVerifier := hashSecret(normalizeRecoveryKey(recoveryKey))
	ws, err := s.Q.GetWorkspaceByRecoveryVerifier(ctx, oldVerifier)
	if err != nil {
		return RecoverResult{}, invalidKey
	}
	if s.workspaceHasPeople(ctx, s.Q, ws.ID) {
		// A workspace with people signs in with Google or an email code; a recovery key would open a session
		// without a role.
		return RecoverResult{}, s.signInWithAccount()
	}
	next := newRecoveryKey()
	var res RecoverResult
	err = s.tx(ctx, func(q *db.Queries) error {
		n, err := q.SwapRecoveryVerifier(ctx, db.SwapRecoveryVerifierParams{NewVerifier: hashSecret(next), RotatedAt: s.now(),
			ID: ws.ID, OldVerifier: oldVerifier})
		if err != nil {
			return err
		}
		if n == 0 {
			return invalidKey // used concurrently
		}
		sys := Principal{WorkspaceID: ws.ID, Kind: PrincipalSystem, ID: "recovery", Name: "Recovery key"}
		replaced, err := s.revokeLostAdminKeys(ctx, q, sys)
		if err != nil {
			return err
		}
		// Never blocked by the plan's key limit: recovery must always restore access. No expiry, like Setup's key:
		// it is the sign-in credential of a workspace from before accounts (workspaces with people refuse recovery).
		key, err := s.insertKey(ctx, q, sys, "Admin key (recovered)", AdminKeyScopes, nil)
		if err != nil {
			return err
		}
		ns, id, err := s.createSessionID(ctx, q, ws.ID, userAgent)
		if err != nil {
			return err
		}
		revoked, err := q.RevokeOtherSessions(ctx, db.RevokeOtherSessionsParams{RevokedAt: nullStr(s.now()), WorkspaceID: ws.ID, KeepID: id})
		if err != nil {
			return err
		}
		if _, err := s.emit(ctx, q, sys, "recovery_key.rotated", "", "", map[string]any{"via": "recovery", "sessions_revoked": revoked}); err != nil {
			return err
		}
		if _, err := s.emit(ctx, q, sys, "session.recovered", "", "", map[string]any{"user_agent": userAgent, "sessions_revoked": revoked,
			"api_key_id": key.ID, "api_keys_replaced": replaced}); err != nil {
			return err
		}
		res = RecoverResult{Session: ns, APIKey: key, RecoveryKey: next}
		return nil
	})
	return res, err
}

// revokeLostAdminKeys: on a plan with a key limit (Starter, Plus), the new
// admin key replaces the lost ones, so the workspace stays within its limit and
// a lost key stops working. Unlimited plans keep every key.
func (s *Service) revokeLostAdminKeys(ctx context.Context, q *db.Queries, sys Principal) ([]string, error) {
	plan, err := s.planFor(ctx, q, sys.WorkspaceID)
	if err != nil || plan.Limits.APIKeys == Unlimited {
		return []string{}, err
	}
	keys, err := q.ListAPIKeys(ctx, sys.WorkspaceID)
	if err != nil {
		return nil, err
	}
	replaced := []string{}
	for _, k := range keys {
		var scopes []string
		if k.RevokedAt.Valid || json.Unmarshal([]byte(k.Scopes), &scopes) != nil || !slices.Contains(scopes, ScopeAdmin) {
			continue
		}
		if _, err := q.RevokeAPIKey(ctx, db.RevokeAPIKeyParams{RevokedAt: nullStr(s.now()), WorkspaceID: sys.WorkspaceID, ID: k.ID}); err != nil {
			return nil, err
		}
		if _, err := s.emit(ctx, q, sys, "api_key.revoked", "", "", map[string]any{"api_key_id": k.ID, "reason": "recovery"}); err != nil {
			return nil, err
		}
		replaced = append(replaced, k.ID)
	}
	return replaced, nil
}

// RotateRecoveryKey is restricted to dashboard sessions: an API key must never
// be able to mint the credential that recovers admin access. Every other
// session is signed out (rotation is what you do after a suspected leak).
func (s *Service) RotateRecoveryKey(ctx context.Context, p Principal) (string, error) {
	if s.Cfg.Mode == ModeLocal {
		return "", errLocalNoRecovery()
	}
	if p.Kind != PrincipalSession {
		return "", &Error{Code: "forbidden", Status: 403, Message: "recovery key can only be rotated from a dashboard session"}
	}
	if p.UserID != "" || s.workspaceHasPeople(ctx, s.Q, p.WorkspaceID) {
		return "", conflict("people recover access with their sign-in methods (email code, Google or a passkey), not a recovery key")
	}
	key, _, err := s.replaceRecoveryKey(ctx, p, p.ID, nil)
	return key, err
}

func (s *Service) Logout(ctx context.Context, p Principal) error {
	if p.Kind != PrincipalSession {
		return nil
	}
	return s.revokeSession(ctx, p, p.ID, "logout")
}

func (s *Service) revokeSession(ctx context.Context, p Principal, id, reason string) error {
	return s.tx(ctx, func(q *db.Queries) error {
		n, err := q.RevokeSession(ctx, db.RevokeSessionParams{RevokedAt: nullStr(s.now()), WorkspaceID: p.WorkspaceID, ID: id})
		if err == nil && n == 0 {
			if old, gerr := q.GetSession(ctx, db.GetSessionParams{WorkspaceID: p.WorkspaceID, ID: id}); gerr == nil && old.RevokedAt.Valid {
				return alreadyErr("session", "revoked")
			}
			return notFound("session")
		}
		if err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "session.revoked", "", "", map[string]any{"session_id": id, "reason": reason})
		return err
	})
}

func (s *Service) ListSessions(ctx context.Context, p Principal) ([]Session, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListSessions(ctx, db.ListSessionsParams{WorkspaceID: p.WorkspaceID, ExpiresAt: s.now()})
	if err != nil {
		return nil, err
	}
	owners, err := s.ownersHiddenFrom(ctx, p)
	if err != nil {
		return nil, err
	}
	out := []Session{}
	for _, r := range rows {
		if r.UserID.Valid && owners[r.UserID.String] {
			continue
		}
		out = append(out, Session{ID: r.ID, UserAgent: r.UserAgent, CreatedAt: r.CreatedAt, LastSeenAt: r.LastSeenAt,
			ExpiresAt: r.ExpiresAt, Current: p.Kind == PrincipalSession && p.ID == r.ID})
	}
	return out, nil
}

func (s *Service) RevokeSession(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopeAdmin); err != nil {
		return err
	}
	owners, err := s.ownersHiddenFrom(ctx, p)
	if err != nil {
		return err
	}
	if len(owners) > 0 {
		if sess, err := s.Q.GetSession(ctx, db.GetSessionParams{WorkspaceID: p.WorkspaceID, ID: id}); err == nil &&
			sess.UserID.Valid && owners[sess.UserID.String] {
			return forbidden("only owners sign out an owner")
		}
	}
	return s.revokeSession(ctx, p, id, "revoked")
}

// ownersHiddenFrom: the owners of the workspace whose sessions p may not see or end,
// like admins never change or remove owners. Empty for owners, and for principals without a role (local
// mode, sessions and keys from before accounts: they hold the whole workspace).
func (s *Service) ownersHiddenFrom(ctx context.Context, p Principal) (map[string]bool, error) {
	if p.Role == "" || p.Role == RoleOwner {
		return nil, nil
	}
	members, err := s.Q.ListMembers(ctx, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	owners := map[string]bool{}
	for _, m := range members {
		if m.Role == RoleOwner {
			owners[m.UserID] = true
		}
	}
	return owners, nil
}

func validateScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return nil, invalid("scopes", "at least one scope is required")
	}
	seen := map[string]bool{}
	out := []string{}
	for _, sc := range scopes {
		ok := false
		for _, a := range AllScopes {
			ok = ok || a == sc
		}
		if !ok {
			return nil, invalid("scopes", "unknown scope "+sc)
		}
		if !seen[sc] {
			seen[sc] = true
			out = append(out, sc)
		}
	}
	return out, nil
}

type CreateAPIKeyInput struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expires_at,omitempty"`
}

// errLocalNoKeys: the Community Edition (local mode) mints no API keys; its access token is the one credential.
func errLocalNoKeys() error {
	return conflict("local mode has no API keys: agents use the access token (Integrations, Agents), other devices sign in with Connect a device")
}

// errLocalNoRecovery: the Community Edition has no recovery key (recovering would mint an admin API key).
func errLocalNoRecovery() error {
	return conflict("local mode has no recovery key: sign in with the access token " +
		"(`staqly serve token`, or `docker exec staqly server token`; `server token --rotate` makes a new one)")
}

// maxKeyLifetime is the longest an API key works (a year).
const maxKeyLifetime = 365 * 24 * time.Hour

func (s *Service) CreateAPIKey(ctx context.Context, p Principal, in CreateAPIKeyInput) (APIKey, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return APIKey{}, err
	}
	// Local agents need no key, and a key would let another device in through the exposed dashboard without
	// a one-time sign-in link, unseen in the local dashboard.
	if s.Cfg.Mode == ModeLocal {
		return APIKey{}, errLocalNoKeys()
	}
	name, err := trimmed(in.Name, "name", 100, true)
	if err != nil {
		return APIKey{}, err
	}
	scopes, err := validateScopes(in.Scopes)
	if err != nil {
		return APIKey{}, err
	}
	// Every key expires within a year: without expires_at it gets the full year, and a later date is refused.
	longest := s.Now().UTC().Add(maxKeyLifetime)
	e := formatTime(longest)
	if in.ExpiresAt != "" {
		v, err := ParseTimeFilter("expires_at", in.ExpiresAt)
		if err != nil {
			return APIKey{}, err
		}
		e = v.(string)
		if e <= s.now() {
			return APIKey{}, invalid("expires_at", "must be in the future")
		}
		if e > formatTime(longest.Add(time.Hour)) {
			return APIKey{}, invalid("expires_at", "can be at most a year from now")
		}
	}
	exp := &e
	var key APIKey
	err = s.tx(ctx, func(q *db.Queries) error {
		var err error
		key, err = s.createKey(ctx, q, p, name, scopes, exp)
		return err
	})
	return key, err
}

func (s *Service) createKey(ctx context.Context, q *db.Queries, p Principal, name string, scopes []string, exp *string) (APIKey, error) {
	plan, err := s.planFor(ctx, q, p.WorkspaceID)
	if err != nil {
		return APIKey{}, err
	}
	n, err := q.CountActiveAPIKeys(ctx, db.CountActiveAPIKeysParams{WorkspaceID: p.WorkspaceID, Now: nullStr(s.now())})
	if err != nil {
		return APIKey{}, err
	}
	if err := checkCount(plan, LimitAPIKeys, plan.Limits.APIKeys, n); err != nil {
		return APIKey{}, err
	}
	key, err := s.insertKey(ctx, q, p, name, scopes, exp)
	if err == nil && p.UserID != "" {
		err = q.SetAPIKeyCreator(ctx, db.SetAPIKeyCreatorParams{CreatedByUserID: nullStr(p.UserID), ID: key.ID})
	}
	return key, err
}

// insertKey creates a key without the plan's key-count check (createKey checks it).
func (s *Service) insertKey(ctx context.Context, q *db.Queries, p Principal, name string, scopes []string, exp *string) (APIKey, error) {
	raw := newAPIKeySecret()
	now := s.now()
	row := db.ApiKey{ID: newID("key"), WorkspaceID: p.WorkspaceID, Name: name, Prefix: raw[:len(apiKeyPrefix)+6],
		KeyHash: hashSecret(raw), Scopes: mustJSON(scopes), CreatedAt: now}
	if exp != nil {
		row.ExpiresAt = nullStr(*exp)
	}
	if err := q.CreateAPIKey(ctx, db.CreateAPIKeyParams{ID: row.ID, WorkspaceID: row.WorkspaceID, Name: name,
		Prefix: row.Prefix, KeyHash: row.KeyHash, Scopes: row.Scopes, CreatedAt: now, ExpiresAt: row.ExpiresAt}); err != nil {
		return APIKey{}, err
	}
	k := toAPIKey(row)
	if _, err := s.emit(ctx, q, p, "api_key.created", "", "", map[string]any{"api_key": k}); err != nil {
		return APIKey{}, err
	}
	k.Key = raw
	return k, nil
}

func (s *Service) ListAPIKeys(ctx context.Context, p Principal) ([]APIKey, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListAPIKeys(ctx, p.WorkspaceID)
	out := []APIKey{}
	for _, r := range rows {
		out = append(out, toAPIKey(r))
	}
	return out, err
}

func (s *Service) RevokeAPIKey(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopeAdmin); err != nil {
		return err
	}
	if err := s.notOwnersKey(ctx, p, id); err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error {
		n, err := q.RevokeAPIKey(ctx, db.RevokeAPIKeyParams{RevokedAt: nullStr(s.now()), WorkspaceID: p.WorkspaceID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			if old, gerr := q.GetAPIKey(ctx, db.GetAPIKeyParams{WorkspaceID: p.WorkspaceID, ID: id}); gerr == nil && old.RevokedAt.Valid {
				return alreadyErr("api key", "revoked")
			}
			return notFound("active api key")
		}
		_, err = s.emit(ctx, q, p, "api_key.revoked", "", "", map[string]any{"api_key_id": id})
		return err
	})
}

// notOwnersKey: an admin manages admins' and editors' keys, not an owner's (an owner's key can carry the
// owners' billing rights), as with an owner's sessions and membership.
func (s *Service) notOwnersKey(ctx context.Context, p Principal, id string) error {
	owners, err := s.ownersHiddenFrom(ctx, p)
	if err != nil || len(owners) == 0 {
		return err
	}
	k, err := s.Q.GetAPIKey(ctx, db.GetAPIKeyParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err != nil {
		return nil // missing keys answer as before (404) in the write
	}
	if k.CreatedByUserID.Valid && owners[k.CreatedByUserID.String] {
		return forbidden("only an owner revokes or rotates an owner's key")
	}
	return nil
}

// RotateAPIKey revokes a key and issues a replacement with the same name and scopes.
func (s *Service) RotateAPIKey(ctx context.Context, p Principal, id string) (APIKey, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return APIKey{}, err
	}
	if s.Cfg.Mode == ModeLocal {
		return APIKey{}, errLocalNoKeys() // a rotation mints a key like CreateAPIKey
	}
	if err := s.notOwnersKey(ctx, p, id); err != nil {
		return APIKey{}, err
	}
	var key APIKey
	err := s.tx(ctx, func(q *db.Queries) error {
		old, err := q.GetAPIKey(ctx, db.GetAPIKeyParams{WorkspaceID: p.WorkspaceID, ID: id})
		if err != nil || old.RevokedAt.Valid {
			return notFound("active api key")
		}
		// The replacement keeps the expiry: of an expired key it would be dead on arrival.
		if old.ExpiresAt.Valid && old.ExpiresAt.String <= s.now() {
			return conflict("the key has expired: create a new key instead")
		}
		if _, err := q.RevokeAPIKey(ctx, db.RevokeAPIKeyParams{RevokedAt: nullStr(s.now()), WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		var scopes []string
		_ = json.Unmarshal([]byte(old.Scopes), &scopes)
		var exp *string
		if old.ExpiresAt.Valid {
			exp = &old.ExpiresAt.String
		}
		if key, err = s.createKey(ctx, q, p, old.Name, scopes, exp); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "api_key.rotated", "", "", map[string]any{"old_key_id": id, "new_key_id": key.ID,
			"name": old.Name})
		return err
	})
	return key, err
}

// signInWithAccount: a workspace with people opens only with a person's sign-in, never with an API key or a
// recovery key. The message names the methods this server offers.
func (s *Service) signInWithAccount() *Error {
	return &Error{Code: "sign_in_with_account", Status: 403, Message: "this workspace has people who sign in: " + s.accountSignInMethods()}
}

// accountSignInMethods: "sign in with Google, an email code or a passkey", as far as this server offers them.
func (s *Service) accountSignInMethods() string {
	methods := []string{}
	if s.GoogleSignIn() {
		methods = append(methods, "Google")
	}
	if s.EmailSignIn() {
		methods = append(methods, "an email code")
	}
	if s.PasskeySignIn() {
		methods = append(methods, "a passkey")
	}
	switch len(methods) {
	case 0:
		return "sign in on the sign-in page"
	case 1:
		return "sign in with " + methods[0]
	}
	return "sign in with " + strings.Join(methods[:len(methods)-1], ", ") + " or " + methods[len(methods)-1]
}
