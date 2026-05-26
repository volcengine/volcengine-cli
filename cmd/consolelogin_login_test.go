package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
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

func TestResolveConsoleLoginRegion(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		commandRegion string
		wantRegion    string
		wantPrompt    bool
		wantDefault   string
	}{
		{
			name:          "command region skips prompt",
			commandRegion: " cn-shanghai ",
			wantRegion:    "cn-shanghai",
			wantPrompt:    false,
		},
		{
			name:        "empty input uses console login default region",
			input:       "\n",
			wantRegion:  defaultConsoleLoginRegion,
			wantPrompt:  true,
			wantDefault: defaultConsoleLoginRegion,
		},
		{
			name:        "typed region overrides default",
			input:       "cn-guangzhou\n",
			wantRegion:  "cn-guangzhou",
			wantPrompt:  true,
			wantDefault: defaultConsoleLoginRegion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer

			gotRegion, err := resolveConsoleLoginRegion(strings.NewReader(tt.input), &output, tt.commandRegion)
			if err != nil {
				t.Fatalf("resolveConsoleLoginRegion returned error: %v", err)
			}
			if gotRegion != tt.wantRegion {
				t.Fatalf("region = %q, want %q", gotRegion, tt.wantRegion)
			}

			gotPrompt := output.Len() > 0
			if gotPrompt != tt.wantPrompt {
				t.Fatalf("prompt output present = %v, want %v; output=%q", gotPrompt, tt.wantPrompt, output.String())
			}
			if tt.wantPrompt && !strings.Contains(output.String(), tt.wantDefault) {
				t.Fatalf("prompt output %q does not include default region %q", output.String(), tt.wantDefault)
			}
		})
	}
}

func TestExtractLoginSessionUsesTRNClaim(t *testing.T) {
	idToken := mustBuildUnsignedIDToken(t, map[string]string{
		"sub": "2100123456",
		"trn": "trn:volcengine:iam:cn-beijing:2100123456:user/Admin",
	})

	loginSession, err := extractLoginSession(idToken)
	if err != nil {
		t.Fatalf("extractLoginSession returned error: %v", err)
	}

	want := "trn:volcengine:iam:cn-beijing:2100123456:user/Admin"
	if loginSession != want {
		t.Fatalf("loginSession = %q, want %q", loginSession, want)
	}
}

func TestRemoteAuthorizeAcceptsRawURLEncodedAuthorizationResponse(t *testing.T) {
	state := "test-state"
	authCode := "test-code"
	query := "code=" + authCode + "&state=" + state
	input := base64.RawURLEncoding.EncodeToString([]byte(query)) + "\n"

	stdin := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdin pipe: %v", err)
	}
	t.Cleanup(func() {
		os.Stdin = stdin
		_ = reader.Close()
		_ = writer.Close()
	})
	if _, err := writer.WriteString(input); err != nil {
		t.Fatalf("write stdin pipe: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdin pipe writer: %v", err)
	}
	os.Stdin = reader

	cl := &ConsoleLogin{EndpointURL: "https://signin.volcengine.com"}
	oauthClient := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{EndpointURL: cl.EndpointURL})

	gotCode, gotRedirectURI, err := cl.remoteAuthorize(oauthClient, ConsoleClientIDCrossDevice, "challenge", state)
	if err != nil {
		t.Fatalf("remoteAuthorize returned error: %v", err)
	}
	if gotCode != authCode {
		t.Fatalf("authCode = %q, want %q", gotCode, authCode)
	}

	wantRedirectURI := "https://signin.volcengine.com/authorize/oauth/authorize"
	if gotRedirectURI != wantRedirectURI {
		t.Fatalf("redirectURI = %q, want %q", gotRedirectURI, wantRedirectURI)
	}
}

func mustBuildUnsignedIDToken(t *testing.T, claims map[string]string) string {
	t.Helper()

	header, err := json.Marshal(map[string]string{
		"alg": "none",
		"typ": "JWT",
	})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}

	return base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload) + "."
}
