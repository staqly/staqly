package cli

import (
	"bufio"
	"flag"
	"fmt"
	"net/url"
	"strings"
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
					t := c.table("ID", "NAME", "ITEMS", "PENDING", "ARCHIVED")
					for _, col := range page.Data {
						t.row(col.ID, col.Name, fmt.Sprint(col.ItemCount), fmt.Sprint(col.StatusCounts["pending"]), yesNo(col.Archived))
					}
					t.flush("No collections yet. Create one with: staqly collections create <name>")
					return nil
				}
			}, "staqly collections list", "staqly collections list --archived --json"),

			leaf("create", "<name>", "Create a collection", 1, 1, func(fs *flag.FlagSet) runFunc {
				desc := fs.String("description", "", "description")
				icon := fs.String("icon", "", "icon (emoji or name)")
				color := fs.String("color", "", "color")
				view := fs.String("view", "", "default view: gallery, table or list")
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					body := map[string]any{"name": args[0]}
					setIf(body, "description", *desc)
					setIf(body, "icon", *icon)
					setIf(body, "color", *color)
					setIf(body, "default_view", *view)
					var col Collection
					raw, err := cl.Post(c, "/v1/collections", body, &col)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					fmt.Fprintf(c.Out, "Created collection %s (%s)\n", c.paint(bold, col.Name), col.ID)
					return nil
				}
			}, "staqly collections create \"Instagram Avatar\" --description \"Generated portraits\" --view gallery"),

			leaf("get", "<id|name>", "Show a collection and its actions", 1, 1, func(fs *flag.FlagSet) runFunc {
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
					for _, s := range []string{"draft", "pending", "approved", "rejected", "processing", "completed", "failed", "archived", "expired"} {
						if n := col.StatusCounts[s]; n > 0 {
							counts = append(counts, fmt.Sprintf("%s %d", s, n))
						}
					}
					c.kv("ID", col.ID, "Name", col.Name, "Description", col.Description, "Default view", col.DefaultView,
						"Items", fmt.Sprint(col.ItemCount), "By status", strings.Join(counts, ", "),
						"Archived", yesNo(col.Archived), "Created", fmtTime(col.CreatedAt))
					if len(col.Actions) > 0 {
						fmt.Fprintln(c.Out)
						t := c.table("ACTION", "LABEL", "EVENT", "SETS STATUS", "FEEDBACK")
						for _, a := range col.Actions {
							t.row(a.Name, a.Label, a.Event, orDash(a.SetStatus), map[bool]string{true: "required", false: "optional"}[a.RequiresFeedback])
						}
						t.flush("")
					}
					return nil
				}
			}, "staqly collections get \"Blog posts\""),

			leaf("rename", "<id|name> <new-name>", "Rename a collection", 2, 2, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					return patchCollection(c, args[0], map[string]any{"name": args[1]}, "Renamed to %s")
				}
			}, "staqly collections rename \"Blog posts\" \"Blog drafts\""),

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

			leaf("delete", "<id|name>", "Permanently delete a collection and all its items", 1, 1, func(fs *flag.FlagSet) runFunc {
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
						fmt.Fprintf(c.Stderr, "This permanently deletes %q and its %d items.\nType the collection name to confirm: ", col.Name, col.ItemCount)
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
	var col Collection
	raw, err := cl.Patch(c, "/v1/collections/"+url.PathEscape(ref), body, &col)
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
