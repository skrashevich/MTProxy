package proxy

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Stats содержит атомарные счётчики производительности прокси.
// Соответствует полям структуры mtfront_worker_stats из mtproto-proxy.c.
type Stats struct {
	// Основные счётчики соединений
	ActiveConnections int64
	TotalConnections  int64

	// Трафик в байтах
	BytesIn  int64
	BytesOut int64

	// Счётчики пакетов
	TotForwardedQueries   int64
	TotForwardedResponses int64
	DroppedQueries        int64
	DroppedResponses      int64
	TotForwardedSimpleAck int64
	DroppedSimpleAck      int64
	MtprotoProxyErrors    int64

	// ext_connections (client ↔ backend mapping table)
	ExtConnections        int64
	ExtConnectionsCreated int64

	// HTTP stats endpoint
	HTTPQueries    int64
	HTTPBadHeaders int64

	// Per-secret counters (sync.Map: string(hex secret) -> *int64)
	perSecretConnections sync.Map
	perSecretAuthKeys    sync.Map

	startTime time.Time
}

// NewStats создаёт новый экземпляр Stats.
func NewStats() *Stats {
	return &Stats{
		startTime: time.Now(),
	}
}

// IncActiveConnections атомарно увеличивает счётчик активных соединений.
func (s *Stats) IncActiveConnections() {
	atomic.AddInt64(&s.ActiveConnections, 1)
	atomic.AddInt64(&s.TotalConnections, 1)
}

// DecActiveConnections атомарно уменьшает счётчик активных соединений.
func (s *Stats) DecActiveConnections() {
	atomic.AddInt64(&s.ActiveConnections, -1)
}

// AddBytesIn атомарно добавляет n к счётчику входящих байт.
func (s *Stats) AddBytesIn(n int64) {
	atomic.AddInt64(&s.BytesIn, n)
}

// AddBytesOut атомарно добавляет n к счётчику исходящих байт.
func (s *Stats) AddBytesOut(n int64) {
	atomic.AddInt64(&s.BytesOut, n)
}

// IncForwardedQuery увеличивает счётчик переданных запросов.
func (s *Stats) IncForwardedQuery() {
	atomic.AddInt64(&s.TotForwardedQueries, 1)
}

// IncDroppedQuery увеличивает счётчик отброшенных запросов.
func (s *Stats) IncDroppedQuery() {
	atomic.AddInt64(&s.DroppedQueries, 1)
}

// IncForwardedResponse увеличивает счётчик переданных ответов.
func (s *Stats) IncForwardedResponse() {
	atomic.AddInt64(&s.TotForwardedResponses, 1)
}

// IncDroppedResponse увеличивает счётчик отброшенных ответов.
func (s *Stats) IncDroppedResponse() {
	atomic.AddInt64(&s.DroppedResponses, 1)
}

// IncExtConn увеличивает счётчики ext_connections.
func (s *Stats) IncExtConn() {
	atomic.AddInt64(&s.ExtConnections, 1)
	atomic.AddInt64(&s.ExtConnectionsCreated, 1)
}

// DecExtConn уменьшает счётчик активных ext_connections.
func (s *Stats) DecExtConn() {
	atomic.AddInt64(&s.ExtConnections, -1)
}

// IncHTTPQuery увеличивает счётчик HTTP-запросов к /stats.
func (s *Stats) IncHTTPQuery() {
	atomic.AddInt64(&s.HTTPQueries, 1)
}

// secretKey возвращает строковый ключ для per-secret map.
func secretKey(secretIndex int) string {
	return fmt.Sprintf("%d", secretIndex)
}

// IncSecretConnections увеличивает счётчик активных соединений для секрета с индексом idx.
func (s *Stats) IncSecretConnections(idx int) {
	key := secretKey(idx)
	v, _ := s.perSecretConnections.LoadOrStore(key, new(int64))
	atomic.AddInt64(v.(*int64), 1)
}

// DecSecretConnections уменьшает счётчик активных соединений для секрета с индексом idx.
func (s *Stats) DecSecretConnections(idx int) {
	key := secretKey(idx)
	if v, ok := s.perSecretConnections.Load(key); ok {
		atomic.AddInt64(v.(*int64), -1)
	}
}

// GetSecretConnections возвращает текущее количество активных соединений для секрета.
func (s *Stats) GetSecretConnections(idx int) int64 {
	key := secretKey(idx)
	if v, ok := s.perSecretConnections.Load(key); ok {
		return atomic.LoadInt64(v.(*int64))
	}
	return 0
}

// IncSecretAuthKeys увеличивает счётчик активных auth_key для секрета с индексом idx.
func (s *Stats) IncSecretAuthKeys(idx int) {
	key := secretKey(idx)
	v, _ := s.perSecretAuthKeys.LoadOrStore(key, new(int64))
	atomic.AddInt64(v.(*int64), 1)
}

// DecSecretAuthKeys уменьшает счётчик активных auth_key для секрета с индексом idx.
func (s *Stats) DecSecretAuthKeys(idx int) {
	key := secretKey(idx)
	if v, ok := s.perSecretAuthKeys.Load(key); ok {
		atomic.AddInt64(v.(*int64), -1)
	}
}

// GetSecretAuthKeys возвращает текущее количество активных auth_key для секрета.
func (s *Stats) GetSecretAuthKeys(idx int) int64 {
	key := secretKey(idx)
	if v, ok := s.perSecretAuthKeys.Load(key); ok {
		return atomic.LoadInt64(v.(*int64))
	}
	return 0
}

// Snapshot возвращает снимок всех счётчиков в виде map для рендеринга.
func (s *Stats) Snapshot(secretCount int) map[string]int64 {
	m := map[string]int64{
		"active_connections":           atomic.LoadInt64(&s.ActiveConnections),
		"total_connections":            atomic.LoadInt64(&s.TotalConnections),
		"bytes_in":                     atomic.LoadInt64(&s.BytesIn),
		"bytes_out":                    atomic.LoadInt64(&s.BytesOut),
		"tot_forwarded_queries":        atomic.LoadInt64(&s.TotForwardedQueries),
		"tot_forwarded_responses":      atomic.LoadInt64(&s.TotForwardedResponses),
		"dropped_queries":              atomic.LoadInt64(&s.DroppedQueries),
		"dropped_responses":            atomic.LoadInt64(&s.DroppedResponses),
		"tot_forwarded_simple_acks":    atomic.LoadInt64(&s.TotForwardedSimpleAck),
		"dropped_simple_acks":          atomic.LoadInt64(&s.DroppedSimpleAck),
		"mtproto_proxy_errors":         atomic.LoadInt64(&s.MtprotoProxyErrors),
		"ext_connections":              atomic.LoadInt64(&s.ExtConnections),
		"ext_connections_created":      atomic.LoadInt64(&s.ExtConnectionsCreated),
		"http_queries":                 atomic.LoadInt64(&s.HTTPQueries),
		"http_bad_headers":             atomic.LoadInt64(&s.HTTPBadHeaders),
	}
	for i := 0; i < secretCount; i++ {
		m[fmt.Sprintf("secret_%d_active_connections", i+1)] = s.GetSecretConnections(i)
		m[fmt.Sprintf("secret_%d_active_auth_keys", i+1)] = s.GetSecretAuthKeys(i)
	}
	return m
}

// Uptime возвращает время работы в секундах.
func (s *Stats) Uptime() float64 {
	return time.Since(s.startTime).Seconds()
}
