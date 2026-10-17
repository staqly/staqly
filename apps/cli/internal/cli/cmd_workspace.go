package cli

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

type WorkspaceInfo struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Plan              string `json:"plan"`
	RecoveryRotatedAt string `json:"recovery_rotated_at"`
	CreatedAt         string `json:"created_at"`
}

type WorkspaceExport struct {
	ID          string  `json:"id"`
	Status      string  `json:"status"`
	SizeBytes   int64   `json:"size_bytes"`
	Error       string  `json:"error"`
	CreatedAt   string  `json:"created_at"`
	StartedAt   *string `json:"started_at"`
	FinishedAt  *string `json:"finished_at"`
	ExpiresAt   string  `json:"expires_at"`
	DownloadURL string  `json:"download_url"`
}

type BrowserSession struct {
	ID         string `json:"id"`
	UserAgent  string `json:"user_agent"`
	CreatedAt  string `json:"created_at"`
	LastSeenAt string `json:"last_seen_at"`
	ExpiresAt  string `json:"expires_at"`
	Current    bool   `json:"current"`
}

type PlanInfo struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	PriceUSDMonth int              `json:"price_usd_month"`
	Limits        map[string]int64 `json:"limits"`
	Features      []string         `json:"features"`
}

// ---- workspace ----

func workspaceCommand() *Command {
	return &Command{
		Name: "workspace", Aliases: []string{"ws"},
		Short: "Show, rename and export the workspace; show usage",
		Subs: []*Command{
			leaf("get", "", "Show the workspace", 0, 0, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var ws WorkspaceInfo
					raw, err := cl.Get(c, "/v1/workspace", nil, &ws)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					c.kv("ID", ws.ID, "Name", ws.Name, "Plan", ws.Plan, "Created", fmtTime(ws.CreatedAt),
						"Recovery key rotated", fmtTime(ws.RecoveryRotatedAt))
					return nil
				}
			}, "staqly workspace get", "staqly workspace get --json"),

			leaf("rename", "<name>", "Rename the workspace (admin)", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var ws WorkspaceInfo
					raw, err := cl.Patch(c, "/v1/workspace", map[string]any{"name": args[0]}, &ws)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					fmt.Fprintf(c.Out, "Renamed workspace to %s (%s)\n", c.paint(bold, ws.Name), ws.ID)
					return nil
				}
			}, "staqly workspace rename \"Acme content\""),

			leaf("usage", "", "Show the plan, usage against its limits and the trial end", 0, 0, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var u usageResponse
					raw, err := cl.Get(c, "/v1/workspace/usage", nil, &u)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					printUsage(c, u)
					return nil
				}
			}, "staqly workspace usage", "staqly workspace usage --json | jq .usage"),

			workspaceExportCommand(),
		},
	}
}

type usageResponse struct {
	Usage
	PlanExpiresAt *string          `json:"plan_expires_at"`
	Suspended     bool             `json:"suspended"`
	ItemsByStatus map[string]int64 `json:"items_by_status"`
	Deliveries    struct {
		Queued, Retrying, Failed int64
	} `json:"webhook_deliveries"`
}

func printUsage(c *Ctx, u usageResponse) {
	st := ""
	if u.Suspended {
		st = c.paint(red, "suspended (read-only until paid or switched to Free: staqly plans free)")
	}
	c.kv("Plan", fmt.Sprintf("%s (%s)", u.Plan.Name, u.Plan.ID), "Period", u.Period,
		"Plan ends", fmtOptTime(u.PlanExpiresAt), "Status", st)
	fmt.Fprintln(c.Out)
	t := c.table("USAGE", "USED", "LIMIT")
	keys := make([]string, 0, len(u.Usage.Usage))
	for k := range u.Usage.Usage {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ls := "-"
		if lim, ok := u.Plan.Limits[k]; ok {
			ls = fmtLimit(k, lim)
		}
		t.row(k, fmtAmount(k, u.Usage.Usage[k]), ls)
	}
	t.flush("")
	if len(u.ItemsByStatus) > 0 {
		fmt.Fprintln(c.Out)
		t = c.table("ITEMS THIS PERIOD", "COUNT")
		statuses := make([]string, 0, len(u.ItemsByStatus))
		for k := range u.ItemsByStatus {
			statuses = append(statuses, k)
		}
		sort.Strings(statuses)
		for _, k := range statuses {
			t.row(k, fmt.Sprint(u.ItemsByStatus[k]))
		}
		t.flush("")
	}
	fmt.Fprintln(c.Out)
	c.kv("Webhook deliveries", fmt.Sprintf("%d queued, %d retrying, %d failed",
		u.Deliveries.Queued, u.Deliveries.Retrying, u.Deliveries.Failed))
}

