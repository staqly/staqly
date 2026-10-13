package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"runtime"
	"sort"
	"strings"
)

func rootCommand() *Command {
	return &Command{
		Name:  "staqly",
		Short: "staqly command line: push AI output for human review and decide from the terminal",
		Long: `staqly — command line client for staqly, the local-first review inbox for AI agent output.

Agents push items into collections; humans approve or reject them. Use
'staqly push … --wait' to block a script or CI job until a human decides.
Local mode needs no API key; cloud mode uses --key / STAQLY_API_KEY or a
stored profile (staqly login).`,
		Examples: []string{
			"staqly status",
			"staqly collections create \"Blog posts\"",
			"staqly push \"Blog posts\" --title \"Launch post\" --file post.md --timeout 2h",
			"staqly inbox",
			"staqly items approve itm_123 --feedback \"ship it\"",
			"staqly login --url https://staqly.example.com --key stk_… --profile work",
		},
		Setup: func(fs *flag.FlagSet) runFunc {
			version := fs.Bool("version", false, "print the version")
			return func(c *Ctx, args []string) error {
				if *version {
					return printVersion(c)
				}
				printHelp(c.Out, c.cmd, fs)
				return nil
			}
		},
		MaxArgs: 0,
		Subs: []*Command{
			serveCommand(),
			statusCommand(),
			pushCommand(),
			inboxCommand(),
			itemsCommand(),
			collectionsCommand(),
			eventsCommand(),
			webhooksCommand(),
			rulesCommand(),
			watchersCommand(),
			keysCommand(),
			loginCommand(),
			logoutCommand(),
			profilesCommand(),
			mcpCommand(),
			completionCommand(),
			versionCommand(),
		},
	}
}

func leaf(name, args, short string, minArgs, maxArgs int, setup func(fs *flag.FlagSet) runFunc, examples ...string) *Command {
	return &Command{Name: name, Args: args, Short: short, MinArgs: minArgs, MaxArgs: maxArgs, Setup: setup, Examples: examples}
}

// ---- version ----

func versionCommand() *Command {
	return leaf("version", "", "Print the CLI version", 0, 0, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, _ []string) error { return printVersion(c) }
	})
}

func printVersion(c *Ctx) error {
	b := c.Env.Build
	if c.JSON {
		return c.printJSON(map[string]string{"version": b.Version, "commit": b.Commit, "date": b.Date,
			"go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH})
	}
	fmt.Fprintf(c.Out, "staqly %s", b.Version)
	if b.Commit != "" {
		fmt.Fprintf(c.Out, " (%s", b.Commit)
		if b.Date != "" {
			fmt.Fprintf(c.Out, ", %s", b.Date)
		}
		fmt.Fprint(c.Out, ")")
	}
	fmt.Fprintf(c.Out, " %s/%s\n", runtime.GOOS, runtime.GOARCH)
	return nil
}

// ---- status ----

func statusCommand() *Command {
	cmd := leaf("status", "", "Show server health, mode, workspace, plan and usage", 0, 0, func(fs *flag.FlagSet) runFunc {
		return runStatus
	}, "staqly status", "staqly status --json", "staqly status --profile work")
	cmd.Long = `Show server health, mode, workspace, plan and usage.

Calls /health, /v1/session, /v1/workspace/usage and counts pending items.
Exits 1 when the server is unreachable or the credentials are rejected.`
	return cmd
}

func runStatus(c *Ctx, _ []string) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	var health struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	healthRaw, err := cl.Get(c, "/health", nil, &health)
	if err != nil {
		return err
	}
	var sess SessionInfo
	sessRaw, err := cl.Get(c, "/v1/session", nil, &sess)
	if err != nil {
		return err
	}
	var usage Usage
	usageRaw, err := cl.Get(c, "/v1/workspace/usage", nil, &usage)
	if err != nil {
		return err
	}
	var pending ItemPage
	if _, err := cl.Get(c, "/v1/items", url.Values{"status": {"pending"}, "limit": {"200"}}, &pending); err != nil {
		return err
	}
	pendingCount := fmt.Sprint(len(pending.Data))
	if pending.NextCursor != "" {
		pendingCount += "+"
	}
	if c.JSON {
		return c.printJSON(map[string]any{
			"url": cl.BaseURL, "profile": c.profile, "authenticated_with_key": cl.Key != "",
			"health": json.RawMessage(healthRaw), "session": json.RawMessage(sessRaw),
			"usage": json.RawMessage(usageRaw), "pending": len(pending.Data), "pending_more": pending.NextCursor != "",
		})
	}
	key := "none (keyless)"
	if cl.Key != "" {
		key = maskKey(cl.Key)
	}
	c.kv(
		"Server", cl.BaseURL+"  "+c.status(health.Status)+"  "+c.paint(dim, health.Version),
		"Profile", c.profile,
		"Mode", sess.Mode,
		"Workspace", fmt.Sprintf("%s (%s)", sess.Workspace.Name, sess.Workspace.ID),
		"Principal", fmt.Sprintf("%s %s", sess.Principal.Type, sess.Principal.Name),
		"API key", key,
		"Plan", fmt.Sprintf("%s (%s)", usage.Plan.Name, usage.Period),
		"Pending items", pendingCount,
	)
	if sess.Principal.Type == "api_key" && len(sess.Principal.Scopes) > 0 {
		c.kv("Scopes", strings.Join(sess.Principal.Scopes, " "))
	}
	fmt.Fprintln(c.Out)
	t := c.table("USAGE", "USED", "LIMIT")
	keys := make([]string, 0, len(usage.Usage))
	for k := range usage.Usage {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		used := usage.Usage[k]
		lim, ok := usage.Plan.Limits[k]
		ls := "-"
		if ok {
			ls = fmtLimit(k, lim)
		}
		t.row(k, fmtAmount(k, used), ls)
	}
	t.flush("")
	return nil
}

func fmtLimit(k string, v int64) string {
	if v < 0 {
		return "unlimited"
	}
	return fmtAmount(k, v)
}

func fmtAmount(k string, v int64) string {
	if strings.HasSuffix(k, "_bytes") {
		return humanBytes(v)
	}
	return fmt.Sprint(v)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
