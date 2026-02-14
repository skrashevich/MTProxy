package proxy

import (
	"testing"
	"time"
)

func TestFixedWindowRateLimiter(t *testing.T) {
	lim := newFixedWindowRateLimiter(2)
	now := time.Unix(1700000000, 0)

	if !lim.Allow(now) {
		t.Fatalf("first allow should pass")
	}
	if !lim.Allow(now) {
		t.Fatalf("second allow should pass")
	}
	if lim.Allow(now) {
		t.Fatalf("third allow in same second should fail")
	}

	if !lim.Allow(now.Add(time.Second)) {
		t.Fatalf("next-second allow should pass after window reset")
	}
}

func TestFixedWindowRateLimiterUnlimited(t *testing.T) {
	lim := newFixedWindowRateLimiter(0)
	now := time.Unix(1700000000, 0)
	for i := 0; i < 10; i++ {
		if !lim.Allow(now) {
			t.Fatalf("unlimited limiter should always allow")
		}
	}
}
