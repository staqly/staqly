package web

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// CountryHeader carries, from the dashboard to the API in the same process (with RelayHeader), the browser's
// country as Cloudflare saw it (see ClientCountry); never the browser's own.
const CountryHeader = "X-Staqly-Country"

// cloudflareRanges: the addresses Cloudflare's proxy connects from (https://www.cloudflare.com/ips-v4 and
// /ips-v6, read 2026-11-15). Only a request from one of them carries a CF-Connecting-IP and CF-IPCountry
// Cloudflare set; anyone else can send those headers too.
// shortcut: a fixed list, as Cloudflare changes it rarely (with notice); update it from those URLs when it does.
var cloudflareRanges = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22", "141.101.64.0/18",
		"108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20", "197.234.240.0/22", "198.41.128.0/17",
		"162.158.0.0/15", "104.16.0.0/13", "104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
		"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32", "2405:8100::/32",
		"2a06:98c0::/29", "2c0f:f248::/32",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// fromCloudflare: whether ip is one of Cloudflare's proxy addresses.
func fromCloudflare(ip netip.Addr) bool {
	ip = ip.Unmap()
	for _, p := range cloudflareRanges {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// peerIP: the address that reached this server or, with trustProxy, your own reverse proxy (the right-most
// X-Forwarded-For entry, which that proxy appended; anything left of it is client-controlled).
func peerIP(r *http.Request, trustProxy bool) (netip.Addr, bool) {
	if xff := r.Header.Values("X-Forwarded-For"); trustProxy && len(xff) > 0 {
		parts := strings.Split(xff[len(xff)-1], ",")
		if ip, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1])); err == nil {
			return ip.Unmap(), true
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	return ip.Unmap(), err == nil
}

// ClientCountry: the browser's country (ISO 3166-1 alpha-2, upper case) from CF-IPCountry, only when the request
// came through Cloudflare (peerIP is one of its addresses); "" otherwise, and for Cloudflare's XX (unknown) and
// T1 (Tor).
func ClientCountry(r *http.Request, trustProxy bool) string {
	ip, ok := peerIP(r, trustProxy)
	if !ok || !fromCloudflare(ip) {
		return ""
	}
	cc := strings.ToUpper(strings.TrimSpace(r.Header.Get("CF-IPCountry")))
	if len(cc) != 2 || cc[0] < 'A' || cc[0] > 'Z' || cc[1] < 'A' || cc[1] > 'Z' || cc == "XX" {
		return ""
	}
	return cc
}
