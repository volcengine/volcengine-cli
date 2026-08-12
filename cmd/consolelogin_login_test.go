package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConsoleLoginValidateOptions(t *testing.T) {
	tests := []struct {
		name    string
		login   ConsoleLogin
		wantErr string
	}{
		{
			name:  "default authorization code flow",
			login: ConsoleLogin{},
		},
		{
			name:  "remote authorization code flow",
			login: ConsoleLogin{Remote: true},
		},
		{
			name:  "device code flow",
			login: ConsoleLogin{UseDeviceCode: true},
		},
		{
			name:  "device code flow without browser",
			login: ConsoleLogin{UseDeviceCode: true, NoBrowser: true},
		},
		{
			name:    "remote conflicts with device code",
			login:   ConsoleLogin{Remote: true, UseDeviceCode: true},
			wantErr: "--remote and --use-device-code",
		},
		{
			name:    "no browser requires device code",
			login:   ConsoleLogin{NoBrowser: true},
			wantErr: "--no-browser requires --use-device-code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.login.validateOptions()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateOptions returned error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestNewLoginCmdDeviceCodeFlags(t *testing.T) {
	command := newLoginCmd()
	for _, flag := range []string{"use-device-code", "no-browser", "remote", "endpoint-url"} {
		if command.Flags().Lookup(flag) == nil {
			t.Fatalf("flag --%s was not registered", flag)
		}
	}
}

func TestNewLoginCmdRejectsInvalidDeviceCodeFlagCombinations(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "remote conflicts with device code",
			args:    []string{"--remote", "--use-device-code", "--region", "cn-beijing"},
			wantErr: "--remote and --use-device-code",
		},
		{
			name:    "no browser requires device code",
			args:    []string{"--no-browser", "--region", "cn-beijing"},
			wantErr: "--no-browser requires --use-device-code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := newLoginCmd()
			command.SilenceUsage = true
			command.SilenceErrors = true
			command.SetArgs(tt.args)
			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestDeviceCodeAuthorizeSlowDownThenSuccess(t *testing.T) {
	var tokenAttempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case consoleDeviceAuthorizationPath:
			_ = json.NewEncoder(w).Encode(ConsoleDeviceAuthorizationResponse{
				DeviceCode:      "device-code",
				UserCode:        "USER-CODE",
				VerificationURI: "https://example.com/device",
				ExpiresIn:       300,
				Interval:        5,
			})
		case consoleTokenPath:
			tokenAttempts++
			if tokenAttempts == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"slow_down"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(ConsoleTokenResponse{
				AccessToken:  `{"access_key_id":"ak","secret_access_key":"sk","session_token":"st"}`,
				TokenType:    "sts",
				ExpiresIn:    900,
				RefreshToken: "refresh-token",
				IDToken:      "id-token",
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	var waits []time.Duration
	now := time.Unix(1000, 0)
	restore := setConsoleDeviceAuthorizationHooks(
		t,
		func(string) error { return nil },
		func(_ context.Context, wait time.Duration) error {
			waits = append(waits, wait)
			now = now.Add(wait)
			return nil
		},
		func() time.Time { return now },
	)
	defer restore()

	client := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
		EndpointURL: server.URL,
		HTTPClient:  server.Client(),
	})
	login := &ConsoleLogin{UseDeviceCode: true}
	resp, err := login.deviceCodeAuthorize(context.Background(), client)
	if err != nil {
		t.Fatalf("deviceCodeAuthorize returned error: %v", err)
	}
	if resp.RefreshToken != "refresh-token" {
		t.Fatalf("response = %+v", resp)
	}
	if len(waits) != 2 || waits[0] != 5*time.Second || waits[1] != 10*time.Second {
		t.Fatalf("waits = %v, want [5s 10s]", waits)
	}
}

func TestDeviceCodeAuthorizeToleratesTransientErrorsThenSucceeds(t *testing.T) {
	var tokenAttempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case consoleDeviceAuthorizationPath:
			_ = json.NewEncoder(w).Encode(ConsoleDeviceAuthorizationResponse{
				DeviceCode:      "device-code",
				UserCode:        "USER-CODE",
				VerificationURI: "https://example.com/device",
				ExpiresIn:       300,
				Interval:        5,
			})
		case consoleTokenPath:
			tokenAttempts++
			switch tokenAttempts {
			case 1:
				// Structured transient failure (upstream 5xx).
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"server_error"}`))
			case 2:
				// Documented transient failure: service temporarily unavailable (503).
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":"temporarily_unavailable"}`))
			case 3:
				// Non-structured failure: 5xx with an unparseable body.
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`gateway down`))
			default:
				_ = json.NewEncoder(w).Encode(ConsoleTokenResponse{
					AccessToken:  `{"access_key_id":"ak","secret_access_key":"sk","session_token":"st"}`,
					TokenType:    "sts",
					ExpiresIn:    900,
					RefreshToken: "refresh-token",
					IDToken:      "id-token",
				})
			}
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	now := time.Unix(1000, 0)
	restore := setConsoleDeviceAuthorizationHooks(
		t,
		func(string) error { return nil },
		func(_ context.Context, wait time.Duration) error {
			now = now.Add(wait)
			return nil
		},
		func() time.Time { return now },
	)
	defer restore()

	client := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
		EndpointURL: server.URL,
		HTTPClient:  server.Client(),
	})
	resp, err := (&ConsoleLogin{UseDeviceCode: true, NoBrowser: true}).deviceCodeAuthorize(context.Background(), client)
	if err != nil {
		t.Fatalf("deviceCodeAuthorize returned error: %v", err)
	}
	if resp.RefreshToken != "refresh-token" {
		t.Fatalf("response = %+v", resp)
	}
	if tokenAttempts != 4 {
		t.Fatalf("token attempts = %d, want 4 (three transient, one success)", tokenAttempts)
	}
}

