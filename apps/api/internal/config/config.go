// Package config reads settings from environment variables.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/logctx"
)

type Config struct {
	Addr string
	// DashboardAddr: also serve the embedded dashboard there ("" = no; the
	// Docker setup runs it as its own container, `staqly serve` sets it).
	DashboardAddr string
	// CORSOrigins: browser origins allowed to call the API with an API key
	// (cloud mode only; default the docs playground).
	CORSOrigins  []string
	DataDir      string
	DBPath       string
	CookieSecure bool
	TrustProxy   bool
	LogLevel     slog.Level
	LogFormat    string // json (default) | text
	// SentryDSN: report error-level log lines (5xx, panics, worker
	// failures) to Sentry; empty = off.
	SentryDSN         string
	SentryEnvironment string
	App               app.Config
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
	c := Config{
		Addr:              env("ADDR", addr),
		DashboardAddr:     dashAddr,
		CORSOrigins:       corsOrigins(local),
		DataDir:           dataDir,
		DBPath:            env("DATABASE_PATH", filepath.Join(dataDir, "workspace.db")),
		CookieSecure:      envBool("COOKIE_SECURE", !local),
		TrustProxy:        envBool("TRUST_PROXY", false),
		SentryDSN:         os.Getenv("SENTRY_DSN"),
		SentryEnvironment: env("SENTRY_ENVIRONMENT", "production"),
		App: app.Config{
			Mode:               mode,
			LocalHosts:         list(os.Getenv("LOCAL_ALLOWED_HOSTS")),
			LocalTrustedPeers:  peers,
			LocalOrigins:       list(os.Getenv("LOCAL_ALLOWED_ORIGINS")),
			UploadDir:          env("UPLOAD_DIR", filepath.Join(dataDir, "uploads")),
			SetupToken:         os.Getenv("SETUP_TOKEN"),
			PublicURL:          env("PUBLIC_API_URL", "http://localhost:3333"),
			PublicDashboardURL: os.Getenv("PUBLIC_DASHBOARD_URL"),
			DashboardPort:      env("DASHBOARD_PORT", dashboardPort),
			// A dashboard listening beyond loopback can't vouch for "this computer".
			DashboardExposed:    envBool("DASHBOARD_EXPOSED", dashAddr != "" && !LoopbackOnly(dashAddr)),
			WatcherAllowPrivate: envBool("WATCHER_ALLOW_PRIVATE", false),
			Tagger: app.TaggerConfig{
				URL:         os.Getenv("TAGGER_URL"),
				Model:       env("TAGGER_MODEL", "gpt-4o-mini"),
				APIKey:      os.Getenv("TAGGER_API_KEY"),
				Command:     taggerCommand(local, os.Getenv("TAGGER_URL"), os.Getenv("TAGGER_COMMAND"), exec.LookPath),
				Concurrency: envInt("TAGGER_CONCURRENCY", 2),
			},
			SessionTTL: envDur("SESSION_TTL", 30*24*time.Hour),
			Webhook: app.WebhookConfig{
				// Local: n8n etc. usually live on the Docker network or LAN.
				// Cloud: tenants must not reach the operator's private network.
				AllowPrivate:  envBool("WEBHOOK_ALLOW_PRIVATE", local),
				AllowLoopback: envBool("WEBHOOK_ALLOW_LOOPBACK", false),
				RetryBase:     envDur("WEBHOOK_RETRY_BASE", 15*time.Second),
				RetryMax:      envDur("WEBHOOK_RETRY_MAX", time.Hour),
			},
			// Cloud billing; on only when all three are set.
			Billing: app.BillingConfig{
				SecretKey:     os.Getenv("STRIPE_SECRET_KEY"),
				WebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
				PricePro:      os.Getenv("STRIPE_PRICE_PRO"),
			},
		},
	}
	if mb, err := strconv.ParseInt(os.Getenv("MAX_UPLOAD_MB"), 10, 64); err == nil && mb > 0 {
		c.App.MaxUploadBytes = mb << 20
	}
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

// taggerCommand picks the local AI CLI for summaries: TAGGER_COMMAND as given ("off" disables), else in local
// mode without TAGGER_URL the user's Claude Code, else Codex, if on PATH. Cloud mode never runs a command: the
// operator sets TAGGER_URL.
func taggerCommand(local bool, url, cmd string, lookPath func(string) (string, error)) []string {
	if !local || strings.EqualFold(strings.TrimSpace(cmd), "off") {
		return nil
	}
	if f := strings.Fields(cmd); len(f) > 0 {
		return f
	}
	if url != "" {
		return nil
	}
	if p, err := lookPath("claude"); err == nil {
		return []string{p, "-p", "--model", "haiku"}
	}
	if p, err := lookPath("codex"); err == nil {
		return []string{p, "exec", "--skip-git-repo-check", "-"}
	}
	return nil
}

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil && v > 0 {
		return v
	}
	return def
}
