// Package config reads settings from environment variables.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/blob"
	"github.com/staqly/staqly/apps/api/internal/logctx"
)

type Config struct {
	Addr string
	// DashboardAddr: also serve the embedded dashboard there ("" = no; the
	// Docker setup runs it as its own container, `staqly serve` sets it).
	DashboardAddr string
	// CORSOrigins: browser origins allowed to call the API with an API key
	// (cloud mode only; default the docs playground).
	CORSOrigins []string
	DataDir     string
	// DBPath: the SQLite file (local mode).
	DBPath string
	// DatabaseURL: the PostgreSQL database (cloud mode, required there; DATABASE_URL).
	DatabaseURL  string
	CookieSecure bool
	TrustProxy   bool
	LogLevel     slog.Level
	LogFormat    string // json (default) | text
	// SentryDSN: report error-level log lines (5xx, panics, worker
	// failures) to Sentry; empty = off.
	SentryDSN         string
	SentryEnvironment string
	// StorageCreateBucket: create a missing S3 bucket at startup (S3_CREATE_BUCKET; dev stacks).
	StorageCreateBucket bool
	App                 app.Config
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	if v, err := strconv.ParseBool(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

func envDur(k string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

// corsOrigins: CORS_ALLOWED_ORIGINS (comma/space separated, "none" to turn it
// off), default https://docs.staqly.ai for the API playground. Local mode is
// keyless, so it never answers cross-origin browser calls.
func corsOrigins(local bool) []string {
	if local {
		return nil
	}
	v := env("CORS_ALLOWED_ORIGINS", "https://docs.staqly.ai")
	if v == "none" {
		return nil
	}
	return list(v)
}

func list(v string) []string {
	return strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' })
}

// ParseMode normalizes STAQLY_MODE: empty means local; anything other than
// local or cloud is an error (a typo must not silently pick a mode).
func ParseMode(v string) (string, error) {
	switch m := strings.ToLower(strings.TrimSpace(v)); m {
	case "", app.ModeLocal:
		return app.ModeLocal, nil
	case app.ModeCloud:
		return app.ModeCloud, nil
	default:
		return "", fmt.Errorf("STAQLY_MODE must be local or cloud (got %q)", v)
	}
}

// ParsePrefixes parses a comma/space separated list of CIDRs or single
// addresses (LOCAL_TRUSTED_PEERS).
func ParsePrefixes(v string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, f := range list(v) {
		if p, err := netip.ParsePrefix(f); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(f)
		if err != nil {
			return nil, fmt.Errorf("LOCAL_TRUSTED_PEERS: %q is not a CIDR or IP address", f)
		}
		out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
	}
	return out, nil
}

// Load reads the environment. Settings whose safe default differs by mode
// (bind address, cookie Secure flag, private webhook targets) are resolved
// here; invalid values refuse to start rather than fall back silently.
func Load() (Config, error) {
	mode, err := ParseMode(os.Getenv("STAQLY_MODE"))
	if err != nil {
		return Config{}, err
	}
	local := mode == app.ModeLocal
	peers, err := ParsePrefixes(os.Getenv("LOCAL_TRUSTED_PEERS"))
	if err != nil {
		return Config{}, err
	}
	if k := os.Getenv("NOTIFY_SECRET_KEY"); k != "" && len(k) < app.MinNotifySecretKeyLen {
		return Config{}, fmt.Errorf("NOTIFY_SECRET_KEY must be at least %d bytes (e.g. `openssl rand -hex 32`)", app.MinNotifySecretKeyLen)
	}
	// Local mode is keyless, so by default it listens on loopback only; the
	// Docker image sets ADDR=:3333 explicitly (compose publishes the port on
	// 127.0.0.1 and LOCAL_TRUSTED_PEERS admits the bridge network).
	addr := ":3333"
	if local {
		addr = "127.0.0.1:3333"
	}
	dashAddr := os.Getenv("DASHBOARD_ADDR")
	dashboardPort := "8080"
	if _, p, err := net.SplitHostPort(dashAddr); err == nil {
		dashboardPort = p
	}
	dataDir := env("DATA_DIR", "./data")
	// Cloud mode runs on PostgreSQL and never falls back to SQLite; local mode is SQLite only.
	databaseURL := os.Getenv("DATABASE_URL")
	switch {
	case !local && databaseURL == "":
		return Config{}, fmt.Errorf("cloud mode needs DATABASE_URL (postgres://user:password@host:5432/staqly); " +
			"an existing cloud database on SQLite moves over with `server import-sqlite <workspace.db>` (https://docs.staqly.ai/self-hosting/cloud-deployment)")
	case local && databaseURL != "":
		return Config{}, fmt.Errorf("DATABASE_URL is for cloud mode (STAQLY_MODE=cloud); local mode keeps its data in SQLite (DATABASE_PATH)")
	}
	processingTimeout := envDur("PROCESSING_TIMEOUT", time.Hour)
	secretKey, err := app.LoadSecretKey(dataDir, os.Getenv("STAQLY_SECRET_KEY"))
	if err != nil {
		return Config{}, err
	}
	c := Config{
		Addr:              env("ADDR", addr),
		DashboardAddr:     dashAddr,
		CORSOrigins:       corsOrigins(local),
		DataDir:           dataDir,
		DBPath:            env("DATABASE_PATH", filepath.Join(dataDir, "workspace.db")),
		DatabaseURL:       databaseURL,
		CookieSecure:      envBool("COOKIE_SECURE", !local),
		TrustProxy:        envBool("TRUST_PROXY", false),
		SentryDSN:         os.Getenv("SENTRY_DSN"),
		SentryEnvironment: env("SENTRY_ENVIRONMENT", "production"),
		App: app.Config{
			Mode: mode,
			AI: app.AIConfig{
				Disabled:      envBool("AI_DISABLED", false),
				OpenAIBaseURL: strings.TrimRight(env("AI_OPENAI_BASE_URL", "https://api.openai.com/v1"), "/"),
			},
			LocalHosts:         list(os.Getenv("LOCAL_ALLOWED_HOSTS")),
			LocalTrustedPeers:  peers,
			LocalOrigins:       list(os.Getenv("LOCAL_ALLOWED_ORIGINS")),
			UploadDir:          env("UPLOAD_DIR", filepath.Join(dataDir, "uploads")),
			SetupToken:         os.Getenv("SETUP_TOKEN"),
			PublicURL:          env("PUBLIC_API_URL", "http://localhost:3333"),
			PublicDashboardURL: os.Getenv("PUBLIC_DASHBOARD_URL"),
			SecretKey:          secretKey,
			DashboardPort:      env("DASHBOARD_PORT", dashboardPort),
			// A dashboard listening beyond loopback can't vouch for "this computer".
			DashboardExposed: envBool("DASHBOARD_EXPOSED", dashAddr != "" && !LoopbackOnly(dashAddr)),
			SessionTTL:       envDur("SESSION_TTL", 30*24*time.Hour),
			// How long a target on your own automation waits for a result (0 = never); a collection can shorten it.
			ProcessingTimeout:      processingTimeout,
			ProcessingTimeoutNever: processingTimeout == 0,
			Webhook: app.WebhookConfig{
				// Local: n8n etc. usually live on the Docker network or LAN.
				// Cloud: tenants must not reach the operator's private network.
				AllowPrivate:  envBool("WEBHOOK_ALLOW_PRIVATE", local),
				AllowLoopback: envBool("WEBHOOK_ALLOW_LOOPBACK", false),
				RetryBase:     envDur("WEBHOOK_RETRY_BASE", 15*time.Second),
				RetryMax:      envDur("WEBHOOK_RETRY_MAX", time.Hour),
			},
			// Cloud billing; on when the key and the webhook secret are set. Prices are found by lookup key
			// (staqly_<plan>_<month|year>); STRIPE_PRICE_<PLAN>_<MONTHLY|YEARLY> overrides one with a price id.
			Billing: app.BillingConfig{
				SecretKey:     os.Getenv("STRIPE_SECRET_KEY"),
				WebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
				Prices:        stripePrices(),
			},
			// Only for test and demo servers: the plan a new workspace's trial gives (default starter).
			TrialPlan: os.Getenv("TRIAL_PLAN"),
		},
	}
	if mb, err := strconv.ParseInt(os.Getenv("MAX_UPLOAD_MB"), 10, 64); err == nil && mb > 0 {
		c.App.MaxUploadBytes = mb << 20
	}
	if !local {
		c.App.DevSignIn = envBool("AUTH_DEV_SIGNIN", false)
		c.App.EmailSignIn = (os.Getenv("MAIL_SMTP_HOST") != "" && os.Getenv("MAIL_FROM") != "") || c.App.DevSignIn
		if c.App.Google, err = googleConfig(c.App.PublicDashboardURL, c.App.EmailSignIn); err != nil {
			return Config{}, err
		}
	}
	if c.App.Blob, err = storage(c.App.UploadDir, dataDir); err != nil {
		return Config{}, err
	}
	c.StorageCreateBucket = envBool("S3_CREATE_BUCKET", false)
	if c.LogLevel, err = logctx.ParseLevel(os.Getenv("LOG_LEVEL")); err != nil {
		return Config{}, err
	}
	if c.LogFormat, err = logctx.ParseFormat(os.Getenv("LOG_FORMAT"), "json"); err != nil {
		return Config{}, err
	}
	return c, nil
}

// LoopbackOnly reports whether addr (host:port) binds to loopback only.
func LoopbackOnly(addr string) bool {
	host := addr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		host = addr[:i]
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil && v > 0 {
		return v
	}
	return def
}

// googleConfig: cloud mode signs people in with Google, with an email code, or both. With neither (no
// Google client and no mail server) it refuses to start; AUTH_DEV_SIGNIN=true counts as email sign-in.
func googleConfig(dashboardURL string, emailSignIn bool) (*app.GoogleConfig, error) {
	id, secret := os.Getenv("GOOGLE_CLIENT_ID"), os.Getenv("GOOGLE_CLIENT_SECRET")
	if id == "" && secret == "" {
		if emailSignIn {
			return nil, nil
		}
		return nil, app.ErrAccountsMisconfigured
	}
	if id == "" || secret == "" {
		return nil, fmt.Errorf("sign-in with Google needs both GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET")
	}
	redirect := os.Getenv("GOOGLE_REDIRECT_URL")
	if redirect == "" {
		if dashboardURL == "" {
			return nil, fmt.Errorf("sign-in with Google needs the dashboard's public address: set PUBLIC_DASHBOARD_URL (https://staqly.example.com) " +
				"or GOOGLE_REDIRECT_URL (https://staqly.example.com/api/v1/auth/google/callback)")
		}
		redirect = strings.TrimRight(dashboardURL, "/") + "/api/v1/auth/google/callback"
	}
	if u, err := url.Parse(redirect); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("GOOGLE_REDIRECT_URL: %q is not an http(s) URL", redirect)
	}
	return &app.GoogleConfig{ClientID: id, ClientSecret: secret, RedirectURL: redirect,
		Issuer: env("GOOGLE_ISSUER", "https://accounts.google.com")}, nil
}

// priceID: what a real Stripe price id looks like (price_ and letters/digits). A copied placeholder such as
// "price_..." is not one.
var priceID = regexp.MustCompile(`^price_[A-Za-z0-9]+$`)

// stripePrices reads the optional STRIPE_PRICE_<PLAN>_<MONTHLY|YEARLY> overrides, keyed "<plan>_<month|year>".
// A value that isn't a price id (a placeholder left from .env.example) is ignored with a warning, so the
// price is found by its lookup key.
func stripePrices() map[string]string {
	out := map[string]string{}
	for _, p := range app.Plans {
		for env, iv := range map[string]string{"MONTHLY": app.IntervalMonth, "YEARLY": app.IntervalYear} {
			name := "STRIPE_PRICE_" + strings.ToUpper(p.ID) + "_" + env
			id := strings.TrimSpace(os.Getenv(name))
			switch {
			case id == "":
			case !priceID.MatchString(id):
				slog.Warn("config: ignoring "+name+": not a Stripe price id (leave it unset to use lookup keys)", "value", id)
			default:
				out[p.ID+"_"+iv] = id
			}
		}
	}
	return out
}

// storage picks where media files live: STORAGE_DRIVER=disk (default, UPLOAD_DIR) or s3 (any S3-compatible
// bucket: AWS S3, Cloudflare R2, MaxIOFS...). s3 with missing settings refuses to start.
func storage(uploadDir, dataDir string) (blob.Store, error) {
	switch d := env("STORAGE_DRIVER", "disk"); d {
	case "disk":
		return blob.NewDisk(uploadDir), nil
	case "s3":
		cfg := blob.S3Config{
			Endpoint:  os.Getenv("S3_ENDPOINT"),
			Region:    os.Getenv("S3_REGION"),
			Bucket:    os.Getenv("S3_BUCKET"),
			AccessKey: os.Getenv("S3_ACCESS_KEY_ID"),
			SecretKey: os.Getenv("S3_SECRET_ACCESS_KEY"),
			PathStyle: envBool("S3_FORCE_PATH_STYLE", false),
			Prefix:    os.Getenv("S3_PREFIX"),
			PublicURL: os.Getenv("S3_PUBLIC_URL"),
			TempDir:   filepath.Join(dataDir, "tmp"),
		}
		if missing := cfg.Missing(); len(missing) > 0 {
			sort.Strings(missing)
			return nil, fmt.Errorf("STORAGE_DRIVER=s3 needs %s (https://docs.staqly.ai/self-hosting/storage)", strings.Join(missing, ", "))
		}
		return blob.NewS3(cfg)
	default:
		return nil, fmt.Errorf("STORAGE_DRIVER must be disk or s3 (got %q)", d)
	}
}
