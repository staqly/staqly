package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Fetching for watchers: public internet only (unless configured), size and
// time limits, robots.txt respected. Fetched content is untrusted data.

const (
	watcherUA       = "staqly-watcher/1.0 (+https://staq.ly)"
	maxFetchBytes   = 3 << 20
	fetchTimeout    = 20 * time.Second
	robotsCacheTime = time.Hour
)

type feedEntry struct {
	ID        string // stable identity for dedup
	Title     string
	URL       string
	Summary   string
	Published string
	Author    string
}

func (s *Service) fetchClient() *http.Client {
	cfg := WebhookConfig{AllowPrivate: s.Cfg.WatcherAllowPrivate}
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return err
		}
		if ip.Unmap().IsLoopback() {
			return fmt.Errorf("destination %s is not allowed", ip)
		}
		if err := checkIP(ip.Unmap(), cfg); err != nil {
			return fmt.Errorf("destination %s is not allowed for watchers (set WATCHER_ALLOW_PRIVATE=true to watch LAN sources)", ip)
		}
		return nil
	}}
	return &http.Client{
		Timeout:   fetchTimeout,
		Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil, TLSHandshakeTimeout: 10 * time.Second, MaxIdleConnsPerHost: 2},
		// Redirects are followed (feeds move), but every hop is re-checked by the dialer.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

func (s *Service) get(ctx context.Context, rawURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", watcherUA)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml, text/html;q=0.8, */*;q=0.5")
	resp, err := s.Fetcher.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, "", fmt.Errorf("source responded %d, will retry", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("source responded %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) > maxFetchBytes {
		return nil, "", errors.New("source response is larger than 3 MB")
	}
	return body, resp.Header.Get("Content-Type"), nil
}

// ---- robots.txt ----

type robotsRules struct {
	disallow []string
	fetched  time.Time
}

type robotsCache struct {
	mu    sync.Mutex
	hosts map[string]robotsRules
}

func (s *Service) robotsAllowed(ctx context.Context, u *url.URL) bool {
	key := u.Scheme + "://" + u.Host
	s.robots.mu.Lock()
	r, ok := s.robots.hosts[key]
	s.robots.mu.Unlock()
	if !ok || time.Since(r.fetched) > robotsCacheTime {
		r = robotsRules{fetched: time.Now()}
		if body, _, err := s.get(ctx, key+"/robots.txt"); err == nil {
			r.disallow = parseRobots(body, "staqly-watcher")
		} // unreachable/missing robots.txt = allowed
		s.robots.mu.Lock()
		s.robots.hosts[key] = r
		s.robots.mu.Unlock()
	}
	p := u.EscapedPath()
	if p == "" {
		p = "/"
	}
	for _, d := range r.disallow {
		if strings.HasPrefix(p, d) {
			return false
		}
	}
	return true
}

// parseRobots returns Disallow prefixes for our agent, falling back to "*".
// ponytail: prefix matching only (no wildcards/Allow precedence).
func parseRobots(body []byte, agent string) []string {
	groups := map[string][]string{}
	var current []string
	inRules := false
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		switch k {
		case "user-agent":
			if inRules {
				current, inRules = nil, false
			}
			current = append(current, strings.ToLower(v))
			groups[strings.ToLower(v)] = groups[strings.ToLower(v)]
		case "disallow":
			inRules = true
			if v != "" {
				for _, a := range current {
					groups[a] = append(groups[a], v)
				}
			}
		default:
			inRules = true
		}
	}
	if d, ok := groups[agent]; ok {
		return d
	}
	return groups["*"]
}

// ---- RSS / Atom ----

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
	Content     string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
	PubDate     string `xml:"pubDate"`
	Date        string `xml:"http://purl.org/dc/elements/1.1/ date"`
	Creator     string `xml:"http://purl.org/dc/elements/1.1/ creator"`
}

