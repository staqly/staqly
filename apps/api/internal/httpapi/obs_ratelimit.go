package httpapi

import (
	"math"
	"slices"
	"sync"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// tokenBucket is a memory-bounded token-bucket limiter keyed by an opaque
// string (client IP or credential hash).
// ponytail: in-memory and per process (single server by design); a
// multi-instance deployment would need a shared store.
type tokenBucket struct {
	mu      sync.Mutex
	rate    float64 // tokens per second
	burst   float64
	max     int // max tracked keys
	buckets map[string]*bucket
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newTokenBucket(perMinute, burst int, maxKeys int) *tokenBucket {
	if perMinute < 1 {
		perMinute = 1
	}
	if burst < 1 {
		burst = 1
	}
	return &tokenBucket{rate: float64(perMinute) / 60, burst: float64(burst), max: maxKeys,
		buckets: map[string]*bucket{}, now: time.Now}
}

// take consumes one token. When none is left it returns false and how long
// until the next token is available.
func (l *tokenBucket) take(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.max {
			l.evict(now)
		}
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	} else {
		b.tokens = math.Min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
	return false, wait
}

// has reports whether key has a bucket (it was taken from recently, or not yet evicted).
func (l *tokenBucket) has(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.buckets[key]
	return ok
}

// evict drops buckets that have refilled completely (they behave exactly like
// a fresh bucket); if the map is still above 90% of max, the fullest buckets
// (least drained, then least recently used) are dropped down to 90%. Memory
// stays bounded under a key-spraying attack while drained buckets — the
// clients actually being limited — are kept, instead of resetting everyone.
func (l *tokenBucket) evict(now time.Time) {
	type entry struct {
		key    string
		tokens float64
		last   time.Time
	}
	var live []entry
	for k, b := range l.buckets {
		t := b.tokens + now.Sub(b.last).Seconds()*l.rate
		if t >= l.burst {
			delete(l.buckets, k)
			continue
		}
		live = append(live, entry{k, t, b.last})
	}
	target := l.max * 9 / 10
	if len(live) <= target {
		return
	}
	slices.SortFunc(live, func(a, b entry) int {
		if a.tokens != b.tokens {
			if a.tokens > b.tokens {
				return -1
			}
			return 1
		}
		return a.last.Compare(b.last)
	})
	for _, e := range live[:len(live)-target] {
		delete(l.buckets, e.key)
	}
}

// TestLimiter rate-limits on-demand outbound test requests (webhook and
// notification channel tests) per principal, with the same budget as the
// REST test endpoints. MCP keeps its own instance.
// ponytail: separate from the REST bucket, so a caller using both transports
// gets twice the budget; share one if that ever matters.
type TestLimiter struct{ b *tokenBucket }

func NewTestLimiter() *TestLimiter { return &TestLimiter{b: newTokenBucket(10, 5, 10_000)} }

// Allow consumes one test request for p; when refused it returns the wait.
func (l *TestLimiter) Allow(p app.Principal) (bool, time.Duration) {
	return l.b.take(p.WorkspaceID + "/" + p.Kind + ":" + p.ID)
}
