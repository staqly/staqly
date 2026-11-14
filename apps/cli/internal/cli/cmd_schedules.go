package cli

import (
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func schedulesCommand() *Command {
	return &Command{
		Name: "schedules", Aliases: []string{"sched"},
		Short: "Publishing schedules: publish a collection's posts at a time or on a cadence",
		Subs: []*Command{
			leaf("list", "", "List schedules", 0, 0, func(fs *flag.FlagSet) runFunc {
				coll := fs.String("collection", "", "only this collection (ID or name)")
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					q := url.Values{}
					setQ(q, "collection_id", *coll)
					var page struct{ Data []Schedule }
					raw, err := cl.Get(c, "/v1/schedules", q, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "NAME", "COLLECTION", "WHEN", "PICK", "SOURCE", "ON", "NEXT RUN", "LAST RESULT")
					for _, s := range page.Data {
						t.row(s.ID, truncate(s.Name, 24), truncate(firstNonEmpty(s.CollectionName, s.CollectionID), 20), describeSchedule(s), s.Pick, s.Source,
							yesNo(s.Enabled), fmtOptTime(s.NextRunAt), truncate(orDash(s.LastResult), 40))
					}
					t.flush("No schedules. Create one with: staqly schedules create <collection> --name <name> --every 6h --channel <channel>")
					return nil
				}
			}, "staqly schedules list", "staqly schedules list --collection \"Blog posts\" --json"),
			leaf("get", "<id>", "Show a schedule", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var s Schedule
					raw, err := cl.Get(c, "/v1/schedules/"+url.PathEscape(args[0]), nil, &s)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					c.kv("ID", s.ID, "Name", s.Name, "Collection", firstNonEmpty(s.CollectionName, s.CollectionID), "When", describeSchedule(s),
						"Channels", strings.Join(s.ChannelNames, ", "), "Pick", s.Pick, "Posts", strings.Join(s.PostIDs, ", "),
						"When nothing waits", s.Source, "Endpoint", s.EndpointURL, "Enabled", yesNo(s.Enabled),
						"Next run", fmtOptTime(s.NextRunAt), "Last run", fmtOptTime(s.LastRunAt), "Last result", s.LastResult, "Created", fmtTime(s.CreatedAt))
					return nil
				}
			}, "staqly schedules get sch_oalirbz67y4m4cqxq5dw"),
			scheduleWriteCommand(true),
			scheduleWriteCommand(false),
			leaf("delete", "<id>", "Delete a schedule", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					return simpleDelete(c, "/v1/schedules/"+url.PathEscape(args[0]), "schedule", args[0])
				}
			}, "staqly schedules delete sch_oalirbz67y4m4cqxq5dw"),
			leaf("run", "<id>", "Run a schedule now; its regular next run is unchanged", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var s Schedule
					raw, err := cl.Post(c, "/v1/schedules/"+url.PathEscape(args[0])+"/run", nil, &s)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					fmt.Fprintf(c.Out, "Queued a run of %s (%s); see staqly jobs list --kind run_schedule\n", c.paint(bold, s.Name), s.ID)
					return nil
				}
			}, "staqly schedules run sch_oalirbz67y4m4cqxq5dw"),
		},
	}
}

var weekdayNames = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// describeSchedule says when a schedule runs in a few words.
func describeSchedule(s Schedule) string {
	if s.Kind == "once" {
		return "once at " + fmtOptTime(s.RunAt)
	}
	r := s.Rule
	if r.EveryMinutes > 0 {
		return "every " + (time.Duration(r.EveryMinutes) * time.Minute).String()
	}
	out := strings.Join(r.Times, ",")
	if len(r.Weekdays) > 0 {
		days := make([]string, len(r.Weekdays))
		for i, d := range r.Weekdays {
			if d >= 0 && d < 7 {
				days[i] = weekdayNames[d]
			}
		}
		out += " on " + strings.Join(days, ",")
	} else {
		out += " daily"
	}
	if r.TZ != "" {
		out += " " + r.TZ
	}
	return out
}

// weekdayFull: the full day names, Sunday first like the API.
var weekdayFull = []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}

// parseDay reads one day: a name (mon, Mon, monday; at least 3 letters) or 0 (Sunday) to 6.
func parseDay(p string) (int, bool) {
	if n, err := strconv.Atoi(p); err == nil {
		return n, n >= 0 && n <= 6
	}
	if len(p) < 3 {
		return 0, false
	}
	for i, name := range weekdayFull {
		if strings.HasPrefix(name, p) {
			return i, true
		}
	}
	return 0, false
}

