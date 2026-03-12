package proxy

import (
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
)

// HTTPStatsServer обслуживает HTTP endpoint /stats совместимый с C-форматом.
// Формат ответа: "key\tvalue\n" (text/plain), как в mtfront_prepare_stats().
type HTTPStatsServer struct {
	addr        string
	stats       *Stats
	secretCount int
	proxyTag    []byte
	version     string
	server      *http.Server
}

// NewHTTPStatsServer создаёт HTTP сервер статистики.
func NewHTTPStatsServer(addr string, stats *Stats, secretCount int, proxyTag []byte, version string) *HTTPStatsServer {
	return &HTTPStatsServer{
		addr:        addr,
		stats:       stats,
		secretCount: secretCount,
		proxyTag:    proxyTag,
		version:     version,
	}
}

// Start запускает HTTP сервер в фоне. Возвращает ошибку если не удалось начать слушать.
func (h *HTTPStatsServer) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/stats", h.handleStats)
	mux.HandleFunc("/", h.handleStats) // C-прокси отвечает на любой GET

	ln, err := net.Listen("tcp", h.addr)
	if err != nil {
		return fmt.Errorf("http_stats listen %s: %w", h.addr, err)
	}

	h.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go h.server.Serve(ln)
	return nil
}

// Stop останавливает HTTP сервер.
func (h *HTTPStatsServer) Stop() {
	if h.server != nil {
		h.server.Close()
	}
}

// handleStats рендерит статистику в формате "key\tvalue\n".
// Совместим с форматом mtfront_prepare_stats() из C.
func (h *HTTPStatsServer) handleStats(w http.ResponseWriter, r *http.Request) {
	h.stats.IncHTTPQuery()

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	snap := h.stats.Snapshot(h.secretCount)
	uptime := h.stats.Uptime()

	var sb strings.Builder

	// Основные счётчики — в том же порядке, что mtfront_prepare_stats()
	writeStat := func(key string, value interface{}) {
		switch v := value.(type) {
		case int64:
			fmt.Fprintf(&sb, "%s\t%d\n", key, v)
		case int:
			fmt.Fprintf(&sb, "%s\t%d\n", key, v)
		case float64:
			fmt.Fprintf(&sb, "%s\t%.6f\n", key, v)
		case string:
			fmt.Fprintf(&sb, "%s\t%s\n", key, v)
		}
	}

	writeStat("uptime", int64(uptime))
	writeStat("tot_forwarded_queries", snap["tot_forwarded_queries"])
	writeStat("tot_forwarded_responses", snap["tot_forwarded_responses"])
	writeStat("dropped_queries", snap["dropped_queries"])
	writeStat("dropped_responses", snap["dropped_responses"])
	writeStat("tot_forwarded_simple_acks", snap["tot_forwarded_simple_acks"])
	writeStat("dropped_simple_acks", snap["dropped_simple_acks"])
	writeStat("total_connections", snap["active_connections"])
	writeStat("ext_connections", snap["ext_connections"])
	writeStat("ext_connections_created", snap["ext_connections_created"])
	writeStat("mtproto_proxy_errors", snap["mtproto_proxy_errors"])
	writeStat("http_queries", snap["http_queries"])
	writeStat("http_bad_headers", snap["http_bad_headers"])
	writeStat("http_qps", float64(snap["http_queries"])/uptime)

	proxyTagSet := 0
	if len(h.proxyTag) == 16 {
		proxyTagSet = 1
	}
	writeStat("proxy_tag_set", int64(proxyTagSet))
	writeStat("version", h.version)

	// per-secret счётчики (secret_1_active_connections, ...)
	// собираем и сортируем для детерминированного вывода
	type kv struct{ k string; v int64 }
	var secretStats []kv
	for k, v := range snap {
		if strings.HasPrefix(k, "secret_") {
			secretStats = append(secretStats, kv{k, v})
		}
	}
	sort.Slice(secretStats, func(i, j int) bool {
		return secretStats[i].k < secretStats[j].k
	})
	for _, s := range secretStats {
		writeStat(s.k, s.v)
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(sb.String()))
}
