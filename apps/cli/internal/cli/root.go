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
		Short: "Staqly command line: write, schedule and publish posts from the terminal",
		Long: `staqly — command line client for staqly, the local-first scheduler for posts
your agents write.

Agents (or you) put posts into collections; staqly publishes them to connected
channels at their time, on a schedule, or right away. Every request needs a
credential: the Community Edition's access token (the CLI reads it from
` + "`staqly serve`" + `'s data folder by itself on this computer; for Docker pass
` + "`docker exec staqly server token`" + `) or, in the cloud, an API key. Pass it with
--key / STAQLY_API_KEY or store it in a profile (staqly login).`,
		Examples: []string{
			"staqly status",
			"staqly channels connect custom --name Announcements   # your own automation publishes",
			"staqly posts create \"Blog posts\" --title \"Launch day\" --content @post.md --channel Announcements --publish-at 2026-11-03T09:00:00Z",
			"staqly posts publish pst_123 --wait",
			"staqly schedule",
			"staqly login --url https://staqly.example.com --key stk_... --profile work",
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
			serviceCommand(),
			statusCommand(),
			postsCommand(),
			scheduleCommand(),
			collectionsCommand(),
			channelsCommand(),
			schedulesCommand(),
			jobsCommand(),
			workCommand(),
			mediaCommand(),
			eventsCommand(),
			webhooksCommand(),
			alertsCommand(),
			workspaceCommand(),
			plansCommand(),
			billingCommand(),
			keysCommand(),
			sessionsCommand(),
			connectCommand(),
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

Calls /health, /v1/session, /v1/workspace/usage and counts scheduled posts.
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
	var scheduled PostPage
	if _, err := cl.Get(c, "/v1/posts", url.Values{"status": {"scheduled"}, "limit": {"200"}}, &scheduled); err != nil {
		return err
	}
	scheduledCount := fmt.Sprint(len(scheduled.Data))
	if scheduled.NextCursor != "" {
		scheduledCount += "+"
	}
	if c.JSON {
		return c.printJSON(map[string]any{
			"url": cl.BaseURL, "profile": c.profile, "authenticated_with_key": cl.Key != "",
			"health": json.RawMessage(healthRaw), "session": json.RawMessage(sessRaw),
			"usage": json.RawMessage(usageRaw), "scheduled": len(scheduled.Data), "scheduled_more": scheduled.NextCursor != "",
		})
	}
	key := "none"
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
		"Scheduled posts", scheduledCount,
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
