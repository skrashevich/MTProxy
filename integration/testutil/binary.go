package testutil

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	buildOnce sync.Once
	buildErr  error
	binPath   string
)

func BuildProxyBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		repoRoot := RepoRoot(t)
		tmpDir, err := os.MkdirTemp("", "mtproxy-go-bin-*")
		if err != nil {
			buildErr = fmt.Errorf("create temp dir for binary: %w", err)
			return
		}
		binPath = filepath.Join(tmpDir, "mtproto-proxy-go")

		cmd := exec.Command("go", "build", "-o", binPath, "./cmd/mtproto-proxy")
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("go build failed: %w: %s", err, string(out))
			return
		}
	})
	if buildErr != nil {
		t.Fatalf("%v", buildErr)
	}
	return binPath
}

func RepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
