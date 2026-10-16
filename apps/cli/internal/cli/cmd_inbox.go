package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

func inboxCommand() *Command {
	cmd := leaf("inbox", "", "Review pending items (interactive on a terminal)", 0, 0, func(fs *flag.FlagSet) runFunc {
		coll := fs.String("collection", "", "only this collection (ID or name)")
		limit := fs.Int("limit", 100, "maximum items to show")
		watch := fs.Bool("watch", false, "keep refreshing (non-interactive: print new items as they arrive)")
		interval := fs.Duration("interval", 5*time.Second, "refresh interval")
		noInteractive := fs.Bool("no-interactive", false, "print a table even on a terminal")
		return func(c *Ctx, _ []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			q := url.Values{"status": {"pending"}, "sort": {"created_asc"}, "limit": {strconv.Itoa(*limit)}}
			if *coll != "" {
				id, err := resolveCollectionID(c, cl, *coll)
				if err != nil {
					return err
				}
				q.Set("collection_id", id)
			}
			if *interval < time.Second {
				*interval = time.Second
			}
			if c.Env.StdinTTY && c.Env.StdoutTTY && !c.JSON && !*noInteractive {
				return runInboxInteractive(c, cl, q, *interval)
			}
			if *watch {
				return runInboxWatch(c, cl, q, *interval)
			}
			var page ItemPage
			raw, err := cl.Get(c, "/v1/items", q, &page)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			renderItems(c, cl, page.Data)
			return nil
		}
	}, "staqly inbox", "staqly inbox --collection \"Blog posts\"", "staqly inbox --watch --json   # NDJSON stream of new pending items")
	cmd.Long = `Review pending items.

On a terminal this opens an interactive inbox:
  j/k or arrows  move            a  approve
  enter/space    show details    r  reject (asks for feedback)
  g              refresh now     q  quit
It refreshes every --interval. Piped or with --json it prints the pending
items once; add --watch to keep printing new ones.`
	return cmd
}

// runInboxWatch prints new pending items until interrupted. On a terminal the
// table is redrawn; otherwise only new items are printed (NDJSON with --json).
func runInboxWatch(c *Ctx, cl *Client, q url.Values, interval time.Duration) error {
	seen := map[string]bool{}
	for {
		var page ItemPage
		_, err := cl.Get(c, "/v1/items", q, &page)
		switch {
		case err != nil && c.Context.Err() == nil && isTransient(err):
			fmt.Fprintf(c.Stderr, "warning: %s\n", strings.SplitN(friendlyError(err), "\n", 2)[0])
		case err != nil:
			if c.Context.Err() != nil {
				return nil
			}
			return err
		case c.Env.StdoutTTY && !c.JSON:
			fmt.Fprint(c.Out, "\x1b[H\x1b[2J")
			fmt.Fprintf(c.Out, "Pending items — %s (Ctrl-C to quit)\n\n", c.Env.now().Format("15:04:05"))
			renderItems(c, cl, page.Data)
		default:
			for _, it := range page.Data {
				if seen[it.ID] {
					continue
				}
				seen[it.ID] = true
				if c.JSON {
					c.printJSONLine(it)
				} else {
					fmt.Fprintf(c.Out, "%s\t%s\t%s\t%s\n", cleanLine(fmtTime(it.CreatedAt)), cleanLine(it.ID), cleanLine(it.Type),
						truncate(cleanLine(orDash(it.Title)), 80))
				}
			}
		}
		if err := c.Env.sleep(c, interval); err != nil {
			return nil // Ctrl-C ends a watch normally
		}
	}
}

// ---- interactive ----

type key struct {
	name string // "up", "down", "enter", "esc", "backspace", "ctrl-c", "ctrl-d", "" for runes
	r    rune
}

