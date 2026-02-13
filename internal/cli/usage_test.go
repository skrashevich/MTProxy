package cli

import (
	"strings"
	"testing"
)

func TestUsageContainsExpectedMarkers(t *testing.T) {
	out := Usage("mtproto-proxy", "mtproxy-go-dev")

	for _, marker := range []string{
		"usage:",
		"Simple MT-Proto proxy",
		"-H<http-port>",
		"-S",
		"--mtproto-secret-file",
		"-P",
		"-D",
		"--slaves",
		"--max-special-connections",
		"--window-clamp",
		"--ping-interval",
		"--allow-skip-dh",
		"--disable-tcp",
	} {
		if !strings.Contains(out, marker) {
			t.Fatalf("usage output does not contain %q:\n%s", marker, out)
		}
	}
}
