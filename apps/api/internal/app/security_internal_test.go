package app

import (
	"net/netip"
	"strings"
	"testing"
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
