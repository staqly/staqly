package cli

import (
	"bufio"
	"flag"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func collectionsCommand() *Command {
	return &Command{
		Name: "collections", Aliases: []string{"collection", "cols"},
		Short: "List, create, rename, archive and delete collections",
		Subs: []*Command{
			leaf("list", "", "List collections", 0, 0, func(fs *flag.FlagSet) runFunc {
				archived := fs.Bool("archived", false, "include archived collections")
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					q := url.Values{}
					if *archived {
						q.Set("include_archived", "true")
					}
					var page struct{ Data []Collection }
					raw, err := cl.Get(c, "/v1/collections", q, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "NAME", "POSTS", "DRAFTS", "SCHEDULED", "ARCHIVED")
					for _, col := range page.Data {
						t.row(col.ID, col.Name, fmt.Sprint(col.PostCount), fmt.Sprint(col.StatusCounts["draft"]), fmt.Sprint(col.StatusCounts["scheduled"]), yesNo(col.Archived))
					}
					t.flush("No collections yet. Create one with: staqly collections create <name>")
					return nil
				}
			}, "staqly collections list", "staqly collections list --archived --json"),

			leaf("create", "<name>", "Create a collection", 1, 1, func(fs *flag.FlagSet) runFunc {
				desc := fs.String("description", "", "description")
				icon := fs.String("icon", "", "icon (emoji or name)")
				color := fs.String("color", "", "color")
				timeout := fs.Duration("processing-timeout", 0, "how long a target on your own automation may wait for a result before it fails, e.g. 30m or 2h (1 minute to 24 hours); default: the server's")
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					body := map[string]any{"name": args[0]}
					setIf(body, "description", *desc)
					setIf(body, "icon", *icon)
					setIf(body, "color", *color)
					if *timeout != 0 {
						body["processing_timeout_seconds"] = int64(*timeout / time.Second)
					}
					var col Collection
					raw, err := cl.Post(c, "/v1/collections", body, &col)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					fmt.Fprintf(c.Out, "Created collection %s (%s)\n", c.paint(bold, col.Name), col.ID)
					return nil
				}
			}, "staqly collections create \"Instagram Avatar\" --description \"Generated portraits\" --color \"#3b82f6\"",
				"staqly collections create \"Pipeline\" --processing-timeout 2h   # how long your own automation may take to report a result"),

			leaf("get", "<id|name>", "Show a collection", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var col Collection
					raw, err := cl.Get(c, "/v1/collections/"+url.PathEscape(args[0]), nil, &col)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					counts := []string{}
					for _, s := range []string{"draft", "scheduled", "publishing", "published", "failed"} {
						if n := col.StatusCounts[s]; n > 0 {
							counts = append(counts, fmt.Sprintf("%s %d", s, n))
						}
					}
					timeout := "server default"
					if col.ProcessingTimeoutSeconds != nil {
						timeout = (time.Duration(*col.ProcessingTimeoutSeconds) * time.Second).String()
					}
					c.kv("ID", col.ID, "Name", col.Name, "Description", col.Description, "Default", yesNo(col.IsDefault),
						"Posts", fmt.Sprint(col.PostCount), "By status", strings.Join(counts, ", "),
						"Processing timeout", timeout,
						"Archived", yesNo(col.Archived), "Created", fmtTime(col.CreatedAt))
					return nil
				}
			}, "staqly collections get \"Blog posts\""),

			leaf("rename", "<id|name> <new-name>", "Rename a collection", 2, 2, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					return patchCollection(c, args[0], map[string]any{"name": args[1]}, "Renamed to %s")
				}
			}, "staqly collections rename \"Blog posts\" \"Blog drafts\""),

			leaf("timeout", "<id|name> <duration|default>", "Set how long targets on your own automation may wait for a result before they fail", 2, 2, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					var v any
					if args[1] != "default" {
						d, err := time.ParseDuration(args[1])
						if err != nil {
							return usageErrf("timeout: a duration like 30m or 2h, or default")
						}
						v = int64(d / time.Second)
					}
					return patchCollection(c, args[0], map[string]any{"processing_timeout_seconds": v}, "Targets in %s now fail as set when nothing is reported")
				}
			}, "staqly collections timeout \"Daily digest\" 2h", "staqly collections timeout \"Daily digest\" default"),

			leaf("archive", "<id|name>", "Archive (or --undo to unarchive) a collection", 1, 1, func(fs *flag.FlagSet) runFunc {
				undo := fs.Bool("undo", false, "unarchive instead")
				return func(c *Ctx, args []string) error {
					msg := "Archived %s"
					if *undo {
						msg = "Unarchived %s"
					}
					return patchCollection(c, args[0], map[string]any{"archived": !*undo}, msg)
				}
			}, "staqly collections archive \"Old campaign\"", "staqly collections archive \"Old campaign\" --undo"),

			leaf("delete", "<id|name>", "Permanently delete a collection and all its posts (API keys: collections:delete)", 1, 1, func(fs *flag.FlagSet) runFunc {
				yes := fs.Bool("yes", false, "do not ask for confirmation")
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var col Collection
					if _, err := cl.Get(c, "/v1/collections/"+url.PathEscape(args[0]), nil, &col); err != nil {
						return err
					}
					if !*yes {
						if !c.Env.StdinTTY {
							return usageErrf("refusing to delete %q without --yes (stdin is not a terminal)", col.Name)
						}
						fmt.Fprintf(c.Stderr, "This permanently deletes %q and its %d posts.\nType the collection name to confirm: ", col.Name, col.PostCount)
						line, _ := bufio.NewReader(c.Env.Stdin).ReadString('\n')
						if strings.TrimSpace(line) != col.Name {
							return &exitErr{code: ExitError, msg: "confirmation did not match; nothing was deleted"}
						}
					}
					if err := cl.Delete(c, "/v1/collections/"+url.PathEscape(col.ID), url.Values{"confirm": {col.Name}}); err != nil {
						return err
					}
					if c.JSON {
						return c.printJSON(map[string]any{"deleted": true, "id": col.ID})
					}
					fmt.Fprintf(c.Out, "Deleted collection %s (%s)\n", col.Name, col.ID)
					return nil
				}
			}, "staqly collections delete \"Scratch\" --yes"),
		},
	}
}

func patchCollection(c *Ctx, ref string, body map[string]any, msg string) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	// PATCH takes an ID only (GET also resolves names), so a name is looked up first.
	id := ref
	if !strings.HasPrefix(ref, "col_") {
		if id, err = resolveCollectionID(c, cl, ref); err != nil {
			return err
		}
	}
	var col Collection
	raw, err := cl.Patch(c, "/v1/collections/"+url.PathEscape(id), body, &col)
	if err != nil || c.JSON {
		return jsonOr(c, raw, err)
	}
	fmt.Fprintf(c.Out, msg+" (%s)\n", col.Name, col.ID)
	return nil
}

// resolveCollectionID turns a collection name or ID into an ID.
func resolveCollectionID(c *Ctx, cl *Client, ref string) (string, error) {
	var col Collection
	if _, err := cl.Get(c, "/v1/collections/"+url.PathEscape(ref), nil, &col); err != nil {
		return "", err
	}
	return col.ID, nil
}

// jsonOr prints raw JSON in --json mode, or passes the error through.
func jsonOr(c *Ctx, raw []byte, err error) error {
	if err != nil {
		return err
	}
	return c.printRaw(raw)
}

func setIf(m map[string]any, k, v string) {
	if v != "" {
		m[k] = v
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
