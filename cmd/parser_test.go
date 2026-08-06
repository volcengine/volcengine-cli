package cmd

import (
	"strings"
	"testing"
)

func TestParserAcceptsEmptyStringFlagValue(t *testing.T) {
	// Explicit empty token is a valid value (shell: --Name "").
	// Missing value remains an error (trailing flag / consecutive flags).
	parser := NewParser([]string{"--Name", "", "--ZoneId", "cn-beijing"})
	ctx := NewContext()
	_, err := parser.ReadArgs(ctx)
	if err != nil {
		t.Fatalf("empty string value should be accepted: %v", err)
	}
	name := ctx.dynamicFlags.GetByName("Name")
	if name == nil {
		t.Fatal("expected Name flag")
	}
	if name.GetValue() != "" {
		t.Fatalf("Name value = %q, want empty string", name.GetValue())
	}
	// Empty string must still count as "set" (values slice non-empty), not as missing.
	if len(name.GetValues()) != 1 || name.GetValues()[0] != "" {
		t.Fatalf("Name GetValues = %v, want [\"\"]", name.GetValues())
	}
	zone := ctx.dynamicFlags.GetByName("ZoneId")
	if zone == nil || zone.GetValue() != "cn-beijing" {
		t.Fatalf("ZoneId should still parse after empty Name, got %#v", zone)
	}
}

func TestParserReturnsErrorWhenTrailingFlagHasNoValue(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "dynamic flag",
			args:    []string{"--InstanceId"},
			wantErr: "--InstanceId must set value.",
		},
		{
			name:    "fixed flag",
			args:    []string{"---profile"},
			wantErr: "---profile must set value.",
		},
		{
			name:    "double dash system flag",
			args:    []string{"--profile"},
			wantErr: "--profile must set value.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser(tt.args)

			_, err := parser.ReadArgs(NewContext())
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestParserRejectsUnsupportedFixedFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "unsupported fixed flag",
			args: []string{"---trace", "true"},
			want: "---trace is not supported",
		},
		{
			name: "misspelled region",
			args: []string{"---rgeioin", "cn-beijing"},
			want: "---rgeioin is not supported",
		},
		{
			name: "removed debug flag",
			args: []string{"---debug", "true"},
			want: "---debug is not supported",
		},
		{
			name: "removed debug log file flag",
			args: []string{"---debug-log-file", "./ve-debug.log"},
			want: "---debug-log-file is not supported",
		},
		{
			name: "language is handled before action parsing",
			args: []string{"---lang", "ZH"},
			want: "---lang is not supported",
		},
		{
			name: "fixed flag equals syntax",
			args: []string{"---region=cn-beijing"},
			want: "---region=cn-beijing is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser(tt.args)

			_, err := parser.ReadArgs(NewContext())
			if err == nil {
				t.Fatal("expected unsupported fixed flag error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tt.want)
			}
			if !strings.Contains(err.Error(), "supported fixed flags") {
				t.Fatalf("error = %q, want supported fixed flags", err.Error())
			}
			// ---lang is preprocessed, so the parser hint lists other triple-dash control flags.
			if !strings.Contains(err.Error(), supportedLegacyFixedFlagsMessage) {
				t.Fatalf("error = %q, want legacy fixed-flag list %q", err.Error(), supportedLegacyFixedFlagsMessage)
			}
		})
	}
}

func TestParserAcceptsOnlySupportedFixedFlags(t *testing.T) {
	parser := NewParser([]string{
		"---profile", "test",
		"---region", "cn-beijing",
		"---endpoint", "sts.volcengineapi.com",
		"---version", "2024-01-01",
		"---force",
	})
	ctx := NewContext()

	_, err := parser.ReadArgs(ctx)
	if err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	for _, name := range []string{"profile", "region", "endpoint", "version", "force"} {
		if ctx.fixedFlags.GetByName(name) == nil {
			t.Fatalf("expected fixed flag %q to be accepted", name)
		}
	}
	if ctx.fixedFlags.GetByName("force").GetValue() != "true" {
		t.Fatalf("expected bare ---force to default to true, got %q", ctx.fixedFlags.GetByName("force").GetValue())
	}
}

