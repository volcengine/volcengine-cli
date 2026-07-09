package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestClientVersionCanBeInjectedByLdflags(t *testing.T) {
	tmpDir := t.TempDir()
	binName := "ve"
	if runtime.GOOS == "windows" {
		binName = "ve.exe"
	}
	binPath := filepath.Join(tmpDir, binName)
	injectedVersion := "9.8.7-test"

	build := exec.Command(
		"go",
		"build",
		"-o",
		binPath,
		"-ldflags",
		"-X github.com/volcengine/volcengine-cli/cmd.clientVersion="+injectedVersion,
		"../run.go",
	)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")

	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, output)
	}

	version := exec.Command(binPath, "--version")
	// Avoid background upgrade notice on stderr polluting CombinedOutput.
	version.Env = append(os.Environ(), "VOLCENGINE_CLI_DISABLE_UPDATE_CHECK=1")
	output, err := version.CombinedOutput()
	if err != nil {
		t.Fatalf("version command failed: %v\n%s", err, output)
	}

	if got := strings.TrimSpace(string(output)); got != injectedVersion {
		t.Fatalf("version = %q, want %q", got, injectedVersion)
	}
}
