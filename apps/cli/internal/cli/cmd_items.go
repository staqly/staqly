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

func itemsCommand() *Command {
	return &Command{
		Name: "items", Aliases: []string{"item"},
		Short: "List, show, create, decide, update and delete items",
		Subs: []*Command{
			itemsListCommand(),
			leaf("get", "<id>", "Show an item, its content, metadata and available actions", 1, 1, func(fs *flag.FlagSet) runFunc {
				return runItemGet
			}, "staqly items get itm_123", "staqly items get itm_123 --json | jq .metadata"),
			createCommand("create", false),
			decideCommand("approve"),
			decideCommand("reject"),
			itemsActionCommand(),
			itemsUpdateCommand(),
			itemsHistoryCommand(),
			itemsVersionsCommand(),
			itemsResultCommand(),
			itemsBulkCommand(),
			itemsDeleteCommand(),
			itemsPurgeCommand(),
		},
	}
}

// ---- list ----

func itemsListCommand() *Command {
	return leaf("list", "", "List or search items across collections", 0, 0, func(fs *flag.FlagSet) runFunc {
		coll := fs.String("collection", "", "collection ID or name")
		status := fs.String("status", "", "draft, pending, approved, rejected, processing, completed, failed, archived")
		typ := fs.String("type", "", "image, video, text, link, json or file")
		q := fs.String("q", "", "full-text search over title, content and metadata")
		source := fs.String("source", "", "filter by source")
		sortBy := fs.String("sort", "", "relevance, created_desc, created_asc, updated_desc, updated_asc, decided_desc, title_asc")
		limit := fs.Int("limit", 50, "page size (max 200)")
		cursor := fs.String("cursor", "", "continue from a previous page's next_cursor")
		all := fs.Bool("all", false, "follow cursors and fetch every page")
		return func(c *Ctx, _ []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			qv := url.Values{}
			if *coll != "" {
				id, err := resolveCollectionID(c, cl, *coll)
				if err != nil {
					return err
				}
				qv.Set("collection_id", id)
			}
			setQ(qv, "status", *status)
			setQ(qv, "type", *typ)
			setQ(qv, "q", *q)
			setQ(qv, "source", *source)
			setQ(qv, "sort", *sortBy)
			setQ(qv, "cursor", *cursor)
			if *limit > 0 {
				qv.Set("limit", strconv.Itoa(*limit))
			}
			items, next, raw, err := fetchItems(c, cl, qv, *all)
			if err != nil {
				return err
			}
			if c.JSON {
				if *all {
					return c.printJSON(map[string]any{"data": items})
				}
				return c.printRaw(raw)
			}
			renderItems(c, cl, items)
			if next != "" {
				fmt.Fprintf(c.Stderr, "%s\n", c.paint(dim, "More items available: add --all, or --cursor "+next))
			}
			return nil
		}
	}, "staqly items list --status pending",
		"staqly items list --collection \"Blog posts\" --q launch --limit 10",
		"staqly items list --status approved --all --json | jq -r '.data[].id'")
}

func setQ(q url.Values, k, v string) {
	if v != "" {
		q.Set(k, v)
	}
}

