package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func postsCommand() *Command {
	return &Command{
		Name: "posts", Aliases: []string{"post"},
		Short: "Write, schedule, publish and inspect posts",
		Subs: []*Command{
			postsListCommand(),
			leaf("get", "<id>", "Show a post, its targets and content", 1, 1, func(fs *flag.FlagSet) runFunc {
				return runPostGet
			}, "staqly posts get pst_123", "staqly posts get pst_123 --json | jq .targets"),
			postsCreateCommand(),
			postsUpdateCommand(),
			postsScheduleCommand(),
			postsPublishCommand(),
			postsSimpleCommand("cancel", "Take a scheduled post off the schedule; it becomes a draft", "/cancel", "Cancelled %s, now a draft"),
			postsSimpleCommand("duplicate", "Copy a post into a new draft", "/duplicate", "Created draft %s"),
			postsRetryCommand(),
			postsResultCommand(),
			postsPreviewCommand(),
			postsStatsCommand(),
			postsShareCommand(),
			postsCommentsCommand(),
			postsHistoryCommand(),
			postsDeleteCommand(),
			postsPurgeCommand(),
			postsTagsCommand(),
		},
	}
}

// ---- list ----

func postsListCommand() *Command {
	return leaf("list", "", "List or search posts across collections", 0, 0, func(fs *flag.FlagSet) runFunc {
		coll := fs.String("collection", "", "collection ID or name")
		status := fs.String("status", "", "draft, scheduled, publishing, published or failed")
		channel := fs.String("channel", "", "posts with a target on this channel (ID or name)")
		tag := fs.String("tag", "", "posts carrying this tag")
		q := fs.String("q", "", "words of the content")
		sortBy := fs.String("sort", "", "publish_asc (default), publish_desc, created_desc, created_asc, updated_desc")
		publishAfter := fs.String("publish-after", "", "publish time >= (RFC3339, YYYY-MM-DD or a relative +2h / +3d); posts without a time never match")
		publishBefore := fs.String("publish-before", "", "publish time < (RFC3339, YYYY-MM-DD or relative)")
		limit := fs.Int("limit", 50, "page size (max 200)")
		cursor := fs.String("cursor", "", "continue from a previous page's next_cursor")
		all := fs.Bool("all", false, "follow cursors and fetch every page")
		return func(c *Ctx, _ []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			qv := url.Values{}
			setQ(qv, "collection_id", *coll)
			setQ(qv, "status", *status)
			setQ(qv, "tag", *tag)
			setQ(qv, "q", *q)
			setQ(qv, "sort", *sortBy)
			setQ(qv, "cursor", *cursor)
			if *channel != "" {
				id, err := resolveChannelID(c, cl, *channel)
				if err != nil {
					return err
				}
				qv.Set("channel_id", id)
			}
			for name, v := range map[string]*string{"publish_after": publishAfter, "publish_before": publishBefore} {
				if *v == "" {
					continue
				}
				t, err := parseWhen(*v)
				if err != nil {
					return usageErrf("--%s: %v", strings.ReplaceAll(name, "_", "-"), err)
				}
				qv.Set(name, t)
			}
			if *limit > 0 {
				qv.Set("limit", strconv.Itoa(*limit))
			}
			posts, next, raw, err := fetchPosts(c, cl, qv, *all)
			if err != nil {
				return err
			}
			if c.JSON {
				if *all {
					return c.printJSON(map[string]any{"data": posts})
				}
				return c.printRaw(raw)
			}
			renderPosts(c, posts)
			if next != "" {
				fmt.Fprintf(c.Stderr, "%s\n", c.paint(dim, "More posts available: add --all, or --cursor "+next))
			}
			return nil
		}
	}, "staqly posts list --status scheduled",
		"staqly posts list --collection \"Blog posts\" --q launch --limit 10",
		"staqly posts list --publish-after 2026-11-02 --publish-before 2026-11-09   # the publishing calendar",
		"staqly posts list --status draft --all --json | jq -r '.data[].id'")
}

func setQ(q url.Values, k, v string) {
	if v != "" {
		q.Set(k, v)
	}
}

// fetchPosts gets one page, or every page when all is set.
func fetchPosts(c *Ctx, cl *Client, q url.Values, all bool) ([]Post, string, json.RawMessage, error) {
	var out []Post
	for {
		var page PostPage
		raw, err := cl.Get(c, "/v1/posts", q, &page)
		if err != nil {
			return nil, "", nil, err
		}
		out = append(out, page.Data...)
		if !all || page.NextCursor == "" {
			return out, page.NextCursor, raw, nil
		}
		q.Set("cursor", page.NextCursor)
	}
}

func renderPosts(c *Ctx, posts []Post) {
	t := c.table("ID", "STATUS", "PUBLISH AT", "COLLECTION", "CHANNELS", "CONTENT")
	for _, p := range posts {
		t.row(p.ID, c.status(p.Status), fmtOptTime(p.PublishAt), truncate(firstNonEmpty(p.CollectionName, p.CollectionID), 24),
			truncate(targetNames(p), 30), truncate(firstLine(p.Content), 60))
	}
	t.flush("No posts. Create one with: staqly posts create <collection> --content \"...\" --channel <channel>")
}