func TestParserForceFlagBeforeActionName(t *testing.T) {
	parser := NewParser([]string{"---version", "2024-01-01", "---force", "DescribeNewResource"})
	ctx := NewContext()

	positional, err := parser.ReadArgs(ctx)
	if err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	if !isForceEnabled(ctx) {
		t.Fatal("expected ---force to be enabled when followed by action name")
	}
	if ctx.fixedFlags.GetByName("version").GetValue() != "2024-01-01" {
		t.Fatalf("unexpected version: %q", ctx.fixedFlags.GetByName("version").GetValue())
	}
	if len(positional) != 1 || positional[0] != "DescribeNewResource" {
		t.Fatalf("expected action as positional arg, got %#v", positional)
	}
}

func TestParserDynamicForceFlagRequiresValue(t *testing.T) {
	parser := NewParser([]string{"--force", "true", "SomeAction"})
	ctx := NewContext()

	positional, err := parser.ReadArgs(ctx)
	if err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	if ctx.dynamicFlags.GetByName("force") == nil {
		t.Fatal("expected dynamic --force flag")
	}
	if got := ctx.dynamicFlags.GetByName("force").GetValue(); got != "true" {
		t.Fatalf("expected dynamic --force=true, got %q", got)
	}
	if isForceEnabled(ctx) {
		t.Fatal("---force switch must not be enabled by dynamic --force")
	}
	if len(positional) != 1 || positional[0] != "SomeAction" {
		t.Fatalf("expected action positional arg, got %#v", positional)
	}
}

func TestParserForceFlagDoesNotConsumeNextToken(t *testing.T) {
	parser := NewParser([]string{"---force", "false", "DescribeNewResource"})
	ctx := NewContext()

	positional, err := parser.ReadArgs(ctx)
	if err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	if !isForceEnabled(ctx) {
		t.Fatal("expected ---force to enable force without consuming next token")
	}
	if len(positional) != 2 || positional[0] != "false" || positional[1] != "DescribeNewResource" {
		t.Fatalf("expected following tokens as positional args, got %#v", positional)
	}
}

func TestParserForceFlagWithoutValueBeforeDynamicFlag(t *testing.T) {
	parser := NewParser([]string{"---force", "--SomeParam", "value"})
	ctx := NewContext()

	_, err := parser.ReadArgs(ctx)
	if err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	if ctx.fixedFlags.GetByName("force").GetValue() != "true" {
		t.Fatalf("expected ---force before --SomeParam to default to true")
	}
	if ctx.dynamicFlags.GetByName("SomeParam").GetValue() != "value" {
		t.Fatalf("expected SomeParam=value, got %q", ctx.dynamicFlags.GetByName("SomeParam").GetValue())
	}
}

func TestParserAcceptsPEMValuesStartingWithHyphens(t *testing.T) {
	publicKey := "-----BEGIN CERTIFICATE-----\ncertificate-data\n-----END CERTIFICATE-----"
	privateKey := "-----BEGIN RSA PRIVATE KEY-----\nprivate-key-data\n-----END RSA PRIVATE KEY-----"
	parser := NewParser([]string{
		"--CertificateName", "repro-test",
		"--PublicKey", publicKey,
		"--PrivateKey", privateKey,
		"---region", "cn-beijing",
	})
	ctx := NewContext()

	if _, err := parser.ReadArgs(ctx); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}

	for name, want := range map[string]string{
		"CertificateName": "repro-test",
		"PublicKey":       publicKey,
		"PrivateKey":      privateKey,
	} {
		flag := ctx.dynamicFlags.GetByName(name)
		if flag == nil {
			t.Fatalf("expected dynamic flag %q", name)
		}
		if got := flag.GetValue(); got != want {
			t.Fatalf("dynamic flag %q value = %q, want %q", name, got, want)
		}
	}

	region := ctx.fixedFlags.GetByName("region")
	if region == nil {
		t.Fatal("expected fixed flag \"region\"")
	}
	if got := region.GetValue(); got != "cn-beijing" {
		t.Fatalf("fixed flag \"region\" value = %q, want %q", got, "cn-beijing")
	}
}

