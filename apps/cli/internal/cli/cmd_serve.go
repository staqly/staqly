package cli

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/staqly/staqly/apps/api/server"
)

// ---- serve ----

func serveCommand() *Command {
	cmd := leaf("serve", "[token|connect-link|backup|storage ...]",
		"Run Staqly Community Edition on this computer: API, MCP and dashboard in one process", 0, -1, func(fs *flag.FlagSet) runFunc {
			data := fs.String("data", "", "data directory (default ~/.local/share/staqly, or $STAQLY_DATA_DIR)")
			port := fs.Int("port", 3333, "API and MCP port (always on 127.0.0.1)")
			dashboardPort := fs.Int("dashboard-port", 8080, "dashboard port")
			adminPort := fs.Int("admin-port", 0, "also serve the admin panel on this port (0 = off), on the dashboard's address")
			expose := fs.Bool("expose", false, "serve the dashboard on all interfaces (phones, tablets); browsers then sign in once with a link")
			return func(c *Ctx, args []string) error {
				dir := *data
				if dir == "" {
					var err error
					if dir, err = defaultDataDir(c); err != nil {
						return err
					}
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
				if *adminPort > 0 {
					env["ADMIN_ADDR"] = net.JoinHostPort(webHost, fmt.Sprint(*adminPort))
				}
				for k, v := range env {
					if err := os.Setenv(k, v); err != nil {
						return err
					}
				}
				// The server prints this when it shows the access token: the command that reaches the same data.
				server.TokenCommand = serveSubcommand(*data, *dashboardPort, "token")
				if len(args) == 0 {
					fmt.Fprintf(c.Stderr, "staqly %s\n  dashboard  http://localhost:%d\n  API + MCP  http://localhost:%d (MCP at /mcp)\n  data       %s\n\n",
						c.Env.Build.Version, *dashboardPort, *port, dir)
					if *expose {
						fmt.Fprintf(c.Stderr, "  The dashboard is open to your network: sign a browser in with `%s`.\n", serveSubcommand(*data, *dashboardPort, "connect-link"))
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
		"staqly serve token",
		"staqly serve token --rotate",
		"staqly serve connect-link",
		"staqly serve backup ~/staqly-backup.db")
	cmd.Long = `Run Staqly Community Edition, the self-hosted version, on this computer without Docker: the REST API and MCP endpoint on
127.0.0.1, the background workers, and the dashboard. Data (SQLite database +
uploads) lives in --data.

Every request needs the access token, made on first start in the data
folder (access_token, owner-only) and printed at every start with a link
that signs the browser in. Agents, SDKs and curl send it as
"Authorization: Bearer <token>"; the staqly CLI on this computer reads it by
itself. ` + "`staqly serve token`" + ` prints it again, --rotate makes a new one and signs
every browser out.

With arguments, runs a server operator command against the same data
instead: token [--rotate], connect-link, backup <file>, storage migrate.
Flags serve does not know go to the operator command.`
	// operator flags (e.g. storage migrate --dry-run) belong to the operator command, not to serve
	cmd.PassUnknownFlags = true
	return cmd
}

// serveSubcommand is the `staqly serve <sub>` that matches these flags (connect-link, token), so it reaches
// the same data and prints the right port.
func serveSubcommand(data string, dashboardPort int, sub string) string {
	cmd := "staqly serve"
	if data != "" {
		cmd += " --data " + shellQuote(data)
	}
	if dashboardPort != 8080 {
		cmd += " --dashboard-port " + fmt.Sprint(dashboardPort)
	}
	return cmd + " " + sub
}

func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-~") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// connectLinkCommand is the `staqly serve connect-link` that matches these flags.
func connectLinkCommand(data string, dashboardPort int) string {
	return serveSubcommand(data, dashboardPort, "connect-link")
}