func targetNames(p Post) string {
	names := make([]string, len(p.Targets))
	for i, tg := range p.Targets {
		names[i] = firstNonEmpty(tg.ChannelName, tg.ChannelID)
	}
	return strings.Join(names, ", ")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// ---- get ----

func runPostGet(c *Ctx, args []string) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	var p Post
	raw, err := cl.Get(c, "/v1/posts/"+url.PathEscape(args[0]), nil, &p)
	if err != nil || c.JSON {
		return jsonOr(c, raw, err)
	}
	printPost(c, p)
	return nil
}

func printPost(c *Ctx, p Post) {
	media := make([]string, len(p.MediaItems))
	for i, m := range p.MediaItems {
		media[i] = fmt.Sprintf("%s (%s, %s)", m.ID, m.MimeType, humanBytes(m.SizeBytes))
	}
	if len(media) == 0 {
		for _, id := range p.Media {
			media = append(media, id)
		}
	}
	c.kv("ID", p.ID, "Status", c.status(p.Status), "Collection", firstNonEmpty(p.CollectionName, p.CollectionID),
		"Publish at", fmtOptTime(p.PublishAt), "Published", fmtOptTime(p.PublishedAt),
		"Tags", strings.Join(p.Tags, ", "), "Media", strings.Join(media, ", "),
		"Repeats", repeatLabel(p.RepeatEvery), "Source", p.Source, "External ref", p.ExternalRef, "Version", strconv.Itoa(p.Version),
		"Created", fmtTime(p.CreatedAt))
	if len(p.Targets) > 0 {
		fmt.Fprintln(c.Out)
		t := c.table("CHANNEL", "PROVIDER", "STATUS", "TRIES", "URL", "ERROR")
		for _, tg := range p.Targets {
			t.row(firstNonEmpty(tg.ChannelName, tg.ChannelID), tg.Provider, c.status(tg.Status), strconv.Itoa(tg.Attempts),
				orDash(tg.ExternalURL), truncate(orDash(tg.Error), 50))
		}
		t.flush("")
	}
	if len(p.Thread) > 0 {
		fmt.Fprintf(c.Out, "\nThread (%s):\n", plural(len(p.Thread), "reply", "replies"))
		for i, part := range p.Thread {
			wait := ""
			if part.DelaySeconds > 0 {
				wait = fmt.Sprintf(" (after %s)", time.Duration(part.DelaySeconds)*time.Second)
			}
			fmt.Fprintf(c.Out, "  %d.%s %s\n", i+1, wait, truncate(clean(part.Content), 80))
			for _, tg := range p.Targets {
				if i < len(tg.Replies) {
					r := tg.Replies[i]
					fmt.Fprintf(c.Out, "     %s: %s %s\n", firstNonEmpty(tg.ChannelName, tg.ChannelID), c.status(r.Status), firstNonEmpty(r.ExternalURL, r.Error))
				}
			}
		}
	}
	if len(p.Metadata) > 0 {
		b, _ := json.MarshalIndent(p.Metadata, "", "  ")
		fmt.Fprintf(c.Out, "\nMetadata:\n%s\n", indent(string(safeJSON(b)), "  "))
	}
	if p.Content != "" {
		fmt.Fprintf(c.Out, "\nContent:\n%s\n", indent(clean(p.Content), "  "))
	}
}

var repeatLabels = map[string]string{"1d": "every day", "2d": "every 2 days", "3d": "every 3 days", "4d": "every 4 days",
	"5d": "every 5 days", "6d": "every 6 days", "1w": "every week", "2w": "every 2 weeks", "1mo": "every month"}

