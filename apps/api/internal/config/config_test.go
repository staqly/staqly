package config

import (
	"log/slog"
	"strings"
	"testing"
)

func clearEnv(t *testing.T) {
	for _, k := range []string{"STAQLY_MODE", "ADDR", "COOKIE_SECURE", "WEBHOOK_ALLOW_PRIVATE", "LOCAL_TRUSTED_PEERS",
		"LOCAL_ALLOWED_ORIGINS", "NOTIFY_SECRET_KEY", "LOG_LEVEL", "LOG_FORMAT", "DASHBOARD_ADDR",
		"DASHBOARD_EXPOSED", "WEB_PORT"} {
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
	if c.Addr != "127.0.0.1:3333" || c.CookieSecure || !c.App.Webhook.AllowPrivate || len(c.App.LocalTrustedPeers) != 0 {
		t.Fatalf("local defaults: addr=%s secure=%v private=%v peers=%v", c.Addr, c.CookieSecure, c.App.Webhook.AllowPrivate, c.App.LocalTrustedPeers)
	}
	t.Setenv("STAQLY_MODE", "cloud")
	if c, err = Load(); err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":3333" || !c.CookieSecure || c.App.Webhook.AllowPrivate || c.App.Mode != "cloud" {
		t.Fatalf("cloud defaults: addr=%s secure=%v private=%v", c.Addr, c.CookieSecure, c.App.Webhook.AllowPrivate)
	}
	// Explicit settings win (the Docker image sets ADDR=:3333).
	t.Setenv("STAQLY_MODE", "local")
	t.Setenv("ADDR", ":3333")
	t.Setenv("COOKIE_SECURE", "true")
	t.Setenv("WEBHOOK_ALLOW_PRIVATE", "false")
	t.Setenv("LOCAL_TRUSTED_PEERS", "10.0.0.0/8,172.16.0.0/12, 192.168.0.0/16 fc00::/7,203.0.113.7")
	t.Setenv("LOCAL_ALLOWED_ORIGINS", "http://localhost:6274")
	if c, err = Load(); err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":3333" || !c.CookieSecure || c.App.Webhook.AllowPrivate || len(c.App.LocalTrustedPeers) != 5 ||
		c.App.LocalTrustedPeers[4].String() != "203.0.113.7/32" || len(c.App.LocalOrigins) != 1 {
		t.Fatalf("explicit: %+v %v", c, c.App.LocalTrustedPeers)
	}
	t.Setenv("LOCAL_TRUSTED_PEERS", "10.0.0.0/8,lan")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "LOCAL_TRUSTED_PEERS") {
		t.Fatalf("invalid peer accepted: %v", err)
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
	if c.DashboardAddr != "" || c.App.DashboardExposed || c.App.WebPort != "8080" {
		t.Fatalf("default: %q exposed=%v port=%s", c.DashboardAddr, c.App.DashboardExposed, c.App.WebPort)
	}
	t.Setenv("DASHBOARD_ADDR", "127.0.0.1:9090")
	if c, _ = Load(); c.App.DashboardExposed || c.App.WebPort != "9090" {
		t.Fatalf("loopback: exposed=%v port=%s", c.App.DashboardExposed, c.App.WebPort)
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
