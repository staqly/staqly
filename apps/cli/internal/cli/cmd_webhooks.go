package cli

import (
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func webhooksCommand() *Command {
	return &Command{
		Name: "webhooks", Aliases: []string{"webhook", "hooks"},
		Short: "Manage outgoing webhooks and inspect deliveries",
		Subs: []*Command{
			leaf("list", "", "List webhooks", 0, 0, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var page struct{ Data []Webhook }
					raw, err := cl.Get(c, "/v1/webhooks", nil, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "URL", "EVENTS", "COLLECTIONS", "ACTIVE", "SECRET")
					for _, w := range page.Data {
						cols := "all"
						if len(w.CollectionIDs) > 0 {
							cols = strings.Join(w.CollectionIDs, ",")
						}
						t.row(w.ID, truncate(w.URL, 50), strings.Join(w.Events, ","), cols, yesNo(w.Active), maskKey(w.Secret))
					}
					t.flush("No webhooks. Create one with: staqly webhooks create --url https://...")
					return nil
				}
			}, "staqly webhooks list"),

			leaf("create", "", "Create a webhook (prints the signing secret)", 0, 0, func(fs *flag.FlagSet) runFunc {
				u := fs.String("url", "", "receiver URL (required)")
				events := &multiFlag{comma: true}
				fs.Var(events, "events", "`events`: names or globs, comma-separated or repeated (default *)")
				cols := &multiFlag{comma: true}
				fs.Var(cols, "collection", "limit to `collections` (ID or name; repeatable)")
				desc := fs.String("description", "", "description")
				timeoutMS := fs.Int("timeout-ms", 0, "delivery timeout in ms (1000–60000)")
				attempts := fs.Int("max-attempts", 0, "delivery attempts (1–20)")
				inactive := fs.Bool("inactive", false, "create disabled")
				return func(c *Ctx, _ []string) error {
					if *u == "" {
						return usageErrf("--url (the receiver URL) is required")
					}
					cl, err := c.Client()
					if err != nil {
						return err
					}
					body := map[string]any{"url": *u, "events": []string{"*"}}
					if len(events.vals) > 0 {
						body["events"] = events.vals
					}
					setIf(body, "description", *desc)
					if *timeoutMS > 0 {
						body["timeout_ms"] = *timeoutMS
					}
					if *attempts > 0 {
						body["max_attempts"] = *attempts
					}
					if *inactive {
						body["active"] = false
					}
					if len(cols.vals) > 0 {
						ids := make([]string, len(cols.vals))
						for i, ref := range cols.vals {
							if ids[i], err = resolveCollectionID(c, cl, ref); err != nil {
								return err
							}
						}
						body["collection_ids"] = ids
					}
					var w Webhook
					raw, err := cl.Post(c, "/v1/webhooks", body, &w)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					fmt.Fprintf(c.Out, "Created webhook %s → %s\n", w.ID, w.URL)
					fmt.Fprintf(c.Out, "Signing secret: %s\n", w.Secret)
					fmt.Fprintln(c.Out, c.paint(dim, "Verify X-Staqly-Signature = sha256=hex(HMAC_SHA256(secret, timestamp + \".\" + body))."))
					return nil
				}
			}, "staqly webhooks create --url https://example.com/hooks/staqly --events 'post.published,post.failed'"),

			leaf("get", "<id>", "Show a webhook (secret masked)", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var w Webhook
					raw, err := cl.Get(c, "/v1/webhooks/"+url.PathEscape(args[0]), nil, &w)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					cols := "all"
					if len(w.CollectionIDs) > 0 {
						cols = strings.Join(w.CollectionIDs, ", ")
					}
					c.kv("ID", w.ID, "URL", w.URL, "Description", w.Description, "Events", strings.Join(w.Events, ", "),
						"Collections", cols, "Active", yesNo(w.Active), "Timeout", fmt.Sprintf("%d ms", w.TimeoutMS),
						"Max attempts", strconv.Itoa(w.MaxAttempts), "Secret", maskKey(w.Secret), "Created", fmtTime(w.CreatedAt))
					return nil
				}
			}, "staqly webhooks get wh_q2vbm4kz7xw3dn5hjr6c"),

			webhooksUpdateCommand(),

			leaf("delete", "<id>", "Delete a webhook", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					return simpleDelete(c, "/v1/webhooks/"+url.PathEscape(args[0]), "webhook", args[0])
				}
			}, "staqly webhooks delete wh_q2vbm4kz7xw3dn5hjr6c"),

			leaf("test", "<id>", "Queue a webhook.test delivery", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var d Delivery
					raw, err := cl.Post(c, "/v1/webhooks/"+url.PathEscape(args[0])+"/test", nil, &d)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					fmt.Fprintf(c.Out, "Queued test delivery %s (%s). Check it with: staqly webhooks deliveries --webhook %s\n", d.ID, d.Status, args[0])
					return nil
				}
			}, "staqly webhooks test wh_q2vbm4kz7xw3dn5hjr6c"),

			leaf("deliveries", "", "Show delivery history", 0, 0, func(fs *flag.FlagSet) runFunc {
				hook := fs.String("webhook", "", "only this webhook")
				status := fs.String("status", "", "pending, succeeded or failed")
				limit := fs.Int("limit", 50, "how many")
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					q := url.Values{"limit": {strconv.Itoa(*limit)}}
					setQ(q, "webhook_id", *hook)
					setQ(q, "status", *status)
					var page struct{ Data []Delivery }
					raw, err := cl.Get(c, "/v1/webhook-deliveries", q, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "WEBHOOK", "EVENT", "STATUS", "ATTEMPTS", "HTTP", "CREATED", "ERROR")
					for _, d := range page.Data {
						code := "-"
						if d.LastStatusCode != nil {
							code = strconv.Itoa(*d.LastStatusCode)
						}
						t.row(d.ID, d.WebhookID, d.EventType, c.status(d.Status), strconv.Itoa(d.Attempts), code, fmtTime(d.CreatedAt), truncate(orDash(d.LastError), 40))
					}
					t.flush("No deliveries.")
					return nil
				}
			}, "staqly webhooks deliveries --status failed"),

			leaf("retry", "<delivery-id>", "Re-queue a finished delivery", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var d Delivery
					raw, err := cl.Post(c, "/v1/webhook-deliveries/"+url.PathEscape(args[0])+"/retry", nil, &d)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					fmt.Fprintf(c.Out, "Re-queued delivery %s\n", d.ID)
					return nil
				}
			}, "staqly webhooks retry dlv_123"),
		},
	}
}

