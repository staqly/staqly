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
	mathrand "math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/posthog/posthog-go"
	"github.com/riverqueue/river"

	"github.com/staqly/staqly/apps/api/internal/blob"
	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/store"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Scopes. Sessions (the dashboard) implicitly hold every scope.
const (
	ScopeCollectionsRead   = "collections:read"
	ScopeCollectionsWrite  = "collections:write"
	ScopeCollectionsDelete = "collections:delete" // deleting is never implied by :write
	ScopePostsRead         = "posts:read"
	ScopePostsWrite        = "posts:write"   // create, edit, duplicate
	ScopePostsDelete       = "posts:delete"  // delete posts and media
	ScopePostsPublish      = "posts:publish" // schedule, publish now, cancel, retry a target, report a custom target's result
	ScopeMediaRead         = "media:read"
	ScopeMediaWrite        = "media:write"
	ScopeEventsRead        = "events:read"
	ScopeChannelsRead      = "channels:read"   // list connected channels and the provider catalog
	ScopeChannelsManage    = "channels:manage" // connect, reconnect, remove channels: tokens live here
	ScopeSchedulesManage   = "schedules:manage"
	ScopeWebhooksManage    = "webhooks:manage"
	ScopeAlertsManage      = "alerts:manage" // alert channels hold secrets: not in the default key
	ScopeAdmin             = "admin"
)

var AllScopes = []string{
	ScopeCollectionsRead, ScopeCollectionsWrite, ScopeCollectionsDelete,
	ScopePostsRead, ScopePostsWrite, ScopePostsDelete, ScopePostsPublish,
	ScopeChannelsRead, ScopeChannelsManage, ScopeSchedulesManage, ScopeMediaRead, ScopeMediaWrite,
	ScopeWebhooksManage, ScopeAlertsManage, ScopeEventsRead, ScopeAdmin,
}

// AdminKeyScopes are granted to the key created at sign-up and to the new key
// recovery issues: the admin key is what signs a browser in to the dashboard
// (POST /v1/sessions/api-key). Agents should get a scoped key instead.
var AdminKeyScopes = []string{ScopeAdmin}

// DefaultKeyScopes: a typical agent key, everything an automation needs but not
// admin (no dashboard sign-in, no key management), not the scopes that hold
// tokens (channels:manage, alerts:manage) and not the delete scopes.
var DefaultKeyScopes = []string{
	ScopeCollectionsRead, ScopeCollectionsWrite, ScopePostsRead, ScopePostsWrite, ScopePostsPublish,
	ScopeMediaRead, ScopeMediaWrite, ScopeChannelsRead, ScopeSchedulesManage, ScopeWebhooksManage, ScopeEventsRead,
}

// Modes. Local: single-user, one workspace, every request with the install's access token (local_token.go).
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
	// UserID: a person signed in with Google (cloud accounts, accounts.go); "" for local mode, API keys
	// without a creator and sessions from before accounts.
	UserID string
	// Role: that person's role in WorkspaceID (owner, admin, editor, viewer); their Scopes come from it.
	Role string
}

// Has: local mode and sessions from before accounts (no user) may do everything in their workspace, as
// always; a signed-in person and an API key have exactly their Scopes (admin covers every scope).
func (p Principal) Has(scope string) bool {
	if p.Kind == PrincipalLocal || (p.Kind == PrincipalSession && p.UserID == "") {
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

// onlyChange reports whether, among the pointer, slice and map fields of an input struct, only the named
// ones are set: "is this request about nothing but X?".
func onlyChange(in any, names ...string) bool {
	v := reflect.ValueOf(in)
	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := v.Field(i)
		switch f.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
			if !f.IsNil() && !slices.Contains(names, t.Field(i).Name) {
				return false
			}
		case reflect.String:
			if f.String() != "" && !slices.Contains(names, t.Field(i).Name) {
				return false
			}
		}
	}
	return true
}

