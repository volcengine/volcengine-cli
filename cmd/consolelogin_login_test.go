package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfirmLoginSessionReplacement(t *testing.T) {
	tests := []struct {
		name                string
		input               string
		profileName         string
		currentLoginSession string
		newLoginSession     string
		wantConfirmed       bool
		wantErr             bool
		wantPrompt          bool
	}{
		{
			name:                "empty current session skips prompt",
			profileName:         "default",
			currentLoginSession: "",
			newLoginSession:     "new-session",
			wantConfirmed:       true,
			wantPrompt:          false,
		},
		{
			name:                "same session skips prompt",
			profileName:         "default",
			currentLoginSession: "same-session",
			newLoginSession:     "same-session",
			wantConfirmed:       true,
			wantPrompt:          false,
		},
		{
			name:                "yes confirms replacement",
			input:               "yes\n",
			profileName:         "default",
			currentLoginSession: "old-session",
			newLoginSession:     "new-session",
			wantConfirmed:       true,
			wantPrompt:          true,
		},
		{
			name:                "empty input rejects replacement",
			input:               "\n",
			profileName:         "default",
			currentLoginSession: "old-session",
			newLoginSession:     "new-session",
			wantConfirmed:       false,
			wantPrompt:          true,
		},
		{
			name:                "no without newline rejects replacement",
			input:               "no",
			profileName:         "default",
			currentLoginSession: "old-session",
			newLoginSession:     "new-session",
			wantConfirmed:       false,
			wantPrompt:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer

			confirmed, err := confirmLoginSessionReplacement(strings.NewReader(tt.input), &output, tt.profileName, tt.currentLoginSession, tt.newLoginSession)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if confirmed != tt.wantConfirmed {
				t.Fatalf("confirmed = %v, want %v", confirmed, tt.wantConfirmed)
			}

			gotPrompt := output.Len() > 0
			if gotPrompt != tt.wantPrompt {
				t.Fatalf("prompt output present = %v, want %v; output=%q", gotPrompt, tt.wantPrompt, output.String())
			}
			if tt.wantPrompt {
				if !strings.Contains(output.String(), tt.currentLoginSession) {
					t.Fatalf("prompt output %q does not include current session %q", output.String(), tt.currentLoginSession)
				}
				if !strings.Contains(output.String(), tt.newLoginSession) {
					t.Fatalf("prompt output %q does not include new session %q", output.String(), tt.newLoginSession)
				}
			}
		})
	}
}

func TestConfirmLoginSessionReplacementNilInput(t *testing.T) {
	var output bytes.Buffer

	confirmed, err := confirmLoginSessionReplacement(nil, &output, "default", "old-session", "new-session")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if confirmed {
		t.Fatal("expected confirmation to be false")
	}
}
