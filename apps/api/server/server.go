// Package server runs the REST API, the MCP Streamable HTTP endpoint, the
// webhook worker and (with DASHBOARD_ADDR) the dashboard in one process
// (SQLite has a single writer anyway). Used by cmd/server (the Docker image)
// and by `staqly serve` (apps/cli).
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"
	api "github.com/staqly/staqly/apps/api"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/blob"
	"github.com/staqly/staqly/apps/api/internal/config"
	"github.com/staqly/staqly/apps/api/internal/httpapi"
	"github.com/staqly/staqly/apps/api/internal/logctx"
	"github.com/staqly/staqly/apps/api/internal/mcpserver"
	"github.com/staqly/staqly/apps/api/internal/store"
	"github.com/staqly/staqly/apps/api/internal/web"
)

// Main runs the server, or one of its operator subcommands (backup, import-sqlite, storage migrate,
// workspaces, recovery-key, set-plan, connect-link, attach-workspace, token), and returns the exit code.
func Main(args []string, version string) int {
	if len(args) > 0 {
		switch args[0] {
		case "import-sqlite":
			return importSQLite(args[1:])
		case "backup":
			return backup(args[1:])
		case "storage":
			return storageCmd(args[1:])
		case "token":
			return tokenCmd(args[1:])
		case "recovery-key", "set-plan", "workspaces", "connect-link", "stats", "attach-workspace":
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
	if cfg.SentryDSN != "" {
		if err := sentry.Init(sentry.ClientOptions{Dsn: cfg.SentryDSN, Environment: cfg.SentryEnvironment,
			Release: "staqly@" + version}); err != nil {
			fmt.Fprintln(os.Stderr, "config: SENTRY_DSN:", err)
			return 2
		}
		defer sentry.Flush(2 * time.Second)
		log = slog.New(sentryHandler{Handler: log.Handler()})
	}
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg, log, version); err != nil {
		log.Error("fatal", "err", err)
		return 1
	}
	return 0
}

// shutdownTimeout bounds the HTTP drain on SIGTERM (compose gives the container 30 s).
var shutdownTimeout = 20 * time.Second

// run serves until ctx is cancelled (SIGINT/SIGTERM in main).
func run(ctx context.Context, cfg config.Config, log *slog.Logger, version string) error {
	database, err := store.OpenFor(ctx, cfg.DatabaseURL, cfg.DBPath)
	if err != nil {
		return err
	}
	defer database.Close()
	svc := app.New(database, log, cfg.App)
	// A bucket server started next to us (the dev stack's MaxIOFS) may take a few seconds to answer.
	for try := 1; ; try++ {
		err := svc.Cfg.Blob.Check(ctx, cfg.StorageCreateBucket)
		if err == nil {
			break
		}
		if try == 15 || ctx.Err() != nil {
			return fmt.Errorf("media storage (%s): %w", svc.Cfg.Blob.Name(), err)
		}
		log.Warn("media storage not ready, retrying", "driver", svc.Cfg.Blob.Name(), "err", err)
		time.Sleep(2 * time.Second)
	}
	log.Info("media storage", "driver", svc.Cfg.Blob.Name())
	if err := svc.MoveLegacyFiles(ctx); err != nil {
		return fmt.Errorf("media storage (%s): moving files to workspace folders: %w", svc.Cfg.Blob.Name(), err)
	}
	localToken := ""
	if cfg.App.Mode == app.ModeLocal {
		if err := svc.EnsureLocalWorkspace(ctx); err != nil {
			return err
		}
		if localToken, err = svc.EnsureLocalToken(); err != nil {
			return fmt.Errorf("access token: %w", err)
		}
		if n, err := svc.SignOutIfTokenChanged(ctx); err != nil {
			return fmt.Errorf("access token: %w", err)
		} else if n > 0 {
			log.Info("the access token changed: browsers signed out", "sessions", n)
		}
	} else if cfg.App.SetupToken == "" {
		log.Info("cloud mode: public signup is open (7-day Pro trial); set SETUP_TOKEN to restrict it")
	}
	if err := svc.EnsureDefaultCollections(ctx); err != nil {
		return err
	}

	if len(cfg.Obsolete) > 0 {
		log.Warn("these settings are no longer read: local mode needs its access token for every request, from any address",
			"settings", cfg.Obsolete)
	}
	if cfg.App.Mode == app.ModeCloud && !cfg.CookieSecure {
		log.Warn("cloud mode with COOKIE_SECURE=false: session cookies are sent over plain HTTP")
	}
	if cfg.App.Mode == app.ModeCloud && cfg.App.DevSignIn {
		// Anyone can read anyone's email code at /v1/auth/email/dev-code: never on a server people use.
		log.Warn("AUTH_DEV_SIGNIN=true: sign-in codes are readable without authentication; development and tests only")
	}
	// The embedded dashboard proves its relayed requests with a per-process secret, so the API keeps each
	// browser's own address for its per-IP limits (else every browser shares the loopback peer and one of
	// them can use up sign-up, recovery and connect-link limits for all).
	relay := ""
	if cfg.DashboardAddr != "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		relay = hex.EncodeToString(b)
	}
	handler := httpapi.New(svc, log, httpapi.Config{
		CookieSecure: cfg.CookieSecure, TrustProxy: cfg.TrustProxy, RelayToken: relay, OpenAPISpec: api.OpenAPI, Version: version,
		MCP: mcpserver.HTTPHandler(svc, version), CORSOrigins: cfg.CORSOrigins,
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
		// Same path as the standalone dashboard (cmd/dashboard): it reaches the API
		// over its listener, so the API's local-mode checks are unchanged.
		site, _ := fs.Sub(api.Web, "web")
		dash := &http.Server{Handler: web.Handler(site, web.DialURL(ln.Addr().String()), log, cfg.TrustProxy, relay),
			ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Minute, IdleTimeout: 120 * time.Second}
		servers = append(servers, dash)
		go func() {
			log.Info("dashboard listening", "addr", dln.Addr().String(), "exposed", cfg.App.DashboardExposed)
			errc <- dash.Serve(dln)
		}()
	}
	if localToken != "" {
		printLocalAccess(cfg, localToken)
	}
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	// In-flight work finishes; leased/pending work resumes on next start. The dashboard first: it relays
	// to the API. Requests still running when the drain ends (slow downloads, stalled uploads) are cut, so
	// none outlives the database closed after this returns, and a SIGTERM never ends as a failure.
	for _, s := range append(servers[1:], srv) {
		if err := s.Shutdown(shutCtx); errors.Is(err, context.DeadlineExceeded) {
			log.Warn("shutdown: closing connections still busy after the drain", "timeout", shutdownTimeout.String())
			_ = s.Close()
		} else if err != nil {
			return err
		}
	}
	return nil
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
	database, err := store.OpenFor(ctx, cfg.DatabaseURL, cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer database.Close()
	if err := store.Backup(ctx, database, fs.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, "backup failed:", err)
		return 1
	}
	// The copy holds webhook secrets and hashed credentials: owner-only, like scripts/backup.sh.
	if err := os.Chmod(fs.Arg(0), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "backup written, but could not make it owner-only:", err)
	}
	fmt.Println("database backed up to", fs.Arg(0))
	return 0
}