func TestDeviceCodeAuthorizeAbortsAfterSustainedTransientErrors(t *testing.T) {
	var tokenAttempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case consoleDeviceAuthorizationPath:
			_ = json.NewEncoder(w).Encode(ConsoleDeviceAuthorizationResponse{
				DeviceCode:      "device-code",
				UserCode:        "USER-CODE",
				VerificationURI: "https://example.com/device",
				ExpiresIn:       3600,
				Interval:        5,
			})
		case consoleTokenPath:
			tokenAttempts++
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"server_error"}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	now := time.Unix(1000, 0)
	restore := setConsoleDeviceAuthorizationHooks(
		t,
		func(string) error { return nil },
		func(_ context.Context, wait time.Duration) error {
			now = now.Add(wait)
			return nil
		},
		func() time.Time { return now },
	)
	defer restore()

	client := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
		EndpointURL: server.URL,
		HTTPClient:  server.Client(),
	})
	_, err := (&ConsoleLogin{UseDeviceCode: true, NoBrowser: true}).deviceCodeAuthorize(context.Background(), client)
	if err == nil || !strings.Contains(err.Error(), "polling device authorization token") {
		t.Fatalf("error = %v, want polling failure", err)
	}
	// Budget is consoleDeviceCodeMaxTransientErrors tolerated + 1 that trips the abort.
	if tokenAttempts != consoleDeviceCodeMaxTransientErrors+1 {
		t.Fatalf("token attempts = %d, want %d", tokenAttempts, consoleDeviceCodeMaxTransientErrors+1)
	}
}

func TestDeviceCodeAuthorizeNoBrowser(t *testing.T) {
	server := newSuccessfulDeviceCodeServer(t)
	defer server.Close()

	browserCalls := 0
	now := time.Unix(1000, 0)
	restore := setConsoleDeviceAuthorizationHooks(
		t,
		func(string) error {
			browserCalls++
			return nil
		},
		func(_ context.Context, wait time.Duration) error {
			now = now.Add(wait)
			return nil
		},
		func() time.Time { return now },
	)
	defer restore()

	output := captureConsoleLoginStdout(t, func() {
		client := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
			EndpointURL: server.URL,
			HTTPClient:  server.Client(),
		})
		_, err := (&ConsoleLogin{UseDeviceCode: true, NoBrowser: true}).deviceCodeAuthorize(context.Background(), client)
		if err != nil {
			t.Fatalf("deviceCodeAuthorize returned error: %v", err)
		}
	})

	if browserCalls != 0 {
		t.Fatalf("browser calls = %d, want 0", browserCalls)
	}
	for _, expected := range []string{
		"Browser will not be automatically opened.",
		"https://example.com/device",
		"USER-CODE",
		"300",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
}

