package providers

import "testing"

func TestApplyLinks(t *testing.T) {
	cases := []struct {
		mode, in, want string
		n              int
	}{
		{LinksKeep, "see https://example.com", "see https://example.com", 0},
		{"", "see https://example.com", "see https://example.com", 0},
		{LinksDefang, "see https://google.com/path?q=1", "see google[.]com/path?q=1", 1},
		{LinksDefang, "HTTP://Example.com", "Example[.]com", 1},
		{LinksDefang, "bare example.com and www.example.co.uk/a.b", "bare example[.]com and www[.]example[.]co[.]uk/a.b", 2},
		{LinksDefang, "port https://example.com:8080/x#top", "port example[.]com:8080/x#top", 1},
		{LinksDefang, "end https://example.com/x.", "end example[.]com/x.", 1},
		{LinksDefang, "(see https://example.com/a), then", "(see example[.]com/a), then", 1},
		{LinksDefang, "wiki https://en.wikipedia.org/wiki/Go_(language) ok", "wiki en[.]wikipedia[.]org/wiki/Go_(language) ok", 1},
		{LinksDefang, "already example[.]com/path", "already example[.]com/path", 0},
		{LinksDefang, "mail me@example.com or mailto:me@example.com", "mail me@example.com or mailto:me@example.com", 0},
		{LinksDefang, "files ftp://example.com/f", "files ftp://example.com/f", 0},
		{LinksDefang, "v1.4 is out, 3.5 times faster", "v1.4 is out, 3.5 times faster", 0},
		{LinksRemove, "Read more: https://example.com/post", "Read more:", 1},
		{LinksRemove, "a https://x.com/1 b example.org c", "a b c", 2},
		{LinksRemove, "line one https://example.com\nline two", "line one\nline two", 1},
		{LinksRemove, "no links here", "no links here", 0},
		{LinksRemove, "mail me@example.com", "mail me@example.com", 0},
	}
	for _, c := range cases {
		got, found := ApplyLinks(c.mode, c.in)
		if n := len(found); got != c.want || n != c.n {
			t.Errorf("ApplyLinks(%q, %q) = %q, %v; want %q, %d", c.mode, c.in, got, found, c.want, c.n)
		}
	}
	// Defanging twice changes nothing more.
	once, _ := ApplyLinks(LinksDefang, "go to https://www.example.com/a and example.org")
	if twice, found := ApplyLinks(LinksDefang, once); twice != once || len(found) != 0 {
		t.Errorf("defang not idempotent: %q -> %q (%v)", once, twice, found)
	}
	if _, found := ApplyLinks(LinksRemove, "a https://example.com/x, b example.org."); len(found) != 2 ||
		found[0] != "https://example.com/x" || found[1] != "example.org" {
		t.Errorf("found links: %v", found)
	}
}

func TestXLinks(t *testing.T) {
	x, _ := Get("x")
	l := x.Limits()
	if l.Links != LinksRemove || l.AllowsLinks(LinksKeep) || !l.AllowsLinks(LinksDefang) {
		t.Fatalf("x links: %+v", l)
	}
	c, _ := Get("custom")
	if c.Limits().Links != LinksKeep || !c.Limits().AllowsLinks(LinksKeep) {
		t.Fatalf("custom links: %+v", c.Limits())
	}
}
