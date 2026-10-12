package httpx

import (
	"sync"
	"time"
)

// DefaultRateLimiterKeys bounds how many distinct keys a RateLimiter tracks.
const DefaultRateLimiterKeys = 10000

// RateLimiter is a small in-memory token bucket per key (typically a client IP): a key can make
// burst requests at once and then one more every refillEvery. Memory is bounded: when the table
// is full, keys that have fully refilled (as good as new) are dropped, and if it is still full a
// new key is refused rather than tracked, so a flood of distinct addresses cannot grow it.
// It is per process, which is the right scope for a single API instance.
type RateLimiter struct {
	burst       float64
	refillEvery time.Duration
	now         func() time.Time
	maxKeys     int

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter allows burst requests per key, refilling one token every refillEvery. now is
// time.Now when nil.
func NewRateLimiter(burst int, refillEvery time.Duration, now func() time.Time) *RateLimiter {
	if now == nil {
		now = time.Now
	}
	if burst < 1 {
		burst = 1
	}
	if refillEvery <= 0 {
		refillEvery = time.Second
	}
	return &RateLimiter{burst: float64(burst), refillEvery: refillEvery, now: now, maxKeys: DefaultRateLimiterKeys, buckets: map[string]*bucket{}}
}

// Allow takes one token for key. When it refuses, retryAfter says when a token is available.
func (l *RateLimiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.now()
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= l.maxKeys {
			l.sweep(t)
			if len(l.buckets) >= l.maxKeys {
				return false, l.refillEvery
			}
		}
		b = &bucket{tokens: l.burst, last: t}
		l.buckets[key] = b
	} else if elapsed := t.Sub(b.last); elapsed > 0 {
		b.tokens = min(l.burst, b.tokens+float64(elapsed)/float64(l.refillEvery))
		b.last = t
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) * float64(l.refillEvery))
}

// sweep drops buckets that have refilled completely.
func (l *RateLimiter) sweep(t time.Time) {
	for k, b := range l.buckets {
		if b.tokens+float64(t.Sub(b.last))/float64(l.refillEvery) >= l.burst {
			delete(l.buckets, k)
		}
	}
}

func (l *RateLimiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
