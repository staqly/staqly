package cli

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strings"

	"golang.org/x/term"
)

// ---- keys ----

func keysCommand() *Command {
	return &Command{
		Name: "keys", Aliases: []string{"key", "api-keys"},
		Short: "Manage scoped API keys (cloud mode, admin)",
		Subs: []*Command{
			leaf("list", "", "List API keys", 0, 0, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var page struct {
						Data            []APIKey `json:"data"`
						AvailableScopes []string `json:"available_scopes"`
					}
					raw, err := cl.Get(c, "/v1/api-keys", nil, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "NAME", "PREFIX", "SCOPES", "LAST USED", "EXPIRES", "STATUS")
					for _, k := range page.Data {
						st := "active"
						if k.RevokedAt != nil {
							st = "revoked"
						}
						t.row(k.ID, truncate(k.Name, 24), k.Prefix, strings.Join(k.Scopes, ","), fmtTime(strPtr(k.LastUsedAt)),
							fmtTime(strPtr(k.ExpiresAt)), c.status(st))
					}
					t.flush("No API keys.")
					if len(page.AvailableScopes) > 0 {
						fmt.Fprintln(c.Stderr, c.paint(dim, "Scopes: "+strings.Join(page.AvailableScopes, " ")))
					}
					return nil
				}
			}, "staqly keys list"),

			leaf("create", "", "Create an API key (the secret is shown once)", 0, 0, func(fs *flag.FlagSet) runFunc {
				name := fs.String("name", "", "key name, e.g. \"CI deploy gate\" (required)")
				scopes := &multiFlag{comma: true}
				fs.Var(scopes, "scope", "`scopes`, comma-separated or repeated (required): collections:read collections:write items:read items:write items:review actions:execute webhooks:manage rules:manage watchers:manage admin")
				expires := fs.String("expires", "", "expiry, RFC3339 or YYYY-MM-DD")
				return func(c *Ctx, _ []string) error {
					if *name == "" || len(scopes.vals) == 0 {
						return usageErrf("--name and --scope are required")
					}
					cl, err := c.Client()
					if err != nil {
						return err
					}
					body := map[string]any{"name": *name, "scopes": scopes.vals}
					setIf(body, "expires_at", *expires)
					var k APIKey
					raw, err := cl.Post(c, "/v1/api-keys", body, &k)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					fmt.Fprintf(c.Out, "Created key %s (%s) with scopes %s\n", k.ID, k.Name, strings.Join(k.Scopes, " "))
					fmt.Fprintf(c.Out, "\n  %s\n\n", c.paint(bold, k.Key))
					fmt.Fprintln(c.Out, "Store it now: it is not shown again. Save it as a profile with:")
					fmt.Fprintf(c.Out, "  staqly login --url %s --key <key> --profile <name>\n", cl.BaseURL)
					return nil
				}
			}, "staqly keys create --name \"CI gate\" --scope items:read,items:write",
				"staqly keys create --name Reviewer --scope collections:read --scope items:read --scope items:review --expires 2027-01-01"),

			leaf("revoke", "<id>", "Revoke an API key", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					if err := cl.Delete(c, "/v1/api-keys/"+url.PathEscape(args[0]), nil); err != nil {
						return err
					}
					if c.JSON {
						return c.printJSON(map[string]any{"revoked": true, "id": args[0]})
					}
					fmt.Fprintf(c.Out, "Revoked key %s\n", args[0])
					return nil
				}
			}, "staqly keys revoke key_123"),
		},
	}
}

// ---- login / logout / profiles ----