// parseKeys decodes a chunk of raw terminal input.
func parseKeys(b []byte) []key {
	var out []key
	for len(b) > 0 {
		switch {
		case b[0] == 0x1b && len(b) >= 3 && (b[1] == '[' || b[1] == 'O'):
			switch b[2] {
			case 'A':
				out = append(out, key{name: "up"})
			case 'B':
				out = append(out, key{name: "down"})
			}
			b = b[3:]
			continue
		case b[0] == 0x1b:
			out = append(out, key{name: "esc"})
		case b[0] == 0x03:
			out = append(out, key{name: "ctrl-c"})
		case b[0] == 0x04:
			out = append(out, key{name: "ctrl-d"})
		case b[0] == '\r' || b[0] == '\n':
			out = append(out, key{name: "enter"})
		case b[0] == 0x7f || b[0] == 0x08:
			out = append(out, key{name: "backspace"})
		default:
			r, n := utf8.DecodeRune(b)
			out = append(out, key{r: r})
			b = b[n:]
			continue
		}
		b = b[1:]
	}
	return out
}

type inboxUI struct {
	c        *Ctx
	cl       *Client
	q        url.Values
	items    []Item
	names    map[string]string
	sel      int
	feedback bool // typing reject feedback
	input    []rune
	detail   bool
	msg      string
	height   int
}

func (u *inboxUI) refresh() {
	var page ItemPage
	if _, err := u.cl.Get(u.c, "/v1/items", u.q, &page); err != nil {
		u.msg = "refresh failed: " + strings.SplitN(friendlyError(err), "\n", 2)[0]
		return
	}
	var cur string
	if u.sel < len(u.items) {
		cur = u.items[u.sel].ID
	}
	u.items = page.Data
	u.sel = 0
	for i, it := range u.items {
		if it.ID == cur {
			u.sel = i
		}
	}
	for _, it := range u.items {
		if _, ok := u.names[it.CollectionID]; !ok {
			u.names = collectionNames(u.c, u.cl)
			break
		}
	}
}

func (u *inboxUI) decide(action, feedback string) {
	if u.sel >= len(u.items) {
		return
	}
	it := u.items[u.sel]
	body := map[string]any{}
	setIf(body, "feedback", feedback)
	if _, err := u.cl.Post(u.c, "/v1/items/"+url.PathEscape(it.ID)+"/actions/"+action, body, nil); err != nil {
		u.msg = action + " failed: " + strings.SplitN(friendlyError(err), "\n", 2)[0]
		return
	}
	u.msg = fmt.Sprintf("%s %s", pastTense(action), truncate(firstNonEmpty(it.Title, it.ID), 50))
	u.items = append(u.items[:u.sel:u.sel], u.items[u.sel+1:]...)
	if u.sel >= len(u.items) && u.sel > 0 {
		u.sel--
	}
}

// handle applies one key; it returns true to quit.
func (u *inboxUI) handle(k key) bool {
	if k.name == "ctrl-c" {
		return true
	}
	if u.feedback {
		switch k.name {
		case "esc":
			u.feedback, u.input, u.msg = false, nil, "reject cancelled"
		case "enter":
			fb := strings.TrimSpace(string(u.input))
			u.feedback, u.input = false, nil
			u.decide("reject", fb)
		case "backspace":
			if len(u.input) > 0 {
				u.input = u.input[:len(u.input)-1]
			}
		case "":
			if k.r >= ' ' {
				u.input = append(u.input, k.r)
			}
		}
		return false
	}
	u.msg = ""
	switch {
	case k.name == "ctrl-d" || k.r == 'q':
		return true
	case k.name == "down" || k.r == 'j':
		if u.sel < len(u.items)-1 {
			u.sel++
		}
	case k.name == "up" || k.r == 'k':
		if u.sel > 0 {
			u.sel--
		}
	case k.name == "enter" || k.r == ' ' || k.r == 'o':
		u.detail = !u.detail
	case k.r == 'a':
		u.decide("approve", "")
	case k.r == 'r':
		if len(u.items) > 0 {
			u.feedback = true
		}
	case k.r == 'g' || k.r == 'R':
		u.refresh()
		u.msg = "refreshed"
	}
	return false
}

