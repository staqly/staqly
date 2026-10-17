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
	// ProcessingTimeoutSeconds: how long an item may stay processing before it fails (0 = never).
	ProcessingTimeoutSeconds int64 `json:"processing_timeout_seconds"`
}

func (s *Service) toWorkspace(w db.Workspace) Workspace {
	return Workspace{ID: w.ID, Name: w.Name, Plan: w.Plan, RecoveryRotatedAt: w.RecoveryRotatedAt, CreatedAt: w.CreatedAt,
		ProcessingTimeoutSeconds: int64(s.processingTimeout(w.ProcessingTimeoutSeconds) / time.Second)}
}

// WorkspaceUpdate changes the name and/or the processing timeout; nil fields stay as they are.
type WorkspaceUpdate struct {
	Name                     *string `json:"name"`
	ProcessingTimeoutSeconds *int64  `json:"processing_timeout_seconds"`
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
	ws := db.Workspace{ID: newID("ws"), Name: name, Plan: "pro", RecoveryRotatedAt: now, CreatedAt: now, UpdatedAt: now} // 7-day trial
	recovery := newRecoveryKey()
	var res SetupResult
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := q.CreateWorkspace(ctx, db.CreateWorkspaceParams{
			ID: ws.ID, Name: ws.Name, RecoveryVerifier: hashSecret(recovery),
			RecoveryRotatedAt: now, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		// New cloud workspaces try Pro first; afterwards they pay or switch to Free.
		trialEnds := formatTime(s.Now().Add(TrialDays * 24 * time.Hour))
		if _, err := q.SetWorkspacePlan(ctx, db.SetWorkspacePlanParams{Plan: "pro",
			PlanExpiresAt: sql.NullString{String: trialEnds, Valid: true}, UpdatedAt: now, ID: ws.ID}); err != nil {
			return err
		}
		if err := s.ensureDefaultCollection(ctx, q, ws.ID); err != nil {
			return err
		}
		sys := Principal{WorkspaceID: ws.ID, Kind: PrincipalSystem, ID: "setup", Name: "Setup"}
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

// LocalPrincipal is the keyless, full-access principal used in local mode.
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

func (s *Service) RenameWorkspace(ctx context.Context, p Principal, name string) (Workspace, error) {
	return s.UpdateWorkspace(ctx, p, WorkspaceUpdate{Name: &name})
}

func (s *Service) UpdateWorkspace(ctx context.Context, p Principal, in WorkspaceUpdate) (Workspace, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return Workspace{}, err
	}
	if in.Name == nil && in.ProcessingTimeoutSeconds == nil {
		return Workspace{}, invalid("name", "send name and/or processing_timeout_seconds")
	}
	var name string
	if in.Name != nil {
		var err error
		if name, err = trimmed(*in.Name, "name", 100, true); err != nil {
			return Workspace{}, err
		}
	}
	if t := in.ProcessingTimeoutSeconds; t != nil {
		if d := time.Duration(*t) * time.Second; d < MinProcessingTimeout || d > MaxProcessingTimeout {
			return Workspace{}, invalid("processing_timeout_seconds", "must be between 60 and 86400 (1 minute to 24 hours)")
		}
	}
	if err := s.tx(ctx, func(q *db.Queries) error {
		data := map[string]any{}
		if in.Name != nil {
			if err := q.UpdateWorkspaceName(ctx, db.UpdateWorkspaceNameParams{Name: name, UpdatedAt: s.now(), ID: p.WorkspaceID}); err != nil {
				return err
			}
			data["name"] = name
		}
		if t := in.ProcessingTimeoutSeconds; t != nil {
			if err := q.UpdateWorkspaceProcessingTimeout(ctx, db.UpdateWorkspaceProcessingTimeoutParams{
				ProcessingTimeoutSeconds: sql.NullInt64{Int64: *t, Valid: true}, UpdatedAt: s.now(), ID: p.WorkspaceID}); err != nil {
				return err
			}
			data["processing_timeout_seconds"] = *t
		}
		_, err := s.emit(ctx, q, p, "workspace.updated", "", "", data)
		return err
	}); err != nil {
		return Workspace{}, err
	}
	return s.GetWorkspace(ctx, p)
}

// AuthenticateAPIKey resolves a bearer key to a principal.
func (s *Service) AuthenticateAPIKey(ctx context.Context, raw string) (Principal, error) {
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
	return Principal{WorkspaceID: k.WorkspaceID, Kind: PrincipalAPIKey, ID: k.ID, Name: k.Name, Scopes: scopes}, nil
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
	name := "Dashboard"
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
// cloud dashboard's sign-in). Agent keys without the admin scope never open
// the dashboard. Other sessions stay signed in.
func (s *Service) SignInWithAPIKey(ctx context.Context, rawKey, userAgent string) (NewSession, error) {
	if s.Cfg.Mode != ModeCloud {
		return NewSession{}, conflict("sign-in with an API key is only used in cloud mode; connect this browser with a connect link instead")
	}
	p, err := s.AuthenticateAPIKey(ctx, strings.TrimSpace(rawKey))
	if err != nil {
		return NewSession{}, &Error{Code: "unauthorized", Status: 401, Message: "invalid, revoked or expired API key"}
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
// The recovery key is single-use (it is rotated out), existing API keys keep
// working, and every other session is signed out (whoever used the key may be
// locking out an intruder). The recovery key is never accepted anywhere else.
func (s *Service) Recover(ctx context.Context, recoveryKey, userAgent string) (RecoverResult, error) {
	// Brute force: 200-bit keys, and the route is rate-limited per IP (sensitiveGroup).
	invalidKey := &Error{Code: "unauthorized", Status: 401, Message: "invalid recovery key (each recovery key works once)"}
	oldVerifier := hashSecret(normalizeRecoveryKey(recoveryKey))
	ws, err := s.Q.GetWorkspaceByRecoveryVerifier(ctx, oldVerifier)
	if err != nil {
		return RecoverResult{}, invalidKey
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
		// Never blocked by the plan's key limit: recovery must always restore access.
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

// revokeLostAdminKeys: on a plan with a key limit (Free allows one), the new
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
	if p.Kind != PrincipalSession {
		return "", &Error{Code: "forbidden", Status: 403, Message: "recovery key can only be rotated from a dashboard session"}
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
	out := []Session{}
	for _, r := range rows {
		out = append(out, Session{ID: r.ID, UserAgent: r.UserAgent, CreatedAt: r.CreatedAt, LastSeenAt: r.LastSeenAt,
			ExpiresAt: r.ExpiresAt, Current: p.Kind == PrincipalSession && p.ID == r.ID})
	}
	return out, err
}

func (s *Service) RevokeSession(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopeAdmin); err != nil {
		return err
	}
	return s.revokeSession(ctx, p, id, "revoked")
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

func (s *Service) CreateAPIKey(ctx context.Context, p Principal, in CreateAPIKeyInput) (APIKey, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return APIKey{}, err
	}
	name, err := trimmed(in.Name, "name", 100, true)
	if err != nil {
		return APIKey{}, err
	}
	scopes, err := validateScopes(in.Scopes)
	if err != nil {
		return APIKey{}, err
	}
	var exp *string
	if in.ExpiresAt != "" {
		v, err := ParseTimeFilter("expires_at", in.ExpiresAt)
		if err != nil {
			return APIKey{}, err
		}
		e := v.(string)
		exp = &e
	}
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
	n, err := q.CountActiveAPIKeys(ctx, p.WorkspaceID)
	if err != nil {
		return APIKey{}, err
	}
	if err := checkCount(plan, LimitAPIKeys, plan.Limits.APIKeys, n); err != nil {
		return APIKey{}, err
	}
	return s.insertKey(ctx, q, p, name, scopes, exp)
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

// RotateAPIKey revokes a key and issues a replacement with the same name and scopes.
func (s *Service) RotateAPIKey(ctx context.Context, p Principal, id string) (APIKey, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return APIKey{}, err
	}
	var key APIKey
	err := s.tx(ctx, func(q *db.Queries) error {
		old, err := q.GetAPIKey(ctx, db.GetAPIKeyParams{WorkspaceID: p.WorkspaceID, ID: id})
		if err != nil || old.RevokedAt.Valid {
			return notFound("active api key")
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