// repeatLabel words a repeat_every value; "no" when the post does not repeat.
func repeatLabel(v string) string {
	if l, ok := repeatLabels[v]; ok {
		return l
	}
	return "no"
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// ---- create ----

// postFlags are the fields create and update share.
type postFlags struct {
	content, metadata, extRef, publishAt, repeat *string
	stdin                                        *bool
	media, channels, tags                        *multiFlag
	meta                                         multiFlag
	files                                        multiFlag
	replies                                      *replyFlags
}

// replyFlags collects --reply parts in order; --reply-delay sets the wait for the --reply that follows it.
type replyFlags struct {
	parts []map[string]any
	delay int64
}

type replyText struct{ r *replyFlags }

func (v replyText) String() string { return "" }
func (v replyText) Set(s string) error {
	v.r.parts = append(v.r.parts, map[string]any{"content": s, "delay_seconds": v.r.delay})
	v.r.delay = 0
	return nil
}

type replyDelay struct{ r *replyFlags }

func (v replyDelay) String() string { return "" }
func (v replyDelay) Set(s string) error {
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 || d > 24*time.Hour {
		return fmt.Errorf("use a duration from 0s to 24h, e.g. 5m")
	}
	v.r.delay = int64(d / time.Second)
	return nil
}

func addPostFlags(fs *flag.FlagSet) *postFlags {
	pf := &postFlags{
		content:   fs.String("content", "", "the text; '-' reads stdin, '@path' reads a file"),
		stdin:     fs.Bool("stdin", false, "read the content from stdin"),
		metadata:  fs.String("metadata", "", "metadata as a JSON object"),
		extRef:    fs.String("external-ref", "", "your own reference (e.g. a ticket number)"),
		publishAt: fs.String("publish-at", "", "when it goes out: RFC3339, YYYY-MM-DD, a local time or relative (+2h, +3d)"),
		repeat:    fs.String("repeat", "", "after each publish, schedule a copy this much later: 1d..6d, 1w, 2w, 1mo (none stops it)"),
		media:     &multiFlag{comma: true},
		channels:  &multiFlag{comma: true},
		tags:      &multiFlag{comma: true},
		replies:   &replyFlags{},
	}
	fs.Var(replyText{pf.replies}, "reply", "a reply published under the post, in order (repeatable; a thread)")
	fs.Var(replyDelay{pf.replies}, "reply-delay", "how long the next --reply waits after the part before it, e.g. 5m (at most 24h)")
	fs.Var(pf.media, "media", "media `ids` from `staqly media upload` (comma-separated or repeated)")
	fs.Var(&pf.files, "file", "upload this `file` and attach it (repeatable)")
	fs.Var(pf.channels, "channel", "`channels` the post goes to, ID or name (comma-separated or repeated)")
	fs.Var(pf.tags, "tag", "`tags` (comma-separated or repeated)")
	fs.Var(&pf.meta, "meta", "metadata `key=value` (repeatable; values that parse as JSON are typed)")
	return pf
}

// body fills the shared fields; update says whether an empty flag means "leave it".
func (pf *postFlags) body(c *Ctx, cl *Client, body map[string]any) error {
	if *pf.content != "" || *pf.stdin {
		s, err := readContent(c, *pf.content, *pf.stdin)
		if err != nil {
			return err
		}
		body["content"] = s
	}
	if *pf.metadata != "" || len(pf.meta.vals) > 0 {
		m := map[string]any{}
		if *pf.metadata != "" {
			var err error
			if m, err = readJSONObjectArg(c, "metadata", *pf.metadata); err != nil {
				return err
			}
		}
		for _, kv := range pf.meta.vals {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || k == "" {
				return usageErrf("--meta expects key=value, got %q", kv)
			}
			m[k] = scalarOrJSON(v)
		}
		body["metadata"] = m
	}
	setIf(body, "external_ref", *pf.extRef)
	if len(pf.replies.parts) > 0 {
		body["thread"] = pf.replies.parts
	}
	if *pf.repeat != "" {
		v := strings.ToLower(strings.TrimSpace(*pf.repeat))
		if v == "none" || v == "off" {
			v = ""
		} else if _, ok := repeatLabels[v]; !ok {
			return usageErrf("--repeat: use 1d, 2d, 3d, 4d, 5d, 6d, 1w, 2w, 1mo or none")
		}
		body["repeat_every"] = v
	}
	if *pf.publishAt != "" {
		t, err := parseWhen(*pf.publishAt)
		if err != nil {
			return usageErrf("--publish-at: %v", err)
		}
		body["publish_at"] = t
	}
	if len(pf.files.vals) > 0 {
		for _, f := range pf.files.vals {
			m, err := uploadMedia(c, cl, f)
			if err != nil {
				return err
			}
			pf.media.vals = append(pf.media.vals, m.ID)
		}
	}
	if len(pf.media.vals) > 0 || len(pf.files.vals) > 0 {
		body["media"] = pf.media.vals
	}
	if len(pf.tags.vals) > 0 {
		body["tags"] = pf.tags.vals
	}
	if len(pf.channels.vals) > 0 {
		targets := make([]map[string]any, 0, len(pf.channels.vals))
		for _, ref := range pf.channels.vals {
			id, err := resolveChannelID(c, cl, ref)
			if err != nil {
				return err
			}
			targets = append(targets, map[string]any{"channel_id": id})
		}
		body["targets"] = targets
	}
	return nil
}

func postsCreateCommand() *Command {
	cmd := leaf("create", "<collection>", "Create a post: a draft, scheduled with --publish-at, or published with --now", 1, 1, func(fs *flag.FlagSet) runFunc {
		pf := addPostFlags(fs)
		now := fs.Bool("now", false, "publish right away (needs --channel)")
		source := fs.String("source", "cli", "who created it, shown in the dashboard")
		idem := fs.String("idempotency-key", "", "make retries safe: the same key returns the same post")
		wait := fs.Bool("wait", false, "with --now or --publish-at: wait until the post is published or failed")
		timeout := fs.Duration("timeout", 10*time.Minute, "how long --wait waits")
		return func(c *Ctx, args []string) error {
			if *now && *pf.publishAt != "" {
				return usageErrf("--now and --publish-at cannot be combined")
			}
			cl, err := c.Client()
			if err != nil {
				return err
			}
			body := map[string]any{"collection": args[0]}
			if err := pf.body(c, cl, body); err != nil {
				return err
			}
			setIf(body, "source", *source)
			if *now {
				body["publish_now"] = true
			} else if *pf.publishAt != "" {
				body["status"] = "scheduled"
			}
			var p Post
			raw, err := cl.call(c, Request{Method: http.MethodPost, Path: "/v1/posts", Body: body, IdempotencyKey: firstNonEmpty(*idem, newIdempotencyKey())}, &p)
			if err != nil {
				return err
			}
			if *wait && (p.Status == "publishing" || p.Status == "scheduled") {
				return waitAndReport(c, cl, p, *timeout)
			}
			if c.JSON {
				return c.printRaw(raw)
			}
			describePost(c, "Created", p)
			return nil
		}
	}, "staqly posts create \"Blog posts\" --content \"Launch day!\" --channel X --publish-at 2026-11-03T09:00:00Z",
		"staqly posts create Announcements --content @post.md --file cover.png --channel x,\"My pipeline\" --now --wait",
		"cat post.txt | staqly posts create Drafts --stdin --tag launch --meta campaign=q4",
		"staqly posts create Tips --content @tip.md --channel X --publish-at \"2026-11-03 09:00\" --repeat 1w",
		"staqly posts create Drafts --content \"Idea\" --json | jq -r .id")
	cmd.Long = `Create a post in a collection (ID or name). Without --publish-at or --now it is a
draft. --publish-at schedules it (needs at least one --channel); --now publishes
it right away. Each --channel becomes a target; the dashboard or 'posts update'
can give a target its own text and provider settings.

--wait follows a scheduled or publishing post until it is published (exit 0) or
failed (exit 2); exit 3 when --timeout passes first.`
	return cmd
}

func describePost(c *Ctx, verb string, p Post) {
	switch p.Status {
	case "scheduled":
		fmt.Fprintf(c.Out, "%s %s, scheduled for %s\n", verb, c.paint(bold, p.ID), fmtOptTime(p.PublishAt))
	case "publishing":
		fmt.Fprintf(c.Out, "%s %s, publishing to %s\n", verb, c.paint(bold, p.ID), targetNames(p))
	default:
		fmt.Fprintf(c.Out, "%s %s (%s)\n", verb, c.paint(bold, p.ID), p.Status)
	}
}

func readContent(c *Ctx, content string, stdin bool) (string, error) {
	switch {
	case stdin || content == "-":
		b, err := io.ReadAll(io.LimitReader(c.Env.Stdin, 16<<20))
		return string(b), err
	case strings.HasPrefix(content, "@"):
		b, err := os.ReadFile(content[1:])
		if err != nil {
			return "", fmt.Errorf("read content file: %w", err)
		}
		return string(b), nil
	}
	return content, nil
}

// uploadMedia streams a file to POST /v1/media.
func uploadMedia(c *Ctx, cl *Client, file string) (Media, error) {
	f, err := os.Open(file)
	if err != nil {
		return Media{}, fmt.Errorf("open --file: %w", err)
	}
	defer f.Close()
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		err := func() error {
			part, err := mw.CreateFormFile("file", filepath.Base(file))
			if err != nil {
				return err
			}
			if _, err := io.Copy(part, f); err != nil {
				return err
			}
			return mw.Close()
		}()
		pw.CloseWithError(err)
	}()
	var m Media
	_, err = cl.call(c, Request{Method: http.MethodPost, Path: "/v1/media", Body: io.Reader(pr), ContentType: mw.FormDataContentType()}, &m)
	return m, err
}