// importSQLite moves a SQLite database into the cloud PostgreSQL database (DATABASE_URL): every table
// in one transaction, row counts checked, into an empty database only. Stop the old server first.
func importSQLite(args []string) int {
	fs := flag.NewFlagSet("import-sqlite", flag.ExitOnError)
	uploads := fs.String("uploads", "", "the old install's uploads folder: its files are copied into this server's media storage")
	_ = fs.Parse(args)
	args = fs.Args()
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: server import-sqlite [--uploads DIR] <workspace.db>   (STAQLY_MODE=cloud, DATABASE_URL set, target empty)")
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 2
	}
	if cfg.DatabaseURL == "" {
		fmt.Fprintln(os.Stderr, "import-sqlite copies into PostgreSQL: set STAQLY_MODE=cloud and DATABASE_URL")
		return 2
	}
	ctx := context.Background()
	pg, err := store.OpenPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer pg.Close()
	report, err := store.ImportSQLite(ctx, pg, args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "import failed, nothing was written:", err)
		return 1
	}
	var total, tables int64
	for t, n := range report {
		if t != store.NULReplaced {
			total += n
			tables++
		}
	}
	fmt.Printf("imported %d rows from %d tables into PostgreSQL\n", total, tables)
	if n := report[store.NULReplaced]; n > 0 {
		fmt.Printf("%d text values held NUL characters, which PostgreSQL cannot store: replaced with U+FFFD\n", n)
	}
	if *uploads == "" {
		fmt.Println("media files were not copied: run `server storage migrate --from <old uploads folder>` (or pass --uploads)")
		return 0
	}
	return copyUploads(ctx, cfg, *uploads, false)
}