func workspaceExportCommand() *Command {
	start := leaf("start", "", "Start a full workspace export (admin); --wait/--output to fetch the ZIP", 0, 0, func(fs *flag.FlagSet) runFunc {
		wait := fs.Bool("wait", false, "wait until the export is ready")
		output := fs.String("output", "", "download the finished ZIP to this `file` (implies --wait)")
		fs.StringVar(output, "o", "", "shorthand for --output")
		timeout := fs.Duration("timeout", 30*time.Minute, "how long --wait waits")
		interval := fs.Duration("interval", 2*time.Second, "poll interval while waiting")
		return func(c *Ctx, _ []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var x WorkspaceExport
			raw, err := cl.Post(c, "/v1/workspace/export", nil, &x)
			if err != nil {
				return err
			}
			if !*wait && *output == "" {
				if c.JSON {
					return c.printRaw(raw)
				}
				fmt.Fprintf(c.Out, "Started export %s (%s)\n", c.paint(bold, x.ID), c.status(x.Status))
				fmt.Fprintf(c.Out, "Check it with: staqly workspace export status %s\n", x.ID)
				return nil
			}
			if !c.JSON {
				fmt.Fprintf(c.Stderr, "Started export %s; waiting for it to finish\n", x.ID)
			}
			x, raw, err = waitForExport(c, cl, x, raw, *timeout, *interval)
			if err != nil {
				return err
			}
			if *output != "" {
				return download(c, cl, "/v1/workspace/exports/"+url.PathEscape(x.ID)+"/download", nil, *output, x.ID+".zip", true)
			}
			if c.JSON {
				return c.printRaw(raw)
			}
			printExport(c, x)
			return nil
		}
	}, "staqly workspace export start", "staqly workspace export start --output staqly-export.zip",
		"staqly workspace export start --wait --json")

	status := leaf("status", "<export-id>", "Show an export job's status (admin)", 1, 1, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, args []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var x WorkspaceExport
			raw, err := cl.Get(c, "/v1/workspace/exports/"+url.PathEscape(args[0]), nil, &x)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			printExport(c, x)
			return nil
		}
	}, "staqly workspace export status exp_oalirbz67y4m4cqxq5dw")

	dl := leaf("download", "<export-id>", "Download a finished export ZIP (admin)", 1, 1, func(fs *flag.FlagSet) runFunc {
		output := fs.String("output", "", "write to this `file` ('-' = stdout; default: stdout when piped, else the server's file name)")
		fs.StringVar(output, "o", "", "shorthand for --output")
		return func(c *Ctx, args []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			return download(c, cl, "/v1/workspace/exports/"+url.PathEscape(args[0])+"/download", nil, *output, args[0]+".zip", true)
		}
	}, "staqly workspace export download exp_oalirbz67y4m4cqxq5dw", "staqly workspace export download exp_oalirbz67y4m4cqxq5dw -o backup.zip")

	cmd := &Command{Name: "export", Short: "Export all workspace data as a ZIP (start, status, download)", Subs: []*Command{start, status, dl}}
	cmd.Long = `Export all workspace data as a ZIP (collections, items with versions,
events, media files, webhooks with secrets redacted, watchers).
Only one export may run at a time; finished archives are deleted after 24 hours.`
	return cmd
}

func waitForExport(c *Ctx, cl *Client, x WorkspaceExport, raw json.RawMessage, timeout, interval time.Duration) (WorkspaceExport, json.RawMessage, error) {
	interval = max(interval, 500*time.Millisecond)
	deadline := c.Env.now().Add(timeout)
	for {
		switch x.Status {
		case "ready":
			return x, raw, nil
		case "failed", "expired":
			return x, raw, &exitErr{code: ExitError, msg: fmt.Sprintf("export %s %s: %s", x.ID, x.Status, orDash(x.Error))}
		}
		if !c.Env.now().Before(deadline) {
			return x, raw, &exitErr{code: ExitTimeout, msg: fmt.Sprintf("export %s is still %s after %s; check it with: staqly workspace export status %s", x.ID, x.Status, timeout, x.ID)}
		}
		if err := c.Env.sleep(c, interval); err != nil {
			return x, raw, err
		}
		var next WorkspaceExport
		r, err := cl.Get(c, "/v1/workspace/exports/"+url.PathEscape(x.ID), nil, &next)
		if err != nil {
			if isTransient(err) {
				continue
			}
			return x, raw, err
		}
		x, raw = next, r
	}
}

func printExport(c *Ctx, x WorkspaceExport) {
	size := ""
	if x.SizeBytes > 0 {
		size = humanBytes(x.SizeBytes)
	}
	c.kv("ID", x.ID, "Status", c.status(x.Status), "Size", size, "Error", x.Error, "Created", fmtTime(x.CreatedAt),
		"Finished", fmtOptTime(x.FinishedAt), "Expires", fmtTime(x.ExpiresAt))
	if x.Status == "ready" {
		fmt.Fprintf(c.Out, "\nDownload it with: staqly workspace export download %s\n", x.ID)
	}
}