// resolveChannelID turns a channel name (case-insensitive) or ID into an ID.
func resolveChannelID(c *Ctx, cl *Client, ref string) (string, error) {
	if strings.HasPrefix(ref, "ch_") {
		return ref, nil
	}
	var page struct{ Data []Channel }
	if _, err := cl.Get(c, "/v1/channels", nil, &page); err != nil {
		return "", err
	}
	for _, ch := range page.Data {
		if strings.EqualFold(ch.Name, ref) || strings.EqualFold(ch.Handle, ref) {
			return ch.ID, nil
		}
	}
	return "", &exitErr{code: ExitError, msg: fmt.Sprintf("no channel named %q; see staqly channels list", ref)}
}

// ---- update ----

func postsUpdateCommand() *Command {
	cmd := leaf("update", "<id>", "Edit a draft, scheduled or failed post", 1, 1, func(fs *flag.FlagSet) runFunc {
		pf := addPostFlags(fs)
		coll := fs.String("collection", "", "move the post to this collection (ID or name)")
		clearPublish := fs.Bool("clear-publish-at", false, "drop the publish time (a scheduled post becomes a draft)")
		clearTargets := fs.Bool("no-channels", false, "remove every target")
		clearReplies := fs.Bool("no-replies", false, "remove the thread (every reply)")
		version := fs.Int("expected-version", 0, "fail with 409 unless the post is at this version")
		return func(c *Ctx, args []string) error {
			if *clearPublish && *pf.publishAt != "" {
				return usageErrf("--clear-publish-at and --publish-at cannot be combined")
			}
			cl, err := c.Client()
			if err != nil {
				return err
			}
			body := map[string]any{}
			if err := pf.body(c, cl, body); err != nil {
				return err
			}
			setIf(body, "collection", *coll)
			if *clearPublish {
				body["publish_at"] = nil
			}
			if *clearTargets {
				body["targets"] = []any{}
			}
			if *clearReplies {
				body["thread"] = []any{}
			}
			if *version > 0 {
				body["expected_version"] = *version
			}
			if len(body) == 0 {
				return usageErrf("nothing to update; pass at least one of --content, --media, --file, --channel, --tag, --metadata, --meta, --external-ref, --collection, --publish-at, --clear-publish-at, --no-channels, --repeat, --reply, --no-replies")
			}
			var p Post
			raw, err := cl.Patch(c, "/v1/posts/"+url.PathEscape(args[0]), body, &p)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			describePost(c, "Updated", p)
			return nil
		}
	}, "staqly posts update pst_123 --content @post.md",
		"staqly posts update pst_123 --channel X,\"My pipeline\" --publish-at +2h",
		"staqly posts update pst_123 --clear-publish-at",
		"staqly posts update pst_123 --tag launch --meta reviewed=true --expected-version 3")
	cmd.Long = `Edit a post. Fields left out stay as they are; --channel, --media and --tag replace
the whole list. A published or publishing post cannot be edited (409).`
	return cmd
}