// fetchItems gets one page, or every page when all is set.
func fetchItems(c *Ctx, cl *Client, q url.Values, all bool) ([]Item, string, json.RawMessage, error) {
	var out []Item
	for {
		var page ItemPage
		raw, err := cl.Get(c, "/v1/items", q, &page)
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

// collectionNames maps IDs to names for human output (best effort).
func collectionNames(c *Ctx, cl *Client) map[string]string {
	var page struct{ Data []Collection }
	m := map[string]string{}
	if _, err := cl.Get(c, "/v1/collections", url.Values{"include_archived": {"true"}}, &page); err == nil {
		for _, col := range page.Data {
			m[col.ID] = col.Name
		}
	}
	return m
}

func renderItems(c *Ctx, cl *Client, items []Item) {
	names := map[string]string{}
	if len(items) > 0 {
		names = collectionNames(c, cl)
	}
	t := c.table("ID", "STATUS", "TYPE", "COLLECTION", "TITLE", "CREATED")
	for _, it := range items {
		t.row(it.ID, c.status(it.Status), it.Type, truncate(firstNonEmpty(names[it.CollectionID], it.CollectionID), 24),
			truncate(orDash(it.Title), 60), fmtTime(it.CreatedAt))
	}
	t.flush("No items.")
}

// ---- get ----

func runItemGet(c *Ctx, args []string) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	var it Item
	raw, err := cl.Get(c, "/v1/items/"+url.PathEscape(args[0]), nil, &it)
	if err != nil || c.JSON {
		return jsonOr(c, raw, err)
	}
	var col Collection
	_, _ = cl.Get(c, "/v1/collections/"+url.PathEscape(it.CollectionID), nil, &col)
	printItem(c, it, firstNonEmpty(col.Name, it.CollectionID))
	var acts struct{ Data []ActionDef }
	if _, err := cl.Get(c, "/v1/items/"+url.PathEscape(it.ID)+"/actions", nil, &acts); err == nil && len(acts.Data) > 0 {
		names := make([]string, len(acts.Data))
		for i, a := range acts.Data {
			names[i] = a.Name
			if a.RequiresFeedback {
				names[i] += "*"
			}
		}
		fmt.Fprintf(c.Out, "\nActions: %s %s\n", strings.Join(names, ", "), c.paint(dim, "(* needs --feedback)"))
	}
	return nil
}

func printItem(c *Ctx, it Item, collection string) {
	media := ""
	if it.Media != nil {
		media = fmt.Sprintf("%s (%s, %s) %s", it.Media.OriginalName, it.Media.MimeType, humanBytes(it.Media.SizeBytes), it.Media.URL)
	}
	c.kv("ID", it.ID, "Title", it.Title, "Status", c.status(it.Status), "Type", it.Type, "Collection", collection,
		"Source", it.Source, "External ref", it.ExternalRef, "URL", it.URL, "Media", media,
		"Feedback", it.Feedback, "Expires", fmtOptTime(it.ExpiresAt), "Editable", strings.Join(it.Editable, ", "), "Version", strconv.Itoa(it.Version), "Created", fmtTime(it.CreatedAt),
		"Decided", decidedAt(it))
	if len(it.Metadata) > 0 {
		b, _ := json.MarshalIndent(it.Metadata, "", "  ")
		fmt.Fprintf(c.Out, "\nMetadata:\n%s\n", indent(string(safeJSON(b)), "  "))
	}
	if it.Content != "" {
		fmt.Fprintf(c.Out, "\nContent:\n%s\n", indent(clean(it.Content), "  "))
	}
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// ---- create / push ----

func pushCommand() *Command {
	cmd := createCommand("push", true)
	cmd.Long = `Push an item for review and wait for the human decision (items create --wait).

Exit codes: 0 approved, 2 rejected, 3 timed out or expired, 4 the item left
pending with another status, 1 error. The decision feedback is printed.
Use --no-wait to return as soon as the item is created.`
	return cmd
}

func createCommand(name string, waitDefault bool) *Command {
	short := "Create an item in a collection (optionally --wait for the decision)"
	if waitDefault {
		short = "Push an item for review and wait for approve/reject (CI gate)"
	}
	cmd := leaf(name, "<collection>", short, 1, 1, func(fs *flag.FlagSet) runFunc {
		title := fs.String("title", "", "item title")
		content := fs.String("content", "", "text content; '-' reads stdin, '@path' reads a file")
		stdin := fs.Bool("stdin", false, "read the content from stdin")
		file := fs.String("file", "", "upload a file as the item's media (image, video, PDF, ...)")
		link := fs.String("url", "", "external URL for the item")
		typ := fs.String("type", "", "image, video, text, link, json or file (inferred when omitted)")
		metadata := fs.String("metadata", "", "metadata as a JSON object")
		var meta multiFlag
		fs.Var(&meta, "meta", "metadata `key=value` (repeatable; values that parse as JSON are typed)")
		source := fs.String("source", "cli", "source label shown to reviewers")
		extRef := fs.String("external-ref", "", "your own reference (e.g. a PR number)")
		status := fs.String("status", "", "initial status (default pending)")
		idem := fs.String("idempotency-key", "", "make retries safe: the same key returns the same item")
		expiresIn := fs.String("expires-in", "", "expire the item if nobody decides in time, e.g. 24h or seconds (60 s – 90 days)")
		editable := &multiFlag{comma: true}
		fs.Var(editable, "editable", "`fields` a reviewer may edit before approving: title, content, url, media_id, metadata, metadata.<key> (repeatable)")
		wait := fs.Bool("wait", waitDefault, "wait until a human approves or rejects")
		noWait := new(bool)
		if waitDefault {
			fs.BoolVar(noWait, "no-wait", false, "return as soon as the item is created")
		}
		timeout := fs.Duration("timeout", 30*time.Minute, "how long --wait waits")
		return func(c *Ctx, args []string) error {
			if *noWait {
				*wait = false
			}
			if *wait && *status != "" && *status != "pending" {
				return usageErrf("--wait needs a pending item, but --status is %q", *status)
			}
			if *timeout <= 0 {
				return usageErrf("--timeout must be positive")
			}
			body := map[string]any{}
			setIf(body, "title", *title)
			setIf(body, "url", *link)
			setIf(body, "type", *typ)
			setIf(body, "source", *source)
			setIf(body, "external_ref", *extRef)
			setIf(body, "status", *status)
			if *expiresIn != "" {
				if n, err := strconv.Atoi(*expiresIn); err == nil {
					body["expires_in"] = n
				} else if _, err := time.ParseDuration(*expiresIn); err == nil {
					body["expires_in"] = *expiresIn
				} else {
					return usageErrf("--expires-in must be a duration like 24h or a number of seconds")
				}
			}
			if len(editable.vals) > 0 {
				body["editable"] = editable.vals
			}
			text, err := readContent(c, *content, *stdin)
			if err != nil {
				return err
			}
			if text != "" {
				body["content"] = text
			}
			md := map[string]any{}
			if *metadata != "" {
				if md, err = parseJSONObject("metadata", *metadata); err != nil {
					return err
				}
			}
			for _, kv := range meta.vals {
				k, v, ok := strings.Cut(kv, "=")
				if !ok || k == "" {
					return usageErrf("--meta expects key=value, got %q", kv)
				}
				md[k] = scalarOrJSON(v)
			}
			if len(md) > 0 {
				body["metadata"] = md
			}
			cl, err := c.Client()
			if err != nil {
				return err
			}
			it, raw, err := createItem(c, cl, args[0], body, *file, *idem)
			if err != nil {
				return err
			}
			if !*wait {
				if c.JSON {
					return c.printRaw(raw)
				}
				fmt.Fprintf(c.Out, "Created %s in %s (status %s)\n", c.paint(bold, it.ID), args[0], c.status(it.Status))
				return nil
			}
			if !c.JSON {
				fmt.Fprintf(c.Stderr, "Created %s. Waiting up to %s for a decision", it.ID, *timeout)
				if c.Env.StderrIsInteractive() {
					fmt.Fprint(c.Stderr, " (approve in the dashboard, `staqly inbox` or `staqly items approve "+it.ID+"`)")
				}
				fmt.Fprintln(c.Stderr, "...")
			}
			return waitAndReport(c, cl, it, *timeout)
		}
	})
	p := "staqly items create"
	if waitDefault {
		p = "staqly push"
	}
	cmd.Examples = []string{
		p + " \"Blog posts\" --title \"Launch post\" --content @post.md",
		"echo \"Delete 3 stale branches?\" | " + p + " Ops --title \"Branch cleanup\" --stdin",
		p + " Avatars --file avatar.png --meta prompt=\"smiling\" --meta seed=42",
		p + " Deploys --title \"Deploy v1.4 to prod\" --external-ref \"$GITHUB_SHA\" --timeout 1h",
	}
	if !waitDefault {
		cmd.Examples = append(cmd.Examples, p+" Deploys --title \"Release\" --wait --timeout 2h && ./deploy.sh")
	}
	return cmd
}

// StderrIsInteractive is a hint for extra guidance; stdout TTY is a good proxy.
func (e *Env) StderrIsInteractive() bool { return e.StdoutTTY }

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

func createItem(c *Ctx, cl *Client, collection string, body map[string]any, file, idem string) (Item, json.RawMessage, error) {
	path := "/v1/collections/" + url.PathEscape(collection) + "/items"
	var req Request
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			return Item{}, nil, fmt.Errorf("open --file: %w", err)
		}
		defer f.Close()
		pr, pw := io.Pipe()
		mw := multipart.NewWriter(pw)
		go func() {
			err := func() error {
				itemJSON, err := json.Marshal(body)
				if err != nil {
					return err
				}
				if err := mw.WriteField("item", string(itemJSON)); err != nil {
					return err
				}
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
		req = Request{Method: http.MethodPost, Path: path, Body: io.Reader(pr), ContentType: mw.FormDataContentType()}
	} else {
		req = Request{Method: http.MethodPost, Path: path, Body: body, IdempotencyKey: firstNonEmpty(idem, newIdempotencyKey())}
	}
	var it Item
	raw, err := cl.call(c, req, &it)
	return it, raw, err
}

// ---- wait ----

// waitForDecision long-polls GET /v1/items/{id}/wait (up to 60 s per call)
// until the item leaves pending or the timeout passes. Transient errors
// (network, 5xx, 429) are retried with backoff. Servers without /wait (404
// while the item itself exists) fall back to polling GET /v1/items/{id}.
func waitForDecision(c *Ctx, cl *Client, id string, timeout time.Duration) (Item, json.RawMessage, bool, error) {
	deadline := c.Env.now().Add(timeout)
	delay := time.Second
	warned, legacy := false, false
	var last Item
	var lastRaw json.RawMessage
	for {
		remaining := deadline.Sub(c.Env.now())
		if remaining <= 0 && lastRaw != nil {
			return last, lastRaw, true, nil
		}
		secs := min(max(int((remaining+time.Second-1)/time.Second), 1), 60)
		started := c.Env.now()
		var it Item
		var raw json.RawMessage
		var err error
		if legacy {
			raw, err = cl.Get(c, "/v1/items/"+url.PathEscape(id), nil, &it)
		} else {
			var w struct {
				Decided bool            `json:"decided"`
				Item    json.RawMessage `json:"item"`
			}
			if _, err = cl.Get(c, "/v1/items/"+url.PathEscape(id)+"/wait", url.Values{"timeout": {strconv.Itoa(secs)}}, &w); err == nil {
				raw = w.Item
				if err = json.Unmarshal(raw, &it); err != nil {
					err = fmt.Errorf("unexpected wait response: %w", err)
				}
			} else if ae := (*APIError)(nil); errors.As(err, &ae) && ae.Status == 404 {
				// Old server without /wait, or the item is gone: GET tells which.
				if raw, err = cl.Get(c, "/v1/items/"+url.PathEscape(id), nil, &it); err == nil {
					legacy = true
				}
			}
		}
		wait := delay
		switch {
		case err == nil:
			last, lastRaw = it, raw
			if it.Status != "pending" {
				return it, raw, false, nil
			}
			// A long-poll that held the connection needs no extra sleep;
			// only back off when the server answered immediately.
			if !legacy && c.Env.now().Sub(started) >= time.Second {
				delay = time.Second
				continue
			}
		case isTransient(err):
			var ae *APIError
			if errors.As(err, &ae) && ae.RetryAfter > 0 {
				wait = ae.RetryAfter
			}
			if !warned && !c.JSON {
				fmt.Fprintf(c.Stderr, "warning: %s; still waiting...\n", strings.SplitN(friendlyError(err), "\n", 2)[0])
				warned = true
			}
		default:
			return last, lastRaw, false, err
		}
		remaining = deadline.Sub(c.Env.now())
		if remaining <= 0 {
			return last, lastRaw, true, nil
		}
		if err := c.Env.sleep(c, min(wait, remaining)); err != nil {
			return last, lastRaw, false, err
		}
		delay = min(time.Duration(float64(delay)*1.5), 10*time.Second)
	}
}

func isTransient(err error) bool {
	var ae *APIError
	var ne *NetError
	if errors.As(err, &ne) {
		return true
	}
	return errors.As(err, &ae) && (ae.Status == 429 || ae.Status >= 500)
}

func waitAndReport(c *Ctx, cl *Client, created Item, timeout time.Duration) error {
	it, raw, timedOut, err := waitForDecision(c, cl, created.ID, timeout)
	if err != nil {
		return err
	}
	if it.ID == "" {
		it = created
	}
	code := decisionExitCode(it.Status, timedOut)
	if c.JSON {
		if raw != nil {
			_ = c.printRaw(raw)
		} else {
			_ = c.printJSON(it)
		}
		return exitWith(code)
	}
	switch {
	case timedOut:
		fmt.Fprintf(c.Stderr, "Timed out after %s: %s is still pending.\n", timeout, it.ID)
	case it.Status == "approved":
		fmt.Fprintf(c.Out, "%s %s\n", c.paint(green, "Approved:"), it.ID)
	case it.Status == "rejected":
		fmt.Fprintf(c.Out, "%s %s\n", c.paint(red, "Rejected:"), it.ID)
	case it.Status == "expired":
		fmt.Fprintf(c.Out, "%s %s expired before anyone decided\n", c.paint(red, "Expired:"), it.ID)
	default:
		fmt.Fprintf(c.Out, "Decided: %s is now %s\n", it.ID, c.status(it.Status))
	}
	if it.Feedback != "" {
		fmt.Fprintf(c.Out, "Feedback: %s\n", it.Feedback)
	}
	return exitWith(code)
}

func decisionExitCode(status string, timedOut bool) int {
	switch {
	case timedOut || status == "expired":
		return ExitTimeout
	case status == "approved":
		return ExitOK
	case status == "rejected":
		return ExitRejected
	}
	return ExitOther
}

// ---- approve / reject / action ----

func decideCommand(action string) *Command {
	short := "Approve one or more items"
	if action == "reject" {
		short = "Reject one or more items (feedback tells the agent what to fix)"
	}
	return leaf(action, "<id>...", short, 1, -1, func(fs *flag.FlagSet) runFunc {
		var feedback string
		fs.StringVar(&feedback, "feedback", "", "feedback for the agent")
		fs.StringVar(&feedback, "m", "", "shorthand for --feedback")
		version := fs.Int("expected-version", 0, "fail with 409 unless the item is at this version")
		return func(c *Ctx, args []string) error {
			body := map[string]any{}
			setIf(body, "feedback", feedback)
			if *version > 0 {
				body["expected_version"] = *version
			}
			return runActions(c, args, action, body)
		}
	}, "staqly items "+action+" itm_123", "staqly items "+action+" itm_123 itm_456 --feedback \"too long\"")
}

func itemsActionCommand() *Command {
	return leaf("action", "<id> <action>", "Run a custom collection action (e.g. regenerate) on an item", 2, 2, func(fs *flag.FlagSet) runFunc {
		var feedback string
		fs.StringVar(&feedback, "feedback", "", "feedback / instructions")
		fs.StringVar(&feedback, "m", "", "shorthand for --feedback")
		payload := fs.String("payload", "", "arbitrary JSON forwarded in the event and webhook")
		return func(c *Ctx, args []string) error {
			body := map[string]any{}
			setIf(body, "feedback", feedback)
			if *payload != "" {
				var p any
				if err := json.Unmarshal([]byte(*payload), &p); err != nil {
					return usageErrf("--payload must be valid JSON")
				}
				body["payload"] = p
			}
			return runActions(c, args[:1], args[1], body)
		}
	}, "staqly items action itm_123 regenerate --feedback \"make her smile\"",
		"staqly items action itm_123 publish --payload '{\"channel\":\"instagram\"}'")
}

func runActions(c *Ctx, ids []string, action string, body map[string]any) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	var results []any
	failed := 0
	var lastErr error
	for _, id := range ids {
		var it Item
		raw, err := cl.Post(c, "/v1/items/"+url.PathEscape(id)+"/actions/"+url.PathEscape(action), body, &it)
		if err != nil {
			failed++
			lastErr = err
			if len(ids) == 1 {
				return err
			}
			if c.JSON {
				results = append(results, map[string]any{"id": id, "ok": false, "error": errorBody(err)})
			} else {
				fmt.Fprintf(c.Stderr, "%s %s: %s\n", c.paint(red, "failed"), id, friendlyError(err))
			}
			continue
		}
		if c.JSON {
			results = append(results, map[string]any{"id": id, "ok": true, "item": raw})
		} else {
			fmt.Fprintf(c.Out, "%s %s → %s\n", pastTense(action), it.ID, c.status(it.Status))
		}
	}
	if c.JSON {
		if len(ids) == 1 {
			_ = c.printRaw(results[0].(map[string]any)["item"].(json.RawMessage))
		} else {
			_ = c.printJSON(results)
		}
	}
	if failed > 0 {
		if c.JSON {
			return exitWith(ExitError)
		}
		return &exitErr{code: ExitError, msg: fmt.Sprintf("%d of %d failed (last: %s)", failed, len(ids), strings.SplitN(friendlyError(lastErr), "\n", 2)[0])}
	}
	return nil
}

func pastTense(action string) string {
	switch action {
	case "approve":
		return "Approved"
	case "reject":
		return "Rejected"
	}
	return "Ran " + action + " on"
}

// ---- update ----

func itemsUpdateCommand() *Command {
	return leaf("update", "<id>", "Update an item's fields, status or metadata", 1, 1, func(fs *flag.FlagSet) runFunc {
		status := fs.String("status", "", "new status (not approved/rejected; use approve/reject)")
		title := fs.String("title", "", "new title")
		content := fs.String("content", "", "new content; '-' reads stdin, '@path' reads a file")
		link := fs.String("url", "", "new URL")
		metadata := fs.String("metadata", "", "replace metadata with this JSON object")
		patch := fs.String("metadata-patch", "", "merge top-level keys (JSON object; null removes a key)")
		collection := fs.String("collection", "", "move to another collection (ID or name)")
		extRef := fs.String("external-ref", "", "set external reference")
		mediaID := fs.String("media-id", "", "attach an uploaded file (see `staqly media upload`); --media-id \"\" removes the media")
		version := fs.Int("expected-version", 0, "fail with 409 unless the item is at this version")
		return func(c *Ctx, args []string) error {
			body := map[string]any{}
			fs.Visit(func(f *flag.Flag) {
				if f.Name == "media-id" {
					body["media_id"] = *mediaID
				}
			})
			setIf(body, "status", *status)
			setIf(body, "title", *title)
			setIf(body, "url", *link)
			setIf(body, "external_ref", *extRef)
			if *content != "" {
				text, err := readContent(c, *content, false)
				if err != nil {
					return err
				}
				body["content"] = text
			}
			var err error
			if *metadata != "" {
				if body["metadata"], err = parseJSONObject("metadata", *metadata); err != nil {
					return err
				}
			}
			if *patch != "" {
				if body["metadata_patch"], err = parseJSONObject("metadata-patch", *patch); err != nil {
					return err
				}
			}
			if *version > 0 {
				body["expected_version"] = *version
			}
			cl, err := c.Client()
			if err != nil {
				return err
			}
			if *collection != "" {
				id, err := resolveCollectionID(c, cl, *collection)
				if err != nil {
					return err
				}
				body["collection_id"] = id
			}
			if len(body) == 0 {
				return usageErrf("nothing to update; pass at least one of --status, --title, --content, --url, --metadata, --metadata-patch, --collection, --external-ref, --media-id")
			}
			var it Item
			raw, err := cl.Patch(c, "/v1/items/"+url.PathEscape(args[0]), body, &it)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			fmt.Fprintf(c.Out, "Updated %s (status %s, version %d)\n", it.ID, c.status(it.Status), it.Version)
			return nil
		}
	}, "staqly items update itm_123 --status completed --metadata-patch '{\"published_url\":\"https://...\"}'",
		"staqly items update itm_123 --title \"Better title\"")
}

// ---- history ----

func itemsHistoryCommand() *Command {
	return leaf("history", "<id>", "Show an item's activity history (oldest first)", 1, 1, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, args []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var page EventPage
			raw, err := cl.Get(c, "/v1/items/"+url.PathEscape(args[0])+"/events", nil, &page)
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
	}, "staqly items history itm_123")
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
	for _, k := range []string{"title", "from", "to", "status", "feedback", "action", "note", "error"} {
		if v, ok := d[k]; ok && v != nil && v != "" {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
	}
	// Event data is decoded here, not by the client: clean it for display.
	return cleanLine(strings.Join(parts, " "))
}

func decidedAt(it Item) string {
	if it.DecidedAt == nil || *it.DecidedAt == "" {
		return ""
	}
	return fmtTime(*it.DecidedAt)
}

func fmtOptTime(p *string) string {
	if p == nil || *p == "" {
		return ""
	}
	return fmtTime(*p)
}

// ---- result ----

func itemsResultCommand() *Command {
	cmd := leaf("result", "<id>", "Report the outcome after acting on an approved item", 1, 1, func(fs *flag.FlagSet) runFunc {
		status := fs.String("status", "", "completed or failed (required)")
		result := fs.String("result", "", "any JSON describing the outcome (stored in metadata.result)")
		errText := fs.String("error", "", "error message (stored in metadata.error)")
		return func(c *Ctx, args []string) error {
			if *status != "completed" && *status != "failed" {
				return usageErrf("--status must be completed or failed")
			}
			body := map[string]any{"status": *status}
			if *result != "" {
				var v any
				if err := json.Unmarshal([]byte(*result), &v); err != nil {
					return usageErrf("--result must be valid JSON")
				}
				body["result"] = v
			}
			setIf(body, "error", *errText)
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var it Item
			raw, err := cl.Post(c, "/v1/items/"+url.PathEscape(args[0])+"/result", body, &it)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			fmt.Fprintf(c.Out, "Reported %s → %s\n", it.ID, c.status(it.Status))
			return nil
		}
	}, "staqly items result itm_123 --status completed --result '{\"url\":\"https://example.com/post/1\"}'",
		"staqly items result itm_123 --status failed --error \"publish API returned 500\"")
	cmd.Long = `Report the outcome after acting on an approved item: --status completed or
failed, with optional --result JSON (metadata.result) and --error text
(metadata.error). Closes the loop: push --wait → act → items result.`
	return cmd
}