// storageCmd: `server storage migrate [--from DIR] [--dry-run]` copies media files from a folder on disk
// (default UPLOAD_DIR) into the configured storage (STORAGE_DRIVER=s3). Safe to run again: files already
// there are skipped.
func storageCmd(args []string) int {
	if len(args) == 0 || args[0] != "migrate" {
		fmt.Fprintln(os.Stderr, "usage: server storage migrate [--from DIR] [--dry-run]")
		return 2
	}
	fs := flag.NewFlagSet("storage migrate", flag.ExitOnError)
	from := fs.String("from", "", "folder with the media files (default UPLOAD_DIR)")
	dry := fs.Bool("dry-run", false, "only list what would be copied")
	_ = fs.Parse(args[1:])
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 2
	}
	dir := *from
	if dir == "" {
		dir = cfg.App.UploadDir
	}
	return copyUploads(context.Background(), cfg, dir, *dry)
}

func copyUploads(ctx context.Context, cfg config.Config, dir string, dry bool) int {
	dst := cfg.App.Blob
	if d, ok := dst.(*blob.Disk); ok && filepath.Clean(d.Root) == filepath.Clean(dir) {
		fmt.Fprintln(os.Stderr, "media storage is this folder already (STORAGE_DRIVER=disk): set STORAGE_DRIVER=s3 and its S3_* settings first")
		return 2
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		fmt.Fprintf(os.Stderr, "%s is not a folder\n", dir)
		return 2
	}
	if err := dst.Check(ctx, cfg.StorageCreateBucket); err != nil {
		fmt.Fprintln(os.Stderr, "media storage:", err)
		return 1
	}
	rep, err := blob.Copy(ctx, blob.NewDisk(dir), dst, dry, func(key string, _ int64) {
		if dry {
			fmt.Println("would copy", key)
		}
	})
	verb := "copied"
	if dry {
		verb = "would copy"
	}
	fmt.Printf("%s %d files (%d bytes) to %s storage; %d were there already\n", verb, rep.Copied, rep.Bytes, dst.Name(), rep.Skipped)
	if err != nil {
		fmt.Fprintln(os.Stderr, "stopped:", err, "(run it again to continue)")
		return 1
	}
	return 0
}

// admin implements operator commands. Whoever can run them already controls
// the data volume, so they need no further authentication:
//
//	server workspaces                       list workspaces and plans
//	server recovery-key [workspace_id]      print a new recovery key (default: local workspace),
//	                                        signing out every browser session of that workspace
//	server set-plan <workspace_id> <plan> [--until DATE]
//	                                        starter | plus | pro | business; without --until it has no end date (ends a
//	                                        trial or suspension), with it the plan is comped until then
//	server stats                            JSON overview: workspaces by plan, trials, signups, usage
//	server connect-link [workspace_id]      one-time sign-in link for a browser (5 min)
//	server attach-workspace <workspace_id> <email>
//	                                        give a workspace without an account (from before accounts, or
//	                                        imported with import-sqlite) to the person with that address:
//	                                        an owner invitation they accept by signing in as that address
func admin(cmd string, args []string) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 2
	}
	ctx := context.Background()
	database, err := store.OpenFor(ctx, cfg.DatabaseURL, cfg.DBPath)
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
			until := "-"
			if w.PlanExpiresAt.Valid {
				until = w.PlanExpiresAt.String // trial or paid period end
			}
			fmt.Printf("%s\t%s\t%s\t%s\t%s\n", w.ID, svc.ShownPlanID(w.Plan), until, w.CreatedAt, w.Name)
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
		fs := flag.NewFlagSet("set-plan", flag.ContinueOnError)
		until := fs.String("until", "", "comp the plan until this date (RFC 3339 or YYYY-MM-DD)")
		pos := []string{}
		for len(args) > 0 { // flags may come before or after the arguments
			if err := fs.Parse(args); err != nil {
				return 2
			}
			if fs.NArg() == 0 {
				break
			}
			pos, args = append(pos, fs.Arg(0)), fs.Args()[1:]
		}
		if len(pos) != 2 {
			fmt.Fprintln(os.Stderr, "usage: server set-plan <workspace_id> <starter|plus|pro|business> [--until YYYY-MM-DD]")
			return 2
		}
		if err := svc.SetPlan(ctx, pos[0], pos[1], *until); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if *until != "" {
			fmt.Printf("workspace %s is on the %s plan until %s\n", pos[0], pos[1], *until)
		} else {
			fmt.Printf("workspace %s is now on the %s plan\n", pos[0], pos[1])
		}
	case "attach-workspace":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "usage: server attach-workspace <workspace_id> <email>")
			return 2
		}
		inv, err := svc.AttachWorkspace(ctx, args[0], args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(inv.Link)
		// stderr: stdout stays just the link for scripts.
		emailed := "Send them this link"
		if inv.Emailed != nil && *inv.Emailed {
			emailed = "We emailed this link to " + inv.Email + "; you can also send it yourself"
		}
		fmt.Fprintf(os.Stderr, "%s. They sign in as %s (Google or an email code) and accept it: the workspace joins their "+
			"account, as its owner. Browser sessions and connect links without a person end then; API keys keep working "+
			"and belong to the owner. The link works once and expires on %s.\n", emailed, inv.Email, inv.ExpiresAt[:10])
	case "stats":
		st, err := svc.OperatorStats(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(st)
	}
	return 0
}

