package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withTestConfigDir(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	oldFunc := configFileDirFunc
	configFileDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	t.Cleanup(func() {
		configFileDirFunc = oldFunc
	})

	return tmpDir
}

func mustMarshalAccessToken(t *testing.T, creds STSCredentials) json.RawMessage {
	t.Helper()

	data, err := json.Marshal(creds)
	if err != nil {
		t.Fatalf("marshal access token: %v", err)
	}
	return json.RawMessage(data)
}

func TestEnsureValidLoginTokenReturnsCachedCredentialsWhenStillValid(t *testing.T) {
	withTestConfigDir(t)

	cfg := &Configure{
		Profiles: map[string]*Profile{
			"default": {
				Name:         "default",
				Mode:         ModeConsoleLogin,
				LoginSession: "valid-session",
			},
		},
	}

	expected := STSCredentials{
		AccessKeyID:     "ak-valid",
		SecretAccessKey: "sk-valid",
		SessionToken:    "st-valid",
	}

	cache := &LoginTokenCache{
		LoginSession: "valid-session",
		AccessToken:  mustMarshalAccessToken(t, expected),
		RefreshToken: "refresh-token",
		ClientID:     ConsoleClientIDSameDevice,
		Scope:        scopeAllAll,
		IssuedAt:     time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339),
		ExpiresIn:    900,
		TokenType:    "sts",
	}
	if err := writeLoginCache(cache); err != nil {
		t.Fatalf("write login cache: %v", err)
	}

	creds, err := EnsureValidLoginToken(cfg, "default")
	if err != nil {
		t.Fatalf("EnsureValidLoginToken returned error: %v", err)
	}

	if *creds != expected {
		t.Fatalf("credentials = %+v, want %+v", *creds, expected)
	}
}

func TestEnsureValidLoginTokenRefreshesExpiredCredentials(t *testing.T) {
	configDir := withTestConfigDir(t)

	cfg := &Configure{
		Profiles: map[string]*Profile{
			"default": {
				Name:         "default",
				Mode:         ModeConsoleLogin,
				LoginSession: "expired-session",
			},
		},
	}

	oldCreds := STSCredentials{
		AccessKeyID:     "ak-old",
		SecretAccessKey: "sk-old",
		SessionToken:    "st-old",
	}
	newCreds := STSCredentials{
		AccessKeyID:     "ak-new",
		SecretAccessKey: "sk-new",
		SessionToken:    "st-new",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != consoleTokenPath {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Fatalf("grant_type = %q, want refresh_token", got)
		}
		if got := r.Form.Get("refresh_token"); got != "refresh-old" {
			t.Fatalf("refresh_token = %q, want refresh-old", got)
		}

		resp := ConsoleTokenResponse{
			AccessToken:  string(mustMarshalAccessToken(t, newCreds)),
			TokenType:    "sts",
			ExpiresIn:    900,
			RefreshToken: "refresh-new",
			IDToken:      "new-id-token",
			Scope:        scopeAllAll,
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	cache := &LoginTokenCache{
		LoginSession: "expired-session",
		AccessToken:  mustMarshalAccessToken(t, oldCreds),
		RefreshToken: "refresh-old",
		ClientID:     ConsoleClientIDSameDevice,
		Scope:        scopeAllAll,
		EndpointURL:  server.URL,
		IssuedAt:     time.Now().UTC().Add(-20 * time.Minute).Format(time.RFC3339),
		ExpiresIn:    900,
		TokenType:    "sts",
	}
	if err := writeLoginCache(cache); err != nil {
		t.Fatalf("write login cache: %v", err)
	}

	creds, err := EnsureValidLoginToken(cfg, "default")
	if err != nil {
		t.Fatalf("EnsureValidLoginToken returned error: %v", err)
	}
	if *creds != newCreds {
		t.Fatalf("credentials = %+v, want %+v", *creds, newCreds)
	}

	cachePath, err := loginCacheFilePath("expired-session")
	if err != nil {
		t.Fatalf("loginCacheFilePath: %v", err)
	}
	if !strings.HasPrefix(cachePath, configDir) {
		t.Fatalf("cache path %q not under test config dir %q", cachePath, configDir)
	}

	data, err := os.ReadFile(filepath.Clean(cachePath))
	if err != nil {
		t.Fatalf("read refreshed cache: %v", err)
	}
	var refreshed LoginTokenCache
	if err := json.Unmarshal(data, &refreshed); err != nil {
		t.Fatalf("unmarshal refreshed cache: %v", err)
	}
	var refreshedCreds STSCredentials
	if err := json.Unmarshal(refreshed.AccessToken, &refreshedCreds); err != nil {
		t.Fatalf("unmarshal refreshed access token: %v", err)
	}
	if refreshedCreds != newCreds {
		t.Fatalf("cached credentials = %+v, want %+v", refreshedCreds, newCreds)
	}
	if refreshed.RefreshToken != "refresh-new" {
		t.Fatalf("cached refresh token = %q, want refresh-new", refreshed.RefreshToken)
	}
}

func TestEnsureValidLoginTokenReturnsHelpfulErrorWhenRefreshFails(t *testing.T) {
	withTestConfigDir(t)

	cfg := &Configure{
		Profiles: map[string]*Profile{
			"default": {
				Name:         "default",
				Mode:         ModeConsoleLogin,
				LoginSession: "expired-session",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"refresh token expired"}`))
	}))
	defer server.Close()

	cache := &LoginTokenCache{
		LoginSession: "expired-session",
		AccessToken: mustMarshalAccessToken(t, STSCredentials{
			AccessKeyID:     "ak-old",
			SecretAccessKey: "sk-old",
			SessionToken:    "st-old",
		}),
		RefreshToken: "refresh-old",
		ClientID:     ConsoleClientIDSameDevice,
		Scope:        scopeAllAll,
		EndpointURL:  server.URL,
		IssuedAt:     time.Now().UTC().Add(-20 * time.Minute).Format(time.RFC3339),
		ExpiresIn:    900,
		TokenType:    "sts",
	}
	if err := writeLoginCache(cache); err != nil {
		t.Fatalf("write login cache: %v", err)
	}

	_, err := EnsureValidLoginToken(cfg, "default")
	if err == nil {
		t.Fatal("expected refresh failure, got nil")
	}
	if !strings.Contains(err.Error(), "failed to refresh session token") {
		t.Fatalf("error %q does not contain expected refresh failure message", err)
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("error %q does not contain upstream oauth error details", err)
	}
}
