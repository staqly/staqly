package httpapi

import (
	"math"
	"sync"
	"time"
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

// evict drops buckets that have refilled completely (they behave exactly like
// a fresh bucket); if that frees less than 10% the map is reset, so the O(n)
// sweep stays amortized and memory stays bounded under a key-spraying attack.
func (l *tokenBucket) evict(now time.Time) {
	for k, b := range l.buckets {
		if b.tokens+now.Sub(b.last).Seconds()*l.rate >= l.burst {
			delete(l.buckets, k)
		}
	}
	if len(l.buckets) >= l.max*9/10 {
		l.buckets = map[string]*bucket{}
	}
}
