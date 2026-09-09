package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/volcengine/volcengine-cli/internal/proxycli"
)

type fakeProxyManager struct {
	resolved proxycli.Resolution
	result   proxycli.UpgradeResult
	err      error
	tools    []proxycli.Tool
}

func (f *fakeProxyManager) Resolve(tool proxycli.Tool) (proxycli.Resolution, error) {
	f.tools = append(f.tools, tool)
	return f.resolved, f.err
}

func (f *fakeProxyManager) Upgrade(tools []proxycli.Tool) (proxycli.UpgradeResult, error) {
	f.tools = append(f.tools, tools...)
	return f.result, f.err
}

func TestRunProxyCLIRefreshesConsoleLoginAndForwardsEveryArgument(t *testing.T) {
	oldEnsure := ensureProxyLoginToken
	oldRun := runProxyExecutable
	defer func() {
		ensureProxyLoginToken = oldEnsure
		runProxyExecutable = oldRun
	}()

	ctx := NewContext()
	ctx.config = &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {Name: "default", Mode: ModeConsoleLogin, LoginSession: "session"},
		},
	}
	var refreshed string
	ensureProxyLoginToken = func(cfg *Configure, profile string) (*STSCredentials, error) {
		refreshed = profile
		return &STSCredentials{}, nil
	}
	var gotPath string
	var gotArgs []string
	runProxyExecutable = func(path string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
		gotPath = path
		gotArgs = append([]string(nil), args...)
		return nil
	}

	manager := &fakeProxyManager{resolved: proxycli.Resolution{
		Path: "/managed/arkcli", Version: "1.2.3", Source: proxycli.SourceCDN,
	}}
	args := []string{"models", "search", "seed", "--profile", "ark-profile", "--lang", "zh_cn", "--", "literal"}
	command := &cobra.Command{}
	command.SetIn(strings.NewReader("stdin"))
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)

	if err := runProxyCLI(command, ctx, manager, proxycli.ToolArkCLI, args); err != nil {
		t.Fatalf("runProxyCLI() error = %v", err)
	}
	if refreshed != "default" {
		t.Fatalf("refreshed profile = %q", refreshed)
	}
	if gotPath != "/managed/arkcli" || !reflect.DeepEqual(gotArgs, args) {
		t.Fatalf("forwarded path=%q args=%q", gotPath, gotArgs)
	}
	if !strings.Contains(stderr.String(), "Installed arkcli 1.2.3 from cdn") {
		t.Fatalf("install notice = %q", stderr.String())
	}
}

func TestRunProxyCLIDoesNotRefreshNonConsoleProfile(t *testing.T) {
	oldEnsure := ensureProxyLoginToken
	oldRun := runProxyExecutable
	defer func() {
		ensureProxyLoginToken = oldEnsure
		runProxyExecutable = oldRun
	}()

	ctx := NewContext()
	ctx.config = &Configure{
		Current:  "default",
		Profiles: map[string]*Profile{"default": {Name: "default", Mode: ModeAK}},
	}
	ensureProxyLoginToken = func(*Configure, string) (*STSCredentials, error) {
		t.Fatal("AK profile must not refresh console-login token")
		return nil, nil
	}
	runProxyExecutable = func(string, []string, io.Reader, io.Writer, io.Writer) error { return nil }
	manager := &fakeProxyManager{resolved: proxycli.Resolution{Path: "/bin/tosutil"}}

	if err := runProxyCLI(&cobra.Command{}, ctx, manager, proxycli.ToolTOSUtil, []string{"version"}); err != nil {
		t.Fatalf("runProxyCLI() error = %v", err)
	}
}

func TestRunProxyCLIRefreshFailurePreventsResolutionAndExecution(t *testing.T) {
	oldEnsure := ensureProxyLoginToken
	oldRun := runProxyExecutable
	defer func() {
		ensureProxyLoginToken = oldEnsure
		runProxyExecutable = oldRun
	}()

	ctx := NewContext()
	ctx.config = &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {Name: "default", Mode: ModeConsoleLogin, LoginSession: "session"},
		},
	}
	want := errors.New("refresh failed")
	ensureProxyLoginToken = func(*Configure, string) (*STSCredentials, error) { return nil, want }
	runProxyExecutable = func(string, []string, io.Reader, io.Writer, io.Writer) error {
		t.Fatal("child must not execute after refresh failure")
		return nil
	}
	manager := &fakeProxyManager{resolved: proxycli.Resolution{Path: "/bin/arkcli"}}

	err := runProxyCLI(&cobra.Command{}, ctx, manager, proxycli.ToolArkCLI, nil)
	if !errors.Is(err, want) {
		t.Fatalf("runProxyCLI() error = %v", err)
	}
	if len(manager.tools) != 0 {
		t.Fatalf("manager resolved after refresh failure: %v", manager.tools)
	}
}

func TestRunProxyCLIRejectsVeProfileOverride(t *testing.T) {
	ctx := NewContext()
	ctx.config = &Configure{Profiles: map[string]*Profile{"prod": {Name: "prod"}}}
	flag, err := ctx.fixedFlags.AddByName("profile")
	if err != nil {
		t.Fatal(err)
	}
	flag.SetValue("prod")

	err = runProxyCLI(&cobra.Command{}, ctx, &fakeProxyManager{}, proxycli.ToolArkCLI, nil)
	if err == nil || !strings.Contains(err.Error(), "ve arkcli --profile") {
		t.Fatalf("runProxyCLI() error = %v", err)
	}
}

func TestDefaultRunProxyExecutablePreservesExitCode(t *testing.T) {
	if os.Getenv("VE_TEST_PROXY_CHILD") == "1" {
		os.Exit(23)
	}
	err := defaultRunProxyExecutable(
		os.Args[0],
		[]string{"-test.run=^TestDefaultRunProxyExecutablePreservesExitCode$"},
		nil,
		io.Discard,
		io.Discard,
		[]string{"VE_TEST_PROXY_CHILD=1"},
	)
	var exitErr *proxyProcessExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 23 {
		t.Fatalf("defaultRunProxyExecutable() error = %#v", err)
	}
}

func TestProxyCommandNamesAreRegistered(t *testing.T) {
	for _, name := range []string{"arkcli", "toscli", "proxycli"} {
		if !isRegisteredRootSubcommand(name) {
			t.Fatalf("root command %q is not registered", name)
		}
	}
}

func TestProxyCLIManagedPathUsesEnvironmentOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(proxycli.EnvProxyPath, dir)
	m, err := proxycli.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if m.BinDir != dir {
		t.Fatalf("BinDir = %q, want %q", m.BinDir, dir)
	}
	if !filepath.IsAbs(m.BinDir) {
		t.Fatalf("BinDir is not absolute: %q", m.BinDir)
	}
}
