package cmd

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withTestConfigDir(t *testing.T) (string, func()) {
	t.Helper()

	tmpDir := tempDirForTest(t)
	oldFunc := configFileDirFunc
	configFileDirFunc = func() (string, error) {
		return tmpDir, nil
	}

	return tmpDir, func() {
		configFileDirFunc = oldFunc
		_ = os.RemoveAll(filepath.Clean(tmpDir))
	}
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
	_, cleanup := withTestConfigDir(t)
	defer cleanup()

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

func TestLoginCacheDirUsesCustomDirectoryEnv(t *testing.T) {
	tmpDir := tempDirForTest(t)
	defer cleanupDirForTest(tmpDir)()
	customDir := filepath.Join(tmpDir, "custom-cache")
	defer setenvForTest(t, loginCacheDirectoryEnv, customDir)()

	oldFunc := configFileDirFunc
	configFileDirFunc = func() (string, error) {
		return "", os.ErrPermission
	}
	defer func() {
		configFileDirFunc = oldFunc
	}()

	cacheDir, err := getLoginCacheDir()
	if err != nil {
		t.Fatalf("getLoginCacheDir returned error: %v", err)
	}
	if cacheDir != customDir {
		t.Fatalf("cache dir = %q, want custom dir %q", cacheDir, customDir)
	}
	if info, err := os.Stat(customDir); err != nil {
		t.Fatalf("custom cache dir was not created: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("custom cache path %q is not a directory", customDir)
	}
}

func TestEnsureValidLoginTokenRefreshesExpiredCredentials(t *testing.T) {
	configDir, cleanup := withTestConfigDir(t)
	defer cleanup()

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

	data, err := ioutil.ReadFile(filepath.Clean(cachePath))
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

func TestEnsureValidLoginTokenUsesRefreshedCredentialsWhenCacheWriteFails(t *testing.T) {
	_, cleanup := withTestConfigDir(t)
	defer cleanup()

	cfg := &Configure{Profiles: map[string]*Profile{"default": {
		Name: "default", Mode: ModeConsoleLogin, LoginSession: "expired-session",
	}}}
	oldCreds := STSCredentials{AccessKeyID: "ak-old", SecretAccessKey: "sk-old", SessionToken: "st-old"}
	newCreds := STSCredentials{AccessKeyID: "ak-new", SecretAccessKey: "sk-new", SessionToken: "st-new"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ConsoleTokenResponse{
			AccessToken: string(mustMarshalAccessToken(t, newCreds)), RefreshToken: "refresh-new", ExpiresIn: 900,
		})
	}))
	defer server.Close()
	cache := &LoginTokenCache{
		LoginSession: "expired-session", AccessToken: mustMarshalAccessToken(t, oldCreds),
		RefreshToken: "refresh-old", ClientID: ConsoleClientIDSameDevice, Scope: scopeAllAll, EndpointURL: server.URL,
		IssuedAt: time.Now().UTC().Add(-20 * time.Minute).Format(time.RFC3339), ExpiresIn: 900,
	}
	if err := writeLoginCache(cache); err != nil {
		t.Fatal(err)
	}

	originalReplace := replaceLoginCacheFile
	replaceLoginCacheFile = func(string, string) error { return errors.New("injected replace failure") }
	defer func() { replaceLoginCacheFile = originalReplace }()

	credentials, err := EnsureValidLoginToken(cfg, "default")
	if err != nil {
		t.Fatalf("refresh should remain usable after cache persistence failure: %v", err)
	}
	if credentials == nil || *credentials != newCreds {
		t.Fatalf("credentials = %#v, want %#v", credentials, newCreds)
	}
	current, err := readLoginCache("expired-session")
	if err != nil {
		t.Fatal(err)
	}
	var currentCreds STSCredentials
	if err := json.Unmarshal(current.AccessToken, &currentCreds); err != nil {
		t.Fatal(err)
	}
	if currentCreds != oldCreds || current.RefreshToken != cache.RefreshToken ||
		current.IssuedAt != cache.IssuedAt || current.ExpiresIn != cache.ExpiresIn {
		t.Fatalf("failed replacement changed old cache: got %#v want %#v", current, cache)
	}
	cachePath, err := loginCacheFilePath("expired-session")
	if err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(cachePath), ".tmp-login-cache-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary login cache files leaked: %v", matches)
	}
}

func TestWriteLoginCacheReplacesExistingFile(t *testing.T) {
	_, cleanup := withTestConfigDir(t)
	defer cleanup()

	oldCache := &LoginTokenCache{
		LoginSession: "replace-session", AccessToken: json.RawMessage(`{"value":"old"}`),
		RefreshToken: "old", IssuedAt: "2026-08-21T00:00:00Z", ExpiresIn: 60,
	}
	newCache := &LoginTokenCache{
		LoginSession: "replace-session", AccessToken: json.RawMessage(`{"value":"new"}`),
		RefreshToken: "new", IssuedAt: "2026-08-21T00:01:00Z", ExpiresIn: 120,
	}
	if err := writeLoginCache(oldCache); err != nil {
		t.Fatal(err)
	}
	if err := writeLoginCache(newCache); err != nil {
		t.Fatalf("replace existing login cache: %v", err)
	}
	got, err := readLoginCache("replace-session")
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken != newCache.RefreshToken || got.IssuedAt != newCache.IssuedAt || got.ExpiresIn != newCache.ExpiresIn {
		t.Fatalf("replaced cache = %#v, want %#v", got, newCache)
	}
}

func TestEnsureValidLoginTokenDoesNotRecreateCacheAfterLogout(t *testing.T) {
	_, cleanup := withTestConfigDir(t)
	defer cleanup()
	cfg := &Configure{Profiles: map[string]*Profile{"default": {
		Name: "default", Mode: ModeConsoleLogin, LoginSession: "expired-session",
	}}}
	oldCreds := STSCredentials{AccessKeyID: "old", SecretAccessKey: "old-sk", SessionToken: "old-st"}
	newCreds := STSCredentials{AccessKeyID: "new", SecretAccessKey: "new-sk", SessionToken: "new-st"}
	requestArrived := make(chan struct{})
	allowResponse := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestArrived)
		<-allowResponse
		_ = json.NewEncoder(w).Encode(ConsoleTokenResponse{
			AccessToken: string(mustMarshalAccessToken(t, newCreds)), RefreshToken: "new-refresh", ExpiresIn: 900,
		})
	}))
	defer server.Close()
	cache := &LoginTokenCache{
		LoginSession: "expired-session", AccessToken: mustMarshalAccessToken(t, oldCreds),
		RefreshToken: "old-refresh", ClientID: ConsoleClientIDSameDevice, Scope: scopeAllAll, EndpointURL: server.URL,
		IssuedAt: time.Now().UTC().Add(-20 * time.Minute).Format(time.RFC3339), ExpiresIn: 900,
	}
	if err := writeLoginCache(cache); err != nil {
		t.Fatal(err)
	}
	cachePath, _ := loginCacheFilePath("expired-session")
	done := make(chan error, 1)
	go func() { _, err := EnsureValidLoginToken(cfg, "default"); done <- err }()
	<-requestArrived
	if err := removeLoginCache("expired-session"); err != nil {
		t.Fatal(err)
	}
	close(allowResponse)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "login session changed") {
		t.Fatalf("refresh after logout error = %v, want session-changed rejection", err)
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("stale refresh recreated removed cache: %v", err)
	}
}

func TestEnsureValidLoginTokenReturnsHelpfulErrorWhenRefreshFails(t *testing.T) {
	_, cleanup := withTestConfigDir(t)
	defer cleanup()

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
