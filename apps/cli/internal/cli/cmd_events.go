package cli

import (
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
		typ := fs.String("type", "", "event type, exact or glob (e.g. post.published, 'post.*')")
		post := fs.String("post", "", "only events of this post")
		channel := fs.String("channel", "", "only events of this channel (ID)")
		schedule := fs.String("schedule", "", "only events of this schedule (ID)")
		coll := fs.String("collection", "", "only events of this collection (ID or name)")
		actor := fs.String("actor-type", "", "local, session, api_key or system")
		actorID := fs.String("actor-id", "", "only events by this actor (e.g. an API key ID)")
		from := fs.String("from", "", "events created at or after this time (RFC3339 or YYYY-MM-DD)")
		to := fs.String("to", "", "events created before this time (RFC3339 or YYYY-MM-DD)")
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
			setQ(q, "post_id", *post)
			setQ(q, "channel_id", *channel)
			setQ(q, "schedule_id", *schedule)
			setQ(q, "actor_type", *actor)
			setQ(q, "actor_id", *actorID)
			setQ(q, "from", *from)
			setQ(q, "to", *to)
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
	}, "staqly events tail", "staqly events tail --follow --type 'post.*'", "staqly events tail -f --json | jq -c 'select(.type==\"post.published\")'")
	tail.Long = `Print recent workspace events (oldest first) and, with --follow, keep polling
for new ones by sequence number. With --json each event is one JSON line (NDJSON).`
	return &Command{Name: "events", Short: "Tail or export the workspace activity log", Subs: []*Command{tail, eventsExportCommand()}}
}

func eventsExportCommand() *Command {
	cmd := leaf("export", "", "Export the activity log as NDJSON or CSV, oldest first", 0, 0, func(fs *flag.FlagSet) runFunc {
		format := fs.String("format", "ndjson", "ndjson or csv")
		from := fs.String("from", "", "events created at or after this time (RFC3339 or YYYY-MM-DD)")
		to := fs.String("to", "", "events created before this time (RFC3339 or YYYY-MM-DD)")
		typ := fs.String("type", "", "exact event type, or a prefix glob such as 'post.*'")
		prefix := fs.String("type-prefix", "", "literal type prefix, e.g. post.")
		actor := fs.String("actor-type", "", "local, session, api_key or system")
		actorID := fs.String("actor-id", "", "only events by this actor (e.g. an API key ID)")
		coll := fs.String("collection", "", "only events of this collection (ID or name)")
		post := fs.String("post", "", "only events of this post")
		output := fs.String("output", "", "write to this `file` instead of stdout")
		fs.StringVar(output, "o", "", "shorthand for --output")
		return func(c *Ctx, _ []string) error {
			if *format != "ndjson" && *format != "csv" {
				return usageErrf("--format must be ndjson or csv")
			}
			q := url.Values{"format": {*format}}
			if *typ != "" {
				base, isPrefix := strings.CutSuffix(*typ, "*")
				switch {
				case strings.ContainsAny(base, "*?["):
					return usageErrf("--type supports an exact type or a trailing * (e.g. 'post.*')")
				case isPrefix && *prefix != "":
					return usageErrf("--type with * and --type-prefix cannot be combined")
				case isPrefix:
					q.Set("type_prefix", base)
				default:
					q.Set("type", base)
				}
			}
			setQ(q, "type_prefix", *prefix)
			setQ(q, "from", *from)
			setQ(q, "to", *to)
			setQ(q, "actor_type", *actor)
			setQ(q, "actor_id", *actorID)
			setQ(q, "post_id", *post)
			cl, err := c.Client()
			if err != nil {
				return err
			}
			if *coll != "" {
				id, err := resolveCollectionID(c, cl, *coll)
				if err != nil {
					return err
				}
				q.Set("collection_id", id)
			}
			out := *output
			if out == "" {
				out = "-"
			}
			return download(c, cl, "/v1/events/export", q, out, "events."+*format, false)
		}
	},
		"staqly events export > events.ndjson",
		"staqly events export --format csv --from 2026-10-01 --to 2026-11-01 -o october.csv",
		"staqly events export --type 'post.*' --collection \"Blog posts\" | jq -c 'select(.type==\"post.failed\")'")
	cmd.Long = `Export every matching event, oldest first, as NDJSON (one JSON event per
line, the default) or CSV with the columns
created_at,id,type,actor_type,actor_id,actor_name,collection_id,post_id,data.
Output goes to stdout unless --output is given. Credential and session audit
events are only included for admin principals.`
	return cmd
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
			c.printJSONLine(e)
			continue
		}
		fmt.Fprintf(c.Out, "%s  %-28s %-26s %-16s %s\n", c.paint(dim, fmtTime(e.CreatedAt)), eventType(c, e.Type),
			cleanLine(orDash(firstNonEmpty(strPtr(e.PostID), strPtr(e.ChannelID), strPtr(e.ScheduleID)))), truncate(cleanLine(actorName(e.Actor)), 16), truncate(eventDetails(e), 60))
	}
}

func eventType(c *Ctx, t string) string {
	t = cleanLine(t)
	pad := fmt.Sprintf("%-28s", t)
	switch {
	case strings.HasSuffix(t, ".published"), strings.HasSuffix(t, ".target_published"), strings.HasSuffix(t, ".ran"):
		return c.paint(green, pad)
	case strings.HasSuffix(t, ".failed"), strings.HasSuffix(t, ".needs_reconnect"):
		return c.paint(red, pad)
	case strings.HasSuffix(t, ".created"), strings.HasSuffix(t, ".scheduled"):
		return c.paint(cyan, pad)
	}
	return pad
}
