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
