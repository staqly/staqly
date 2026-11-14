package cli

import (
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Scheduled publishing: a post's publish_at is when it goes out. `staqly schedule` is the
// terminal view of that calendar; --publish-at on posts create / update and `posts schedule` set it.

// parseWhen turns a user time into RFC3339 UTC for the API: RFC3339 (any zone), a local
// "2006-01-02T15:04" or "2006-01-02 15:04", a day "2006-01-02" (local midnight), or a relative
// "+2h", "+3d", "+1w" (hours, minutes, days, weeks from now).
func parseWhen(v string) (string, error) {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "+") {
		d, err := parseRelative(v[1:])
		if err != nil {
			return "", err
		}
		return time.Now().Add(d).UTC().Format(time.RFC3339), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return t.UTC().Format(time.RFC3339), nil
		}
	}
	return "", fmt.Errorf("%q is not a time; use RFC3339 (2026-11-03T09:00:00Z), a local day or time (2026-11-03, 2026-11-03 09:00) or a relative +2h, +3d, +1w", v)
}

// parseRelative reads 90m, 2h, 3d or 1w.
func parseRelative(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty relative time")
	}
	unit := s[len(s)-1]
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("relative time must be a positive number and a unit: +90m, +2h, +3d, +1w")
	}
	switch unit {
	case 'm':
		return time.Duration(n) * time.Minute, nil
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	}
	return 0, fmt.Errorf("unknown unit %q in relative time; use m, h, d or w", string(unit))
}

func scheduleCommand() *Command {
	cmd := leaf("schedule", "", "Show the publishing calendar: posts scheduled to go out", 0, 0, func(fs *flag.FlagSet) runFunc {
		coll := fs.String("collection", "", "only this collection (ID or name)")
		days := fs.Int("days", 7, "how many days ahead to show")
		all := fs.Bool("all", false, "every post with a publish time, past and future, in any status")
		limit := fs.Int("limit", 200, "maximum posts to show")
		return func(c *Ctx, _ []string) error {
			if *days <= 0 {
				return usageErrf("--days must be positive")
			}
			cl, err := c.Client()
			if err != nil {
				return err
			}
			q := url.Values{"sort": {"publish_asc"}, "limit": {strconv.Itoa(min(max(*limit, 1), 200))}}
			now := c.Env.now().UTC()
			if *all {
				q.Set("publish_after", "1970-01-01")
			} else {
				// Waiting to go out (overdue ones included, they start on the next tick).
				q.Set("status", "scheduled")
				q.Set("publish_after", "1970-01-01")
				q.Set("publish_before", now.Add(time.Duration(*days)*24*time.Hour).Format(time.RFC3339))
			}
			setQ(q, "collection_id", *coll)
			var page PostPage
			raw, err := cl.Get(c, "/v1/posts", q, &page)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			t := c.table("PUBLISH AT", "ID", "STATUS", "COLLECTION", "CHANNELS", "CONTENT")
			for _, p := range page.Data {
				t.row(fmtOptTime(p.PublishAt), p.ID, c.status(p.Status), truncate(firstNonEmpty(p.CollectionName, p.CollectionID), 24),
					truncate(targetNames(p), 30), truncate(firstLine(p.Content), 50))
			}
			empty := fmt.Sprintf("Nothing scheduled in the next %d days.", *days)
			if *all {
				empty = "No scheduled posts."
			}
			t.flush(empty)
			if page.NextCursor != "" {
				fmt.Fprintf(c.Stderr, "%s\n", c.paint(dim, "More scheduled posts: raise --limit or use `staqly posts list --publish-after ... --all`"))
			}
			return nil
		}
	}, "staqly schedule", "staqly schedule --days 30 --collection \"Blog posts\"", "staqly schedule --all --json")
	cmd.Long = `The publishing calendar: posts with a publish time, soonest first.

By default the next --days days (7) of scheduled posts. --all lists every post
with a publish time in any status, including ones already published or failed.

Set a time with --publish-at on 'posts create' or 'posts update', or with
'posts schedule <id> <when>' (RFC3339, a local day or time, or relative: +2h,
+3d, +1w); 'posts cancel' takes a post off the calendar. Recurring publishing
lives in 'staqly schedules'.`
	return cmd
}