// ---- schedule / publish / cancel / duplicate ----

func postsScheduleCommand() *Command {
	return leaf("schedule", "<id> <when>", "Set the publish time and make the post scheduled", 2, 2, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, args []string) error {
			when, err := parseWhen(args[1])
			if err != nil {
				return usageErrf("when: %v", err)
			}
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var p Post
			raw, err := cl.Post(c, "/v1/posts/"+url.PathEscape(args[0])+"/schedule", map[string]any{"publish_at": when}, &p)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			describePost(c, "Scheduled", p)
			return nil
		}
	}, "staqly posts schedule pst_123 2026-11-03T09:00:00Z", "staqly posts schedule pst_123 +2h", "staqly posts schedule pst_123 \"2026-11-03 09:00\"")
}

func postsPublishCommand() *Command {
	cmd := leaf("publish", "<id>", "Publish a post now", 1, 1, func(fs *flag.FlagSet) runFunc {
		wait := fs.Bool("wait", false, "wait until the post is published or failed")
		timeout := fs.Duration("timeout", 10*time.Minute, "how long --wait waits")
		return func(c *Ctx, args []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var p Post
			raw, err := cl.Post(c, "/v1/posts/"+url.PathEscape(args[0])+"/publish", nil, &p)
			if err != nil {
				return err
			}
			if *wait {
				return waitAndReport(c, cl, p, *timeout)
			}
			if c.JSON {
				return c.printRaw(raw)
			}
			describePost(c, "Publishing", p)
			return nil
		}
	}, "staqly posts publish pst_123", "staqly posts publish pst_123 --wait --timeout 5m")
	cmd.Long = `Publish a draft, scheduled or failed post now. A job publishes each target; the
post ends published when every target is, failed when one is not ('posts retry'
tries a failed target again). Targets on your own automation wait for
'posts result'.

--wait polls the post until it settles: exit 0 published, 2 failed, 3 timed out.`
	return cmd
}

func postsSimpleCommand(name, short, suffix, msg string) *Command {
	return leaf(name, "<id>", short, 1, 1, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, args []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var p Post
			raw, err := cl.Post(c, "/v1/posts/"+url.PathEscape(args[0])+suffix, nil, &p)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			fmt.Fprintf(c.Out, msg+"\n", c.paint(bold, p.ID))
			return nil
		}
	}, "staqly posts "+name+" pst_123")
}

func postsRetryCommand() *Command {
	return leaf("retry", "<id> <channel>", "Try a failed target once more, now", 2, 2, func(fs *flag.FlagSet) runFunc {
		wait := fs.Bool("wait", false, "wait until the post is published or failed")
		timeout := fs.Duration("timeout", 10*time.Minute, "how long --wait waits")
		return func(c *Ctx, args []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			ch, err := resolveChannelID(c, cl, args[1])
			if err != nil {
				return err
			}
			var p Post
			raw, err := cl.Post(c, "/v1/posts/"+url.PathEscape(args[0])+"/targets/"+url.PathEscape(ch)+"/retry", nil, &p)
			if err != nil {
				return err
			}
			if *wait {
				return waitAndReport(c, cl, p, *timeout)
			}
			if c.JSON {
				return c.printRaw(raw)
			}
			describePost(c, "Retrying", p)
			return nil
		}
	}, "staqly posts retry pst_123 ch_456", "staqly posts retry pst_123 X --wait")
}

