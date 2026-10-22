package providers

import (
	"regexp"
	"strings"

	"mvdan.cc/xurls/v2"
)

// What a channel does with the links in a post's text. Some platforms charge more for a post with a link
// or show it to fewer people; the text a post keeps is never changed, only what the channel receives.
const (
	LinksKeep   = "keep"   // send the text as written
	LinksRemove = "remove" // drop every link
	LinksDefang = "defang" // example.com/path becomes example[.]com/path: readable, not clickable
)

// ValidLinksMode says whether m is one of the modes above.
func ValidLinksMode(m string) bool { return m == LinksKeep || m == LinksRemove || m == LinksDefang }

// xurls' relaxed matcher finds links with and without a scheme (example.com, www.example.com) against the
// IANA list of top-level domains, and leaves trailing punctuation and unbalanced parentheses out.
var linkRe = xurls.Relaxed()

var (
	schemeRe = regexp.MustCompile(`(?i)^https?://`)
	spacesRe = regexp.MustCompile(`[ \t]{2,}`)
	// A markdown link, [text](url): its text stays when the url goes.
	markdownLinkRe = regexp.MustCompile(`\[([^\]\n]+)\]\(([^)\s]+)\)`)
)

// ApplyLinks rewrites text for a links mode and returns the links it changed, as written. Email addresses and
// other schemes (mailto:, ftp://) stay as they are; an already defanged link is not matched again.
func ApplyLinks(mode, text string) (string, []string) {
	if mode != LinksRemove && mode != LinksDefang {
		return text, nil
	}
	var found []string
	text = markdownLinkRe.ReplaceAllStringFunc(text, func(m string) string {
		g := markdownLinkRe.FindStringSubmatch(m)
		if !isWebLink(g[2]) {
			return m
		}
		found = append(found, g[2])
		if mode == LinksRemove {
			return g[1]
		}
		return g[1] + " (" + defang(g[2]) + ")"
	})
	out := linkRe.ReplaceAllStringFunc(text, func(u string) string {
		if !isWebLink(u) {
			return u
		}
		found = append(found, u)
		if mode == LinksRemove {
			return ""
		}
		return defang(u)
	})
	if mode == LinksRemove && len(found) > 0 {
		// Removing a link leaves its surrounding spaces behind: fold them per line, keep line breaks.
		lines := strings.Split(out, "\n")
		for i, l := range lines {
			lines[i] = strings.TrimRight(spacesRe.ReplaceAllString(l, " "), " \t")
		}
		out = strings.TrimSpace(strings.Join(lines, "\n"))
	}
	return out, found
}

// isWebLink: an http(s) link or a bare host (example.com/x), not an email or another scheme. A bare name on a
// two-letter top-level domain without a path (README.md, setup.py, deploy.sh) is a file name, not a link: X
// links those only with a scheme or a path (.co is the exception), so they stay as written.
func isWebLink(u string) bool {
	if schemeRe.MatchString(u) {
		return true
	}
	if strings.Contains(u, "://") || strings.Contains(strings.ToLower(u), "mailto:") {
		return false
	}
	host, path, _ := strings.Cut(u, "/")
	if strings.Contains(host, "@") {
		return false // an email address (mastodon.social/@staqly is a link: the @ is in the path)
	}
	if path == "" && !strings.HasPrefix(strings.ToLower(host), "www.") {
		tld := host[strings.LastIndex(host, ".")+1:]
		if len(tld) == 2 && !strings.EqualFold(tld, "co") {
			return false
		}
	}
	return true
}

// defang drops the scheme and brackets the dots of the host: https://www.example.com:8080/a.b?q=1 becomes
// www[.]example[.]com:8080/a.b?q=1.
func defang(u string) string {
	u = schemeRe.ReplaceAllString(u, "")
	end := strings.IndexAny(u, "/?#")
	if end < 0 {
		end = len(u)
	}
	return strings.ReplaceAll(u[:end], ".", "[.]") + u[end:]
}

// HasLiveLink: the text has a web link a platform turns into a link (not a defanged or removed one, not an
// email address). X bills such a post at a higher rate.
func HasLiveLink(text string) bool {
	for _, u := range linkRe.FindAllString(text, -1) {
		if isWebLink(u) {
			return true
		}
	}
	return false
}
