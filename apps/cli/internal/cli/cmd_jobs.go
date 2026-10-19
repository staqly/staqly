package cli

import (
	"flag"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

func jobsCommand() *Command {
	return &Command{
		Name: "jobs", Aliases: []string{"queue"},
		Short: "The background queue: publishing, schedule runs and sign-in refreshes",
		Subs: []*Command{
			leaf("list", "", "List jobs, newest first", 0, 0, func(fs *flag.FlagSet) runFunc {
				status := fs.String("status", "", "queued, running, done, failed or cancelled")
				kind := fs.String("kind", "", "publish_post, publish_target, target_timeout, run_schedule or refresh_channel")
				limit := fs.Int("limit", 50, "page size (max 200)")
				cursor := fs.String("cursor", "", "continue from a previous page's next_cursor")
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					q := url.Values{}
					setQ(q, "status", *status)
					setQ(q, "kind", *kind)
					setQ(q, "cursor", *cursor)
					if *limit > 0 {
						q.Set("limit", strconv.Itoa(*limit))
					}
					var page JobPage
					raw, err := cl.Get(c, "/v1/jobs", q, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "KIND", "STATUS", "TRIES", "RUN AT", "SUBJECT", "ERROR")
					for _, j := range page.Data {
						t.row(j.ID, j.Kind, c.status(j.Status), fmt.Sprintf("%d/%d", j.Attempts, j.MaxAttempts), fmtTime(j.RunAt),
							firstNonEmpty(j.Payload.PostID, j.Payload.ScheduleID, j.Payload.ChannelID), truncate(orDash(j.LastError), 40))
					}
					t.flush("The queue is empty. Jobs appear when a post is scheduled or published.")
					if page.NextCursor != "" {
						fmt.Fprintf(c.Stderr, "%s\n", c.paint(dim, "More jobs: --cursor "+page.NextCursor))
					}
					return nil
				}
			}, "staqly jobs list", "staqly jobs list --status failed", "staqly jobs list --kind run_schedule --json"),
			leaf("stats", "", "Queue counts", 0, 0, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var s JobStats
					raw, err := cl.Get(c, "/v1/jobs/stats", nil, &s)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					c.kv("Queued", strconv.Itoa(s.Queued), "Running", strconv.Itoa(s.Running), "Failed", strconv.Itoa(s.Failed), "Done (24 h)", strconv.Itoa(s.Done24h))
					if len(s.ByKind) > 0 {
						fmt.Fprintln(c.Out)
						kinds := make([]string, 0, len(s.ByKind))
						for k := range s.ByKind {
							kinds = append(kinds, k)
						}
						sort.Strings(kinds)
						t := c.table("KIND", "QUEUED", "RUNNING", "FAILED")
						for _, k := range kinds {
							v := s.ByKind[k]
							t.row(k, strconv.Itoa(v.Queued), strconv.Itoa(v.Running), strconv.Itoa(v.Failed))
						}
						t.flush("")
					}
					return nil
				}
			}, "staqly jobs stats"),
			leaf("get", "<id>", "Show a job", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var j Job
					raw, err := cl.Get(c, "/v1/jobs/"+url.PathEscape(args[0]), nil, &j)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					c.kv("ID", j.ID, "Kind", j.Kind, "Key", j.Key, "Status", c.status(j.Status), "Tries", fmt.Sprintf("%d of %d", j.Attempts, j.MaxAttempts),
						"Run at", fmtTime(j.RunAt), "Running on", j.LeasedBy, "Post", j.Payload.PostID, "Channel", j.Payload.ChannelID, "Schedule", j.Payload.ScheduleID,
						"Last error", j.LastError, "Done", fmtOptTime(j.DoneAt), "Created", fmtTime(j.CreatedAt))
					return nil
				}
			}, "staqly jobs get job_oalirbz67y4m4cqxq5dw"),
			jobAction("retry", "Queue a failed job again, now", "Queued %s again"),
			jobAction("cancel", "Cancel a queued job", "Cancelled %s"),
		},
	}
}

func jobAction(name, short, msg string) *Command {
	return leaf(name, "<id>", short, 1, 1, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, args []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var j Job
			raw, err := cl.Post(c, "/v1/jobs/"+url.PathEscape(args[0])+"/"+name, nil, &j)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			fmt.Fprintf(c.Out, msg+" (%s %s)\n", c.paint(bold, j.ID), j.Kind, c.status(j.Status))
			return nil
		}
	}, "staqly jobs "+name+" job_oalirbz67y4m4cqxq5dw")
}

func workCommand() *Command {
	cmd := leaf("work", "", "What the workspace asks your automation for", 0, 0, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, _ []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var page struct{ Data []WorkItem }
			raw, err := cl.Get(c, "/v1/work", nil, &page)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			t := c.table("KIND", "SINCE", "COLLECTION", "SUBJECT", "NEXT STEP")
			for _, w := range page.Data {
				coll := ""
				if w.Collection != nil {
					coll = w.Collection.Name
				}
				switch w.Kind {
				case "target":
					post, ch := "", ""
					if w.Post != nil {
						post = w.Post.ID
					}
					if w.Channel != nil {
						ch = w.Channel.ID
					}
					t.row("publish", fmtTime(w.RequestedAt), truncate(coll, 20), post+" on "+ch,
						fmt.Sprintf("staqly posts result %s %s --status published --url ...", post, ch))
				default:
					sched := ""
					if w.Schedule != nil {
						sched = w.Schedule.Name + " (" + w.Schedule.ID + ")"
					}
					t.row("new posts", fmtTime(w.RequestedAt), truncate(coll, 20), truncate(sched, 40),
						fmt.Sprintf("staqly posts create %q --content ...", coll))
				}
			}
			t.flush("Nothing is waiting for you.")
			return nil
		}
	}, "staqly work", "staqly work --json | jq '.data[] | select(.kind==\"target\")'")
	cmd.Long = `Lists what your own automation owes the workspace: posts publishing on a channel
of provider custom that wait for 'staqly posts result', and schedules of
--source agent whose last run found nothing to publish and want new posts.`
	return cmd
}

var _ = strings.TrimSpace