func loginCommand() *Command {
	cmd := leaf("login", "", "Save a server URL and API key as a named profile", 0, 0, func(fs *flag.FlagSet) runFunc {
		noVerify := fs.Bool("no-verify", false, "save without contacting the server")
		return func(c *Ctx, _ []string) error {
			cfg, err := c.config()
			if err != nil {
				return err
			}
			name := firstNonEmpty(c.g.profile, c.Env.Getenv("STAQLY_PROFILE"), defaultProfile)
			prev := cfg.Profiles[name]
			if prev == nil {
				prev = &Profile{}
			}
			base := strings.TrimRight(firstNonEmpty(c.g.url, c.Env.Getenv("STAQLY_API_URL"), prev.URL, defaultURL), "/")
			if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
				return usageErrf("--url must start with http:// or https://")
			}
			key := c.g.key
			switch {
			case key == "-":
				line, err := bufio.NewReader(c.Env.Stdin).ReadString('\n')
				if err != nil && err != io.EOF {
					return err
				}
				key = strings.TrimSpace(line)
			case key == "" && c.Env.Getenv("STAQLY_API_KEY") != "":
				key = c.Env.Getenv("STAQLY_API_KEY")
			case key == "" && c.Env.StdinTTY:
				fmt.Fprint(c.Stderr, "API key (stk_…, leave empty for a local keyless server): ")
				b, err := term.ReadPassword(c.Env.StdinFd)
				fmt.Fprintln(c.Stderr)
				if err != nil {
					return err
				}
				key = strings.TrimSpace(string(b))
			}
			if key != "" && !strings.HasPrefix(key, "stk_") {
				fmt.Fprintln(c.Stderr, "warning: API keys normally start with stk_")
			}
			var sess SessionInfo
			if !*noVerify {
				cl := &Client{BaseURL: base, Key: key, HTTP: c.Env.HTTP, UserAgent: "staqly-cli/" + c.Env.Build.Version}
				if _, err := cl.Get(c, "/v1/session", nil, &sess); err != nil {
					return fmt.Errorf("%s\nNothing was saved (use --no-verify to save anyway)", friendlyError(err))
				}
			}
			cfg.Profiles[name] = &Profile{URL: base, Key: key}
			cfg.Current = name
			if err := cfg.save(c.cfgPath); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			if c.JSON {
				return c.printJSON(map[string]any{"profile": name, "url": base, "key": maskKey(key),
					"mode": sess.Mode, "workspace": sess.Workspace, "config": c.cfgPath})
			}
			if !*noVerify {
				fmt.Fprintf(c.Out, "Connected to %s (%s mode) — workspace %q as %s.\n", base, sess.Mode, sess.Workspace.Name, sess.Principal.Name)
			}
			fmt.Fprintf(c.Out, "Saved profile %q to %s (now current).\n", name, c.cfgPath)
			return nil
		}
	},
		"staqly login                                    # local server, keyless",
		"staqly login --url https://staqly.example.com --key stk_… --profile work",
		"echo \"$STAQLY_KEY\" | staqly login --url https://staqly.example.com --key - --profile ci")
	cmd.Long = `Save a server URL and API key as a named profile in
~/.config/staqly/config.json (mode 0600) and make it the current profile.

The key comes from --key, '--key -' (read stdin), STAQLY_API_KEY, or a
hidden prompt. The credentials are checked against /v1/session first.`
	return cmd
}

func logoutCommand() *Command {
	return leaf("logout", "", "Remove a stored profile (default: the current one)", 0, 0, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, _ []string) error {
			cfg, err := c.config()
			if err != nil {
				return err
			}
			name := c.profileName(cfg)
			if cfg.Profiles[name] == nil {
				return &exitErr{code: ExitError, msg: fmt.Sprintf("no profile %q in %s", name, c.cfgPath)}
			}
			delete(cfg.Profiles, name)
			if cfg.Current == name {
				cfg.Current = ""
			}
			if err := cfg.save(c.cfgPath); err != nil {
				return err
			}
			if c.JSON {
				return c.printJSON(map[string]any{"removed": name})
			}
			fmt.Fprintf(c.Out, "Removed profile %q.\n", name)
			return nil
		}
	}, "staqly logout", "staqly logout --profile work")
}

func profilesCommand() *Command {
	list := func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, _ []string) error {
			cfg, err := c.config()
			if err != nil {
				return err
			}
			cur := c.profileName(cfg)
			if c.JSON {
				out := []map[string]any{}
				for _, n := range cfg.names() {
					p := cfg.Profiles[n]
					out = append(out, map[string]any{"name": n, "url": p.URL, "key": maskKey(p.Key), "current": n == cur})
				}
				return c.printJSON(map[string]any{"config": c.cfgPath, "profiles": out})
			}
			t := c.table("", "NAME", "URL", "KEY")
			for _, n := range cfg.names() {
				p := cfg.Profiles[n]
				mark := " "
				if n == cur {
					mark = "*"
				}
				t.row(mark, n, p.URL, orDash(maskKey(p.Key)))
			}
			t.flush("No profiles yet. Add one with: staqly login --url URL [--key stk_…] [--profile name]")
			return nil
		}
	}
	return &Command{
		Name: "profiles", Aliases: []string{"profile"}, Short: "List stored profiles, or switch with `profiles use <name>`",
		Setup: list, Examples: []string{"staqly profiles", "staqly profiles use work"},
		Subs: []*Command{
			leaf("list", "", "List stored profiles", 0, 0, list),
			leaf("use", "<name>", "Make a profile the current one", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cfg, err := c.config()
					if err != nil {
						return err
					}
					if cfg.Profiles[args[0]] == nil {
						return &exitErr{code: ExitError, msg: fmt.Sprintf("no profile %q (see `staqly profiles`)", args[0])}
					}
					cfg.Current = args[0]
					if err := cfg.save(c.cfgPath); err != nil {
						return err
					}
					fmt.Fprintf(c.Out, "Now using profile %q (%s).\n", args[0], cfg.Profiles[args[0]].URL)
					return nil
				}
			}, "staqly profiles use work"),
		},
	}
}