// ---- plans ----

func plansCommand() *Command {
	return &Command{
		Name: "plans", Aliases: []string{"plan"},
		Short: "List cloud plans and switch the workspace to Free",
		Subs: []*Command{
			leaf("list", "", "List cloud plans and their limits", 0, 0, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var page struct{ Data []PlanInfo }
					raw, err := cl.Get(c, "/v1/plans", nil, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "NAME", "USD/MONTH", "ITEMS/MONTH", "DECISIONS/MONTH", "STORAGE", "WEBHOOKS", "API KEYS", "WATCHERS", "CHANNELS")
					lim := func(p PlanInfo, k string) string {
						v, ok := p.Limits[k]
						if !ok {
							return "-"
						}
						return fmtLimit(k, v)
					}
					for _, p := range page.Data {
						t.row(p.ID, p.Name, fmt.Sprint(p.PriceUSDMonth), lim(p, "items_per_month"), lim(p, "decisions_per_month"),
							lim(p, "storage_bytes"), lim(p, "webhooks"), lim(p, "api_keys"), lim(p, "watchers"),
							lim(p, "notification_channels"))
					}
					t.flush("No plans.")
					return nil
				}
			}, "staqly plans list", "staqly plans list --json"),

			leaf("free", "", "Switch the workspace to the Free plan (admin, cloud mode)", 0, 0, func(fs *flag.FlagSet) runFunc {
				yes := fs.Bool("yes", false, "do not ask for confirmation")
				return func(c *Ctx, _ []string) error {
					if !*yes {
						if !c.Env.StdinTTY {
							return usageErrf("refusing to change the plan without --yes (stdin is not a terminal)")
						}
						fmt.Fprint(c.Stderr, "Switch this workspace to the Free plan (lower limits apply right away)? [y/N] ")
						line, _ := bufio.NewReader(c.Env.Stdin).ReadString('\n')
						if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
							return &exitErr{code: ExitError, msg: "not confirmed; the plan was not changed"}
						}
					}
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var u usageResponse
					raw, err := cl.Post(c, "/v1/workspace/plan", map[string]any{"plan": "free"}, &u)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					fmt.Fprintf(c.Out, "Switched to the %s plan.\n\n", u.Plan.Name)
					printUsage(c, u)
					return nil
				}
			}, "staqly plans free", "staqly plan free --yes"),
		},
	}
}

// ---- sessions ----

func sessionsCommand() *Command {
	return &Command{
		Name: "sessions", Aliases: []string{"session"},
		Short: "List and revoke signed-in browser sessions (admin)",
		Subs: []*Command{
			leaf("list", "", "List active browser sessions", 0, 0, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var page struct{ Data []BrowserSession }
					raw, err := cl.Get(c, "/v1/sessions", nil, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "BROWSER", "CREATED", "LAST SEEN", "EXPIRES")
					for _, s := range page.Data {
						t.row(s.ID, truncate(s.UserAgent, 50), fmtTime(s.CreatedAt), fmtTime(s.LastSeenAt), fmtTime(s.ExpiresAt))
					}
					t.flush("No active browser sessions.")
					return nil
				}
			}, "staqly sessions list"),

			leaf("revoke", "<id>", "Sign a browser session out", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					if err := cl.Delete(c, "/v1/sessions/"+url.PathEscape(args[0]), nil); err != nil {
						return err
					}
					if c.JSON {
						return c.printJSON(map[string]any{"revoked": true, "id": args[0]})
					}
					fmt.Fprintf(c.Out, "Revoked session %s\n", args[0])
					return nil
				}
			}, "staqly sessions revoke ses_oalirbz67y4m4cqxq5dw"),
		},
	}
}

// ---- connect (device link) ----

func connectCommand() *Command {
	cmd := leaf("connect", "", "Create a one-time sign-in link for another browser (local mode)", 0, 0, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, _ []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var link struct {
				URL       string `json:"url"`
				ExpiresAt string `json:"expires_at"`
				Reachable bool   `json:"reachable"`
				Hint      string `json:"hint"`
			}
			raw, err := cl.Post(c, "/v1/device-links", nil, &link)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			fmt.Fprintf(c.Out, "%s\n", link.URL)
			fmt.Fprintf(c.Stderr, "One-time sign-in link, valid until %s. Anyone with it can sign in once; open it only on your own device.\n", fmtTime(link.ExpiresAt))
			if !link.Reachable {
				fmt.Fprintln(c.Stderr, c.paint(yellow, "This link points at localhost, so other devices cannot open it. "+link.Hint))
			}
			return nil
		}
	}, "staqly connect", "staqly connect --json | jq -r .url")
	cmd.Long = `Create a one-time sign-in link for another browser (phone, tablet, second
computer). The link is valid for 5 minutes and works once.

The server only issues links to the dashboard or a local-mode principal, so
this works keyless against a local server; API keys get 403.`
	return cmd
}
