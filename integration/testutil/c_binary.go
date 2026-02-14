package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	cBuildOnce sync.Once
	cBuildErr  error
	cBinPath   string
)

func BuildCProxyBinary(t *testing.T) string {
	t.Helper()

	if p := os.Getenv("MTPROXY_C_BIN"); p != "" {
		return p
	}

	cBuildOnce.Do(func() {
		repoRoot := RepoRoot(t)
		cmd := exec.Command("make", "objs/bin/mtproto-proxy")
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			cBuildErr = fmt.Errorf("make objs/bin/mtproto-proxy failed: %w: %s", err, string(out))
			return
		}

		cBinPath = filepath.Join(repoRoot, "objs", "bin", "mtproto-proxy")
		if _, err := os.Stat(cBinPath); err != nil {
			cBuildErr = fmt.Errorf("c binary not found at %s: %w", cBinPath, err)
		}
	})

	if cBuildErr != nil {
		t.Fatalf("%v", cBuildErr)
	}
	return cBinPath
}
