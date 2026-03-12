package proxy

import (
	"sync"
	"testing"
)

func TestRateLimiter_AllowAndRelease(t *testing.T) {
	rl := NewRateLimiter(2)

	if !rl.Allow(0) {
		t.Fatal("first Allow should succeed")
	}
	if !rl.Allow(0) {
		t.Fatal("second Allow should succeed")
	}
	if rl.Allow(0) {
		t.Fatal("third Allow should be denied (limit=2)")
	}

	rl.Release(0)
	if !rl.Allow(0) {
		t.Fatal("Allow after Release should succeed")
	}
}

func TestRateLimiter_NoLimit(t *testing.T) {
	rl := NewRateLimiter(0)
	for i := 0; i < 1000; i++ {
		if !rl.Allow(0) {
			t.Fatalf("Allow %d failed with no limit", i)
		}
	}
}

func TestRateLimiter_MultipleSecrets(t *testing.T) {
	rl := NewRateLimiter(1)

	if !rl.Allow(0) {
		t.Fatal("secret 0 first Allow should succeed")
	}
	if rl.Allow(0) {
		t.Fatal("secret 0 second Allow should fail")
	}
	// secret 1 независим от secret 0
	if !rl.Allow(1) {
		t.Fatal("secret 1 first Allow should succeed")
	}
}

func TestRateLimiter_Concurrent(t *testing.T) {
	const limit = 10
	rl := NewRateLimiter(limit)

	var wg sync.WaitGroup
	allowed := make(chan struct{}, 100)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rl.Allow(0) {
				allowed <- struct{}{}
			}
		}()
	}
	wg.Wait()
	close(allowed)

	count := len(allowed)
	if count != limit {
		t.Errorf("concurrent Allow: %d succeeded, want %d", count, limit)
	}
}
