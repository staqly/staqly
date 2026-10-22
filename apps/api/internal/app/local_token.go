package app

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Local mode's access token. A local install has one workspace and no people, keys or plans, but every
// request still proves it may come in: API clients (REST, MCP over HTTP, SDKs, the CLI, n8n) send
// `Authorization: Bearer <token>`, and a browser trades the token once for the usual session cookie
// (POST /v1/sessions/api-key, or the dashboard's #token= link). The token lives in a file in the data folder
// (owner-only, 0600), created on first start and printed at every start; the file is the only copy, so
// `server token` prints it again and `server token --rotate` replaces it and signs every browser out. A
// server that runs while the token is rotated by another process (`docker exec ... server token --rotate`)
// picks the new file up on the next request. STAQLY_LOCAL_TOKEN fixes the token instead (tests, CI).
// cmd/mcp-stdio opens the database itself and needs no token.

// LocalTokenPrefix: an API key's prefix (clients that check for stk_ accept it), then what it is.
const LocalTokenPrefix = apiKeyPrefix + "local_"

// LocalTokenFile is the token's file name in DATA_DIR.
const LocalTokenFile = "access_token"

func newLocalToken() string { return LocalTokenPrefix + randomString(32) }

// ErrNoLocalToken answers a local-mode request without credentials.
var ErrNoLocalToken = &Error{Code: "unauthorized", Status: 401,
	Message: "local mode needs the access token: send Authorization: Bearer <token>; print it with `staqly serve token` or `docker exec staqly server token`"}

// localTokenCache: the file's hash, read again when the file changes (another file, size or modification time).
type localTokenCache struct {
	mu   sync.Mutex
	file fs.FileInfo
	hash []byte
}

var localTokens sync.Map // *Service → *localTokenCache

func (s *Service) localTokenPath() string {
	if s.Cfg.LocalTokenPath != "" {
		return s.Cfg.LocalTokenPath
	}
	return filepath.Join(s.notifyDataDir(), LocalTokenFile)
}

// EnsureLocalToken returns the access token, creating its file on first start.
func (s *Service) EnsureLocalToken() (string, error) {
	if s.Cfg.LocalToken != "" {
		return s.Cfg.LocalToken, nil
	}
	path := s.localTokenPath()
	if tok, err := readLocalToken(path); err == nil {
		return tok, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	tok := newLocalToken()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return readLocalToken(path) // created concurrently by another process
	}
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(tok + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return tok, err
}

func readLocalToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(b))
	if !strings.HasPrefix(tok, LocalTokenPrefix) || len(tok) < 32 {
		return "", fmt.Errorf("%s is not an access token: remove it and start again (a new token is made) or run `server token --rotate`", path)
	}
	return tok, nil
}

// localTokenHash: SHA-256 of the current token; nil when there is none yet.
func (s *Service) localTokenHash() ([]byte, error) {
	if s.Cfg.LocalToken != "" {
		h := sha256.Sum256([]byte(s.Cfg.LocalToken))
		return h[:], nil
	}
	v, _ := localTokens.LoadOrStore(s, &localTokenCache{})
	c := v.(*localTokenCache)
	c.mu.Lock()
	defer c.mu.Unlock()
	// ponytail: one stat per authenticated request; cheap next to the database lookup a key costs.
	st, err := os.Stat(s.localTokenPath())
	if errors.Is(err, fs.ErrNotExist) {
		c.hash = nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// A rotation renames a new file over the old one: another file, even within the clock's resolution.
	if c.hash != nil && os.SameFile(st, c.file) && st.Size() == c.file.Size() && st.ModTime().Equal(c.file.ModTime()) {
		return c.hash, nil
	}
	tok, err := readLocalToken(s.localTokenPath())
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256([]byte(tok))
	c.hash, c.file = h[:], st
	return c.hash, nil
}

// localTokenSeenPath records (as SHA-256, never the token) the access token browsers last signed in with.
func (s *Service) localTokenSeenPath() string { return s.localTokenPath() + ".seen" }

// SignOutIfTokenChanged runs at start: when the access token is not the one browsers signed in with (a new
// STAQLY_LOCAL_TOKEN, or an access_token file replaced while the server was down), every browser is signed out,
// as `server token --rotate` does. The first start that records the token signs nobody out.
func (s *Service) SignOutIfTokenChanged(ctx context.Context) (int64, error) {
	if s.Cfg.Mode != ModeLocal {
		return 0, nil
	}
	h, err := s.localTokenHash()
	if err != nil || h == nil {
		return 0, err
	}
	cur := hex.EncodeToString(h)
	seen, err := os.ReadFile(s.localTokenSeenPath())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}
	var revoked int64
	if len(seen) > 0 && strings.TrimSpace(string(seen)) != cur {
		p, err := s.LocalPrincipal(ctx)
		if err != nil {
			return 0, err
		}
		sys := Principal{WorkspaceID: p.WorkspaceID, Kind: PrincipalSystem, ID: "server", Name: "Server"}
		err = s.tx(ctx, func(q *db.Queries) error {
			var err error
			if revoked, err = q.RevokeOtherSessions(ctx, db.RevokeOtherSessionsParams{RevokedAt: nullStr(s.now()), WorkspaceID: p.WorkspaceID, KeepID: ""}); err != nil {
				return err
			}
			_, err = s.emit(ctx, q, sys, "access_token.rotated", "", "", map[string]any{"sessions_revoked": revoked})
			return err
		})
		if err != nil {
			return 0, err
		}
	}
	return revoked, s.rememberLocalToken(cur)
}

