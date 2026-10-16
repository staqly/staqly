package cli

import (
	"flag"
	"fmt"
	"net/url"
	"strconv"
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
					t.flush("No watchers. Create one with: staqly watchers create --url <feed or page URL> --collection <collection>")
					return nil
				}
			}, "staqly watchers list"),

			leaf("get", "<id>", "Show a watcher", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var w Watcher
					raw, err := cl.Get(c, "/v1/watchers/"+url.PathEscape(args[0]), nil, &w)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					c.kv("ID", w.ID, "Name", w.Name, "Kind", w.Kind, "URL", w.URL, "Collection", w.CollectionID,
						"Status", c.status(w.Status), "Every", w.Every, "Items created", strconv.Itoa(w.ItemsCreated),
						"Last run", fmtOptTime(w.LastRunAt), "Next run", fmtTime(w.NextRunAt), "Last error", w.LastError)
					return nil
				}
			}, "staqly watchers get wat_oalirbz67y4m4cqxq5dw"),

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
			}, "staqly watchers run wat_rk2pd7gqx5vm4tn3hyc6"),

			watcherStatusCommand("pause", "paused", "Pause a watcher"),
			watcherStatusCommand("resume", "active", "Resume a paused watcher"),

			leaf("delete", "<id>", "Delete a watcher", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					return simpleDelete(c, "/v1/watchers/"+url.PathEscape(args[0]), "watcher", args[0])
				}
			}, "staqly watchers delete wat_rk2pd7gqx5vm4tn3hyc6"),
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
	}, "staqly watchers "+name+" wat_rk2pd7gqx5vm4tn3hyc6")
}

func watchersCreateCommand() *Command {
	cmd := leaf("create", "", "Create a watcher from a feed or page URL", 0, 0, func(fs *flag.FlagSet) runFunc {
		coll := &multiFlag{comma: true}
		fs.Var(coll, "collection", "`collection` that receives the items (ID or name, required; repeatable: each entry goes to every one)")
		name := fs.String("name", "", "watcher name")
		every := fs.String("every", "", "check interval, e.g. 30m or 6h")
		kind := fs.String("kind", "", "rss or url_diff (default: detected from the URL)")
		u := fs.String("url", "", "feed or page URL (required)")
		kw := &multiFlag{comma: true}
		fs.Var(kw, "keyword", "only entries containing a `keyword` (repeatable)")
		ex := &multiFlag{comma: true}
		fs.Var(ex, "exclude", "skip entries containing a `keyword` (repeatable)")
		paused := fs.Bool("paused", false, "create paused")
		return func(c *Ctx, _ []string) error {
			if len(coll.vals) == 0 {
				return usageErrf("--collection is required")
			}
			if *u == "" {
				return usageErrf("--url is required")
			}
			cl, err := c.Client()
			if err != nil {
				return err
			}
			body := map[string]any{"collections": coll.vals, "url": *u}
			setIf(body, "kind", *kind)
			setIf(body, "name", *name)
			setIf(body, "every", *every)
			if len(kw.vals) > 0 {
				body["keywords"] = kw.vals
			}
			if len(ex.vals) > 0 {
				body["exclude_keywords"] = ex.vals
			}
			var w Watcher
			raw, err := cl.Post(c, "/v1/watchers", body, &w)
			if err == nil && *paused { // the API creates watchers active
				raw, err = cl.Patch(c, "/v1/watchers/"+url.PathEscape(w.ID), map[string]any{"status": "paused"}, &w)
			}
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			fmt.Fprintf(c.Out, "Created watcher %s (%s, every %s, %s)\n", w.ID, w.Kind, w.Every, c.status(w.Status))
			return nil
		}
	},
		"staqly watchers create --url https://github.com/golang/go/releases.atom --collection Releases",
		"staqly watchers create --url https://blog.example.com/feed.xml --collection Reading --every 6h --keyword AI",
		"staqly watchers create --url https://example.com/pricing --collection Competitors")
	return cmd
}
