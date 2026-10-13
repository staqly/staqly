// Package config reads settings from environment variables.
package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

type Config struct {
	Addr         string
	DataDir      string
	DBPath       string
	CookieSecure bool
	TrustProxy   bool
	LogLevel     slog.Level
	App          app.Config
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

func Load() Config {
	dataDir := env("DATA_DIR", "./data")
	c := Config{
		Addr:         env("ADDR", ":3333"),
		DataDir:      dataDir,
		DBPath:       env("DATABASE_PATH", filepath.Join(dataDir, "workspace.db")),
		CookieSecure: envBool("COOKIE_SECURE", false),
		TrustProxy:   envBool("TRUST_PROXY", false),
		App: app.Config{
			Mode:                env("STAQLY_MODE", app.ModeLocal),
			LocalHosts:          strings.FieldsFunc(os.Getenv("LOCAL_ALLOWED_HOSTS"), func(r rune) bool { return r == ',' || r == ' ' }),
			UploadDir:           env("UPLOAD_DIR", filepath.Join(dataDir, "uploads")),
			SetupToken:          os.Getenv("SETUP_TOKEN"),
			PublicURL:           env("PUBLIC_API_URL", "http://localhost:3333"),
			PublicWebURL:        os.Getenv("PUBLIC_WEB_URL"),
			WebPort:             env("WEB_PORT", "8080"),
			DashboardExposed:    envBool("DASHBOARD_EXPOSED", false),
			WatcherAllowPrivate: envBool("WATCHER_ALLOW_PRIVATE", false),
			SessionTTL:          envDur("SESSION_TTL", 30*24*time.Hour),
			Webhook: app.WebhookConfig{
				AllowPrivate:  envBool("WEBHOOK_ALLOW_PRIVATE", true),
				AllowLoopback: envBool("WEBHOOK_ALLOW_LOOPBACK", false),
				RetryBase:     envDur("WEBHOOK_RETRY_BASE", 15*time.Second),
				RetryMax:      envDur("WEBHOOK_RETRY_MAX", time.Hour),
			},
		},
	}
	if mb, err := strconv.ParseInt(os.Getenv("MAX_UPLOAD_MB"), 10, 64); err == nil && mb > 0 {
		c.App.MaxUploadBytes = mb << 20
	}
	if os.Getenv("LOG_LEVEL") == "debug" {
		c.LogLevel = slog.LevelDebug
	}
	return c
}
