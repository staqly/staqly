// Command mcp-stdio serves the staqly MCP tools over STDIO for local agents
// (Claude Code, Codex). It opens the same SQLite database as the server,
// typically via `docker exec -i staqly mcp-stdio`. In local mode no key
// is needed; in cloud mode it authenticates with STAQLY_API_KEY.
// Webhook deliveries it queues are sent by the server's worker.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/config"
	"github.com/staqly/staqly/apps/api/internal/logctx"
	"github.com/staqly/staqly/apps/api/internal/mcpserver"
	"github.com/staqly/staqly/apps/api/internal/store"
)

var version = "dev"

func main() {
	// stdout is the protocol channel; all logging goes to stderr (text and
	// warn by default; LOG_FORMAT / LOG_LEVEL override).
	level, format := slog.Level(slog.LevelWarn), "text"
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		l, err := logctx.ParseLevel(v)
		if err != nil {
			fmt.Fprintln(os.Stderr, "mcp-stdio:", err)
			os.Exit(2)
		}
		level = l
	}
	format, err := logctx.ParseFormat(os.Getenv("LOG_FORMAT"), format)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcp-stdio:", err)
		os.Exit(2)
	}
	log := logctx.New(os.Stderr, format, level).With("service", "staqly-mcp-stdio", "version", version)
	if err := run(log); err != nil {
		fmt.Fprintln(os.Stderr, "mcp-stdio:", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	key := os.Getenv("STAQLY_API_KEY")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	database, err := store.OpenFor(ctx, cfg.DatabaseURL, cfg.DBPath)
	if err != nil {
		return err
	}
	defer database.Close()
	svc := app.New(database, log, cfg.App)
	var p app.Principal
	switch {
	case key != "":
		if _, err := svc.AuthenticateAPIKey(ctx, key); err != nil {
			return fmt.Errorf("STAQLY_API_KEY is invalid or revoked")
		}
		// The process can live for days: re-check the key on every tool call
		// (cached up to a minute) so revocation and expiry take effect.
		return mcpserver.NewWithAuth(svc, mcpserver.KeyAuth(svc, key, time.Minute), version).Run(ctx, &mcp.StdioTransport{})
	case cfg.App.Mode == app.ModeLocal:
		if err := svc.EnsureLocalWorkspace(ctx); err != nil {
			return err
		}
		if err := svc.EnsureDefaultCollections(ctx); err != nil {
			return err
		}
		if p, err = svc.LocalPrincipal(ctx); err != nil {
			return err
		}
	default:
		return fmt.Errorf("cloud mode: STAQLY_API_KEY is not set")
	}
	return mcpserver.New(svc, p, version).Run(ctx, &mcp.StdioTransport{})
}
