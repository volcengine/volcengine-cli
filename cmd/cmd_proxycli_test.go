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

func TestRunProxyCLIMapsArkCLIEnvironment(t *testing.T) {
	tests := []struct {
		name      string
		tool      proxycli.Tool
		env       map[string]string
		additions map[string]string
	}{
		{
			name:      "default source without caller metadata",
			tool:      proxycli.ToolArkCLI,
			additions: map[string]string{"SOURCE_FROM": "ve"},
		},
		{
			name:      "caller type alone",
			tool:      proxycli.ToolArkCLI,
			env:       map[string]string{"VE_CALLER_TYPE": "ai_agent"},
			additions: map[string]string{"SOURCE_FROM": "ve", "ARKCLI_CALLER_TYPE": "ai_agent"},
		},
		{
			name:      "caller name alone",
			tool:      proxycli.ToolArkCLI,
			env:       map[string]string{"VE_CALLER_NAME": "custom-agent"},
			additions: map[string]string{"SOURCE_FROM": "ve", "ARKCLI_CALLER_NAME": "custom-agent"},
		},
		{
			name:      "skill name alone",
			tool:      proxycli.ToolArkCLI,
			env:       map[string]string{"VE_SKILL_NAME": "arkcli-models"},
			additions: map[string]string{"SOURCE_FROM": "ve", "ARKCLI_SKILL_NAME": "arkcli-models"},
		},
		{
			name: "all mappings replace existing targets and preserve raw values",
			tool: proxycli.ToolArkCLI,
			env: map[string]string{
				"VE_CALLER_TYPE": "automation", "VE_CALLER_NAME": " custom=agent ", "VE_SKILL_NAME": "arkcli-models",
				"ARKCLI_CALLER_TYPE": "old-type", "ARKCLI_CALLER_NAME": "old-agent", "ARKCLI_SKILL_NAME": "old-skill",
			},
			additions: map[string]string{
				"SOURCE_FROM": "ve", "ARKCLI_CALLER_TYPE": "automation", "ARKCLI_CALLER_NAME": " custom=agent ", "ARKCLI_SKILL_NAME": "arkcli-models",
			},
		},
		{
			name: "explicitly empty source values are also mapped",
			tool: proxycli.ToolArkCLI,
			env: map[string]string{
				"VE_CALLER_TYPE": "", "VE_CALLER_NAME": "", "VE_SKILL_NAME": "",
				"ARKCLI_CALLER_TYPE": "old-type", "ARKCLI_CALLER_NAME": "old-agent", "ARKCLI_SKILL_NAME": "old-skill",
			},
			additions: map[string]string{
				"SOURCE_FROM": "ve", "ARKCLI_CALLER_TYPE": "", "ARKCLI_CALLER_NAME": "", "ARKCLI_SKILL_NAME": "",
			},
		},
		{
			name: "existing source and unmapped arkcli values are preserved",
			tool: proxycli.ToolArkCLI,
			env: map[string]string{
				"SOURCE_FROM": "custom-source", "ARKCLI_CALLER_TYPE": "ai_agent", "ARKCLI_CALLER_NAME": "direct-agent", "ARKCLI_SKILL_NAME": "direct-skill",
			},
		},
		{
			name: "tosutil receives the original environment",
			tool: proxycli.ToolTOSUtil,
			env: map[string]string{
				"VE_CALLER_TYPE": "ai_agent", "VE_CALLER_NAME": "custom-agent", "VE_SKILL_NAME": "volcengine-tos",
				"ARKCLI_CALLER_NAME": "direct-agent",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := []string{
				"SOURCE_FROM", "VE_CALLER_TYPE", "VE_CALLER_NAME", "VE_SKILL_NAME",
				"ARKCLI_CALLER_TYPE", "ARKCLI_CALLER_NAME", "ARKCLI_SKILL_NAME", "PROXY_ENV_PASSTHROUGH",
			}
			tracked := make(map[string]bool, len(keys))
			for _, key := range keys {
				tracked[key] = true
				t.Cleanup(unsetenvForTest(t, key))
			}
			want := map[string]string{"PROXY_ENV_PASSTHROUGH": "original=value"}
			t.Setenv("PROXY_ENV_PASSTHROUGH", "original=value")
			for key, value := range tt.env {
				t.Setenv(key, value)
				want[key] = value
			}
			for key, value := range tt.additions {
				want[key] = value
			}
			parentEnv := os.Environ()
			oldRun := runProxyExecutable
			t.Cleanup(func() { runProxyExecutable = oldRun })
			got := make(map[string]string)
			runProxyExecutable = func(_ string, _ []string, _ io.Reader, _, _ io.Writer, env []string) error {
				for _, entry := range env {
					key, value, _ := strings.Cut(entry, "=")
					if tracked[key] {
						if _, exists := got[key]; exists {
							t.Fatalf("duplicate child environment key %q", key)
						}
						got[key] = value
					}
				}
				return nil
			}
			manager := &fakeProxyManager{resolved: proxycli.Resolution{Tool: tt.tool, Path: "/managed/" + string(tt.tool)}}
			if err := runProxyCLI(&cobra.Command{}, nil, manager, tt.tool, []string{"--help"}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("child metadata = %#v, want %#v", got, want)
			}
			if !reflect.DeepEqual(os.Environ(), parentEnv) {
				t.Fatal("delegation changed the parent process environment")
			}
		})
	}
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
	runProxyExecutable = func(path string, args []string, stdin io.Reader, stdout, stderr io.Writer, env []string) error {
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
	runProxyExecutable = func(string, []string, io.Reader, io.Writer, io.Writer, []string) error { return nil }
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
	runProxyExecutable = func(string, []string, io.Reader, io.Writer, io.Writer, []string) error {
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
	runProxyExecutable = func(string, []string, io.Reader, io.Writer, io.Writer, []string) error { return nil }
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
	runProxyExecutable = func(string, []string, io.Reader, io.Writer, io.Writer, []string) error { return nil }

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
	for _, name := range []string{"arkcli", "tosutil", "toscli", "proxycli"} {
		if !isRegisteredRootSubcommand(name) {
			t.Fatalf("root command %q is not registered", name)
		}
	}
}

func TestProxyManagementHelpUsesLocalCommandSyntax(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want []string
	}{
		{"management", []string{"proxycli", "--help"}, []string{"ve proxycli [command]", "upgrade"}},
		{"upgrade", []string{"proxycli", "upgrade", "--help"}, []string{"ve proxycli upgrade [tool-id]", "--yes"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := &cobra.Command{Use: "ve"}
			root.SetUsageTemplate(rootUsageTemplate())
			root.AddCommand(newProxyCLIManagementCommand())
			var output bytes.Buffer
			root.SetOut(&output)
			root.SetErr(&output)
			root.SetArgs(test.args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			for _, want := range test.want {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("help missing %q: %s", want, output.String())
				}
			}
			if strings.Contains(output.String(), "[service]") {
				t.Fatalf("proxy management inherited API usage: %s", output.String())
			}
		})
	}
}

func TestRunProxyCLIPrintsManagedUpdateNotice(t *testing.T) {
	oldRun := runProxyExecutable
	defer func() { runProxyExecutable = oldRun }()
	runProxyExecutable = func(string, []string, io.Reader, io.Writer, io.Writer, []string) error { return nil }
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
	runProxyExecutable = func(string, []string, io.Reader, io.Writer, io.Writer, []string) error {
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

func TestProxyRuntimePrefixAndPackageEnvironment(t *testing.T) {
	old := runProxyExecutable
	defer func() { runProxyExecutable = old }()
	manager := &fakeProxyManager{resolved: proxycli.Resolution{Tool: "emr-cli", Path: "/private/python", ArgsPrefix: []string{"-I", "-m", "scripts.cli"}, Environment: map[string]string{"PATH": "/private/bin", "LD_LIBRARY_PATH": "/private/lib"}}}
	child := []string{"--profile", "", "Unicode 空格", "--help"}
	runProxyExecutable = func(path string, args []string, _ io.Reader, _, _ io.Writer, env []string) error {
		if path != "/private/python" || !reflect.DeepEqual(args, append([]string{"-I", "-m", "scripts.cli"}, child...)) {
			t.Fatal(path, args)
		}
		for _, want := range []string{"PATH=/private/bin", "LD_LIBRARY_PATH=/private/lib"} {
			count := 0
			for _, entry := range env {
				if entry == want {
					count++
				}
			}
			if count != 1 {
				t.Fatal("package environment not replaced exactly once", want)
			}
		}
		return nil
	}
	if err := runProxyCLI(&cobra.Command{}, nil, manager, "emr-cli", child); err != nil {
		t.Fatal(err)
	}
}
