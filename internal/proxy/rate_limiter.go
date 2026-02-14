package proxy

import (
	"sync"
	"time"
)

// fixedWindowRateLimiter allows up to limit events per Unix-second window.
// A zero/negative limit is treated as unlimited (always allow).
type fixedWindowRateLimiter struct {
	limit int

	mu        sync.Mutex
	windowSec int64
	count     int
}

func newFixedWindowRateLimiter(limit int) *fixedWindowRateLimiter {
	if limit <= 0 {
		return nil
	}
	return &fixedWindowRateLimiter{
		limit: limit,
	}
}

func (l *fixedWindowRateLimiter) Allow(now time.Time) bool {
	if l == nil {
		return true
	}

	sec := now.Unix()
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.count == 0 || l.windowSec != sec {
		l.windowSec = sec
		l.count = 1
		return true
	}
	if l.count >= l.limit {
		return false
	}

	l.count++
	return true
}
