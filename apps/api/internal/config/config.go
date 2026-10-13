// Package config reads settings from environment variables.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
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
	DataDir       string
	DBPath        string
	CookieSecure  bool
	TrustProxy    bool
	LogLevel      slog.Level
	LogFormat     string // json (default) | text
	App           app.Config
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
	webPort := "8080"
	if _, p, err := net.SplitHostPort(dashAddr); err == nil {
		webPort = p
	}
	dataDir := env("DATA_DIR", "./data")
	c := Config{
		Addr:          env("ADDR", addr),
		DashboardAddr: dashAddr,
		DataDir:       dataDir,
		DBPath:        env("DATABASE_PATH", filepath.Join(dataDir, "workspace.db")),
		CookieSecure:  envBool("COOKIE_SECURE", !local),
		TrustProxy:    envBool("TRUST_PROXY", false),
		App: app.Config{
			Mode:              mode,
			LocalHosts:        list(os.Getenv("LOCAL_ALLOWED_HOSTS")),
			LocalTrustedPeers: peers,
			LocalOrigins:      list(os.Getenv("LOCAL_ALLOWED_ORIGINS")),
			UploadDir:         env("UPLOAD_DIR", filepath.Join(dataDir, "uploads")),
			SetupToken:        os.Getenv("SETUP_TOKEN"),
			PublicURL:         env("PUBLIC_API_URL", "http://localhost:3333"),
			PublicWebURL:      os.Getenv("PUBLIC_WEB_URL"),
			WebPort:           env("WEB_PORT", webPort),
			// A dashboard listening beyond loopback can't vouch for "this computer".
			DashboardExposed:    envBool("DASHBOARD_EXPOSED", dashAddr != "" && !LoopbackOnly(dashAddr)),
			WatcherAllowPrivate: envBool("WATCHER_ALLOW_PRIVATE", false),
			SessionTTL:          envDur("SESSION_TTL", 30*24*time.Hour),
			Webhook: app.WebhookConfig{
				// Local: n8n etc. usually live on the Docker network or LAN.
				// Cloud: tenants must not reach the operator's private network.
				AllowPrivate:  envBool("WEBHOOK_ALLOW_PRIVATE", local),
				AllowLoopback: envBool("WEBHOOK_ALLOW_LOOPBACK", false),
				RetryBase:     envDur("WEBHOOK_RETRY_BASE", 15*time.Second),
				RetryMax:      envDur("WEBHOOK_RETRY_MAX", time.Hour),
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
