package httpapi

import (
	"crypto/subtle"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/staqly/staqly/apps/api/internal/web"
)

// rateLimiter is a sliding-window counter per key.
// ponytail: in-memory and per process; fine for one server, use a shared store if you scale out.
type rateLimiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	maxKeys int
	hits    map[string][]time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{max: max, window: window, maxKeys: 100_000, hits: map[string][]time.Time{}}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	recent := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			recent = append(recent, t)
		}
	}
	if len(recent) >= l.max {
		l.hits[key] = recent
		return false
	}
	if _, known := l.hits[key]; !known && len(l.hits) >= l.maxKeys {
		l.evict(now)
	}
	l.hits[key] = append(recent, now)
	return true
}

// evict bounds memory under key spraying without forgetting active
// offenders: drop keys whose hits all expired, then (if still near the cap)
// the keys with the fewest recent hits, oldest first, down to 90% of
// maxKeys. Sprayed keys carry one hit each, so a key that is at its limit
// survives; resetting the whole map would let an attacker clear it.
func (l *rateLimiter) evict(now time.Time) {
	type entry struct {
		key  string
		hits int
		last time.Time
	}
	var live []entry
	for k, ts := range l.hits {
		if len(ts) == 0 || now.Sub(ts[len(ts)-1]) >= l.window {
			delete(l.hits, k)
			continue
		}
		live = append(live, entry{k, len(ts), ts[len(ts)-1]})
	}
	target := l.maxKeys * 9 / 10
	if len(live) <= target {
		return
	}
	slices.SortFunc(live, func(a, b entry) int {
		if a.hits != b.hits {
			return a.hits - b.hits
		}
		return a.last.Compare(b.last)
	})
	for _, e := range live[:len(live)-target] {
		delete(l.hits, e.key)
	}
}

// clientIP returns the client address used as a rate-limit key. With
// TRUST_PROXY it is the right-most X-Forwarded-For entry (the one appended
// by your own proxy; anything to its left is client-controlled). IPv6
// clients are keyed by their /64, since one host typically owns a whole /64.
func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy || s.fromRelay(r) {
		if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
			parts := strings.Split(xff[len(xff)-1], ",")
			if ip, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1])); err == nil {
				return rateKeyIP(ip)
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return rateKeyIP(ip)
	}
	return host
}

func rateKeyIP(ip netip.Addr) string {
	ip = ip.Unmap()
	if ip.Is6() {
		p, _ := ip.Prefix(64)
		return p.String()
	}
	return ip.String()
}

// fromRelay reports whether r was relayed by the embedded dashboard (which replaced X-Forwarded-For with the
// browser's address).
func (s *Server) fromRelay(r *http.Request) bool {
	got := r.Header.Get(web.RelayHeader)
	return s.cfg.RelayToken != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.RelayToken)) == 1
}
