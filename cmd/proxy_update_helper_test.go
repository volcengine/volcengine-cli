package cmd

import (
	"reflect"
	"testing"

	"github.com/volcengine/volcengine-cli/internal/proxycli"
)

func TestProxyUpdateHelperDetachStartsWorker(t *testing.T) {
	oldExecutable := proxyExecutablePath
	oldStart := startDetachedProxyUpdateWorker
	defer func() {
		proxyExecutablePath = oldExecutable
		startDetachedProxyUpdateWorker = oldStart
	}()
	proxyExecutablePath = func() (string, error) { return "/path/to/ve", nil }
	var gotPath string
	var gotArgs, gotEnv []string
	startDetachedProxyUpdateWorker = func(path string, args, env []string) error {
		gotPath = path
		gotArgs = append([]string(nil), args...)
		gotEnv = append([]string(nil), env...)
		return nil
	}
	if err := defaultLaunchProxyUpdateCheck(proxycli.ToolArkCLI, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/path/to/ve" {
		t.Fatalf("worker path = %q", gotPath)
	}
	if want := []string{internalProxyUpdateCommand, "worker", "arkcli", "1.2.3"}; !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("worker args = %q, want %q", gotArgs, want)
	}
	foundGuard := false
	for _, entry := range gotEnv {
		if entry == proxyUpdateHelperEnv+"=1" {
			foundGuard = true
		}
	}
	if !foundGuard {
		t.Fatal("worker environment is missing internal helper guard")
	}
}

func TestProxyUpdateHelperWorkerRefreshesCache(t *testing.T) {
	oldRefresh := refreshProxyUpdateCheck
	defer func() { refreshProxyUpdateCheck = oldRefresh }()
	t.Setenv(proxyUpdateHelperEnv, "1")
	var gotTool proxycli.Tool
	var gotCurrent string
	refreshProxyUpdateCheck = func(tool proxycli.Tool, current string) error {
		gotTool, gotCurrent = tool, current
		return nil
	}
	command := newProxyCLIUpdateHelperCommand()
	if err := command.RunE(command, []string{"worker", "toscli", "4.1.2"}); err != nil {
		t.Fatal(err)
	}
	if gotTool != proxycli.ToolTOSUtil || gotCurrent != "4.1.2" {
		t.Fatalf("worker refresh = (%s, %s)", gotTool, gotCurrent)
	}
}
