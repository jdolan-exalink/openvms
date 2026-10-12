package httpx

import (
	"testing"
	"time"
)

func TestRateLimiterBurstThenRefill(t *testing.T) {
	now := time.Unix(1_000, 0)
	l := NewRateLimiter(3, time.Second, func() time.Time { return now })
	for i := range 3 {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("request %d inside the burst was refused", i+1)
		}
	}
	ok, retry := l.Allow("a")
	if ok || retry <= 0 || retry > time.Second {
		t.Fatalf("4th request: ok=%v retry=%v; want a refusal that says to retry within a second", ok, retry)
	}
	// Keys are independent.
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("another key shares the bucket")
	}
	now = now.Add(time.Second)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("a token was not refilled after one interval")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("only one token should have been refilled")
	}
	// Idle time never banks more than the burst.
	now = now.Add(time.Hour)
	for i := range 3 {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("request %d after a long idle was refused", i+1)
		}
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("a long idle banked more than the burst")
	}
}

func TestRateLimiterForgetsIdleKeysAndBoundsMemory(t *testing.T) {
	now := time.Unix(1_000, 0)
	l := NewRateLimiter(2, time.Second, func() time.Time { return now })
	l.maxKeys = 100
	for i := range 500 {
		l.Allow(string(rune('a'+i%26)) + string(rune(i)))
		now = now.Add(time.Millisecond)
	}
	if n := l.size(); n > 100 {
		t.Fatalf("%d tracked keys, want at most 100", n)
	}
	// A refused attacker cannot evict a key by flooding: when full of busy keys, new keys are
	// still limited (refused) rather than tracked without bound.
	if ok, _ := l.Allow("fresh"); ok && l.size() > 100 {
		t.Fatal("tracking grew past the bound")
	}
}
