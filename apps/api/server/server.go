// Package server runs the REST API, the MCP Streamable HTTP endpoint, the
// webhook worker and (with DASHBOARD_ADDR) the dashboard in one process
// (SQLite has a single writer anyway). Used by cmd/server (the Docker image)
// and by `staqly serve` (apps/cli).
package server

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	api "github.com/staqly/staqly/apps/api"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/config"
	"github.com/staqly/staqly/apps/api/internal/httpapi"
	"github.com/staqly/staqly/apps/api/internal/logctx"
	"github.com/staqly/staqly/apps/api/internal/mcpserver"
	"github.com/staqly/staqly/apps/api/internal/store"
	"github.com/staqly/staqly/apps/api/internal/web"
)

// Main runs the server, or one of its operator subcommands (backup,
// workspaces, recovery-key, set-plan, connect-link), and returns the exit code.
func Main(args []string, version string) int {
	if len(args) > 0 {
		switch args[0] {
		case "backup":
			return backup(args[1:])
		case "recovery-key", "set-plan", "workspaces", "connect-link":
			return admin(args[0], args[1:])
		}
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 2
	}
	// JSON lines on stdout: what Cloudflare Logpush / Loki / Datadog ingest.
	log := logctx.Base(logctx.New(os.Stdout, cfg.LogFormat, cfg.LogLevel), version, cfg.App.Mode)
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg, log, version); err != nil {
		log.Error("fatal", "err", err)
		return 1
	}
	return 0
}

// run serves until ctx is cancelled (SIGINT/SIGTERM in main).
func run(ctx context.Context, cfg config.Config, log *slog.Logger, version string) error {
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

	if cfg.App.Mode == app.ModeLocal && !config.LoopbackOnly(cfg.Addr) && len(cfg.App.LocalTrustedPeers) == 0 {
		log.Warn("local mode listens beyond loopback but LOCAL_TRUSTED_PEERS is empty: only clients on this machine get keyless access",
			"addr", cfg.Addr)
	}
	if cfg.App.Mode == app.ModeCloud && !cfg.CookieSecure {
		log.Warn("cloud mode with COOKIE_SECURE=false: session cookies are sent over plain HTTP")
	}

	handler := httpapi.New(svc, log, httpapi.Config{
		CookieSecure: cfg.CookieSecure, TrustProxy: cfg.TrustProxy, OpenAPISpec: api.OpenAPI, Version: version,
		MCP: mcpserver.HTTPHandler(svc, version),
	})
	// No WriteTimeout on purpose: long-polls (/items/{id}/wait), MCP streams
	// and large export/media downloads legitimately take minutes. Uploads get
	// ReadTimeout (15 min for MAX_UPLOAD_MB files on slow links); slowloris on
	// headers is cut by ReadHeaderTimeout.
	srv := &http.Server{Addr: cfg.Addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 15 * time.Minute, IdleTimeout: 120 * time.Second}

	// Bind before starting workers: a failed start (port in use) must not
	// leave workers running against a database that is about to be closed.
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	var dln net.Listener
	if cfg.DashboardAddr != "" {
		if dln, err = net.Listen("tcp", cfg.DashboardAddr); err != nil {
			ln.Close()
			return err
		}
	}
	workerCtx, stopWorkers := context.WithCancel(ctx)
	workersDone := make(chan struct{})
	go func() { svc.RunBackground(workerCtx); close(workersDone) }()
	defer func() { stopWorkers(); <-workersDone }() // before database.Close (deferred earlier)

	errc := make(chan error, 2)
	go func() {
		log.Info("listening", "addr", ln.Addr().String(), "db", cfg.DBPath) // mode, version: base attrs
		errc <- srv.Serve(ln)
	}()
	servers := []*http.Server{srv}
	if dln != nil {
		// Same path as the staqly-web container: the dashboard reaches the API
		// over its listener, so the API's local-mode checks are unchanged.
		site, _ := fs.Sub(api.Web, "web")
		dash := &http.Server{Handler: web.Handler(site, web.DialURL(ln.Addr().String()), log),
			ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Minute, IdleTimeout: 120 * time.Second}
		servers = append(servers, dash)
		go func() {
			log.Info("dashboard listening", "addr", dln.Addr().String(), "exposed", cfg.App.DashboardExposed)
			errc <- dash.Serve(dln)
		}()
	}
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
	// In-flight work finishes; leased/pending work resumes on next start.
	for _, s := range servers[1:] {
		_ = s.Shutdown(shutCtx)
	}
	return srv.Shutdown(shutCtx)
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
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 2
	}
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
//	server recovery-key [workspace_id]      print a new recovery key (default: local workspace),
//	                                        signing out every browser session of that workspace
//	server set-plan <workspace_id> <plan>   free | indie | team (no payments yet)
//	server connect-link [workspace_id]      one-time sign-in link for a browser (5 min)
func admin(cmd string, args []string) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 2
	}
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
		key, revoked, err := svc.ResetRecoveryKey(ctx, ws)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(key)
		// stderr: stdout stays just the key for scripts.
		fmt.Fprintf(os.Stderr, "Revoked %d browser session(s) of this workspace.\n", revoked)
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
