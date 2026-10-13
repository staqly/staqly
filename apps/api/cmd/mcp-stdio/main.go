// Command mcp-stdio serves the Staqly MCP tools over STDIO for local agents
// (Claude Code, Codex). It opens the same SQLite database as the server,
// typically via `docker exec -i staqly-api mcp-stdio`. In local mode no key
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

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/config"
	"github.com/staqly/staqly/apps/api/internal/mcpserver"
	"github.com/staqly/staqly/apps/api/internal/store"
)

var version = "dev"

func main() {
	// stdout is the protocol channel; all logging goes to stderr.
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := run(log); err != nil {
		fmt.Fprintln(os.Stderr, "mcp-stdio:", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	key := os.Getenv("STAQLY_API_KEY")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg := config.Load()
	database, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer database.Close()
	svc := app.New(database, log, cfg.App)
	var p app.Principal
	switch {
	case key != "":
		if p, err = svc.AuthenticateAPIKey(ctx, key); err != nil {
			return fmt.Errorf("STAQLY_API_KEY is invalid or revoked")
		}
	case cfg.App.Mode == app.ModeLocal:
		if err := svc.EnsureLocalWorkspace(ctx); err != nil {
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