func (u *inboxUI) render(w io.Writer) {
	var lines []string
	c := u.c
	lines = append(lines, c.paint(bold, fmt.Sprintf("staqly inbox — %d pending", len(u.items))), "")
	if len(u.items) == 0 {
		lines = append(lines, c.paint(dim, "Nothing to review. Waiting for new items..."))
	}
	avail := max(u.height-8, 3)
	if u.detail {
		avail = max(avail-12, 3)
	}
	start := 0
	if u.sel >= avail {
		start = u.sel - avail + 1
	}
	for i := start; i < len(u.items) && i < start+avail; i++ {
		it := u.items[i]
		// Everything shown here comes from the server: clean each field.
		line := fmt.Sprintf("%-5s %-18s %-14s %s", cleanLine(it.Type),
			truncate(cleanLine(firstNonEmpty(u.names[it.CollectionID], it.CollectionID)), 18),
			cleanLine(shortTime(it.CreatedAt)), truncate(cleanLine(orDash(it.Title)), 60))
		if i == u.sel {
			lines = append(lines, c.paint(cyan, "> "+line))
		} else {
			lines = append(lines, "  "+line)
		}
	}
	if u.detail && u.sel < len(u.items) {
		it := u.items[u.sel]
		lines = append(lines, "", c.paint(bold, it.ID)+"  "+cleanLine(it.Title))
		if it.URL != "" {
			lines = append(lines, "url: "+cleanLine(it.URL))
		}
		if it.Media != nil {
			lines = append(lines, "media: "+cleanLine(it.Media.OriginalName+" "+it.Media.URL))
		}
		if len(it.Metadata) > 0 {
			b, _ := json.Marshal(it.Metadata)
			lines = append(lines, "metadata: "+truncate(cleanLine(string(b)), 200))
		}
		content := strings.Split(strings.TrimRight(it.Content, "\n"), "\n")
		for i, l := range content {
			if i == 8 {
				lines = append(lines, c.paint(dim, fmt.Sprintf("... %d more lines (staqly items get %s)", len(content)-8, it.ID)))
				break
			}
			lines = append(lines, "  "+truncate(cleanLine(l), 120))
		}
	}
	lines = append(lines, "")
	if u.feedback {
		lines = append(lines, "Reject feedback (Enter to send, Esc to cancel): "+cleanLine(string(u.input)))
	} else {
		if u.msg != "" {
			lines = append(lines, cleanLine(u.msg))
		}
		lines = append(lines, c.paint(dim, "j/k move · enter details · a approve · r reject · g refresh · q quit"))
	}
	fmt.Fprint(w, "\x1b[H\x1b[2J"+strings.Join(lines, "\r\n"))
}

func runInboxInteractive(c *Ctx, cl *Client, q url.Values, interval time.Duration) error {
	fd := c.Env.StdinFd
	state, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("cannot switch the terminal to raw mode (use --no-interactive): %w", err)
	}
	fmt.Fprint(c.Out, "\x1b[?1049h\x1b[?25l") // alternate screen, hide cursor
	defer func() {
		fmt.Fprint(c.Out, "\x1b[?25h\x1b[?1049l")
		_ = term.Restore(fd, state)
	}()
	u := &inboxUI{c: c, cl: cl, q: q, names: map[string]string{}, height: 24}
	u.names = collectionNames(c, cl)
	u.refresh()
	keys := make(chan []byte)
	go func() {
		buf := make([]byte, 64)
		for {
			n, err := c.Env.Stdin.Read(buf)
			if err != nil {
				close(keys)
				return
			}
			keys <- append([]byte(nil), buf[:n]...)
		}
	}()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, h, err := term.GetSize(fd); err == nil && h > 0 {
			u.height = h
		}
		u.render(c.Out)
		select {
		case <-c.Done():
			return nil
		case <-ticker.C:
			if !u.feedback {
				u.refresh()
			}
		case b, ok := <-keys:
			if !ok {
				return nil
			}
			for _, k := range parseKeys(b) {
				if u.handle(k) {
					return nil
				}
			}
		}
	}
}

// shortTime drops the year: "10-13 14:05".
func shortTime(s string) string {
	t := fmtTime(s)
	if len(t) == len("2006-01-02 15:04") {
		return t[5:]
	}
	return t
}
