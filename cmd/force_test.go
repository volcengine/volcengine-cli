package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestIsForceEnabled(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		want           bool
		wantPositional []string
	}{
		{name: "bare force flag", args: []string{"---force"}, want: true},
		{name: "force does not accept true value", args: []string{"---force", "true"}, want: true, wantPositional: []string{"true"}},
		{name: "force does not accept false value", args: []string{"---force", "false"}, want: true, wantPositional: []string{"false"}},
		{name: "no force", args: []string{"---version", "2024-01-01"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewContext()
			parser := NewParser(tt.args)
			positional, err := parser.ReadArgs(c)
			if err != nil {
				t.Fatalf("ReadArgs returned error: %v", err)
			}
			if got := isForceEnabled(c); got != tt.want {
				t.Fatalf("isForceEnabled() = %v, want %v", got, tt.want)
			}
			if tt.wantPositional != nil {
				if len(positional) != len(tt.wantPositional) {
					t.Fatalf("positional = %v, want %v", positional, tt.wantPositional)
				}
				for i := range tt.wantPositional {
					if positional[i] != tt.wantPositional[i] {
						t.Fatalf("positional[%d] = %q, want %q", i, positional[i], tt.wantPositional[i])
					}
				}
			}
		})
	}
}

func TestValidateForceCallRequiresVersion(t *testing.T) {
	c := NewContext()
	parser := NewParser([]string{"---force"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	err := validateForceCall(c, "newservice")
	if err == nil {
		t.Fatal("expected missing version error")
	}
	if !strings.Contains(err.Error(), "---version is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateForceCallRequiresEndpointForUnlistedService(t *testing.T) {
	c := NewContext()
	parser := NewParser([]string{"---force", "---version", "2024-01-01"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}

	err := validateForceCall(c, "newservice")
	if err == nil {
		t.Fatal("expected missing endpoint error")
	}
	if !strings.Contains(err.Error(), "---endpoint is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateForceCallFallsBackToMetadataVersionForKnownService(t *testing.T) {
	c := NewContext()
	parser := NewParser([]string{"---force"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	if err := validateForceCall(c, "sts"); err != nil {
		t.Fatalf("known service should fall back to metadata version, got: %v", err)
	}
	if got := apiVersionForCall(c, "sts"); got == "" {
		t.Fatal("expected non-empty metadata version for sts")
	}
}

func TestCallSdkReturnsErrorWhenEndpointResolutionFails(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ACCESS_KEY", "ak-test")()
	defer setenvForTest(t, "VOLCENGINE_SECRET_KEY", "sk-test")()
	defer setenvForTest(t, "VOLCENGINE_REGION", "cn-beijing")()

	c := NewContext()
	c.useStandardEndpointResolver = true
	sdk, err := NewSimpleClient(c)
	if err != nil {
		t.Fatalf("NewSimpleClient returned error: %v", err)
	}

	_, err = sdk.CallSdk(SdkClientInfo{
		ServiceName: "definitely_unlisted_service",
		Action:      "DescribeResource",
		Version:     "2024-01-01",
		Method:      "GET",
	}, &map[string]interface{}{})
	if err == nil {
		t.Fatal("expected endpoint resolution error")
	}
	if !strings.Contains(err.Error(), "failed to initialize SDK client") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildForceInvocationInputSetsJSONBodyWithoutMetadata(t *testing.T) {
	c := NewContext()
	parser := NewParser([]string{"--SomeParam", "value"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}

	built, err := buildForceInvocationInput(c, "newservice", "NewAction", "application/json")
	if err != nil {
		t.Fatalf("buildForceInvocationInput returned error: %v", err)
	}
	if !built.jsonBody {
		t.Fatal("expected jsonBody for application/json content type")
	}
	if built.fromBody {
		t.Fatal("expected fallback input without metadata")
	}
	input, ok := built.value.(map[string]interface{})
	if !ok || input["SomeParam"] != "value" {
		t.Fatalf("unexpected fallback input: %#v", built.value)
	}
}

func TestBuildForceInvocationInputUsesActionInputWithMetadata(t *testing.T) {
	for _, svc := range rootSupport.GetAllSvc() {
		for _, action := range rootSupport.GetAllAction(svc) {
			apiInfo := rootSupport.GetApiInfo(svc, action)
			if apiInfo == nil || strings.ToLower(apiInfo.ContentType) != "application/json" {
				continue
			}
			if rootSupport.GetApiMeta(svc, action) == nil {
				continue
			}
			c := NewContext()
			parser := NewParser([]string{"--body", `{"probe":"ok"}`})
			if _, err := parser.ReadArgs(c); err != nil {
				t.Fatalf("ReadArgs returned error: %v", err)
			}
			built, err := buildForceInvocationInput(c, svc, action, apiInfo.ContentType)
			if err != nil {
				t.Fatalf("buildForceInvocationInput(%s,%s): %v", svc, action, err)
			}
			if !built.jsonBody || !built.fromBody {
				t.Fatalf("expected metadata JSON action to use buildActionInput, got jsonBody=%v fromBody=%v", built.jsonBody, built.fromBody)
			}
			return
		}
	}
	t.Fatal("expected at least one application/json action in metadata")
}

func TestBuildForceInputOmitsFixedFlags(t *testing.T) {
	c := NewContext()
	parser := NewParser([]string{
		"---version", "2024-01-01",
		"---endpoint", "open.volcengineapi.com",
		"---force",
		"--SomeParam", "value",
		"--IPList", `["10.20.30.40"]`,
	})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}

	input := buildForceInput(c)
	if len(input) != 2 {
		t.Fatalf("expected 2 API params, got %#v", input)
	}
	if input["SomeParam"] != "value" {
		t.Fatalf("unexpected SomeParam: %#v", input["SomeParam"])
	}
	list, ok := input["IPList"].([]interface{})
	if !ok || len(list) != 1 {
		t.Fatalf("expected parsed IPList array, got %#v", input["IPList"])
	}
}

func TestTryExecuteGenericInvokeSkipsKnownService(t *testing.T) {
	err := tryExecuteGenericInvoke([]string{"sts", "GetCallerIdentity"})
	if !errors.Is(err, errNotGenericInvoke) {
		t.Fatalf("expected errNotGenericInvoke, got %v", err)
	}
}

func TestTryExecuteGenericInvokeSkipsBuiltinCommand(t *testing.T) {
	err := tryExecuteGenericInvoke([]string{"configure", "list"})
	if !errors.Is(err, errNotGenericInvoke) {
		t.Fatalf("expected errNotGenericInvoke, got %v", err)
	}
}

// ensureInitRootCmd 保证 help/version 等已注册。initRootCmd 非幂等（重复注册 flag 会 panic）。
func ensureInitRootCmd() {
	if rootCmd.Flags().Lookup("version") != nil {
		return
	}
	initRootCmd()
}

func TestTryExecuteGenericInvokeSkipsHelpCommand(t *testing.T) {
	// help/version 在 initRootCmd 中注册（与 runMain 一致）。
	ensureInitRootCmd()
	err := tryExecuteGenericInvoke([]string{"help"})
	if !errors.Is(err, errNotGenericInvoke) {
		t.Fatalf("expected errNotGenericInvoke, got %v", err)
	}
}

func TestIsRegisteredRootSubcommandCoversRootCommands(t *testing.T) {
	ensureInitRootCmd()

	// 动态覆盖：凡已挂到 rootCmd 的名称都必须被识别（新增根命令无需再改名单）。
	for _, c := range rootCmd.Commands() {
		name := c.Name()
		if !isRegisteredRootSubcommand(name) {
			t.Fatalf("isRegisteredRootSubcommand(%q) = false, want true", name)
		}
		if err := tryExecuteGenericInvoke([]string{name}); !errors.Is(err, errNotGenericInvoke) {
			t.Fatalf("tryExecuteGenericInvoke(%q) = %v, want errNotGenericInvoke", name, err)
		}
	}

	// 抽检若干 builtin / service
	for _, name := range []string{"configure", "login", "upgrade", "version", "sts", "help"} {
		if !isRegisteredRootSubcommand(name) {
			t.Fatalf("expected %q to be a registered root subcommand", name)
		}
	}
	if isRegisteredRootSubcommand("definitely-not-a-registered-service") {
		t.Fatal("unknown name should not be registered")
	}
}

func TestIsRegisteredRootSubcommandTracksNewRootCommand(t *testing.T) {
	const name = "force-reg-probe-cmd"
	if isRegisteredRootSubcommand(name) {
		t.Fatalf("%q unexpectedly already registered", name)
	}
	probe := &cobra.Command{Use: name, Hidden: true}
	rootCmd.AddCommand(probe)
	t.Cleanup(func() {
		rootCmd.RemoveCommand(probe)
	})
	if !isRegisteredRootSubcommand(name) {
		t.Fatalf("after AddCommand, isRegisteredRootSubcommand(%q) = false", name)
	}
	if err := tryExecuteGenericInvoke([]string{name, "AnyAction", "---force", "---version", "2024-01-01"}); !errors.Is(err, errNotGenericInvoke) {
		t.Fatalf("new root command should defer to cobra, got: %v", err)
	}
}

func TestTryExecuteGenericInvokeSkipsRootFlags(t *testing.T) {
	for _, arg := range []string{"-v", "--version", "-h", "--help", "-x", "--foo", "---force"} {
		t.Run(arg, func(t *testing.T) {
			err := tryExecuteGenericInvoke([]string{arg})
			if !errors.Is(err, errNotGenericInvoke) {
				t.Fatalf("expected errNotGenericInvoke, got %v", err)
			}
		})
	}
}

func TestTryExecuteGenericInvokeForceBeforeActionName(t *testing.T) {
	stubExecuteInvocation(t, errStubInvocation)
	err := tryExecuteGenericInvoke([]string{
		"newservice", "---version", "2024-01-01", "---endpoint", "newservice.cn-beijing.volcengineapi.com", "---force", "DescribeNewResource",
	})
	if !errors.Is(err, errStubInvocation) {
		t.Fatalf("expected stub invocation after force path, got: %v", err)
	}
	if strings.Contains(err.Error(), "use ---force with ---version") {
		t.Fatalf("---force before action should be recognized, got: %v", err)
	}
}

func TestTryExecuteGenericInvokeRequiresForce(t *testing.T) {
	err := tryExecuteGenericInvoke([]string{"newservice", "DescribeNewResource", "---version", "2024-01-01"})
	if err == nil {
		t.Fatal("expected error without ---force")
	}
	if !strings.Contains(err.Error(), "---force") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrintUnknownServiceHelp(t *testing.T) {
	oldStdout := os.Stdout
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("os.Pipe: %v", pipeErr)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = oldStdout })

	err := tryExecuteGenericInvoke([]string{"newservice", "-h"})
	w.Close()
	var buf bytes.Buffer
	if _, copyErr := io.Copy(&buf, r); copyErr != nil {
		t.Fatalf("read captured stdout: %v", copyErr)
	}
	if err != nil {
		t.Fatalf("expected help output without error, got: %v", err)
	}
	if !strings.Contains(buf.String(), "Use ---force with ---version and ---endpoint") {
		t.Fatalf("expected unknown service help text, got: %q", buf.String())
	}
}

func TestTryExecuteGenericInvokeShowsHelpWithoutAction(t *testing.T) {
	err := tryExecuteGenericInvoke([]string{"newservice"})
	if err != nil {
		t.Fatalf("expected usage help without error, got: %v", err)
	}
}

func TestResolveCallStyleUsesMetadataWhenAvailable(t *testing.T) {
	c := NewContext()
	method, contentType, err := resolveCallStyle(c, "sts", "GetCallerIdentity")
	if err != nil {
		t.Fatalf("resolveCallStyle: %v", err)
	}
	if method == "" {
		t.Fatal("expected method from metadata or default")
	}
	_ = contentType
}

func TestResolveCallStyleDefaultsToGETLikeNormalPath(t *testing.T) {
	c := NewContext()
	method, _, err := resolveCallStyle(c, "sts", "TotallyUnknownAction")
	if err != nil {
		t.Fatalf("resolveCallStyle: %v", err)
	}
	if method != "GET" {
		t.Fatalf("expected default GET, got %q", method)
	}
}

func TestExplicitHTTPMethodRejectsInvalidValue(t *testing.T) {
	c := NewContext()
	parser := NewParser([]string{"---method", "DELETE"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs: %v", err)
	}
	if _, err := explicitHTTPMethod(c); err == nil {
		t.Fatal("expected unsupported method error")
	}
}

func TestDispatchServiceActionRejectsUnknownWithoutForce(t *testing.T) {
	c := NewContext()
	err := dispatchServiceAction(c, "sts", "UnknownAction", false)
	if err == nil {
		t.Fatal("expected unsupported action error")
	}
	if !strings.Contains(err.Error(), "is not a supported action") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAPIVersionForCallPrefersFlag(t *testing.T) {
	c := NewContext()
	parser := NewParser([]string{"---version", "2099-01-01"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs: %v", err)
	}
	if got := apiVersionForCall(c, "sts"); got != "2099-01-01" {
		t.Fatalf("expected flag version, got %q", got)
	}
}

func TestTryExecuteGenericInvokeRequiresActionWhenOnlyFlags(t *testing.T) {
	err := tryExecuteGenericInvoke([]string{"newservice", "---version", "2024-01-01"})
	if err == nil {
		t.Fatal("expected error when only flags and no action")
	}
	if !strings.Contains(err.Error(), "specify an action name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunServiceCmdForceUnknownActionParsesFlags(t *testing.T) {
	captured := stubExecuteInvocation(t, errStubInvocation)
	err := runServiceCmd(&cobra.Command{}, "sts", []string{"GetCallerIdentity"},
		[]string{"UnknownAction", "---version", "2024-01-01", "---force"})
	if !errors.Is(err, errStubInvocation) {
		t.Fatalf("expected stub invocation error, got: %v", err)
	}
	if captured.action != "UnknownAction" || captured.version != "2024-01-01" {
		t.Fatalf("unexpected invocation params: action=%q version=%q", captured.action, captured.version)
	}
}
