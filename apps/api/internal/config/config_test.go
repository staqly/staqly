package config

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

func clearEnv(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir()) // Load writes secret.key into DATA_DIR
	for _, k := range []string{"STAQLY_MODE", "ADDR", "COOKIE_SECURE", "WEBHOOK_ALLOW_PRIVATE", "LOCAL_TRUSTED_PEERS",
		"LOCAL_ALLOWED_ORIGINS", "STAQLY_LOCAL_TOKEN", "NOTIFY_SECRET_KEY", "LOG_LEVEL", "LOG_FORMAT", "DASHBOARD_ADDR",
		"DASHBOARD_EXPOSED", "DASHBOARD_PORT", "CORS_ALLOWED_ORIGINS", "DATABASE_URL", "GOOGLE_CLIENT_ID",
		"GOOGLE_CLIENT_SECRET", "GOOGLE_REDIRECT_URL", "GOOGLE_ISSUER", "AUTH_DEV_SIGNIN", "PUBLIC_DASHBOARD_URL",
		"MAIL_SMTP_HOST", "MAIL_FROM", "STORAGE_DRIVER", "S3_ENDPOINT", "S3_REGION",
		"S3_BUCKET", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY", "S3_FORCE_PATH_STYLE", "S3_PREFIX", "S3_PUBLIC_URL", "S3_SIGN_ENDPOINT",
		"POSTHOG_KEY", "POSTHOG_HOST", "POSTHOG_ENVIRONMENT"} {
		t.Setenv(k, "")
	}
}

// Audit M6: the mode is normalized and a typo refuses to start.
func TestParseMode(t *testing.T) {
	for in, want := range map[string]string{"": "local", "local": "local", " Cloud ": "cloud", "LOCAL\n": "local"} {
		if got, err := ParseMode(in); err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"clould", "prod", "local,cloud"} {
		if _, err := ParseMode(bad); err == nil {
			t.Errorf("ParseMode(%q) accepted", bad)
		}
	}
	clearEnv(t)
	t.Setenv("STAQLY_MODE", "cloudy")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted an unknown mode")
	}
}