// rememberLocalToken writes the token's hash as the one browsers sign in with.
func (s *Service) rememberLocalToken(hash string) error {
	if err := os.MkdirAll(filepath.Dir(s.localTokenSeenPath()), 0o700); err != nil {
		return err
	}
	return os.WriteFile(s.localTokenSeenPath(), []byte(hash+"\n"), 0o600)
}

// AuthenticateLocalToken resolves the access token of a local install to the local principal (everything
// in the one workspace). Not a local token: ok is false and the caller tries API keys.
func (s *Service) AuthenticateLocalToken(ctx context.Context, raw string) (p Principal, ok bool, err error) {
	if s.Cfg.Mode != ModeLocal || !strings.HasPrefix(raw, LocalTokenPrefix) {
		return Principal{}, false, nil
	}
	want, err := s.localTokenHash()
	if err != nil {
		return Principal{}, true, err
	}
	got := sha256.Sum256([]byte(raw))
	if want == nil || subtle.ConstantTimeCompare(got[:], want) != 1 {
		return Principal{}, true, ErrUnauthorized
	}
	p, err = s.LocalPrincipal(ctx)
	return p, true, err
}

// SignInWithLocalToken trades the access token for a browser session (the local dashboard's sign-in).
func (s *Service) SignInWithLocalToken(ctx context.Context, raw, userAgent string) (NewSession, error) {
	p, _, err := s.AuthenticateLocalToken(ctx, strings.TrimSpace(raw))
	if err != nil || p.WorkspaceID == "" {
		return NewSession{}, &Error{Code: "unauthorized", Status: 401,
			Message: "this is not the access token of this computer: copy it again from where staqly started, or print it with `staqly serve token`"}
	}
	var ns NewSession
	err = s.tx(ctx, func(q *db.Queries) error {
		var err error
		var id string
		if ns, id, err = s.createSessionID(ctx, q, p.WorkspaceID, userAgent); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "session.signed_in", "", "", map[string]any{"user_agent": userAgent, "session_id": id, "via": "access_token"})
		return err
	})
	return ns, err
}

// LocalAccessToken: the token for the dashboard's agents page (who holds a session or the token may see it).
func (s *Service) LocalAccessToken(p Principal) (string, error) {
	if s.Cfg.Mode != ModeLocal {
		return "", notFound("access token")
	}
	if p.Kind != PrincipalLocal && p.Kind != PrincipalSession {
		return "", forbidden("the access token is shown to the dashboard only")
	}
	return s.EnsureLocalToken()
}

// RotateLocalToken writes a new access token and signs out every browser session except keepSessionID
// ("" = all, the server CLI). The old token stops working at once, also in a running server.
func (s *Service) RotateLocalToken(ctx context.Context, p Principal, keepSessionID string) (string, int64, error) {
	if s.Cfg.Mode != ModeLocal {
		return "", 0, notFound("access token")
	}
	if p.Kind != PrincipalLocal && p.Kind != PrincipalSession && p.Kind != PrincipalSystem {
		return "", 0, forbidden("the access token is rotated from the dashboard or the server CLI")
	}
	if s.Cfg.LocalToken != "" {
		return "", 0, conflict("the access token is set with STAQLY_LOCAL_TOKEN: change it there and restart")
	}
	tok := newLocalToken()
	path := s.localTokenPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", 0, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".access_token-*") // 0600, then renamed over the old file
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(tok + "\n"); err != nil {
		tmp.Close()
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", 0, err
	}
	var revoked int64
	err = s.tx(ctx, func(q *db.Queries) error {
		var err error
		revoked, err = q.RevokeOtherSessions(ctx, db.RevokeOtherSessionsParams{RevokedAt: nullStr(s.now()), WorkspaceID: p.WorkspaceID, KeepID: keepSessionID})
		if err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "access_token.rotated", "", "", map[string]any{"sessions_revoked": revoked})
		return err
	})
	if err == nil {
		// The browser that rotated it keeps its session: the next start must not sign it out again.
		h := sha256.Sum256([]byte(tok))
		err = s.rememberLocalToken(hex.EncodeToString(h[:]))
	}
	return tok, revoked, err
}
