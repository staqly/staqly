// Command server runs the REST API, the MCP Streamable HTTP endpoint and the
// webhook worker in one process (SQLite has a single writer anyway).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	api "github.com/staqly/staqly/apps/api"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/config"
	"github.com/staqly/staqly/apps/api/internal/httpapi"
	"github.com/staqly/staqly/apps/api/internal/mcpserver"
	"github.com/staqly/staqly/apps/api/internal/store"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		os.Exit(backup(os.Args[2:]))
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "recovery-key", "set-plan", "workspaces", "connect-link":
			os.Exit(admin(os.Args[1], os.Args[2:]))
		}
	}
	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)
	if err := run(cfg, log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(cfg config.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer database.Close()
	svc := app.New(database, log, cfg.App)
	if cfg.App.Mode == app.ModeLocal {
		if err := svc.EnsureLocalWorkspace(ctx); err != nil {
			return err
		}
	} else if cfg.App.SetupToken == "" {
		log.Info("cloud mode: public signup is open (Free plan); set SETUP_TOKEN to restrict it")
	}

	handler := httpapi.New(svc, log, httpapi.Config{
		CookieSecure: cfg.CookieSecure, TrustProxy: cfg.TrustProxy, OpenAPISpec: api.OpenAPI, Version: version,
		MCP: mcpserver.HTTPHandler(svc, version),
	})
	srv := &http.Server{Addr: cfg.Addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}

	workersDone := make(chan struct{})
	go func() { svc.RunBackground(ctx); close(workersDone) }()

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "mode", cfg.App.Mode, "addr", cfg.Addr, "db", cfg.DBPath, "version", version)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err = srv.Shutdown(shutCtx)
	<-workersDone // in-flight work finishes; leased/pending work resumes on next start
	return err
}

// backup writes a consistent copy of the database (VACUUM INTO) while the
// server keeps running: `server backup /data/backups/workspace-2026.db`.
func backup(args []string) int {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: server backup <destination.db>")
		return 2
	}
	cfg := config.Load()
	ctx := context.Background()
	database, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer database.Close()
	if err := store.Backup(ctx, database, fs.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, "backup failed:", err)
		return 1
	}
	fmt.Println("database backed up to", fs.Arg(0))
	return 0
}

// admin implements operator commands. Whoever can run them already controls
// the data volume, so they need no further authentication:
//
//	server workspaces                       list workspaces and plans
//	server recovery-key [workspace_id]      print a new recovery key (default: local workspace)
//	server set-plan <workspace_id> <plan>   free | indie | team (no payments yet)
//	server connect-link [workspace_id]      one-time sign-in link for a browser (5 min)
func admin(cmd string, args []string) int {
	cfg := config.Load()
	ctx := context.Background()
	database, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer database.Close()
	svc := app.New(database, slog.New(slog.DiscardHandler), cfg.App)
	switch cmd {
	case "workspaces":
		rows, err := svc.Q.ListWorkspaces(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		for _, w := range rows {
			fmt.Printf("%s\t%s\t%s\t%s\n", w.ID, w.Plan, w.CreatedAt, w.Name)
		}
	case "recovery-key":
		ws := ""
		if len(args) > 0 {
			ws = args[0]
		}
		key, err := svc.ResetRecoveryKey(ctx, ws)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(key)
	case "connect-link":
		ws := ""
		if len(args) > 0 {
			ws = args[0]
		}
		if ws == "" {
			inst, err := svc.Q.GetInstallation(ctx)
			if err != nil {
				fmt.Fprintln(os.Stderr, "no local workspace; pass a workspace id (see `server workspaces`)")
				return 1
			}
			ws = inst.WorkspaceID
		}
		link, err := svc.CreateDeviceLink(ctx, app.Principal{WorkspaceID: ws, Kind: app.PrincipalSystem, ID: "cli", Name: "Server CLI"}, "")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(link.URL)
		fmt.Fprintln(os.Stderr, "Open this link in the browser to sign it in. It works once and expires in 5 minutes.")
	case "set-plan":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "usage: server set-plan <workspace_id> <free|indie|team>")
			return 2
		}
		if err := svc.SetPlan(ctx, args[0], args[1]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("workspace %s is now on the %s plan\n", args[0], args[1])
	}
	return 0
}
