package cli

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/staqly/staqly/apps/api/server"
)

// ---- serve ----

func serveCommand() *Command {
	cmd := leaf("serve", "[backup|workspaces|recovery-key|set-plan|stats|connect-link ...]",
		"Run staqly on this computer: API, MCP and dashboard in one process", 0, -1, func(fs *flag.FlagSet) runFunc {
			data := fs.String("data", "", "data directory (default ~/.local/share/staqly, or $STAQLY_DATA_DIR)")
			port := fs.Int("port", 3333, "API and MCP port (always on 127.0.0.1)")
			dashboardPort := fs.Int("dashboard-port", 8080, "dashboard port")
			expose := fs.Bool("expose", false, "serve the dashboard on all interfaces (phones, tablets); browsers then sign in once with a link")
			return func(c *Ctx, args []string) error {
				dir := firstNonEmpty(*data, c.Env.Getenv("STAQLY_DATA_DIR"))
				if dir == "" {
					home, err := os.UserHomeDir()
					if err != nil {
						return err
					}
					dir = filepath.Join(home, ".local", "share", "staqly")
				}
				if err := os.MkdirAll(dir, 0o700); err != nil {
					return err
				}
				webHost := "127.0.0.1"
				if *expose {
					webHost = ""
				}
				// Explicit flags, but environment variables the server reads
				// (STAQLY_MODE, LOG_LEVEL, PUBLIC_DASHBOARD_URL, ...) still apply.
				env := map[string]string{
					"DATA_DIR":       dir,
					"ADDR":           net.JoinHostPort("127.0.0.1", fmt.Sprint(*port)),
					"DASHBOARD_ADDR": net.JoinHostPort(webHost, fmt.Sprint(*dashboardPort)),
					"PUBLIC_API_URL": firstNonEmpty(c.Env.Getenv("PUBLIC_API_URL"), fmt.Sprintf("http://localhost:%d", *port)),
					"LOG_FORMAT":     firstNonEmpty(c.Env.Getenv("LOG_FORMAT"), "text"),
				}
				for k, v := range env {
					if err := os.Setenv(k, v); err != nil {
						return err
					}
				}
				if len(args) == 0 {
					fmt.Fprintf(c.Stderr, "staqly %s\n  dashboard  http://localhost:%d\n  API + MCP  http://localhost:%d (MCP at /mcp)\n  data       %s\n\n",
						c.Env.Build.Version, *dashboardPort, *port, dir)
					if *expose {
						fmt.Fprintf(c.Stderr, "  The dashboard is open to your network: sign a browser in with `%s`.\n", connectLinkCommand(*data, *dashboardPort))
					}
				}
				if code := server.Main(args, c.Env.Build.Version); code != 0 {
					return exitWith(code)
				}
				return nil
			}
		},
		"staqly serve",
		"staqly serve --expose",
		"staqly serve --data ~/staqly-data --port 4333 --dashboard-port 9090",
		"staqly serve connect-link",
		"staqly serve backup ~/staqly-backup.db")
	cmd.Long = `Run staqly on this computer without Docker: the REST API and MCP endpoint on
127.0.0.1 (no keys for agents on this machine), the background workers, and
the dashboard. Data (SQLite database + uploads) lives in --data.

With arguments, runs a server operator command against the same data
instead: backup <file>, workspaces, recovery-key, set-plan, stats, connect-link.`
	return cmd
}

// connectLinkCommand is the `staqly serve connect-link` that matches these flags, so the link has the right port and data.
func connectLinkCommand(data string, dashboardPort int) string {
	cmd := "staqly serve"
	if data != "" {
		cmd += " --data " + shellQuote(data)
	}
	if dashboardPort != 8080 {
		cmd += " --dashboard-port " + fmt.Sprint(dashboardPort)
	}
	return cmd + " connect-link"
}

func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-~") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
