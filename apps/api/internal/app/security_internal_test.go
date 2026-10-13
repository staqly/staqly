package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Audit M4: outbound destination policy, including addresses that embed an
// IPv4 address in IPv6 and ranges that are never legitimate targets.
func TestCheckIPPolicy(t *testing.T) {
	strict := WebhookConfig{}
	open := WebhookConfig{AllowPrivate: true, AllowLoopback: true}
	cases := []struct {
		ip           string
		strict, open bool // allowed?
	}{
		{"93.184.216.34", true, true},
		{"2606:4700::1111", true, true},
		{"10.1.2.3", false, true},
		{"192.168.1.1", false, true},
		{"fd00::1", false, true},
		{"100.64.1.1", false, true}, // CGNAT: governed by *_ALLOW_PRIVATE
		{"127.0.0.1", false, true},
		{"::1", false, true},
		{"169.254.169.254", false, false}, // metadata, always blocked
		{"0.0.0.0", false, false},
		{"0.1.2.3", false, false},
		{"192.0.0.170", false, false},
		{"198.18.0.1", false, false},
		{"240.0.0.1", false, false},
		{"255.255.255.255", false, false},
		{"168.63.129.16", false, false},
		{"2001::1", false, false},            // Teredo
		{"2001:db8::1", false, false},        // documentation
		{"::ffff:10.0.0.1", false, true},     // IPv4-mapped
		{"64:ff9b::a9fe:a9fe", false, false}, // NAT64 → 169.254.169.254
		{"64:ff9b::7f00:1", false, true},     // NAT64 → 127.0.0.1
		{"64:ff9b::5db8:d822", true, true},   // NAT64 → public
		{"64:ff9b:1::a00:1", false, true},    // local-use NAT64 → 10.0.0.1
		{"::a9fe:a9fe", false, false},        // IPv4-compatible → metadata
		{"::c0a8:101", false, true},          // IPv4-compatible → 192.168.1.1
		{"2002:a9fe:a9fe::1", false, false},  // 6to4 → metadata
		{"2002:0a00:0001::1", false, true},   // 6to4 → 10.0.0.1
		{"2002:5db8:d822::1", true, true},    // 6to4 → public
	}
	for _, c := range cases {
		ip := netip.MustParseAddr(c.ip)
		if got := checkIP(ip, strict) == nil; got != c.strict {
			t.Errorf("%s strict: allowed=%v want %v (%v)", c.ip, got, c.strict, checkIP(ip, strict))
		}
		if got := checkIP(ip, open) == nil; got != c.open {
			t.Errorf("%s permissive: allowed=%v want %v (%v)", c.ip, got, c.open, checkIP(ip, open))
		}
	}
	if err := checkIP(netip.MustParseAddr("10.0.0.1"), WebhookConfig{envPrefix: "NOTIFY"}); err == nil || !strings.Contains(err.Error(), "NOTIFY_ALLOW_PRIVATE") {
		t.Fatalf("error should name the right env var: %v", err)
	}
}

// Audit M10: the robots.txt cache is bounded, reads are capped and the rule
// list per host is bounded.
func TestRobotsLimits(t *testing.T) {
	var c robotsCache
	c.hosts = map[string]robotsRules{}
	base := time.Now()
	for i := range robotsMaxHosts + 100 {
		c.put(fmt.Sprintf("https://h%d.example", i), robotsRules{fetched: base.Add(time.Duration(i) * time.Second)})
	}
	if len(c.hosts) != robotsMaxHosts {
		t.Fatalf("cache size %d", len(c.hosts))
	}
	if _, ok := c.hosts["https://h0.example"]; ok {
		t.Fatal("oldest entry should have been evicted")
	}
	if _, ok := c.hosts[fmt.Sprintf("https://h%d.example", robotsMaxHosts+99)]; !ok {
		t.Fatal("newest entry missing")
	}

	var b strings.Builder
	b.WriteString("User-agent: *\n")
	for i := range 2000 {
		fmt.Fprintf(&b, "Disallow: /p%d\n", i)
	}
	if n := len(parseRobots([]byte(b.String()), "staqly-watcher")); n != robotsMaxRules {
		t.Fatalf("rules kept: %d", n)
	}
	// An agent group that disallows nothing allows everything (SA4018 fix).
	if d := parseRobots([]byte("User-agent: staqly-watcher\nAllow: /\n\nUser-agent: *\nDisallow: /"), "staqly-watcher"); len(d) != 0 {
		t.Fatalf("own empty group: %v", d)
	}

	big := "User-agent: *\nDisallow: /blocked\n" + strings.Repeat("# padding padding padding\n", 20000) // ~500 KB
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, big) }))
	defer srv.Close()
	s := &Service{Fetcher: srv.Client(), robots: robotsCache{hosts: map[string]robotsRules{}}}
	body, _, err := s.getRobots(t.Context(), srv.URL+"/robots.txt")
	if err != nil || len(body) != robotsMaxBytes {
		t.Fatalf("robots read: %d bytes, %v", len(body), err)
	}
	u, _ := url.Parse(srv.URL + "/blocked/page")
	if s.robotsAllowed(t.Context(), u) {
		t.Fatal("truncated robots.txt must still apply its leading rules")
	}
}

// Telegram reason prompts: version-bound refs and the older unbound format
// (prompts sent before the upgrade) both parse; the latter decides unbound,
// exactly like unversioned button payloads.
func TestTelegramReasonRefFormats(t *testing.T) {
	for text, want := range map[string][2]any{
		"ref: itm_abc123.7.AAAAAAAAAAA": {"itm_abc123", int64(7)},
		"ref: itm_abc123.AAAAAAAAAAA":   {"itm_abc123", int64(0)},
		"ref: itm_abc123.12345678901":   {"itm_abc123", int64(0)}, // all-digit signature, no version
	} {
		m := reTgRef.FindStringSubmatch(text)
		if m == nil {
			t.Fatalf("%q: no match", text)
		}
		id, v := parseItemRef(m[1])
		if id != want[0] || v != want[1] || len(m[2]) != 11 {
			t.Errorf("%q: %s %d sig %q", text, id, v, m[2])
		}
	}
}
