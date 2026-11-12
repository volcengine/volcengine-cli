package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// A flag whose value was forgotten swallows the next unknown flag name as a
// literal value, which leaves that flag's value behind as a positional token.
// Every entry point must reject that token instead of silently dropping it.
func TestInvocationRejectsLeftoverPositionalArguments(t *testing.T) {
	forceFlags := []string{"--version", "2024-01-01", "--endpoint", "127.0.0.1:1", "--force"}
	tests := []struct {
		name    string
		run     func() error
		wantArg string
	}{
		{
			name: "unlisted service, missing value",
			run: func() error {
				return tryExecuteGenericInvoke(append([]string{"newservice", "Act", "--A", "--B", "1"}, forceFlags...))
			},
			wantArg: `"1"`,
		},
		{
			name: "unlisted service, value after presence-only --force",
			run: func() error {
				return tryExecuteGenericInvoke([]string{"newservice", "Act", "--version", "2024-01-01", "--endpoint", "127.0.0.1:1", "--force", "true"})
			},
			wantArg: `"true"`,
		},
		{
			name: "service command, unknown action, missing value",
			run: func() error {
				return runServiceCmd(&cobra.Command{}, "sts", []string{"GetCallerIdentity"},
					append([]string{"NoSuchAction", "--A", "--B", "1"}, forceFlags...))
			},
			wantArg: `"1"`,
		},
		{
			name: "action command, unknown flag name after missing value",
			run: func() error {
				return parseInvocationFlags([]string{"--InstanceId", "--Nmae", "x"}, map[string]struct{}{"InstanceId": {}, "Name": {}})
			},
			wantArg: `"x"`,
		},
		{
			name: "action command, value after presence-only --force",
			run: func() error {
				return parseInvocationFlags([]string{"--force", "true"}, map[string]struct{}{})
			},
			wantArg: `"true"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubExecuteInvocation(t, errStubInvocation)
			err := tt.run()
			if err == nil || errors.Is(err, errStubInvocation) {
				t.Fatalf("err = %v, want unexpected argument error before invocation", err)
			}
			if !strings.Contains(err.Error(), "unexpected argument "+tt.wantArg) {
				t.Fatalf("err = %q, want it to name %s", err.Error(), tt.wantArg)
			}
		})
	}
}

// Valid calls, including literal values that look like flags, must still reach
// the invocation.
func TestInvocationAcceptsSingleActionPositional(t *testing.T) {
	forceFlags := []string{"--version", "2024-01-01", "--endpoint", "127.0.0.1:1", "--force"}
	tests := []struct {
		name string
		run  func() error
	}{
		{"unlisted service", func() error {
			return tryExecuteGenericInvoke(append([]string{"newservice", "Act", "--A", "x", "--B", "-1", "--C", ""}, forceFlags...))
		}},
		{"unlisted service, flag-like literal value", func() error {
			return tryExecuteGenericInvoke(append([]string{"newservice", "Act", "--A", "--literal"}, forceFlags...))
		}},
		{"service command, unknown action", func() error {
			return runServiceCmd(&cobra.Command{}, "sts", []string{"GetCallerIdentity"},
				append([]string{"NoSuchAction", "--A", "x"}, forceFlags...))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubExecuteInvocation(t, errStubInvocation)
			if err := tt.run(); !errors.Is(err, errStubInvocation) {
				t.Fatalf("err = %v, want the call to reach invocation", err)
			}
		})
	}
	if err := parseInvocationFlags([]string{"--InstanceId", "i-1", "--Name", "--literal", "--force"}, map[string]struct{}{"InstanceId": {}, "Name": {}}); err != nil {
		t.Fatalf("parseInvocationFlags valid args: %v", err)
	}
}

// Leftover arguments are checked only once the call would otherwise proceed,
// so the more fundamental errors keep priority.
func TestInvocationKeepsPriorErrorsBeforeLeftoverArgumentCheck(t *testing.T) {
	stubExecuteInvocation(t, errStubInvocation)

	err := tryExecuteGenericInvoke([]string{"newservice", "Act", "extra", "--version", "2024-01-01"})
	if err == nil || !strings.Contains(err.Error(), `unknown service "newservice": use --force`) {
		t.Fatalf("unlisted service without --force: err = %v, want the --force hint", err)
	}

	err = runServiceCmd(&cobra.Command{}, "sts", []string{"GetCallerIdentity"}, []string{"GetCallerIdentityy", "--A", "--B", "1"})
	if err == nil || !strings.Contains(err.Error(), `"GetCallerIdentityy" is not a supported action of "sts"`) {
		t.Fatalf("unknown action without --force: err = %v, want the unsupported action error", err)
	}
}
