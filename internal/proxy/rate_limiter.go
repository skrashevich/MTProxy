package proxy

import (
	"sync"
	"sync/atomic"
)

// RateLimiter ограничивает количество одновременных соединений на секрет.
// Соответствует active_connections_per_secret[] из mtproto-proxy.c.
type RateLimiter struct {
	mu      sync.Mutex
	maxConn int // максимум соединений на один секрет (0 = без ограничений)
	counts  map[int]int64
}

// NewRateLimiter создаёт RateLimiter с заданным лимитом на секрет.
// maxConn <= 0 означает отсутствие лимита.
func NewRateLimiter(maxConn int) *RateLimiter {
	return &RateLimiter{
		maxConn: maxConn,
		counts:  make(map[int]int64),
	}
}

// Allow возвращает true и увеличивает счётчик, если соединение для данного
// секрета разрешено. Если лимит превышен — возвращает false.
func (r *RateLimiter) Allow(secretIdx int) bool {
	if r.maxConn <= 0 {
		r.mu.Lock()
		r.counts[secretIdx]++
		r.mu.Unlock()
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.counts[secretIdx] >= int64(r.maxConn) {
		return false
	}
	r.counts[secretIdx]++
	return true
}

// Release уменьшает счётчик соединений для секрета после разрыва.
func (r *RateLimiter) Release(secretIdx int) {
	r.mu.Lock()
	if r.counts[secretIdx] > 0 {
		r.counts[secretIdx]--
	}
	r.mu.Unlock()
}

// Count возвращает текущее число активных соединений для секрета.
func (r *RateLimiter) Count(secretIdx int) int64 {
	r.mu.Lock()
	v := r.counts[secretIdx]
	r.mu.Unlock()
	return v
}

// atomicRateLimiter — lock-free вариант для одного секрета (используется в тестах).
type atomicCounter struct {
	v int64
}

func (c *atomicCounter) Inc() int64 { return atomic.AddInt64(&c.v, 1) }
func (c *atomicCounter) Dec()       { atomic.AddInt64(&c.v, -1) }
func (c *atomicCounter) Load() int64 { return atomic.LoadInt64(&c.v) }