func TestDeviceCodeAuthorizeBrowserFailureContinues(t *testing.T) {
	server := newSuccessfulDeviceCodeServer(t)
	defer server.Close()

	browserCalls := 0
	now := time.Unix(1000, 0)
	restore := setConsoleDeviceAuthorizationHooks(
		t,
		func(gotURL string) error {
			browserCalls++
			if gotURL != "https://example.com/device?user_code=USER-CODE" {
				t.Fatalf("browser URL = %q", gotURL)
			}
			return errors.New("browser unavailable")
		},
		func(_ context.Context, wait time.Duration) error {
			now = now.Add(wait)
			return nil
		},
		func() time.Time { return now },
	)
	defer restore()

	output := captureConsoleLoginStdout(t, func() {
		client := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
			EndpointURL: server.URL,
			HTTPClient:  server.Client(),
		})
		_, err := (&ConsoleLogin{UseDeviceCode: true}).deviceCodeAuthorize(context.Background(), client)
		if err != nil {
			t.Fatalf("deviceCodeAuthorize returned error: %v", err)
		}
	})

	if browserCalls != 1 {
		t.Fatalf("browser calls = %d, want 1", browserCalls)
	}
	for _, expected := range []string{
		"Attempting to open your default browser.",
		"https://example.com/device",
		"USER-CODE",
		"browser unavailable",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
}

