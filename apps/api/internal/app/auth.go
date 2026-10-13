package app

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
}

func toWorkspace(w db.Workspace) Workspace {
	return Workspace{ID: w.ID, Name: w.Name, Plan: w.Plan, RecoveryRotatedAt: w.RecoveryRotatedAt, CreatedAt: w.CreatedAt}
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

// Setup creates a new cloud workspace on the Free plan with a default API key,
// a recovery key and a browser session. Each call creates a separate,
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
	ws := db.Workspace{ID: newID("ws"), Name: name, Plan: "free", RecoveryRotatedAt: now, CreatedAt: now, UpdatedAt: now}
	recovery := newRecoveryKey()
	var res SetupResult
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := q.CreateWorkspace(ctx, db.CreateWorkspaceParams{
			ID: ws.ID, Name: ws.Name, RecoveryVerifier: hashSecret(recovery),
			RecoveryRotatedAt: now, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		sys := Principal{WorkspaceID: ws.ID, Kind: PrincipalSystem, ID: "setup", Name: "Setup"}
		key, err := s.createKey(ctx, q, sys, "Default key", DefaultKeyScopes, nil)
		if err != nil {
			return err
		}
		sess, err := s.createSession(ctx, q, ws.ID, userAgent)
		if err != nil {
			return err
		}
		if _, err := s.emit(ctx, q, sys, "workspace.created", "", "", map[string]any{"workspace": toWorkspace(ws)}); err != nil {
			return err
		}
		res = SetupResult{Workspace: toWorkspace(ws), APIKey: key, RecoveryKey: recovery, Session: sess}
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
		sys := Principal{WorkspaceID: ws.ID, Kind: PrincipalSystem, ID: "local", Name: "Local setup"}
		_, err := s.emit(ctx, q, sys, "workspace.created", "", "", map[string]any{"workspace": toWorkspace(ws)})
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
	return Principal{WorkspaceID: inst.WorkspaceID, Kind: PrincipalLocal, ID: "local", Name: "Local"}, nil
}

// ResetRecoveryKey issues a new recovery key for a workspace (default: the
// local one). It is only reachable from the server CLI (`server
// recovery-key`), i.e. by whoever controls the host and its data volume.
func (s *Service) ResetRecoveryKey(ctx context.Context, workspaceID string) (string, error) {
	if workspaceID == "" {
		inst, err := s.Q.GetInstallation(ctx)
		if err != nil {
			return "", fmt.Errorf("no local workspace; pass a workspace id (see `server workspaces`)")
		}
		workspaceID = inst.WorkspaceID
	}
	if _, err := s.Q.GetWorkspace(ctx, workspaceID); err != nil {
		return "", fmt.Errorf("workspace %s not found", workspaceID)
	}
	p := Principal{WorkspaceID: workspaceID, Kind: PrincipalSystem, ID: "cli", Name: "Server CLI"}
	key := newRecoveryKey()
	now := s.now()
	err := s.tx(ctx, func(q *db.Queries) error {
		if err := q.UpdateRecoveryVerifier(ctx, db.UpdateRecoveryVerifierParams{
			RecoveryVerifier: hashSecret(key), RecoveryRotatedAt: now, UpdatedAt: now, ID: p.WorkspaceID}); err != nil {
			return err
		}
		_, err := s.emit(ctx, q, p, "recovery_key.rotated", "", "", map[string]any{"via": "cli"})
		return err
	})
	return key, err
}

func (s *Service) GetWorkspace(ctx context.Context, p Principal) (Workspace, error) {
	if p.WorkspaceID == "" {
		return Workspace{}, ErrUnauthorized
	}
	w, err := s.Q.GetWorkspace(ctx, p.WorkspaceID)
	if err != nil {
		return Workspace{}, err
	}
	out := toWorkspace(w)
	if plan, err := s.planFor(ctx, s.Q, p.WorkspaceID); err == nil {
		out.Plan = plan.ID
	}
	return out, nil
}

func (s *Service) RenameWorkspace(ctx context.Context, p Principal, name string) (Workspace, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return Workspace{}, err
	}
	name, err := trimmed(name, "name", 100, true)
	if err != nil {
		return Workspace{}, err
	}
	if err := s.Q.UpdateWorkspaceName(ctx, db.UpdateWorkspaceNameParams{Name: name, UpdatedAt: s.now(), ID: p.WorkspaceID}); err != nil {
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
	// ponytail: a write per request; throttle if key traffic gets heavy.
	_ = s.Q.TouchAPIKey(ctx, db.TouchAPIKeyParams{LastUsedAt: nullStr(now), ID: k.ID})
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
		_ = s.Q.TouchSession(ctx, db.TouchSessionParams{LastSeenAt: formatTime(now), ID: sess.ID})
	}
	return Principal{WorkspaceID: sess.WorkspaceID, Kind: PrincipalSession, ID: sess.ID, Name: "Dashboard"}, sess.CsrfToken, nil
}

func (s *Service) createSession(ctx context.Context, q *db.Queries, workspaceID, userAgent string) (NewSession, error) {
	now := s.Now().UTC()
	ns := NewSession{Token: randomString(32), CSRFToken: randomString(20), ExpiresAt: now.Add(s.Cfg.SessionTTL)}
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	err := q.CreateSession(ctx, db.CreateSessionParams{
		ID: newID("ses"), WorkspaceID: workspaceID, TokenHash: hashSecret(ns.Token), CsrfToken: ns.CSRFToken,
		UserAgent: userAgent, CreatedAt: formatTime(now), LastSeenAt: formatTime(now), ExpiresAt: formatTime(ns.ExpiresAt),
	})
	return ns, err
}

// Recover exchanges the recovery key for a new browser session. The recovery
// key is never accepted anywhere else.
func (s *Service) Recover(ctx context.Context, recoveryKey, userAgent string) (NewSession, error) {
	// ponytail: no rate limit; 200-bit keys make online guessing pointless.
	ws, err := s.Q.GetWorkspaceByRecoveryVerifier(ctx, hashSecret(normalizeRecoveryKey(recoveryKey)))
	if err != nil {
		return NewSession{}, &Error{Code: "unauthorized", Status: 401, Message: "invalid recovery key"}
	}
	var ns NewSession
	err = s.tx(ctx, func(q *db.Queries) error {
		var err error
		if ns, err = s.createSession(ctx, q, ws.ID, userAgent); err != nil {
			return err
		}
		sys := Principal{WorkspaceID: ws.ID, Kind: PrincipalSystem, ID: "recovery", Name: "Recovery key"}
		_, err = s.emit(ctx, q, sys, "session.recovered", "", "", map[string]any{"user_agent": userAgent})
		return err
	})
	return ns, err
}

// RotateRecoveryKey is restricted to dashboard sessions: an API key must never
// be able to mint the credential that recovers admin access.
func (s *Service) RotateRecoveryKey(ctx context.Context, p Principal) (string, error) {
	if p.Kind != PrincipalSession {
		return "", &Error{Code: "forbidden", Status: 403, Message: "recovery key can only be rotated from a dashboard session"}
	}
	key := newRecoveryKey()
	now := s.now()
	err := s.tx(ctx, func(q *db.Queries) error {
		if err := q.UpdateRecoveryVerifier(ctx, db.UpdateRecoveryVerifierParams{
			RecoveryVerifier: hashSecret(key), RecoveryRotatedAt: now, UpdatedAt: now, ID: p.WorkspaceID,
		}); err != nil {
			return err
		}
		_, err := s.emit(ctx, q, p, "recovery_key.rotated", "", "", map[string]any{})
		return err
	})
	return key, err
}

func (s *Service) Logout(ctx context.Context, p Principal) error {
	if p.Kind != PrincipalSession {
		return nil
	}
	_, err := s.Q.RevokeSession(ctx, db.RevokeSessionParams{RevokedAt: nullStr(s.now()), WorkspaceID: p.WorkspaceID, ID: p.ID})
	return err
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
	n, err := s.Q.RevokeSession(ctx, db.RevokeSessionParams{RevokedAt: nullStr(s.now()), WorkspaceID: p.WorkspaceID, ID: id})
	if err == nil && n == 0 {
		return notFound("session")
	}
	return err
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
		key, err = s.createKey(ctx, q, p, old.Name, scopes, exp)
		return err
	})
	return key, err
}