func TestParserTreatsNextTokenAsValueRegardlessOfLeadingHyphens(t *testing.T) {
	parser := NewParser([]string{
		"--SingleHyphen", "-value",
		"--DoubleHyphen", "--value",
		"--TripleHyphen", "---value",
		"--FourHyphens", "----value",
		"--FiveHyphens", "-----value",
		"---profile", "---profile-value",
		"---region", "cn-beijing",
	})
	ctx := NewContext()

	if _, err := parser.ReadArgs(ctx); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}

	for name, want := range map[string]string{
		"SingleHyphen": "-value",
		"DoubleHyphen": "--value",
		"TripleHyphen": "---value",
		"FourHyphens":  "----value",
		"FiveHyphens":  "-----value",
	} {
		flag := ctx.dynamicFlags.GetByName(name)
		if flag == nil {
			t.Fatalf("expected dynamic flag %q", name)
		}
		if got := flag.GetValue(); got != want {
			t.Fatalf("dynamic flag %q value = %q, want %q", name, got, want)
		}
	}

	profile := ctx.fixedFlags.GetByName("profile")
	if profile == nil || profile.GetValue() != "---profile-value" {
		t.Fatalf("fixed flag \"profile\" = %#v, want value %q", profile, "---profile-value")
	}
	region := ctx.fixedFlags.GetByName("region")
	if region == nil || region.GetValue() != "cn-beijing" {
		t.Fatalf("fixed flag \"region\" = %#v, want value %q", region, "cn-beijing")
	}
}

func TestParserUsesLegacyDiagnosticsForOddArgumentCount(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "dynamic flag before dynamic flag",
			args:    []string{"--Foo", "--Bar", "1"},
			wantErr: "--Foo must set value.",
		},
		{
			name:    "dynamic flag before fixed flag",
			args:    []string{"--Foo", "---region", "cn-beijing"},
			wantErr: "--Foo must set value.",
		},
		{
			name:    "fixed flag before dynamic flag",
			args:    []string{"---profile", "--Foo", "1"},
			wantErr: "---profile must set value.",
		},
		{
			name:    "trailing flag after complete pair",
			args:    []string{"--Foo", "value", "--Bar"},
			wantErr: "--Bar must set value.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewParser(tt.args).ReadArgs(NewContext())
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestParserContinuesToIgnoreUnpairedPositionalArgument(t *testing.T) {
	ctx := NewContext()
	positional, err := NewParser([]string{"--Foo", "value", "unexpected"}).ReadArgs(ctx)
	if err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	if len(positional) != 1 || positional[0] != "unexpected" {
		t.Fatalf("positional arguments = %#v, want []string{%q}", positional, "unexpected")
	}
	foo := ctx.dynamicFlags.GetByName("Foo")
	if foo == nil || foo.GetValue() != "value" {
		t.Fatalf("dynamic flag \"Foo\" = %#v, want value %q", foo, "value")
	}
}

func TestParserContinuesToIgnorePositionalArguments(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		wantPositional  []string
		wantDynamicName string
		wantDynamicVal  string
	}{
		{
			name:           "two positional arguments",
			args:           []string{"unexpected", "value"},
			wantPositional: []string{"unexpected", "value"},
		},
		{
			name:            "two trailing positional arguments",
			args:            []string{"--Foo", "value", "extra1", "extra2"},
			wantPositional:  []string{"extra1", "extra2"},
			wantDynamicName: "Foo",
			wantDynamicVal:  "value",
		},
		{
			name:            "leading and trailing positional arguments",
			args:            []string{"extra1", "--Foo", "value", "extra2"},
			wantPositional:  []string{"extra1", "extra2"},
			wantDynamicName: "Foo",
			wantDynamicVal:  "value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewContext()
			positional, err := NewParser(tt.args).ReadArgs(ctx)
			if err != nil {
				t.Fatalf("ReadArgs returned error: %v", err)
			}
			if len(positional) != len(tt.wantPositional) {
				t.Fatalf("positional arguments = %#v, want %#v", positional, tt.wantPositional)
			}
			for i := range positional {
				if positional[i] != tt.wantPositional[i] {
					t.Fatalf("positional arguments = %#v, want %#v", positional, tt.wantPositional)
				}
			}
			if tt.wantDynamicName == "" {
				return
			}
			flag := ctx.dynamicFlags.GetByName(tt.wantDynamicName)
			if flag == nil || flag.GetValue() != tt.wantDynamicVal {
				t.Fatalf("dynamic flag %q = %#v, want value %q", tt.wantDynamicName, flag, tt.wantDynamicVal)
			}
		})
	}
}

