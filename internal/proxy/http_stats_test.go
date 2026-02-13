package proxy

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TelegramMessenger/MTProxy/internal/cli"
	"github.com/TelegramMessenger/MTProxy/internal/config"
)

func TestStatsServerServesStats(t *testing.T) {
	rt := NewRuntime(NewLifecycle(config.NewManager("/tmp/non-existent-config"), cli.Options{}), io.Discard)
	h := NewStatsHandler(rt)

	req := httptest.NewRequest("GET", "/stats", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	resp := rec.Result()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	text := string(body)
	for _, marker := range []string{
		"stats_generated_at\t",
		"has_current_config\t",
		"forward_total\t",
		"router_clusters\t",
	} {
		if !strings.Contains(text, marker) {
			t.Fatalf("stats response missing marker %q:\n%s", marker, text)
		}
	}
}