// TokenCommand is how people print the access token again; `staqly serve` sets its own. In a container
// it is `docker exec <container> server token` (staqly, the name the docs use).
var TokenCommand = "server token"

var stderr io.Writer = os.Stderr

func tokenCommand() string {
	if TokenCommand == "server token" {
		if _, err := os.Stat("/.dockerenv"); err == nil {
			return "docker exec staqly server token"
		}
	}
	return TokenCommand
}

// localDashboardURL: where this machine opens the dashboard ("" without one).
func localDashboardURL(cfg config.Config) string {
	if u := strings.TrimRight(cfg.App.PublicDashboardURL, "/"); u != "" {
		return u
	}
	if cfg.DashboardAddr == "" && os.Getenv("DASHBOARD_PORT") == "" {
		return ""
	}
	return "http://localhost:" + cfg.App.DashboardPort
}

// printLocalAccess shows the access token at every start, with a link that signs this browser in. Plain
// text on stderr, not a log line: log shippers never get it. The token rides in the URL fragment, which
// browsers never send to a server.
func printLocalAccess(cfg config.Config, token string) {
	fmt.Fprintln(stderr, "\nStaqly Community Edition (local mode): every request needs the access token.")
	if u := localDashboardURL(cfg); u != "" {
		fmt.Fprintf(stderr, "  Open the dashboard:  %s/#token=%s\n", u, token)
	}
	fmt.Fprintf(stderr, "  Access token:        %s\n", token)
	fmt.Fprintln(stderr, "  Agents, SDKs, curl and n8n send it as `Authorization: Bearer <token>` (or STAQLY_API_KEY).")
	fmt.Fprintf(stderr, "  Print it again with `%s`; `%s --rotate` makes a new one and signs every browser out.\n\n",
		tokenCommand(), tokenCommand())
}

// tokenCmd: `server token [--rotate]` prints local mode's access token (stdout: just the token, for scripts).
func tokenCmd(args []string) int {
	fs := flag.NewFlagSet("token", flag.ContinueOnError)
	rotate := fs.Bool("rotate", false, "make a new token: the old one stops working and every browser is signed out")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: server token [--rotate]")
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 2
	}
	if cfg.App.Mode != app.ModeLocal {
		fmt.Fprintln(os.Stderr, "the access token belongs to the Community Edition (local mode); cloud mode uses API keys (Settings, API keys)")
		return 2
	}
	ctx := context.Background()
	database, err := store.OpenFor(ctx, cfg.DatabaseURL, cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer database.Close()
	svc := app.New(database, slog.New(slog.DiscardHandler), cfg.App)
	if err := svc.EnsureLocalWorkspace(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var tok string
	if *rotate {
		p, err := svc.LocalPrincipal(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		p.Kind, p.ID, p.Name = app.PrincipalSystem, "cli", "Server CLI"
		var revoked int64
		if tok, revoked, err = svc.RotateLocalToken(ctx, p, ""); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "New access token. The old one stopped working; %d browser session(s) were signed out.\n", revoked)
	} else if tok, err = svc.EnsureLocalToken(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(tok)
	if u := localDashboardURL(cfg); u != "" {
		fmt.Fprintf(os.Stderr, "Sign a browser in: %s/#token=%s\n", u, tok)
	}
	return 0
}
