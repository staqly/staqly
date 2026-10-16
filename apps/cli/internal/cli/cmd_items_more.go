package cli

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ---- delete ----

func itemsDeleteCommand() *Command {
	cmd := leaf("delete", "<id>...", "Delete one or more items (their history is kept)", 1, -1, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, ids []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			if len(ids) == 1 {
				if err := cl.Delete(c, "/v1/items/"+url.PathEscape(ids[0]), nil); err != nil {
					return err
				}
				if c.JSON {
					return c.printJSON(map[string]any{"deleted": true, "id": ids[0]})
				}
				fmt.Fprintf(c.Out, "Deleted item %s\n", ids[0])
				return nil
			}
			var results []any
			failed := 0
			var lastErr error
			for _, id := range ids {
				err := cl.Delete(c, "/v1/items/"+url.PathEscape(id), nil)
				switch {
				case err != nil:
					failed++
					lastErr = err
					if c.JSON {
						results = append(results, map[string]any{"id": id, "ok": false, "error": errorBody(err)})
					} else {
						fmt.Fprintf(c.Stderr, "%s %s: %s\n", c.paint(red, "failed"), id, friendlyError(err))
					}
				case c.JSON:
					results = append(results, map[string]any{"id": id, "ok": true})
				default:
					fmt.Fprintf(c.Out, "Deleted item %s\n", id)
				}
			}
			if c.JSON {
				_ = c.printJSON(results)
			}
			return multiFailure(c, failed, len(ids), lastErr)
		}
	}, "staqly items delete itm_oalirbz67y4m4cqxq5dw", "staqly items delete itm_oalirbz67y4m4cqxq5dw itm_q2w8n5k7r3t9y4u6i1o0")
	cmd.Long = `Delete one or more items. Their events stay in the activity log as
history; use 'staqly items purge' to remove those too.`
	return cmd
}

func multiFailure(c *Ctx, failed, total int, lastErr error) error {
	if failed == 0 {
		return nil
	}
	if c.JSON {
		return exitWith(ExitError)
	}
	return &exitErr{code: ExitError, msg: fmt.Sprintf("%d of %d failed (last: %s)", failed, total, strings.SplitN(friendlyError(lastErr), "\n", 2)[0])}
}

// ---- purge ----

func itemsPurgeCommand() *Command {
	cmd := leaf("purge", "<id>", "Delete an item and every event about it (admin, irreversible)", 1, 1, func(fs *flag.FlagSet) runFunc {
		yes := fs.Bool("yes", false, "do not ask for confirmation")
		return func(c *Ctx, args []string) error {
			id := args[0]
			if !*yes {
				if !c.Env.StdinTTY {
					return usageErrf("refusing to purge %s without --yes (stdin is not a terminal)", id)
				}
				fmt.Fprintf(c.Stderr, "This permanently deletes %s, its history (events and webhook deliveries) and its media if unused.\nType the item ID to confirm: ", id)
				line, _ := bufio.NewReader(c.Env.Stdin).ReadString('\n')
				if strings.TrimSpace(line) != id {
					return &exitErr{code: ExitError, msg: "confirmation did not match; nothing was purged"}
				}
			}
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var res struct {
				ItemID        string `json:"item_id"`
				EventsDeleted int    `json:"events_deleted"`
				MediaDeleted  bool   `json:"media_deleted"`
			}
			raw, err := cl.Post(c, "/v1/items/"+url.PathEscape(id)+"/purge", nil, &res)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			media := "media kept or none"
			if res.MediaDeleted {
				media = "media deleted"
			}
			fmt.Fprintf(c.Out, "Purged %s: %d events deleted, %s\n", res.ItemID, res.EventsDeleted, media)
			return nil
		}
	}, "staqly items purge itm_oalirbz67y4m4cqxq5dw", "staqly items purge itm_oalirbz67y4m4cqxq5dw --yes")
	cmd.Long = `Delete an item together with its history: every event about it, their
webhook deliveries (including pending ones) and its media when no other item
uses it. One content-free item.purged event is recorded. Needs the admin
scope. Without --yes you type the item ID to confirm.`
	return cmd
}

// ---- bulk ----

const bulkChunk = 100