// ---- result (custom provider) ----

func postsResultCommand() *Command {
	cmd := leaf("result", "<id> <channel>", "Report what your own automation did with a target", 2, 2, func(fs *flag.FlagSet) runFunc {
		status := fs.String("status", "", "published or failed (required)")
		link := fs.String("url", "", "where the post lives now")
		extID := fs.String("external-id", "", "the platform's id of the post")
		errMsg := fs.String("error", "", "why it failed")
		return func(c *Ctx, args []string) error {
			if *status != "published" && *status != "failed" {
				return usageErrf("--status must be published or failed")
			}
			cl, err := c.Client()
			if err != nil {
				return err
			}
			ch, err := resolveChannelID(c, cl, args[1])
			if err != nil {
				return err
			}
			body := map[string]any{"status": *status}
			setIf(body, "url", *link)
			setIf(body, "external_id", *extID)
			setIf(body, "error", *errMsg)
			var p Post
			raw, err := cl.Post(c, "/v1/posts/"+url.PathEscape(args[0])+"/targets/"+url.PathEscape(ch)+"/result", body, &p)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			fmt.Fprintf(c.Out, "Recorded %s for %s on %s; the post is %s\n", *status, p.ID, args[1], c.status(p.Status))
			return nil
		}
	}, "staqly posts result pst_123 ch_456 --status published --url https://example.com/p/1",
		"staqly posts result pst_123 \"My pipeline\" --status failed --error \"API 500\"")
	cmd.Long = `Close a target on a channel of your own automation (provider custom). When such a
post publishes, staqly emits post.publish_requested and lists the target under
'staqly work' until you report published or failed here (or the collection's
processing timeout passes and it fails).`
	return cmd
}

// ---- preview ----

func postsPreviewCommand() *Command {
	return leaf("preview", "<id>", "Show the text each target will carry (links removed or defanged per channel) and what its provider objects to", 1, 1, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, args []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var pv PostPreview
			raw, err := cl.Get(c, "/v1/posts/"+url.PathEscape(args[0])+"/preview", nil, &pv)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			t := c.table("CHANNEL", "OK", "LINKS", "WARNINGS", "TEXT")
			for _, tg := range pv.Targets {
				t.row(tg.ChannelID, yesNo(tg.OK), orDash(tg.Links), truncate(orDash(strings.Join(tg.Warnings, "; ")), 50), truncate(firstLine(tg.Text), 60))
			}
			t.flush("No targets.")
			return nil
		}
	}, "staqly posts preview pst_123")
}

// ---- stats, share, comments ----

type postMetric struct {
	ChannelID   string           `json:"channel_id"`
	ChannelName string           `json:"channel_name"`
	Available   bool             `json:"available"`
	Metrics     map[string]int64 `json:"metrics"`
	UpdatedAt   *string          `json:"updated_at"`
	Error       string           `json:"error"`
}

func postsStatsCommand() *Command {
	return leaf("stats", "<id>", "Show a post's numbers per channel (views, likes, reposts...) where the platform reports them", 1, 1, func(fs *flag.FlagSet) runFunc {
		refresh := fs.Bool("refresh", false, "read the numbers from the platforms now")
		return func(c *Ctx, args []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			q := url.Values{}
			if *refresh {
				q.Set("refresh", "true")
			}
			var res struct{ Data []postMetric }
			raw, err := cl.Get(c, "/v1/posts/"+url.PathEscape(args[0])+"/metrics", q, &res)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			t := c.table("CHANNEL", "VIEWS", "LIKES", "REPOSTS", "REPLIES", "UPDATED")
			num := func(m map[string]int64, k string) string {
				if v, ok := m[k]; ok {
					return strconv.FormatInt(v, 10)
				}
				return "-"
			}
			for _, m := range res.Data {
				if !m.Available {
					t.row(m.ChannelName, "not available for this platform", "", "", "", "")
					continue
				}
				updated := "-"
				if m.UpdatedAt != nil {
					updated = fmtTime(*m.UpdatedAt)
				}
				t.row(m.ChannelName, num(m.Metrics, "impressions"), num(m.Metrics, "likes"), num(m.Metrics, "reposts"), num(m.Metrics, "replies"), updated)
			}
			t.flush("No channels.")
			return nil
		}
	}, "staqly posts stats pst_123", "staqly posts stats pst_123 --refresh --json")
}

func postsShareCommand() *Command {
	return leaf("share", "<id>", "Make a preview link anyone can open and comment on, without signing in", 1, 1, func(fs *flag.FlagSet) runFunc {
		days := fs.Int("expires", 0, "days until the link stops working (0 never, up to 365)")
		return func(c *Ctx, args []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var l struct {
				ID  string `json:"id"`
				URL string `json:"url"`
			}
			raw, err := cl.Post(c, "/v1/posts/"+url.PathEscape(args[0])+"/previews", map[string]any{"expires_in_days": *days}, &l)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			fmt.Fprintf(c.Out, "Preview link %s (shown only now):\n\n  %s\n", l.ID, c.paint(bold, l.URL))
			return nil
		}
	}, "staqly posts share pst_123", "staqly posts share pst_123 --expires 7")
}