// alreadyErr: the change asked for is already the case (409 with code already_<state>).
func alreadyErr(what, state string) *Error {
	return &Error{Code: "already_" + state, Status: 409, Message: what + " is already " + state}
}

func notFound(what string) *Error {
	return &Error{Code: "not_found", Status: 404, Message: what + " not found"}
}

func isNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == "not_found"
}

type Config struct {
	Mode string // ModeLocal (default) or ModeCloud
	// PostHog: the operator's own PostHog project for product analytics and error reporting (POSTHOG_KEY);
	// empty Key = off, so an install sends nothing unless its operator sets it (analytics.go).
	PostHog PostHogConfig
	// Analytics receives the product events (server.Main makes it from PostHog); nil = off.
	Analytics posthog.EnqueueClient
	// AI: the design editor's AI tools, called with each workspace's own provider key (see ai.go).
	AI AIConfig
	// Stock: the design editor's stock photos (PIXABAY_API_KEY; see stock.go).
	Stock StockConfig
	// TrialPlan: the plan a new cloud workspace tries for TrialDays (TRIAL_PLAN). Empty = Starter, the only plan
	// sold with a trial; test and demo servers may give a bigger one.
	TrialPlan string
	// LocalToken fixes local mode's access token (STAQLY_LOCAL_TOKEN: tests, CI); empty = the token file
	// LocalTokenPath (default DATA_DIR/access_token), created on first start (local_token.go).
	LocalToken     string
	LocalTokenPath string
	UploadDir      string
	// Blob is where media files live (STORAGE_DRIVER): nil means files on disk in UploadDir.
	Blob               blob.Store
	MaxUploadBytes     int64
	SessionTTL         time.Duration
	SetupToken         string // optional; required by Setup when set
	PublicURL          string // base URL of this API as seen by webhook receivers
	PublicDashboardURL string // dashboard URL other devices use (QR "connect a device" links)
	// PublicMediaURL (PUBLIC_MEDIA_URL): where this server answers /v1/media-links, on an address platforms reach.
	// Set, platforms that download a post's media get signed links there instead of the store's own (S3).
	PublicMediaURL string
	DashboardPort  string // dashboard port, for links printed by the CLI
	// DashboardExposed: the dashboard listens beyond loopback (default in compose.yml), so
	// requests arriving through it can't be trusted as "this computer".
	DashboardExposed bool
	// SecretKey seals channel credentials and developer app secrets at rest (32 bytes, see LoadSecretKey).
	SecretKey []byte
	// ProcessingTimeout: how long a custom target waits for your automation's result before it fails
	// (PROCESSING_TIMEOUT, default 1h); ProcessingTimeoutNever turns the wait off (0 = never).
	ProcessingTimeout      time.Duration
	ProcessingTimeoutNever bool
	Webhook                WebhookConfig
	Billing                BillingConfig
	// Google: sign-in with Google (cloud accounts, accounts.go). nil: cloud mode keeps the sign-in from before
	// accounts (setup token, admin API key, recovery key), which config.Load allows only with AUTH_DEV_SIGNIN.
	Google *GoogleConfig
	// EmailSignIn: people sign in with a code sent to their email address (cloud mode with MAIL_SMTP_HOST and
	// MAIL_FROM, or AUTH_DEV_SIGNIN). DevSignIn (AUTH_DEV_SIGNIN, development and tests only) also keeps the
	// sign-in from before accounts and shows the last email code to tests (email_codes.go).
	EmailSignIn bool
	DevSignIn   bool
}