func TestParserContinuesToRejectEqualsSyntax(t *testing.T) {
	parser := NewParser([]string{"--Description=value"})

	_, err := parser.ReadArgs(NewContext())
	if err == nil {
		t.Fatal("expected missing value error, got nil")
	}
	if !strings.Contains(err.Error(), "--Description=value must set value.") {
		t.Fatalf("error = %q, want missing value error", err.Error())
	}
}

func TestParserTreatsEqualsAsLiteralFlagNameInPairedMode(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantName  string
		wantValue string
	}{
		{
			name:      "two equals-style tokens",
			args:      []string{"--A=1", "--B=2"},
			wantName:  "A=1",
			wantValue: "--B=2",
		},
		{
			name:      "equals-style name with plain value",
			args:      []string{"--A=1", "value"},
			wantName:  "A=1",
			wantValue: "value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewContext()
			if _, err := NewParser(tt.args).ReadArgs(ctx); err != nil {
				t.Fatalf("ReadArgs returned error: %v", err)
			}
			flag := ctx.dynamicFlags.GetByName(tt.wantName)
			if flag == nil || flag.GetValue() != tt.wantValue {
				t.Fatalf("dynamic flag %q = %#v, want value %q", tt.wantName, flag, tt.wantValue)
			}
			if splitFlag := ctx.dynamicFlags.GetByName("A"); splitFlag != nil {
				t.Fatalf("unexpected split dynamic flag \"A\": %#v", splitFlag)
			}
		})
	}
}

func TestParserAllowsEqualsSyntaxInPairedValuePosition(t *testing.T) {
	ctx := NewContext()
	if _, err := NewParser([]string{"--Description", "--A=1"}).ReadArgs(ctx); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}

	flag := ctx.dynamicFlags.GetByName("Description")
	if flag == nil || flag.GetValue() != "--A=1" {
		t.Fatalf("dynamic flag \"Description\" = %#v, want value %q", flag, "--A=1")
	}
}

// ReadArgs 的 ctx 参数公开，调用方理论上可以传入 nil 或未初始化的 Context。
// 生产路径走 NewContext() 不会触发，但契约层面应返回错误而不是 panic。
func TestParserReadArgsRejectsInvalidContext(t *testing.T) {
	tests := []struct {
		name string
		ctx  *Context
	}{
		{name: "nil context", ctx: nil},
		{name: "empty context", ctx: &Context{}},
		{name: "missing dynamicFlags", ctx: &Context{fixedFlags: NewFlagSet()}},
		{name: "missing fixedFlags", ctx: &Context{dynamicFlags: NewFlagSet()}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser([]string{"--InstanceId", "i-xxx"})

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("ReadArgs panicked on %s: %v", tt.name, r)
				}
			}()

			_, err := parser.ReadArgs(tt.ctx)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "invalid context") {
				t.Fatalf("error = %q, want to contain %q", err.Error(), "invalid context")
			}
		})
	}
}