func postsCommentsCommand() *Command {
	return leaf("comments", "<id>", "Show what reviewers said on the post's preview pages", 1, 1, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, args []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var res struct {
				Data []struct {
					AuthorName  string `json:"author_name"`
					AuthorEmail string `json:"author_email"`
					Body        string `json:"body"`
					Quote       string `json:"quote"`
					CreatedAt   string `json:"created_at"`
				}
			}
			raw, err := cl.Get(c, "/v1/posts/"+url.PathEscape(args[0])+"/comments", nil, &res)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			t := c.table("TIME", "FROM", "COMMENT", "ON")
			for _, cm := range res.Data {
				from := cm.AuthorName
				if cm.AuthorEmail != "" {
					from += " <" + cm.AuthorEmail + ">"
				}
				t.row(fmtTime(cm.CreatedAt), from, truncate(firstLine(cm.Body), 60), truncate(orDash(cm.Quote), 30))
			}
			t.flush("No comments yet.")
			return nil
		}
	}, "staqly posts comments pst_123")
}

// ---- history ----

func postsHistoryCommand() *Command {
	return leaf("history", "<id>", "Show a post's history (oldest first)", 1, 1, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, args []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var page EventPage
			raw, err := cl.Get(c, "/v1/posts/"+url.PathEscape(args[0])+"/events", nil, &page)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			sort.SliceStable(page.Data, func(i, j int) bool { return page.Data[i].Seq < page.Data[j].Seq })
			t := c.table("TIME", "EVENT", "ACTOR", "DETAILS")
			for _, e := range page.Data {
				t.row(fmtTime(e.CreatedAt), e.Type, actorName(e.Actor), truncate(eventDetails(e), 70))
			}
			t.flush("No history.")
			return nil
		}
	}, "staqly posts history pst_123")
}

func actorName(a Actor) string {
	if a.Name != "" {
		return a.Name
	}
	return a.Type
}

// eventDetails summarizes the interesting parts of event data.
func eventDetails(e Event) string {
	var d map[string]any
	if json.Unmarshal(e.Data, &d) != nil {
		return ""
	}
	var parts []string
	for _, k := range []string{"status", "previous_status", "channel", "provider", "url", "error", "result", "note"} {
		if v, ok := d[k]; ok && v != nil && v != "" {
			if m, isMap := v.(map[string]any); isMap {
				v = firstNonEmpty(fmt.Sprint(m["name"]), fmt.Sprint(m["id"]))
			}
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
	}
	// Event data is decoded here, not by the client: clean it for display.
	return cleanLine(strings.Join(parts, " "))
}

func fmtOptTime(p *string) string {
	if p == nil || *p == "" {
		return ""
	}
	return fmtTime(*p)
}

// ---- delete / purge ----

func postsDeleteCommand() *Command {
	cmd := leaf("delete", "<id>...", "Delete one or more posts (their history is kept)", 1, -1, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, ids []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			if len(ids) == 1 {
				return simpleDelete(c, "/v1/posts/"+url.PathEscape(ids[0]), "post", ids[0])
			}
			var results []any
			failed := 0
			var lastErr error
			for _, id := range ids {
				err := cl.Delete(c, "/v1/posts/"+url.PathEscape(id), nil)
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
					fmt.Fprintf(c.Out, "Deleted post %s\n", id)
				}
			}
			if c.JSON {
				_ = c.printJSON(results)
			}
			return multiFailure(c, failed, len(ids), lastErr)
		}
	}, "staqly posts delete pst_123", "staqly posts delete pst_123 pst_456")
	cmd.Long = `Delete one or more posts. Their events stay in the activity log as history; use
'staqly posts purge' to remove those too. With an API key this needs the
posts:delete scope (posts:write alone is not enough). A publishing post cannot
be deleted (409).`
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

func postsPurgeCommand() *Command {
	cmd := leaf("purge", "<id>", "Delete a post and its whole history (admin)", 1, 1, func(fs *flag.FlagSet) runFunc {
		yes := fs.Bool("yes", false, "do not ask for confirmation")
		return func(c *Ctx, args []string) error {
			if !*yes {
				if !c.Env.StdinTTY {
					return usageErrf("refusing to purge %s without --yes (stdin is not a terminal)", args[0])
				}
				fmt.Fprintf(c.Stderr, "This deletes %s and every event about it. Continue? [y/N] ", args[0])
				var line string
				fmt.Fscanln(c.Env.Stdin, &line)
				if !strings.EqualFold(strings.TrimSpace(line), "y") {
					return &exitErr{code: ExitError, msg: "cancelled; nothing was deleted"}
				}
			}
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var res struct {
				PostID        string `json:"post_id"`
				EventsDeleted int    `json:"events_deleted"`
				MediaDeleted  int    `json:"media_deleted"`
			}
			raw, err := cl.Post(c, "/v1/posts/"+url.PathEscape(args[0])+"/purge", nil, &res)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			fmt.Fprintf(c.Out, "Purged %s: %d events and %d media files deleted\n", res.PostID, res.EventsDeleted, res.MediaDeleted)
			return nil
		}
	}, "staqly posts purge pst_123 --yes")
	cmd.Long = `Delete a post together with its events, their webhook deliveries and its media
when no other post uses them. Only a content-free post.purged event remains.
Needs the admin scope.`
	return cmd
}

