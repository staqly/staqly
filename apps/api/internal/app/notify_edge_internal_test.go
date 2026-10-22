package app

import (
	"html"
	"net/mail"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// A digest of many long entries stays within Telegram's 4096 characters (it was refused, again and again).
func TestTelegramDigestFits(t *testing.T) {
	content := strings.Repeat("Launch week day one: everything we shipped and why it matters for your team ", 4)
	msg := &notifyMessage{}
	for range 15 {
		msg.Entries = append(msg.Entries, notifyEntry{EventType: "post.published", Title: postTitle(content),
			Preview: truncateRunes(content, 280), Channel: "Acme on X", Collection: "Marketing"})
	}
	visible := html.UnescapeString(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(telegramText(msg, 1500), ""))
	if n := utf8.RuneCountInString(visible); n > 4096 {
		t.Errorf("15-entry Telegram digest is %d characters; sendMessage takes at most 4096", n)
	}
}

// A long non-ASCII subject is folded: no header line over RFC 5322's 998 octets.
func TestEmailSubjectIsFolded(t *testing.T) {
	from, _ := mail.ParseAddress("a@example.com")
	raw := buildEmail(from, []string{"b@example.com"}, "Published: "+strings.Repeat("日本語", 60), "x", "<p>x</p>")
	for _, l := range strings.Split(string(raw), "\r\n") {
		if len(l) > 998 {
			t.Errorf("header line of %d octets (RFC 5322 max 998): %.60s...", len(l), l)
		}
	}
}