func itemsBulkCommand() *Command {
	cmd := leaf("bulk", "<action> [<id>...]", "Run approve, reject or a custom action on many items at once", 1, -1, func(fs *flag.FlagSet) runFunc {
		var feedback string
		fs.StringVar(&feedback, "feedback", "", "feedback for the agent (applies to every item)")
		fs.StringVar(&feedback, "m", "", "shorthand for --feedback")
		payload := fs.String("payload", "", "arbitrary JSON forwarded in each event and webhook")
		stdin := fs.Bool("stdin", false, "also read item IDs from stdin (whitespace or newline separated)")
		return func(c *Ctx, args []string) error {
			action, ids := args[0], append([]string(nil), args[1:]...)
			if *stdin {
				sc := bufio.NewScanner(c.Env.Stdin)
				sc.Buffer(make([]byte, 64<<10), 1<<20)
				sc.Split(bufio.ScanWords)
				for sc.Scan() {
					ids = append(ids, sc.Text())
				}
				if err := sc.Err(); err != nil {
					return err
				}
			}
			if len(ids) == 0 {
				return usageErrf("no item IDs given; pass them as arguments or with --stdin")
			}
			body := map[string]any{"action": action}
			setIf(body, "feedback", feedback)
			if *payload != "" {
				var p any
				if err := json.Unmarshal([]byte(*payload), &p); err != nil {
					return usageErrf("--payload must be valid JSON")
				}
				body["payload"] = p
			}
			cl, err := c.Client()
			if err != nil {
				return err
			}
			type result struct {
				ID    string `json:"id"`
				OK    bool   `json:"ok"`
				Error *struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
				Item *Item `json:"item"`
			}
			var all []json.RawMessage
			failed := 0
			lastMsg := ""
			for start := 0; start < len(ids); start += bulkChunk {
				chunk := ids[start:min(start+bulkChunk, len(ids))]
				body["ids"] = chunk
				var page struct{ Data []result }
				raw, err := cl.Post(c, "/v1/items/bulk-actions", body, &page)
				if err != nil {
					return err
				}
				var rawPage struct{ Data []json.RawMessage }
				if err := json.Unmarshal(raw, &rawPage); err != nil {
					return fmt.Errorf("unexpected response from server: %w", err)
				}
				all = append(all, rawPage.Data...)
				for _, r := range page.Data {
					if !r.OK {
						failed++
						msg := "failed"
						if r.Error != nil {
							msg = r.Error.Message
							if r.Error.Code != "" {
								msg += " (" + r.Error.Code + ")"
							}
						}
						lastMsg = msg
						if !c.JSON {
							fmt.Fprintf(c.Stderr, "%s %s: %s\n", c.paint(red, "failed"), r.ID, msg)
						}
						continue
					}
					if !c.JSON {
						st := ""
						if r.Item != nil {
							st = r.Item.Status
						}
						fmt.Fprintf(c.Out, "%s %s → %s\n", pastTense(action), r.ID, c.status(st))
					}
				}
			}
			if c.JSON {
				if all == nil {
					all = []json.RawMessage{}
				}
				_ = c.printJSON(map[string]any{"data": all})
				if failed > 0 {
					return exitWith(ExitError)
				}
				return nil
			}
			if failed > 0 {
				return &exitErr{code: ExitError, msg: fmt.Sprintf("%d of %d failed (last: %s)", failed, len(ids), lastMsg)}
			}
			return nil
		}
	}, "staqly items bulk approve itm_oalirbz67y4m4cqxq5dw itm_q2w8n5k7r3t9y4u6i1o0",
		"staqly items bulk reject itm_oalirbz67y4m4cqxq5dw -m \"off brand\"",
		"staqly items list --status pending --q \"typo fix\" --all --json | jq -r '.data[].id' | staqly items bulk approve --stdin")
	cmd.Long = `Run one action (approve, reject or a custom collection action such as
regenerate) on many items with POST /v1/items/bulk-actions, 100 IDs per
request. Each item succeeds or fails on its own; the command exits 1 when any
item failed. With --json it prints {"data":[{"id","ok","item"|"error"}]}.`
	return cmd
}

// ---- versions ----

func itemsVersionsCommand() *Command {
	return leaf("versions", "<id>", "Show snapshots of an item before each change", 1, 1, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, args []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var page struct {
				Data []struct {
					Version   int    `json:"version"`
					Snapshot  Item   `json:"snapshot"`
					Actor     string `json:"actor"`
					CreatedAt string `json:"created_at"`
				} `json:"data"`
			}
			raw, err := cl.Get(c, "/v1/items/"+url.PathEscape(args[0])+"/versions", nil, &page)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			t := c.table("VERSION", "STATUS", "TITLE", "ACTOR", "SAVED")
			for _, v := range page.Data {
				t.row(strconv.Itoa(v.Version), c.status(v.Snapshot.Status), truncate(orDash(v.Snapshot.Title), 50), orDash(v.Actor), fmtTime(v.CreatedAt))
			}
			t.flush("No earlier versions.")
			return nil
		}
	}, "staqly items versions itm_oalirbz67y4m4cqxq5dw", "staqly items versions itm_oalirbz67y4m4cqxq5dw --json | jq '.data[0].snapshot.content'")
}