type Service struct {
	agents agentTallies // per-agent request tallies (agents.go)
	DB     *sql.DB
	// Q reads through the pool. Writes outside s.tx go through W, which
	// takes the same in-process write gate as s.tx (see writeGate).
	Q      *db.Queries
	W      *db.Queries
	writes writeGate
	Log    *slog.Logger
	Cfg    Config
	Now    func() time.Time
	notify chan struct{}

	// Getenv overrides environment lookups for Env (tests); nil = os.Getenv.
	Getenv func(string) string

	// The job queue (queue.go): River's client, the transaction behind each s.tx's queries (enqueue inserts
	// in it), and the jobs this process runs per workspace.
	river      *river.Client[*sql.Tx]
	completeTx func(context.Context, *sql.Tx, *river.Job[jobArgs]) error
	txs        sync.Map // *db.Queries → *sql.Tx
	tracked    sync.Map // *db.Queries → *[]posthog.Capture: product events sent once their transaction commits (analytics.go)
	active     activeUsers
	fair       fairShare
	oidc       oidcCache  // Google sign-in, discovered once (accounts.go)
	devCodes   devCodeBox // AUTH_DEV_SIGNIN: the last email code per address (email_codes.go)
	// SearchTimeout bounds one post listing/search query (default 5 s).
	SearchTimeout time.Duration
	thumbs        *thumbnailer
	// Instance names this process in job leases (jobs.go).
	Instance string
	// pg: the database is PostgreSQL (cloud mode); otherwise SQLite.
	pg bool
}

func New(database *sql.DB, log *slog.Logger, cfg Config) *Service {
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 30 * 24 * time.Hour
	}
	if cfg.MaxUploadBytes == 0 {
		cfg.MaxUploadBytes = 100 << 20
	}
	if len(cfg.SecretKey) == 0 { // ponytail: tests and embedders; the server always loads one (LoadSecretKey), so sealed channels survive restarts
		cfg.SecretKey = make([]byte, 32)
		_, _ = rand.Read(cfg.SecretKey)
	}
	if cfg.Mode != ModeCloud {
		cfg.Mode = ModeLocal
	}
	if cfg.Blob == nil {
		cfg.Blob = blob.NewDisk(cfg.UploadDir)
	}
	cfg.Webhook.defaults()
	if cfg.ProcessingTimeout == 0 && !cfg.ProcessingTimeoutNever {
		cfg.ProcessingTimeout = time.Hour
	}
	svc := &Service{
		DB: database, Q: db.New(database), Log: log, Cfg: cfg,
		writes:   make(writeGate, 1),
		Now:      time.Now,
		notify:   make(chan struct{}, 1),
		thumbs:   newThumbnailer(),
		Instance: randomString(6),
	}
	if store.IsPostgres(database) {
		// PostgreSQL takes concurrent writers itself: no process-wide gate, transactions are SERIALIZABLE
		// and retried on serialization failures (tx).
		svc.pg = true
		svc.W = db.New(database)
	} else {
		svc.W = db.New(gatedDB{database, svc.writes})
	}
	if svc.Log == nil { // tools and tests that pass none
		svc.Log = slog.New(slog.DiscardHandler)
	}
	svc.initQueue()
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

// writeGate lets one write (transaction or single statement) of this process
// at a time touch SQLite; the others queue here, in Go, without holding a pool
// connection.
//
// Without it, N concurrent writers each took a pooled connection and waited in
// SQLite's busy handler (BEGIN IMMEDIATE). The writer that held the lock often
// needed a second pooled connection for a read (e.g. toItem's media lookup
// through s.Q), found the pool exhausted by the waiters, and stalled until a
// waiter hit busy_timeout and failed with SQLITE_BUSY. SQLite allows one
// writer anyway, so serialising in Go costs no throughput. busy_timeout still
// covers other processes (mcp-stdio, backups) sharing the file.
type writeGate chan struct{}

func (g writeGate) lock(ctx context.Context) error {
	select {
	case g <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g writeGate) unlock() { <-g }

// gatedDB runs single write statements (sqlc :exec queries via Service.W)
// under the write gate. Never use W inside s.tx: the gate is not reentrant.
// Query/QueryRow are not gated (a *sql.Row executes on Scan, after the gate
// would be released): writes that return rows belong in s.tx.
type gatedDB struct {
	db *sql.DB
	g  writeGate
}

func (d gatedDB) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	if err := d.g.lock(ctx); err != nil {
		return nil, err
	}
	defer d.g.unlock()
	return d.db.ExecContext(ctx, q, args...)
}

