package cmd

import (
	"bytes"
	"errors"
	"io"
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

type fakeProxyManagerWithUpdate struct {
	*fakeProxyManager
	plan proxycli.UpdateCheckPlan
}

func (f *fakeProxyManagerWithUpdate) PrepareUpdateCheck(proxycli.Resolution) proxycli.UpdateCheckPlan {
	return f.plan
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

func TestRunProxyCLIRefreshFailureWarnsAndContinues(t *testing.T) {
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
	executed := false
	runProxyExecutable = func(string, []string, io.Reader, io.Writer, io.Writer) error {
		executed = true
		return nil
	}
	manager := &fakeProxyManager{resolved: proxycli.Resolution{Path: "/bin/arkcli"}}
	command := &cobra.Command{}
	var stderr bytes.Buffer
	command.SetErr(&stderr)

	err := runProxyCLI(command, ctx, manager, proxycli.ToolArkCLI, []string{"models", "list"})
	if err != nil {
		t.Fatalf("runProxyCLI() error = %v", err)
	}
	if !executed || len(manager.tools) != 1 {
		t.Fatalf("proxy did not continue: executed=%v tools=%v", executed, manager.tools)
	}
	if !strings.Contains(stderr.String(), "Warning") || !strings.Contains(stderr.String(), want.Error()) {
		t.Fatalf("refresh warning = %q", stderr.String())
	}
}

func TestRunProxyCLINoLoginSessionContinuesWithoutRefresh(t *testing.T) {
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
			"default": {Name: "default", Mode: ModeConsoleLogin},
		},
	}
	ensureProxyLoginToken = func(*Configure, string) (*STSCredentials, error) {
		t.Fatal("logged-out profile must not attempt refresh")
		return nil, nil
	}
	runProxyExecutable = func(string, []string, io.Reader, io.Writer, io.Writer) error { return nil }
	manager := &fakeProxyManager{resolved: proxycli.Resolution{Path: "/bin/arkcli"}}

	if err := runProxyCLI(&cobra.Command{}, ctx, manager, proxycli.ToolArkCLI, []string{"models", "list"}); err != nil {
		t.Fatalf("runProxyCLI() error = %v", err)
	}
}

func TestRunProxyCLILocalHelpAndVersionSkipCredentialRead(t *testing.T) {
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
	ensureProxyLoginToken = func(*Configure, string) (*STSCredentials, error) {
		t.Fatal("local help/version must not read credentials")
		return nil, nil
	}
	runProxyExecutable = func(string, []string, io.Reader, io.Writer, io.Writer) error { return nil }

	for _, args := range [][]string{
		{"--help"}, {"-h"}, {"--version"}, {"-v"}, {"help", "models"}, {"version"}, {"models", "list", "--help"},
	} {
		manager := &fakeProxyManager{resolved: proxycli.Resolution{Path: "/bin/arkcli"}}
		if err := runProxyCLI(&cobra.Command{}, ctx, manager, proxycli.ToolArkCLI, args); err != nil {
			t.Fatalf("runProxyCLI(%v) error = %v", args, err)
		}
	}
}

func TestProxyLocalInvocationDoesNotTreatBusinessArgumentsAsVersionFlags(t *testing.T) {
	for _, args := range [][]string{
		{"ls", "tos://bucket", "-v"},
		{"chat", "--prompt=--help"},
	} {
		if isProxyLocalInvocation(args) {
			t.Fatalf("business invocation was classified as local: %v", args)
		}
	}
}

func TestRunProxyCLIRejectsVeSystemOverrides(t *testing.T) {
	for _, name := range []string{"profile", "region", "endpoint"} {
		t.Run(name, func(t *testing.T) {
			ctx := NewContext()
			ctx.config = &Configure{Profiles: map[string]*Profile{"prod": {Name: "prod"}}}
			flag, err := ctx.fixedFlags.AddByName(name)
			if err != nil {
				t.Fatal(err)
			}
			flag.SetValue("value")

			err = runProxyCLI(&cobra.Command{}, ctx, &fakeProxyManager{}, proxycli.ToolArkCLI, nil)
			if err == nil || !strings.Contains(err.Error(), "ve arkcli --"+name) {
				t.Fatalf("runProxyCLI() error = %v", err)
			}
		})
	}
}

func TestProxyCommandNamesAreRegistered(t *testing.T) {
	for _, name := range []string{"arkcli", "toscli", "proxycli"} {
		if !isRegisteredRootSubcommand(name) {
			t.Fatalf("root command %q is not registered", name)
		}
	}
}

func TestRunProxyCLIPrintsManagedUpdateNotice(t *testing.T) {
	oldRun := runProxyExecutable
	defer func() { runProxyExecutable = oldRun }()
	runProxyExecutable = func(string, []string, io.Reader, io.Writer, io.Writer) error { return nil }
	manager := &fakeProxyManagerWithUpdate{
		fakeProxyManager: &fakeProxyManager{resolved: proxycli.Resolution{
			Tool: proxycli.ToolArkCLI, Path: "/managed/arkcli", Version: "1.0.0", Source: proxycli.SourceManaged,
		}},
		plan: proxycli.UpdateCheckPlan{
			HasNotice: true,
			Notice:    proxycli.UpdateCheckResult{Tool: proxycli.ToolArkCLI, Current: "1.0.0", Latest: "2.0.0", HasUpdate: true},
		},
	}
	command := &cobra.Command{}
	var stderr bytes.Buffer
	command.SetErr(&stderr)
	if err := runProxyCLI(command, NewContext(), manager, proxycli.ToolArkCLI, []string{"models", "list"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "2.0.0") || !strings.Contains(stderr.String(), "ve proxycli upgrade arkcli") {
		t.Fatalf("update notice = %q", stderr.String())
	}
}

func TestRunProxyCLILaunchesUpdateHelperBeforeDelegation(t *testing.T) {
	oldRun := runProxyExecutable
	oldLaunch := launchProxyUpdateCheck
	defer func() {
		runProxyExecutable = oldRun
		launchProxyUpdateCheck = oldLaunch
	}()
	var order []string
	launchProxyUpdateCheck = func(tool proxycli.Tool, current string) error {
		order = append(order, "helper:"+string(tool)+":"+current)
		return nil
	}
	runProxyExecutable = func(string, []string, io.Reader, io.Writer, io.Writer) error {
		order = append(order, "exec")
		return nil
	}
	manager := &fakeProxyManagerWithUpdate{
		fakeProxyManager: &fakeProxyManager{resolved: proxycli.Resolution{
			Tool: proxycli.ToolArkCLI, Path: "/managed/arkcli", Version: "1.0.0", Source: proxycli.SourceManaged,
		}},
		plan: proxycli.UpdateCheckPlan{Refresh: true},
	}
	if err := runProxyCLI(&cobra.Command{}, NewContext(), manager, proxycli.ToolArkCLI, []string{"models", "list"}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"helper:arkcli:1.0.0", "exec"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
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
