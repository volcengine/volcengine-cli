package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestIsForceEnabled(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "bare force flag", args: []string{"---force"}, want: true},
		{name: "force true", args: []string{"---force", "true"}, want: true},
		{name: "force false", args: []string{"---force", "false"}, want: false},
		{name: "no force", args: []string{"---version", "2024-01-01"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewContext()
			parser := NewParser(tt.args)
			if _, err := parser.ReadArgs(c); err != nil {
				t.Fatalf("ReadArgs returned error: %v", err)
			}
			if got := isForceEnabled(c); got != tt.want {
				t.Fatalf("isForceEnabled() = %v, want %v", got, tt.want)
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

func TestTryExecuteGenericInvokeForceBeforeActionName(t *testing.T) {
	err := tryExecuteGenericInvoke([]string{
		"newservice", "---version", "2024-01-01", "---force", "DescribeNewResource",
	})
	if err == nil {
		t.Fatal("expected downstream error because credentials are not configured in unit test")
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
	err := tryExecuteGenericInvoke([]string{"newservice", "-h"})
	if err != nil {
		t.Fatalf("expected help output without error, got: %v", err)
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
	err := runServiceCmd(&cobra.Command{}, "sts", []string{"GetCallerIdentity"},
		[]string{"UnknownAction", "---version", "2024-01-01", "---force"})
	if err == nil {
		t.Fatal("expected error because SDK call is not configured in unit test")
	}
	if strings.Contains(err.Error(), "is not a supported action") {
		t.Fatalf("force path should bypass unsupported action error, got: %v", err)
	}
}