func (d gatedDB) PrepareContext(ctx context.Context, q string) (*sql.Stmt, error) {
	return d.db.PrepareContext(ctx, q)
}

func (d gatedDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return d.db.QueryContext(ctx, q, args...)
}

func (d gatedDB) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return d.db.QueryRowContext(ctx, q, args...)
}

// IsBusy reports whether err is SQLite's SQLITE_BUSY (any extended code):
// the database stayed locked longer than busy_timeout, typically by another
// process. It is transient, so transports answer 503 + Retry-After.
func IsBusy(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code()&0xff == sqlite3.SQLITE_BUSY
}

// tx runs fn in a write transaction and wakes the workers after the commit.
//
// SQLite: BEGIN IMMEDIATE (via the DSN), one at a time per process (writeGate).
// PostgreSQL: SERIALIZABLE, any number at once and across processes; a serialization failure or deadlock
// runs fn again (for up to txRetryFor), so read-then-write checks (plan limits, versions, idempotency, unique
// defaults) hold as they do under SQLite's single writer. fn must only touch the database (no network,
// no side effects outside it): it may run more than once.
func (s *Service) tx(ctx context.Context, fn func(q *db.Queries) error) error {
	if s.pg {
		return s.txPostgres(ctx, fn)
	}
	if err := s.writes.lock(ctx); err != nil {
		return err
	}
	defer s.writes.unlock()
	if txRehearse {
		if err := s.rehearseTx(ctx, fn); err != nil {
			return err
		}
	}
	t, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer t.Rollback()
	q := s.Q.WithTx(t)
	s.txs.Store(q, t)
	defer s.txs.Delete(q)
	defer s.tracked.Delete(q)
	if err := fn(q); err != nil {
		return err
	}
	if err := t.Commit(); err != nil {
		return err
	}
	s.sendTracked(q)
	s.wake() // the transaction may have queued webhook deliveries
	return nil
}

// txRehearse (tests only) runs every SQLite transaction's fn once more before the real run, in a transaction
// that is rolled back: what PostgreSQL does on a serialization failure, so a fn that keeps state across runs
// (a slice it appends to) shows up on SQLite too.
var txRehearse bool

func (s *Service) rehearseTx(ctx context.Context, fn func(q *db.Queries) error) error {
	t, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer t.Rollback()
	q := s.Q.WithTx(t)
	s.txs.Store(q, t)
	defer s.txs.Delete(q)
	defer s.tracked.Delete(q)
	return fn(q)
}

// txRetryFor bounds the reruns of one transaction after serialization failures (40001/40P01). It is a
// time, not a count: dozens of writers on the same row (a workspace's usage counter) commit one at a
// time, so a writer may lose as many rounds as there are writers.
const txRetryFor = 15 * time.Second

func (s *Service) txPostgres(ctx context.Context, fn func(q *db.Queries) error) error {
	var err error
	start := time.Now()
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			// Full jitter under a low cap: a writer that lost many rounds waits about as long as a newcomer,
			// so it is not starved by the fresh transactions that keep winning the row.
			d := time.Duration(mathrand.Int64N(int64(min(time.Duration(attempt)*2*time.Millisecond, 50*time.Millisecond)))) +
				time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
			}
		}
		err = s.txOnce(ctx, fn)
		if !store.RetryableTx(err) || time.Since(start) > txRetryFor {
			break
		}
	}
	if err != nil {
		return err
	}
	s.wake()
	return nil
}

func (s *Service) txOnce(ctx context.Context, fn func(q *db.Queries) error) error {
	t, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer t.Rollback()
	q := s.Q.WithTx(t)
	s.txs.Store(q, t)
	defer s.txs.Delete(q)
	defer s.tracked.Delete(q) // a rerun after a serialization failure starts with no events
	if err := fn(q); err != nil {
		return err
	}
	if err := t.Commit(); err != nil {
		return err
	}
	s.sendTracked(q)
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
	if utf8.RuneCountInString(v) > max { // characters, as the dashboard's maxLength counts them
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