// Audit H1/H2/L13: safe defaults depend on the mode.
func TestModeDependentDefaults(t *testing.T) {
	clearEnv(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.App.Webhook.Concurrency != 4 {
		t.Fatalf("local webhook concurrency = %d, want 4", c.App.Webhook.Concurrency)
	}
	if c.Addr != "127.0.0.1:3333" || c.CookieSecure || !c.App.Webhook.AllowPrivate || c.App.LocalToken != "" ||
		!strings.HasSuffix(c.App.LocalTokenPath, "access_token") || len(c.Obsolete) != 0 {
		t.Fatalf("local defaults: addr=%s secure=%v private=%v token=%q at %s obsolete=%v", c.Addr, c.CookieSecure,
			c.App.Webhook.AllowPrivate, c.App.LocalToken, c.App.LocalTokenPath, c.Obsolete)
	}
	// Cloud mode needs PostgreSQL and never falls back to SQLite; local mode refuses a DATABASE_URL.
	t.Setenv("STAQLY_MODE", "cloud")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("cloud mode without DATABASE_URL must not start: %v", err)
	}
	t.Setenv("DATABASE_URL", "postgres://staqly@db:5432/staqly")
	// Cloud mode signs people in with Google or an email code: with neither it refuses to start.
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GOOGLE_CLIENT_ID") || !strings.Contains(err.Error(), "MAIL_SMTP_HOST") {
		t.Fatalf("cloud without a sign-in method must not start: %v", err)
	}
	t.Setenv("MAIL_SMTP_HOST", "smtp.example.com")
	t.Setenv("MAIL_FROM", "staqly@example.com")
	if c, err = Load(); err != nil || c.App.Google != nil || !c.App.EmailSignIn || c.App.DevSignIn {
		t.Fatalf("email sign-in only: %v %+v", err, c.App)
	}
	t.Setenv("MAIL_SMTP_HOST", "")
	t.Setenv("MAIL_FROM", "")
	t.Setenv("GOOGLE_CLIENT_ID", "id.apps.googleusercontent.com")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GOOGLE_CLIENT_SECRET") {
		t.Fatalf("half a Google client must not start: %v", err)
	}
	t.Setenv("GOOGLE_CLIENT_ID", "id.apps.googleusercontent.com")
	t.Setenv("GOOGLE_CLIENT_SECRET", "secret")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PUBLIC_DASHBOARD_URL") {
		t.Fatalf("Google without a dashboard address must not start: %v", err)
	}
	t.Setenv("PUBLIC_DASHBOARD_URL", "https://staqly.example.com/")
	if c, err = Load(); err != nil {
		t.Fatal(err)
	}
	if g := c.App.Google; g == nil || g.RedirectURL != "https://staqly.example.com/api/v1/auth/google/callback" || g.Issuer != "https://accounts.google.com" {
		t.Fatalf("google: %+v", g)
	}
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("GOOGLE_CLIENT_SECRET", "")
	t.Setenv("AUTH_DEV_SIGNIN", "true")
	if c, err = Load(); err != nil || c.App.Google != nil || !c.App.EmailSignIn || !c.App.DevSignIn {
		t.Fatalf("dev sign-in: %v %+v", err, c.App.Google)
	}
	if c.DatabaseURL != "postgres://staqly@db:5432/staqly" {
		t.Fatalf("database url: %q", c.DatabaseURL)
	}
	if c.App.Webhook.Concurrency != 16 {
		t.Fatalf("cloud webhook concurrency = %d, want 16", c.App.Webhook.Concurrency)
	}
	if c.Addr != ":3333" || !c.CookieSecure || c.App.Webhook.AllowPrivate || c.App.Mode != "cloud" {
		t.Fatalf("cloud defaults: addr=%s secure=%v private=%v", c.Addr, c.CookieSecure, c.App.Webhook.AllowPrivate)
	}
	t.Setenv("STAQLY_MODE", "local")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "DATABASE_URL is for cloud mode") {
		t.Fatalf("local mode must refuse DATABASE_URL: %v", err)
	}
	t.Setenv("DATABASE_URL", "")
	// Explicit settings win (the Docker image sets ADDR=:3333).
	t.Setenv("STAQLY_MODE", "local")
	t.Setenv("ADDR", ":3333")
	t.Setenv("COOKIE_SECURE", "true")
	t.Setenv("WEBHOOK_ALLOW_PRIVATE", "false")
	// The keyless guard's settings are gone: still set, they are named for a warning, never parsed.
	t.Setenv("LOCAL_TRUSTED_PEERS", "10.0.0.0/8,lan")
	t.Setenv("LOCAL_ALLOWED_ORIGINS", "http://localhost:6274")
	t.Setenv("STAQLY_LOCAL_TOKEN", "stk_local_fixedfixedfixedfixedfixedfixed")
	if c, err = Load(); err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":3333" || !c.CookieSecure || c.App.Webhook.AllowPrivate || len(c.Obsolete) != 2 ||
		c.App.LocalToken != "stk_local_fixedfixedfixedfixedfixedfixed" {
		t.Fatalf("explicit: %+v", c)
	}
	t.Setenv("STAQLY_LOCAL_TOKEN", "short")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STAQLY_LOCAL_TOKEN") {
		t.Fatalf("invalid token accepted: %v", err)
	}
}

// Audit L8: a short NOTIFY_SECRET_KEY refuses to start.
func TestNotifySecretKeyLength(t *testing.T) {
	clearEnv(t)
	t.Setenv("NOTIFY_SECRET_KEY", "short")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "NOTIFY_SECRET_KEY") {
		t.Fatalf("short key accepted: %v", err)
	}
	t.Setenv("NOTIFY_SECRET_KEY", strings.Repeat("k", 32))
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}

func TestLoopbackOnly(t *testing.T) {
	for addr, want := range map[string]bool{"127.0.0.1:3333": true, "[::1]:3333": true, "localhost:3333": true,
		":3333": false, "0.0.0.0:3333": false, "192.168.1.2:3333": false, "[::]:3333": false} {
		if got := LoopbackOnly(addr); got != want {
			t.Errorf("LoopbackOnly(%q) = %v", addr, got)
		}
	}
}