// ---- tags ----

func postsTagsCommand() *Command {
	return leaf("tags", "", "List the workspace's tags, most used first", 0, 0, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, _ []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var page struct{ Data []Tag }
			raw, err := cl.Get(c, "/v1/tags", nil, &page)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			t := c.table("TAG", "POSTS")
			for _, tg := range page.Data {
				t.row(tg.Name, strconv.Itoa(tg.Count))
			}
			t.flush("No tags yet.")
			return nil
		}
	}, "staqly posts tags")
}

// ---- wait ----

// waitForPublished polls GET /v1/posts/{id} until the post is published or
// failed, or the timeout passes. Transient errors (network, 5xx, 429) are
// retried with backoff.
func waitForPublished(c *Ctx, cl *Client, id string, timeout time.Duration) (Post, json.RawMessage, bool, error) {
	deadline := c.Env.now().Add(timeout)
	delay := 2 * time.Second
	var last Post
	var lastRaw json.RawMessage
	for {
		var p Post
		raw, err := cl.Get(c, "/v1/posts/"+url.PathEscape(id), nil, &p)
		switch {
		case err == nil:
			last, lastRaw = p, raw
			if p.Status == "published" || p.Status == "failed" {
				return p, raw, false, nil
			}
			delay = 2 * time.Second
		case isTransient(err):
			fmt.Fprintf(c.Stderr, "warning: %s (retrying)\n", strings.SplitN(friendlyError(err), "\n", 2)[0])
			delay = min(delay*2, 30*time.Second)
		default:
			return last, lastRaw, false, err
		}
		if remaining := deadline.Sub(c.Env.now()); remaining <= 0 {
			return last, lastRaw, true, nil
		} else if delay > remaining {
			delay = remaining
		}
		if err := c.Env.sleep(c, delay); err != nil {
			return last, lastRaw, false, err
		}
	}
}

func isTransient(err error) bool {
	var ne *NetError
	if errors.As(err, &ne) {
		return true
	}
	var ae *APIError
	return errors.As(err, &ae) && (ae.Status == 429 || ae.Status >= 500)
}

// waitAndReport waits for the post to settle and exits 0 (published), 2
// (failed) or 3 (timed out).
func waitAndReport(c *Ctx, cl *Client, p Post, timeout time.Duration) error {
	if !c.JSON {
		fmt.Fprintf(c.Stderr, "%s\n", c.paint(dim, fmt.Sprintf("Waiting for %s to publish (%s)...", p.ID, targetNames(p))))
	}
	final, raw, timedOut, err := waitForPublished(c, cl, p.ID, timeout)
	if err != nil {
		if c.Context.Err() != nil {
			return exitWith(ExitInterrupted)
		}
		return err
	}
	if raw == nil {
		final = p
	}
	if c.JSON {
		if raw != nil {
			if err := c.printRaw(raw); err != nil {
				return err
			}
		} else if err := c.printJSON(p); err != nil {
			return err
		}
		if timedOut {
			return exitWith(ExitTimeout)
		}
		return exitWith(publishExitCode(final.Status))
	}
	if timedOut {
		return &exitErr{code: ExitTimeout, msg: fmt.Sprintf("timed out after %s; %s is still %s", timeout, final.ID, final.Status)}
	}
	for _, tg := range final.Targets {
		name := firstNonEmpty(tg.ChannelName, tg.ChannelID)
		switch tg.Status {
		case "published":
			fmt.Fprintf(c.Out, "%s %s %s\n", c.paint(green, "published"), name, tg.ExternalURL)
		case "failed":
			fmt.Fprintf(c.Out, "%s %s: %s\n", c.paint(red, "failed"), name, clean(tg.Error))
		default:
			fmt.Fprintf(c.Out, "%s %s\n", c.status(tg.Status), name)
		}
	}
	if final.Status == "failed" {
		return &exitErr{code: ExitFailed, msg: fmt.Sprintf("%s failed on %d of %d targets; fix it and run: staqly posts retry %s <channel>", final.ID, failedTargets(final), len(final.Targets), final.ID)}
	}
	fmt.Fprintf(c.Out, "Published %s\n", c.paint(bold, final.ID))
	return nil
}

func failedTargets(p Post) int {
	n := 0
	for _, tg := range p.Targets {
		if tg.Status == "failed" {
			n++
		}
	}
	return n
}

func publishExitCode(status string) int {
	switch status {
	case "published":
		return ExitOK
	case "failed":
		return ExitFailed
	}
	return ExitOther
}

// plural: "1 reply", "3 replies".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