func TestDeviceCodeAuthorizeTerminalErrors(t *testing.T) {
	tests := []struct {
		code    string
		wantErr string
	}{
		{code: "access_denied", wantErr: "was denied"},
		{code: "expired_token", wantErr: "invalid or expired"},
		{code: "invalid_device_code", wantErr: "invalid or expired"},
		{code: "invalid_client", wantErr: "invalid_client"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case consoleDeviceAuthorizationPath:
					_ = json.NewEncoder(w).Encode(ConsoleDeviceAuthorizationResponse{
						DeviceCode:      "device-code",
						UserCode:        "USER-CODE",
						VerificationURI: "https://example.com/device",
						ExpiresIn:       300,
						Interval:        5,
					})
				case consoleTokenPath:
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"` + tt.code + `"}`))
				}
			}))
			defer server.Close()

			now := time.Unix(1000, 0)
			restore := setConsoleDeviceAuthorizationHooks(
				t,
				func(string) error { return nil },
				func(_ context.Context, wait time.Duration) error {
					now = now.Add(wait)
					return nil
				},
				func() time.Time { return now },
			)
			defer restore()

			client := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
				EndpointURL: server.URL,
				HTTPClient:  server.Client(),
			})
			_, err := (&ConsoleLogin{UseDeviceCode: true, NoBrowser: true}).deviceCodeAuthorize(context.Background(), client)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestDeviceCodeAuthorizeTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != consoleDeviceAuthorizationPath {
			t.Fatalf("unexpected token request after timeout: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(ConsoleDeviceAuthorizationResponse{
			DeviceCode:      "device-code",
			UserCode:        "USER-CODE",
			VerificationURI: "https://example.com/device",
			ExpiresIn:       3,
			Interval:        5,
		})
	}))
	defer server.Close()

	now := time.Unix(1000, 0)
	restore := setConsoleDeviceAuthorizationHooks(
		t,
		func(string) error { return nil },
		func(_ context.Context, wait time.Duration) error {
			now = now.Add(wait)
			return nil
		},
		func() time.Time { return now },
	)
	defer restore()

	client := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
		EndpointURL: server.URL,
		HTTPClient:  server.Client(),
	})
	_, err := (&ConsoleLogin{UseDeviceCode: true, NoBrowser: true}).deviceCodeAuthorize(context.Background(), client)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want timeout", err)
	}
}

func TestConsoleLoginDeviceCodePersistsProfileAndCache(t *testing.T) {
	configDir := tempDirForTest(t)
	defer cleanupDirForTest(configDir)()
	defer withConfigDirForTest(configDir)()
	defer setenvForTest(t, loginCacheDirectoryEnv, filepath.Join(configDir, "login-cache"))()

	oldConfig := config
	oldContextConfig := ctx.config
	defer func() {
		config = oldConfig
		ctx.config = oldContextConfig
	}()
	setRuntimeConfig(&Configure{Profiles: map[string]*Profile{}})

	loginSession := "trn:volcengine:iam:cn-beijing:2100123456:user/Admin"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case consoleDeviceAuthorizationPath:
			_ = json.NewEncoder(w).Encode(ConsoleDeviceAuthorizationResponse{
				DeviceCode:      "device-code",
				UserCode:        "USER-CODE",
				VerificationURI: "https://example.com/device",
				ExpiresIn:       300,
				Interval:        5,
			})
		case consoleTokenPath:
			_ = json.NewEncoder(w).Encode(ConsoleTokenResponse{
				AccessToken:  `{"access_key_id":"ak","secret_access_key":"sk","session_token":"st"}`,
				TokenType:    "sts",
				ExpiresIn:    900,
				RefreshToken: "refresh-token",
				IDToken: mustBuildUnsignedIDToken(t, map[string]string{
					"trn": loginSession,
				}),
				Scope: scopeAllAll,
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	now := time.Unix(1000, 0)
	restore := setConsoleDeviceAuthorizationHooks(
		t,
		func(string) error { return nil },
		func(_ context.Context, wait time.Duration) error {
			now = now.Add(wait)
			return nil
		},
		func() time.Time { return now },
	)
	defer restore()

	err := (&ConsoleLogin{
		Profile:       "device-profile",
		Region:        "cn-beijing",
		UseDeviceCode: true,
		NoBrowser:     true,
		EndpointURL:   server.URL,
	}).Login()
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}

	cfg := runtimeConfig()
	profile := cfg.Profiles["device-profile"]
	if profile == nil {
		t.Fatal("device profile was not created")
	}
	if profile.Mode != ModeConsoleLogin || profile.Region != "cn-beijing" || profile.LoginSession != loginSession {
		t.Fatalf("profile = %+v", profile)
	}

	cache, err := readLoginCache(loginSession)
	if err != nil {
		t.Fatalf("readLoginCache returned error: %v", err)
	}
	if cache.ClientID != ConsoleClientIDCrossDevice {
		t.Fatalf("cache client ID = %q", cache.ClientID)
	}
	if cache.Scope != scopeAllAll {
		t.Fatalf("cache scope = %q", cache.Scope)
	}
	if cache.EndpointURL != server.URL {
		t.Fatalf("cache endpoint = %q", cache.EndpointURL)
	}
	if cache.RefreshToken != "refresh-token" {
		t.Fatalf("cache refresh token = %q", cache.RefreshToken)
	}
}

func newSuccessfulDeviceCodeServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case consoleDeviceAuthorizationPath:
			_ = json.NewEncoder(w).Encode(ConsoleDeviceAuthorizationResponse{
				DeviceCode:              "device-code",
				UserCode:                "USER-CODE",
				VerificationURI:         "https://example.com/device",
				VerificationURIComplete: "https://example.com/device?user_code=USER-CODE",
				ExpiresIn:               300,
				Interval:                5,
			})
		case consoleTokenPath:
			_ = json.NewEncoder(w).Encode(ConsoleTokenResponse{
				AccessToken:  `{"access_key_id":"ak","secret_access_key":"sk","session_token":"st"}`,
				TokenType:    "sts",
				ExpiresIn:    900,
				RefreshToken: "refresh-token",
				IDToken:      "id-token",
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
}

func setConsoleDeviceAuthorizationHooks(
	t *testing.T,
	openBrowser func(string) error,
	sleep func(context.Context, time.Duration) error,
	now func() time.Time,
) func() {
	t.Helper()
	oldOpenBrowser := consoleLoginOpenBrowser
	oldSleep := consoleDeviceAuthorizationSleep
	oldCurrentTime := consoleDeviceAuthorizationCurrentTime
	consoleLoginOpenBrowser = openBrowser
	consoleDeviceAuthorizationSleep = sleep
	consoleDeviceAuthorizationCurrentTime = now
	return func() {
		consoleLoginOpenBrowser = oldOpenBrowser
		consoleDeviceAuthorizationSleep = oldSleep
		consoleDeviceAuthorizationCurrentTime = oldCurrentTime
	}
}

func captureConsoleLoginStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = writer
	// Restore os.Stdout and release the pipe even if fn() aborts via t.Fatal
	// (runtime.Goexit skips non-deferred cleanup), otherwise a leaked writer
	// corrupts stdout for every subsequent test in the package.
	restore := func() {
		os.Stdout = oldStdout
		_ = writer.Close()
		_ = reader.Close()
	}
	defer restore()

	fn()

	_ = writer.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(output)
}

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
	defer func() {
		os.Stdin = stdin
		_ = reader.Close()
		_ = writer.Close()
	}()
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
