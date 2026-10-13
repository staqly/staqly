package cli

import (
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func watchersCommand() *Command {
	return &Command{
		Name: "watchers", Aliases: []string{"watcher", "watch"},
		Short: "Manage watchers (RSS feeds and page monitors that create items)",
		Subs: []*Command{
			leaf("list", "", "List watchers", 0, 0, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var page struct{ Data []Watcher }
					raw, err := cl.Get(c, "/v1/watchers", nil, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "NAME", "KIND", "STATUS", "EVERY", "ITEMS", "LAST RUN", "NEXT RUN", "ERROR")
					for _, w := range page.Data {
						t.row(w.ID, truncate(w.Name, 30), w.Kind, c.status(w.Status), w.Every, strconv.Itoa(w.ItemsCreated),
							fmtTime(strPtr(w.LastRunAt)), fmtTime(w.NextRunAt), truncate(orDash(w.LastError), 40))
					}
					t.flush("No watchers. Browse presets with: staqly watchers presets")
					return nil
				}
			}, "staqly watchers list"),

			leaf("presets", "", "List ready-made watcher presets", 0, 0, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var page struct{ Data []WatcherPreset }
					raw, err := cl.Get(c, "/v1/watcher-presets", nil, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "CATEGORY", "KIND", "EVERY", "PARAMS", "TITLE")
					for _, p := range page.Data {
						var params []string
						for _, pa := range p.Params {
							n := pa.Name
							if pa.Required {
								n += "*"
							}
							params = append(params, n)
						}
						t.row(p.ID, p.Category, p.Kind, p.DefaultEvery, orDash(strings.Join(params, ",")), truncate(p.Title, 50))
					}
					t.flush("No presets.")
					fmt.Fprintln(c.Stderr, c.paint(dim, "* required. Create one with: staqly watchers create --preset <id> --param name=value --collection <collection>"))
					return nil
				}
			}, "staqly watchers presets"),

			watchersCreateCommand(),

			leaf("run", "<id>", "Check a watcher's source now", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var res struct {
						Status      string `json:"status"`
						NewItems    int    `json:"new_items"`
						EntriesSeen int    `json:"entries_seen"`
						Baseline    bool   `json:"baseline"`
						Error       string `json:"error"`
					}
					raw, err := cl.Post(c, "/v1/watchers/"+url.PathEscape(args[0])+"/run", nil, &res)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					if res.Status == "error" {
						return &exitErr{code: ExitError, msg: "watcher run failed: " + res.Error}
					}
					fmt.Fprintf(c.Out, "Run %s: %d new items, %d entries seen", c.status(res.Status), res.NewItems, res.EntriesSeen)
					if res.Baseline {
						fmt.Fprint(c.Out, " (first run: existing entries recorded as baseline)")
					}
					fmt.Fprintln(c.Out)
					return nil
				}
			}, "staqly watchers run wch_123"),

			watcherStatusCommand("pause", "paused", "Pause a watcher"),
			watcherStatusCommand("resume", "active", "Resume a paused watcher"),

			leaf("delete", "<id>", "Delete a watcher", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					return simpleDelete(c, "/v1/watchers/"+url.PathEscape(args[0]), "watcher", args[0])
				}
			}, "staqly watchers delete wch_123"),
		},
	}
}

func watcherStatusCommand(name, status, short string) *Command {
	return leaf(name, "<id>", short, 1, 1, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, args []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var w Watcher
			raw, err := cl.Patch(c, "/v1/watchers/"+url.PathEscape(args[0]), map[string]any{"status": status}, &w)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			fmt.Fprintf(c.Out, "Watcher %s is %s\n", w.ID, c.status(w.Status))
			return nil
		}
	}, "staqly watchers "+name+" wch_123")
}

func watchersCreateCommand() *Command {
	cmd := leaf("create", "", "Create a watcher from a preset or from a feed/page URL", 0, 0, func(fs *flag.FlagSet) runFunc {
		preset := fs.String("preset", "", "preset ID (see `staqly watchers presets`)")
		var params multiFlag
		fs.Var(&params, "param", "preset parameter `name=value` (repeatable)")
		coll := fs.String("collection", "", "collection that receives the items (ID or name, required)")
		name := fs.String("name", "", "watcher name")
		every := fs.String("every", "", "check interval, e.g. 30m or 6h")
		kind := fs.String("kind", "", "rss or url_diff (without --preset)")
		u := fs.String("url", "", "feed or page URL (without --preset)")
		kw := &multiFlag{comma: true}
		fs.Var(kw, "keyword", "only entries containing a `keyword` (repeatable)")
		ex := &multiFlag{comma: true}
		fs.Var(ex, "exclude", "skip entries containing a `keyword` (repeatable)")
		paused := fs.Bool("paused", false, "create paused")
		return func(c *Ctx, _ []string) error {
			if *coll == "" {
				return usageErrf("--collection is required")
			}
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var path string
			body := map[string]any{"collection": *coll}
			setIf(body, "name", *name)
			setIf(body, "every", *every)
			if *preset != "" {
				if *kind != "" || *u != "" {
					return usageErrf("use either --preset (with --param) or --kind/--url, not both")
				}
				path = "/v1/watchers/from-preset"
				body["preset_id"] = *preset
				pm := map[string]string{}
				for _, kv := range params.vals {
					k, v, ok := strings.Cut(kv, "=")
					if !ok || k == "" {
						return usageErrf("--param expects name=value, got %q", kv)
					}
					pm[k] = v
				}
				body["params"] = pm
			} else {
				if *u == "" {
					return usageErrf("pass --preset <id>, or --url (and --kind rss|url_diff)")
				}
				path = "/v1/watchers"
				body["url"] = *u
				body["kind"] = firstNonEmpty(*kind, "rss")
				if len(kw.vals) > 0 {
					body["keywords"] = kw.vals
				}
				if len(ex.vals) > 0 {
					body["exclude_keywords"] = ex.vals
				}
				if *paused {
					body["status"] = "paused"
				}
			}
			var w Watcher
			raw, err := cl.Post(c, path, body, &w)
			if err != nil {
				return err
			}
			if *paused && *preset != "" {
				raw, err = cl.Patch(c, "/v1/watchers/"+url.PathEscape(w.ID), map[string]any{"status": "paused"}, &w)
				if err != nil {
					return err
				}
			}
			if c.JSON {
				return c.printRaw(raw)
			}
			fmt.Fprintf(c.Out, "Created watcher %s (%s, every %s, %s)\n", w.ID, w.Kind, w.Every, c.status(w.Status))
			return nil
		}
	},
		"staqly watchers create --preset github-releases --param owner=golang --param repo=go --collection Releases",
		"staqly watchers create --kind rss --url https://blog.example.com/feed.xml --collection Reading --every 6h --keyword AI",
		"staqly watchers create --kind url_diff --url https://example.com/pricing --collection Competitors")
	return cmd
}