// parseWeekdays reads a comma-separated list of days and ranges (mon,wed; mon-fri; sat-sun; fri-mon wraps
// over the weekend; 1-5 with 0 = Sunday) into the API's sorted 0-6.
func parseWeekdays(v string) ([]int, error) {
	seen := [7]bool{}
	for _, p := range strings.Split(v, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		from, to, isRange := strings.Cut(p, "-")
		a, okA := parseDay(strings.TrimSpace(from))
		b, okB := a, okA
		if isRange {
			b, okB = parseDay(strings.TrimSpace(to))
		}
		if !okA || !okB {
			return nil, fmt.Errorf("unknown weekday %q; use mon..sun or 0 (Sunday) to 6, comma-separated, with ranges like mon-fri", p)
		}
		for d := a; ; d = (d + 1) % 7 {
			seen[d] = true
			if d == b {
				break
			}
		}
	}
	out := []int{}
	for d, on := range seen {
		if on {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no weekdays in %q; use mon..sun or 0 (Sunday) to 6, comma-separated, with ranges like mon-fri", v)
	}
	return out, nil
}

// scheduleWriteCommand builds `schedules create` (create=true) or `schedules update`.
func scheduleWriteCommand(create bool) *Command {
	name, args, short, minArgs := "update", "<id>", "Change a schedule; the next run is recomputed", 1
	if create {
		name, args, short = "create", "<collection>", "Create a schedule for a collection's posts"
	}
	cmd := leaf(name, args, short, minArgs, minArgs, func(fs *flag.FlagSet) runFunc {
		schedName := fs.String("name", "", "schedule name"+map[bool]string{true: " (required)", false: ""}[create])
		channels := &multiFlag{comma: true}
		fs.Var(channels, "channel", "`channels` the chosen posts go to, ID or name (a post's own targets win)")
		at := fs.String("at", "", "run once at this time (RFC3339, YYYY-MM-DD, local time or relative +2h)")
		every := fs.Duration("every", 0, "run every `interval`, 5m to 7d (recurring)")
		times := fs.String("times", "", "run at these times, `HH:MM` comma-separated, e.g. 09:00,17:30 (recurring)")
		weekdays := fs.String("weekdays", "", "only on these days with --times: mon,wed,fri, ranges like mon-fri or sat-sun, or 0 (Sunday) to 6 (default every day)")
		tz := fs.String("tz", "", "time zone of --times, an IANA name like Europe/Warsaw (default the workspace's time zone)")
		pick := fs.String("pick", "", "which waiting drafts to publish: next (the oldest), all, or selected (--post)")
		posts := &multiFlag{comma: true}
		fs.Var(posts, "post", "post `ids` for --pick selected (comma-separated or repeated)")
		source := fs.String("source", "", "when nothing waits: none, agent (ask an agent through schedule.posts_requested and `staqly work`) or endpoint (fetch posts from --endpoint-url)")
		endpoint := fs.String("endpoint-url", "", "with --source endpoint: the URL that returns new posts")
		endpointSecret := fs.String("endpoint-secret", "", "signs the endpoint request like a webhook; generated when empty and printed once")
		enable, disable := new(bool), new(bool)
		if create {
			fs.BoolVar(disable, "disabled", false, "create it turned off")
		} else {
			fs.BoolVar(enable, "enable", false, "turn the schedule on")
			fs.BoolVar(disable, "disable", false, "turn the schedule off")
		}
		return func(c *Ctx, a []string) error {
			if *enable && *disable {
				return usageErrf("--enable and --disable cannot be combined")
			}
			if *at != "" && (*every != 0 || *times != "") {
				return usageErrf("--at (once) cannot be combined with --every or --times (recurring)")
			}
			if *every != 0 && *times != "" {
				return usageErrf("--every and --times cannot be combined")
			}
			cl, err := c.Client()
			if err != nil {
				return err
			}
			body := map[string]any{}
			if create {
				if *schedName == "" {
					return usageErrf("--name is required")
				}
				if *at == "" && *every == 0 && *times == "" {
					return usageErrf("say when it runs: --at <time> (once), --every <interval> or --times HH:MM[,HH:MM] (recurring)")
				}
				body["collection"] = a[0]
			}
			setIf(body, "name", *schedName)
			if *at != "" {
				when, err := parseWhen(*at)
				if err != nil {
					return usageErrf("--at: %v", err)
				}
				body["kind"] = "once"
				body["run_at"] = when
			}
			if *every != 0 {
				if *every < 5*time.Minute || *every > 7*24*time.Hour {
					return usageErrf("--every must be between 5m and 168h")
				}
				body["kind"] = "recurring"
				body["rule"] = map[string]any{"every_minutes": int(*every / time.Minute)}
			}
			if *times != "" {
				rule := map[string]any{"times": strings.Split(strings.ReplaceAll(*times, " ", ""), ",")}
				if *weekdays != "" {
					days, err := parseWeekdays(*weekdays)
					if err != nil {
						return usageErrf("--weekdays: %v", err)
					}
					rule["weekdays"] = days
				}
				if *tz != "" {
					rule["tz"] = *tz
				}
				body["kind"] = "recurring"
				body["rule"] = rule
			} else if *weekdays != "" || *tz != "" {
				return usageErrf("--weekdays and --tz go with --times")
			}
			if len(channels.vals) > 0 {
				ids := make([]string, len(channels.vals))
				for i, ref := range channels.vals {
					if ids[i], err = resolveChannelID(c, cl, ref); err != nil {
						return err
					}
				}
				body["channels"] = ids
			}
			setIf(body, "pick", *pick)
			if len(posts.vals) > 0 {
				body["post_ids"] = posts.vals
				if *pick == "" {
					body["pick"] = "selected"
				}
			}
			setIf(body, "source", *source)
			setIf(body, "endpoint_url", *endpoint)
			setIf(body, "endpoint_secret", *endpointSecret)
			if *endpoint != "" && *source == "" {
				body["source"] = "endpoint"
			}
			if *enable {
				body["enabled"] = true
			}
			if *disable {
				body["enabled"] = false
			}
			var s Schedule
			var raw []byte
			if create {
				raw, err = cl.Post(c, "/v1/schedules", body, &s)
			} else {
				if len(body) == 0 {
					return usageErrf("nothing to update; pass at least one of --name, --channel, --at, --every, --times, --weekdays, --tz, --pick, --post, --source, --endpoint-url, --enable, --disable")
				}
				raw, err = cl.Patch(c, "/v1/schedules/"+url.PathEscape(a[0]), body, &s)
			}
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			verb := map[bool]string{true: "Created", false: "Updated"}[create]
			fmt.Fprintf(c.Out, "%s schedule %s (%s): %s", verb, c.paint(bold, s.Name), s.ID, describeSchedule(s))
			if s.NextRunAt != nil && *s.NextRunAt != "" {
				fmt.Fprintf(c.Out, "; next run %s", fmtOptTime(s.NextRunAt))
			}
			fmt.Fprintln(c.Out)
			if s.EndpointSecret != "" && !strings.Contains(s.EndpointSecret, "*") && *endpointSecret == "" {
				// Generated by the server and shown only in this response.
				fmt.Fprintf(c.Out, "Endpoint signing secret (copy it now, it is not shown again): %s\n", s.EndpointSecret)
			}
			return nil
		}
	})
	if create {
		cmd.Examples = []string{
			"staqly schedules create \"Blog posts\" --name \"Weekday mornings\" --times 09:00 --weekdays mon-fri --tz Europe/Warsaw --channel X,\"My pipeline\"",
			"staqly schedules create Announcements --name \"Every 6 hours\" --every 6h --channel \"My pipeline\" --pick next",
			"staqly schedules create Launch --name \"Launch day\" --at 2026-11-03T09:00:00Z --pick all",
			"staqly schedules create Digest --name \"Daily, ask Claude\" --times 08:00 --source agent   # schedule.posts_requested when nothing waits",
			"staqly schedules create News --name \"Fetch\" --every 1h --endpoint-url https://my.app/staqly/posts",
		}
		cmd.Long = `A schedule publishes a collection's waiting drafts: at one time (--at) or on a
cadence (--every, or --times on --weekdays in --tz). Each run picks the oldest
waiting draft (--pick next, the default), every waiting draft (all) or the posts
named with --post (selected), adds the schedule's --channel targets to posts
without their own, and publishes them.

When nothing waits, --source agent emits schedule.posts_requested and lists the
schedule under 'staqly work' so an agent can write new posts; --source endpoint
fetches posts from --endpoint-url instead (the request is signed like a webhook).`
	} else {
		cmd.Examples = []string{
			"staqly schedules update sch_oalirbz67y4m4cqxq5dw --times 09:00,17:30",
			"staqly schedules update sch_oalirbz67y4m4cqxq5dw --disable",
			"staqly schedules update sch_oalirbz67y4m4cqxq5dw --channel X --pick all",
		}
	}
	return cmd
}
