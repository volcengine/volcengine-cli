package cmd

import (
	"strings"
	"testing"
)

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
			name:    "fixed flag before dynamic flag",
			args:    []string{"---profile", "--InstanceId"},
			wantErr: "---profile must set value.",
		},
		{
			name:    "dynamic flag before fixed flag",
			args:    []string{"--InstanceId", "---profile"},
			wantErr: "--InstanceId must set value.",
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