// LOG_LEVEL / LOG_FORMAT: case-insensitive, invalid values refuse to start.
func TestLogSettings(t *testing.T) {
	clearEnv(t)
	c, err := Load()
	if err != nil || c.LogLevel != slog.LevelInfo || c.LogFormat != "json" {
		t.Fatalf("defaults: %v %v %v", c.LogLevel, c.LogFormat, err)
	}
	t.Setenv("LOG_LEVEL", "WARN")
	t.Setenv("LOG_FORMAT", "Text")
	if c, err = Load(); err != nil || c.LogLevel != slog.LevelWarn || c.LogFormat != "text" {
		t.Fatalf("explicit: %v %v %v", c.LogLevel, c.LogFormat, err)
	}
	t.Setenv("LOG_LEVEL", "loud")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "LOG_LEVEL") {
		t.Fatalf("invalid level accepted: %v", err)
	}
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("LOG_FORMAT", "xml")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "LOG_FORMAT") {
		t.Fatalf("invalid format accepted: %v", err)
	}
}

func TestDashboardAddr(t *testing.T) {
	clearEnv(t)
	c, _ := Load()
	if c.DashboardAddr != "" || c.App.DashboardExposed || c.App.DashboardPort != "8080" {
		t.Fatalf("default: %q exposed=%v port=%s", c.DashboardAddr, c.App.DashboardExposed, c.App.DashboardPort)
	}
	t.Setenv("DASHBOARD_ADDR", "127.0.0.1:9090")
	if c, _ = Load(); c.App.DashboardExposed || c.App.DashboardPort != "9090" {
		t.Fatalf("loopback: exposed=%v port=%s", c.App.DashboardExposed, c.App.DashboardPort)
	}
	t.Setenv("DASHBOARD_ADDR", ":8080")
	if c, _ = Load(); !c.App.DashboardExposed {
		t.Fatal("a dashboard on all interfaces must require sessions")
	}
	t.Setenv("DASHBOARD_EXPOSED", "false")
	if c, _ = Load(); c.App.DashboardExposed {
		t.Fatal("DASHBOARD_EXPOSED=false not honoured")
	}
}

func TestCORSOrigins(t *testing.T) {
	clearEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://a.example")
	if c, _ := Load(); c.CORSOrigins != nil {
		t.Fatalf("local mode must not allow cross-origin calls: %v", c.CORSOrigins)
	}
	t.Setenv("STAQLY_MODE", "cloud")
	t.Setenv("DATABASE_URL", "postgres://staqly@db:5432/staqly")
	t.Setenv("AUTH_DEV_SIGNIN", "true")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	if c, _ := Load(); len(c.CORSOrigins) != 1 || c.CORSOrigins[0] != "https://docs.staqly.ai" {
		t.Fatalf("cloud default: %v", c.CORSOrigins)
	}
	t.Setenv("CORS_ALLOWED_ORIGINS", "none")
	if c, _ := Load(); c.CORSOrigins != nil {
		t.Fatalf("none: %v", c.CORSOrigins)
	}
}

// A STRIPE_PRICE_* value that isn't a price id (the .env.example placeholder "price_...") is ignored, so the
// price is found by its lookup key; a real id pins it.
func TestStripePricePlaceholdersAreIgnored(t *testing.T) {
	t.Setenv("STRIPE_PRICE_PLUS_MONTHLY", "price_...")
	t.Setenv("STRIPE_PRICE_PLUS_YEARLY", "price_1QabcDEF234")
	t.Setenv("STRIPE_PRICE_PRO_MONTHLY", " ")
	got := stripePrices()
	if _, ok := got["plus_month"]; ok || got["plus_year"] != "price_1QabcDEF234" || len(got) != 1 {
		t.Fatalf("prices: %v", got)
	}
}

// PostHog is off unless POSTHOG_KEY is set; the host defaults to the EU cloud.
func TestPostHogConfig(t *testing.T) {
	clearEnv(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.App.PostHog.Key != "" || c.App.PostHog.Host != "https://eu.i.posthog.com" || c.App.PostHog.Environment != "production" {
		t.Fatalf("defaults = %+v", c.App.PostHog)
	}
	t.Setenv("POSTHOG_KEY", "phc_test")
	t.Setenv("POSTHOG_HOST", "https://us.i.posthog.com/")
	t.Setenv("POSTHOG_ENVIRONMENT", "development")
	if c, err = Load(); err != nil {
		t.Fatal(err)
	}
	if c.App.PostHog != (app.PostHogConfig{Key: "phc_test", Host: "https://us.i.posthog.com", Environment: "development"}) {
		t.Fatalf("set = %+v", c.App.PostHog)
	}
}
