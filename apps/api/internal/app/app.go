// Package app holds the domain services shared by the REST API and the MCP
// server. Every exported method takes a Principal and performs its own
// authorization, so no transport can bypass it.
package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Scopes. Sessions (the dashboard) implicitly hold every scope.
const (
	ScopeCollectionsRead  = "collections:read"
	ScopeCollectionsWrite = "collections:write"
	ScopeItemsRead        = "items:read"
	ScopeItemsWrite       = "items:write"
	ScopeItemsReview      = "items:review"
	ScopeActionsExecute   = "actions:execute"
	ScopeWebhooksManage   = "webhooks:manage"
	ScopeRulesManage      = "rules:manage" // rules can auto-approve: grant deliberately
	ScopeWatchersManage   = "watchers:manage"
	ScopeChannelsManage   = "channels:manage" // notification channels hold secrets: not in the default key
	ScopeAdmin            = "admin"
)

var AllScopes = []string{
	ScopeCollectionsRead, ScopeCollectionsWrite, ScopeItemsRead, ScopeItemsWrite,
	ScopeItemsReview, ScopeActionsExecute, ScopeWebhooksManage, ScopeRulesManage, ScopeWatchersManage, ScopeChannelsManage, ScopeAdmin,
}

// DefaultKeyScopes are granted to the key created during setup: everything an
// automation needs, but not admin (key management stays in the dashboard).
var DefaultKeyScopes = []string{
	ScopeCollectionsRead, ScopeCollectionsWrite, ScopeItemsRead, ScopeItemsWrite,
	ScopeItemsReview, ScopeActionsExecute, ScopeWebhooksManage, ScopeWatchersManage,
}

// Modes. Local: single-user, keyless, reachable from this machine only.
// Cloud: the same server deployed remotely; API keys, sessions and setup apply.
const (
	ModeLocal = "local"
	ModeCloud = "cloud"
)

const (
	PrincipalLocal   = "local"
	PrincipalSession = "session"
	PrincipalAPIKey  = "api_key"
	PrincipalSystem  = "system"
)

type Principal struct {
	WorkspaceID string
	Kind        string
	ID          string
	Name        string
	Scopes      []string
}

func (p Principal) Has(scope string) bool {
	if p.Kind == PrincipalSession || p.Kind == PrincipalLocal {
		return true
	}
	for _, s := range p.Scopes {
		if s == scope || s == ScopeAdmin {
			return true
		}
	}
	return false
}

func (p Principal) require(scope string) error {
	if p.WorkspaceID == "" {
		return ErrUnauthorized
	}
	if !p.Has(scope) {
		return &Error{Code: "forbidden", Status: 403, Message: "missing required scope " + scope}
	}
	return nil
}

// Error is a domain error that maps directly onto an HTTP status.
type Error struct {
	Code    string            `json:"code"`
	Status  int               `json:"-"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
	Details map[string]any    `json:"details,omitempty"`
}

func (e *Error) Error() string { return e.Message }

var (
	ErrUnauthorized = &Error{Code: "unauthorized", Status: 401, Message: "authentication required"}
	ErrNotFound     = &Error{Code: "not_found", Status: 404, Message: "not found"}
)

func invalid(field, msg string) *Error {
	return &Error{Code: "validation_failed", Status: 422, Message: field + ": " + msg, Fields: map[string]string{field: msg}}
}

func conflict(msg string) *Error {
	return &Error{Code: "conflict", Status: 409, Message: msg}
}

func isConflict(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == "conflict"
}

func notFound(what string) *Error {
	return &Error{Code: "not_found", Status: 404, Message: what + " not found"}
}

type Config struct {
	Mode           string   // ModeLocal (default) or ModeCloud
	LocalHosts     []string // extra Host names accepted in local mode
	UploadDir      string
	MaxUploadBytes int64
	SessionTTL     time.Duration
	SetupToken     string // optional; required by Setup when set
	PublicURL      string // base URL of this API as seen by webhook receivers
	PublicWebURL   string // dashboard URL other devices use (QR "connect a device" links)
	WebPort        string // dashboard port, for links printed by the CLI
	// DashboardExposed: the dashboard listens beyond loopback (default in compose.yml), so
	// requests arriving through it can't be trusted as "this computer".
	DashboardExposed bool
	// WatcherAllowPrivate lets watchers fetch LAN/private addresses (default: public internet only).
	WatcherAllowPrivate bool
	Webhook             WebhookConfig
}

type Service struct {
	DB     *sql.DB
	Q      *db.Queries
	Log    *slog.Logger
	Cfg    Config
	Now    func() time.Time
	notify chan struct{}

	// Getenv overrides environment lookups for Env (tests); nil = os.Getenv.
	Getenv func(string) string

	watcherWake chan struct{}
	changes     changeBus
	Fetcher     *http.Client // outbound client for watchers (SSRF-guarded); replaceable in tests
	robots      robotsCache
}

func New(database *sql.DB, log *slog.Logger, cfg Config) *Service {
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 30 * 24 * time.Hour
	}
	if cfg.MaxUploadBytes == 0 {
		cfg.MaxUploadBytes = 100 << 20
	}
	if cfg.Mode != ModeCloud {
		cfg.Mode = ModeLocal
	}
	cfg.Webhook.defaults()
	svc := &Service{
		DB: database, Q: db.New(database), Log: log, Cfg: cfg,
		Now:         time.Now,
		notify:      make(chan struct{}, 1),
		watcherWake: make(chan struct{}, 1),
		robots:      robotsCache{hosts: map[string]robotsRules{}},
	}
	svc.Fetcher = svc.fetchClient()
	return svc
}

const timeFormat = "2006-01-02T15:04:05.000000Z"

func (s *Service) now() string { return s.Now().UTC().Format(timeFormat) }

func formatTime(t time.Time) string { return t.UTC().Format(timeFormat) }

// ParseTimeFilter accepts RFC3339 or YYYY-MM-DD and normalizes it to the
// stored format so string comparison in SQL is correct.
func ParseTimeFilter(field, v string) (any, error) {
	if v == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return formatTime(t), nil
		}
	}
	return nil, invalid(field, "must be RFC3339 or YYYY-MM-DD")
}

var b32 = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return b32.EncodeToString(b)
}

func newID(prefix string) string { return prefix + "_" + randomString(12) }

// tx runs fn in a write transaction (BEGIN IMMEDIATE via the DSN).
func (s *Service) tx(ctx context.Context, fn func(q *db.Queries) error) error {
	t, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer t.Rollback()
	if err := fn(s.Q.WithTx(t)); err != nil {
		return err
	}
	if err := t.Commit(); err != nil {
		return err
	}
	s.wake()              // the transaction may have queued webhook deliveries
	s.changes.broadcast() // and may have decided an item someone is waiting for
	return nil
}

func nullStr(v string) sql.NullString { return sql.NullString{String: v, Valid: v != ""} }

func optAny(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func trimmed(v string, field string, max int, required bool) (string, error) {
	v = strings.TrimSpace(v)
	if required && v == "" {
		return "", invalid(field, "is required")
	}
	if len(v) > max {
		return "", invalid(field, fmt.Sprintf("must be at most %d characters", max))
	}
	return v, nil
}

// Wake nudges the webhook worker after a commit that queued deliveries.
func (s *Service) wake() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func asError(err error, target **Error) bool { return errors.As(err, target) }
