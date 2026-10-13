package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

func eventsCommand() *Command {
	tail := leaf("tail", "", "Print recent events; --follow polls for new ones", 0, 0, func(fs *flag.FlagSet) runFunc {
		typ := fs.String("type", "", "event type, exact or glob (e.g. item.approved, 'item.*')")
		item := fs.String("item", "", "only events of this item")
		coll := fs.String("collection", "", "only events of this collection (ID or name)")
		actor := fs.String("actor-type", "", "local, session, api_key or system")
		limit := fs.Int("limit", 20, "how many recent events to print first")
		var follow bool
		fs.BoolVar(&follow, "follow", false, "keep polling for new events")
		fs.BoolVar(&follow, "f", false, "shorthand for --follow")
		interval := fs.Duration("interval", 2*time.Second, "poll interval with --follow")
		return func(c *Ctx, _ []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			q := url.Values{}
			setQ(q, "item_id", *item)
			setQ(q, "actor_type", *actor)
			glob := strings.ContainsAny(*typ, "*?[")
			if *typ != "" && !glob {
				q.Set("type", *typ)
			}
			if *coll != "" {
				id, err := resolveCollectionID(c, cl, *coll)
				if err != nil {
					return err
				}
				q.Set("collection_id", id)
			}
			match := func(e Event) bool {
				if !glob {
					return true
				}
				ok, _ := path.Match(*typ, e.Type)
				return ok
			}
			first := url.Values{}
			for k, v := range q {
				first[k] = v
			}
			first.Set("limit", strconv.Itoa(min(max(*limit, 1), 200)))
			var page EventPage
			if _, err := cl.Get(c, "/v1/events", first, &page); err != nil {
				return err
			}
			var last int64
			for _, e := range page.Data {
				last = max(last, e.Seq)
			}
			printEvents(c, page.Data, match)
			if !follow {
				return nil
			}
			if *interval < 500*time.Millisecond {
				*interval = 500 * time.Millisecond
			}
			for {
				if err := c.Env.sleep(c, *interval); err != nil {
					return nil // Ctrl-C ends a follow normally
				}
				evs, err := eventsSince(c, cl, q, last)
				if err != nil {
					if isTransient(err) {
						fmt.Fprintf(c.Stderr, "warning: %s\n", strings.SplitN(friendlyError(err), "\n", 2)[0])
						continue
					}
					if c.Context.Err() != nil {
						return nil
					}
					return err
				}
				for _, e := range evs {
					last = max(last, e.Seq)
				}
				printEvents(c, evs, match)
			}
		}
	}, "staqly events tail", "staqly events tail --follow --type 'item.*'", "staqly events tail -f --json | jq -c 'select(.type==\"item.approved\")'")
	tail.Long = `Print recent workspace events (oldest first) and, with --follow, keep polling
for new ones by sequence number. With --json each event is one JSON line (NDJSON).`
	return &Command{Name: "events", Short: "Tail the workspace activity log", Subs: []*Command{tail}}
}

// eventsSince pages backwards from the newest event until it reaches seq <= last.
func eventsSince(c *Ctx, cl *Client, q url.Values, last int64) ([]Event, error) {
	page := url.Values{}
	for k, v := range q {
		page[k] = v
	}
	page.Set("limit", "100")
	var out []Event
	for range 20 {
		var p EventPage
		if _, err := cl.Get(c, "/v1/events", page, &p); err != nil {
			return nil, err
		}
		done := p.NextCursor == ""
		for _, e := range p.Data {
			if e.Seq <= last {
				done = true
				break
			}
			out = append(out, e)
		}
		if done {
			break
		}
		page.Set("cursor", p.NextCursor)
	}
	return out, nil
}

func printEvents(c *Ctx, evs []Event, match func(Event) bool) {
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].Seq < evs[j].Seq })
	for _, e := range evs {
		if !match(e) {
			continue
		}
		if c.JSON {
			b, _ := json.Marshal(e)
			fmt.Fprintln(c.Out, string(b))
			continue
		}
		fmt.Fprintf(c.Out, "%s  %-28s %-26s %-16s %s\n", c.paint(dim, fmtTime(e.CreatedAt)), eventType(c, e.Type),
			orDash(strPtr(e.ItemID)), truncate(actorName(e.Actor), 16), truncate(eventDetails(e), 60))
	}
}

func eventType(c *Ctx, t string) string {
	pad := fmt.Sprintf("%-28s", t)
	switch {
	case strings.HasSuffix(t, ".approved"):
		return c.paint(green, pad)
	case strings.HasSuffix(t, ".rejected"), strings.HasSuffix(t, ".failed"):
		return c.paint(red, pad)
	case strings.HasSuffix(t, ".created"):
		return c.paint(cyan, pad)
	}
	return pad
}