type atomEntry struct {
	Title string `xml:"title"`
	ID    string `xml:"id"`
	Links []struct {
		Href string `xml:"href,attr"`
		Rel  string `xml:"rel,attr"`
	} `xml:"link"`
	Summary   string `xml:"summary"`
	Content   string `xml:"content"`
	Published string `xml:"published"`
	Updated   string `xml:"updated"`
	Author    struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Group struct { // YouTube
		Description string `xml:"http://search.yahoo.com/mrss/ description"`
	} `xml:"http://search.yahoo.com/mrss/ group"`
}

type feedDoc struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
	Items   []rssItem   `xml:"item"` // RSS 1.0 (RDF) puts items at the root
	Entries []atomEntry `xml:"entry"`
}

func parseFeed(body []byte) ([]feedEntry, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	dec.CharsetReader = func(charset string, in io.Reader) (io.Reader, error) {
		switch strings.ToLower(charset) {
		case "utf-8", "us-ascii", "ascii":
			return in, nil
		case "iso-8859-1", "latin1", "windows-1252":
			b, err := io.ReadAll(in)
			r := make([]rune, len(b))
			for i, c := range b {
				r[i] = rune(c)
			}
			return strings.NewReader(string(r)), err
		}
		return nil, fmt.Errorf("unsupported feed charset %s", charset)
	}
	var doc feedDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("not a valid RSS/Atom feed: %w", err)
	}
	var out []feedEntry
	for _, it := range append(doc.Channel.Items, doc.Items...) {
		summary := it.Description
		if summary == "" {
			summary = it.Content
		}
		pub := it.PubDate
		if pub == "" {
			pub = it.Date
		}
		out = append(out, feedEntry{ID: firstNonEmpty(it.GUID, it.Link, it.Title), Title: cleanText(it.Title, 300),
			URL: strings.TrimSpace(it.Link), Summary: cleanText(summary, 1500), Published: pub, Author: cleanText(it.Creator, 200)})
	}
	for _, e := range doc.Entries {
		link := ""
		for _, l := range e.Links {
			if l.Rel == "" || l.Rel == "alternate" {
				link = l.Href
				break
			}
		}
		if link == "" && len(e.Links) > 0 {
			link = e.Links[0].Href
		}
		summary := firstNonEmpty(e.Summary, e.Content, e.Group.Description)
		out = append(out, feedEntry{ID: firstNonEmpty(e.ID, link, e.Title), Title: cleanText(e.Title, 300), URL: strings.TrimSpace(link),
			Summary: cleanText(summary, 1500), Published: firstNonEmpty(e.Published, e.Updated), Author: cleanText(e.Author.Name, 200)})
	}
	if out == nil && len(doc.Channel.Items) == 0 && len(doc.Entries) == 0 && !bytes.Contains(body, []byte("<rss")) &&
		!bytes.Contains(body, []byte("<feed")) && !bytes.Contains(body, []byte("<rdf")) {
		return nil, errors.New("not an RSS/Atom feed")
	}
	return out, nil
}

var (
	tagRe   = regexp.MustCompile(`(?s)<[^>]*>`)
	blockRe = regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</(script|style|noscript)>`)
	spaceRe = regexp.MustCompile(`\s+`)
)

// cleanText turns (possibly HTML) text into plain text capped at max runes.
func cleanText(s string, max int) string {
	s = blockRe.ReplaceAllString(s, " ")
	s = tagRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(spaceRe.ReplaceAllString(html.UnescapeString(s), " "))
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

func pageTitle(body []byte) string {
	if m := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`).FindSubmatch(body); m != nil {
		return cleanText(string(m[1]), 200)
	}
	return ""
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// keywordFilter: with keywords, at least one must appear; excluded words veto.
func keywordFilter(text string, keywords, exclude []string) bool {
	t := strings.ToLower(text)
	for _, x := range exclude {
		if x != "" && strings.Contains(t, strings.ToLower(x)) {
			return false
		}
	}
	if len(keywords) == 0 {
		return true
	}
	for _, k := range keywords {
		if k != "" && strings.Contains(t, strings.ToLower(k)) {
			return true
		}
	}
	return false
}