func webhooksUpdateCommand() *Command {
	cmd := leaf("update", "<id>", "Update a webhook, or rotate its signing secret", 1, 1, func(fs *flag.FlagSet) runFunc {
		u := fs.String("url", "", "new receiver URL")
		events := &multiFlag{comma: true}
		fs.Var(events, "events", "replace the `events` (names or globs, comma-separated or repeated)")
		cols := &multiFlag{comma: true}
		fs.Var(cols, "collection", "replace the collection filter with these `collections` (ID or name; repeatable)")
		allCols := fs.Bool("all-collections", false, "deliver events of every collection again")
		desc := fs.String("description", "", "new description")
		timeoutMS := fs.Int("timeout-ms", 0, "delivery timeout in ms (1000-60000)")
		attempts := fs.Int("max-attempts", 0, "delivery attempts (1-20)")
		enable := fs.Bool("enable", false, "activate the webhook")
		disable := fs.Bool("disable", false, "deactivate the webhook")
		rotate := fs.Bool("rotate-secret", false, "issue a new signing secret (printed once; the old one stops working)")
		return func(c *Ctx, args []string) error {
			if *enable && *disable {
				return usageErrf("--enable and --disable cannot be combined")
			}
			if *allCols && len(cols.vals) > 0 {
				return usageErrf("--all-collections and --collection cannot be combined")
			}
			body := map[string]any{}
			setIf(body, "url", *u)
			fs.Visit(func(f *flag.Flag) {
				if f.Name == "description" {
					body["description"] = *desc
				}
			})
			if len(events.vals) > 0 {
				body["events"] = events.vals
			}
			if *timeoutMS > 0 {
				body["timeout_ms"] = *timeoutMS
			}
			if *attempts > 0 {
				body["max_attempts"] = *attempts
			}
			if *enable {
				body["active"] = true
			}
			if *disable {
				body["active"] = false
			}
			if *rotate {
				body["rotate_secret"] = true
			}
			if *allCols {
				body["collection_ids"] = []string{}
			}
			cl, err := c.Client()
			if err != nil {
				return err
			}
			if len(cols.vals) > 0 {
				ids := make([]string, len(cols.vals))
				for i, ref := range cols.vals {
					if ids[i], err = resolveCollectionID(c, cl, ref); err != nil {
						return err
					}
				}
				body["collection_ids"] = ids
			}
			if len(body) == 0 {
				return usageErrf("nothing to update; pass --url, --events, --collection, --all-collections, --description, --timeout-ms, --max-attempts, --enable, --disable or --rotate-secret")
			}
			var w Webhook
			raw, err := cl.Patch(c, "/v1/webhooks/"+url.PathEscape(args[0]), body, &w)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			fmt.Fprintf(c.Out, "Updated webhook %s → %s (active %s)\n", w.ID, w.URL, yesNo(w.Active))
			if *rotate {
				fmt.Fprintf(c.Out, "New signing secret: %s\n", w.Secret)
				fmt.Fprintln(c.Out, c.paint(dim, "Store it now: it is not shown again, and the old secret no longer verifies deliveries."))
			}
			return nil
		}
	},
		"staqly webhooks update wh_q2vbm4kz7xw3dn5hjr6c --disable",
		"staqly webhooks update wh_q2vbm4kz7xw3dn5hjr6c --events 'post.*' --collection \"Blog posts\"",
		"staqly webhooks update wh_q2vbm4kz7xw3dn5hjr6c --rotate-secret")
	return cmd
}

func simpleDelete(c *Ctx, path, what, id string) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	if err := cl.Delete(c, path, nil); err != nil {
		return err
	}
	if c.JSON {
		return c.printJSON(map[string]any{"deleted": true, "id": id})
	}
	fmt.Fprintf(c.Out, "Deleted %s %s\n", what, id)
	return nil
}
