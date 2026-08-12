package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestConsoleOAuthClientStartDeviceAuthorization(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_LOGIN_HEADERS", "x-tt-env=boe_device_code;x-real-ip=10.0.0.1")()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != consoleDeviceAuthorizationPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, consoleDeviceAuthorizationPath)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("Content-Type = %q", got)
		}
		if got := r.Header.Get("x-tt-env"); got != "boe_device_code" {
			t.Fatalf("x-tt-env = %q", got)
		}
		if got := r.Header.Get("x-real-ip"); got != "10.0.0.1" {
			t.Fatalf("x-real-ip = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("client_id"); got != ConsoleClientIDCrossDevice {
			t.Fatalf("client_id = %q", got)
		}
		if got := r.Form.Get("scope"); got != scopeAllAll {
			t.Fatalf("scope = %q", got)
		}
		if got := r.Form.Get("device_info"); got != consoleDeviceInfo {
			t.Fatalf("device_info = %q", got)
		}
		if got := r.Form.Get("client_secret"); got != "" {
			t.Fatalf("client_secret = %q, want empty", got)
		}

		_ = json.NewEncoder(w).Encode(ConsoleDeviceAuthorizationResponse{
			DeviceCode:              "device-code",
			UserCode:                "USER-CODE",
			VerificationURI:         "https://example.com/device",
			VerificationURIComplete: "https://example.com/device?user_code=USER-CODE",
			ExpiresIn:               300,
			Interval:                5,
		})
	}))
	defer server.Close()

	client := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
		EndpointURL: server.URL,
		HTTPClient:  server.Client(),
	})
	resp, err := client.StartDeviceAuthorization(context.Background(), &ConsoleDeviceAuthorizationRequest{
		ClientID:   ConsoleClientIDCrossDevice,
		Scope:      scopeAllAll,
		DeviceInfo: consoleDeviceInfo,
	})
	if err != nil {
		t.Fatalf("StartDeviceAuthorization returned error: %v", err)
	}
	if resp.DeviceCode != "device-code" || resp.UserCode != "USER-CODE" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestConsoleOAuthClientStartDeviceAuthorizationValidatesResponse(t *testing.T) {
	tests := []struct {
		name     string
		response ConsoleDeviceAuthorizationResponse
		wantErr  string
	}{
		{
			name: "missing device code",
			response: ConsoleDeviceAuthorizationResponse{
				UserCode:        "code",
				VerificationURI: "https://example.com",
				ExpiresIn:       300,
			},
			wantErr: "device_code",
		},
		{
			name: "missing user code",
			response: ConsoleDeviceAuthorizationResponse{
				DeviceCode:      "device",
				VerificationURI: "https://example.com",
				ExpiresIn:       300,
			},
			wantErr: "user_code",
		},
		{
			name: "missing verification uri",
			response: ConsoleDeviceAuthorizationResponse{
				DeviceCode: "device",
				UserCode:   "code",
				ExpiresIn:  300,
			},
			wantErr: "verification_uri",
		},
		{
			name: "invalid expiration",
			response: ConsoleDeviceAuthorizationResponse{
				DeviceCode:      "device",
				UserCode:        "code",
				VerificationURI: "https://example.com",
			},
			wantErr: "expires_in",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
				EndpointURL: server.URL,
				HTTPClient:  server.Client(),
			})
			_, err := client.StartDeviceAuthorization(context.Background(), &ConsoleDeviceAuthorizationRequest{
				ClientID: ConsoleClientIDCrossDevice,
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestConsoleOAuthClientExchangeDeviceCodeToken(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_LOGIN_HEADERS", "x-tt-env=boe_device_code")()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != consoleTokenPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, consoleTokenPath)
		}
		if got := r.Header.Get("x-tt-env"); got != "boe_device_code" {
			t.Fatalf("x-tt-env = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != deviceCodeGrantType {
			t.Fatalf("grant_type = %q", got)
		}
		if got := r.Form.Get("device_code"); got != "device-code" {
			t.Fatalf("device_code = %q", got)
		}
		if got := r.Form.Get("client_secret"); got != "" {
			t.Fatalf("client_secret = %q, want empty", got)
		}

		_ = json.NewEncoder(w).Encode(ConsoleTokenResponse{
			AccessToken:  `{"access_key_id":"ak","secret_access_key":"sk","session_token":"st"}`,
			TokenType:    "sts",
			ExpiresIn:    900,
			RefreshToken: "refresh-token",
			IDToken:      "id-token",
		})
	}))
	defer server.Close()

	client := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
		EndpointURL: server.URL,
		HTTPClient:  server.Client(),
	})
	resp, err := client.ExchangeToken(context.Background(), &ConsoleTokenRequest{
		GrantType:  deviceCodeGrantType,
		DeviceCode: "device-code",
		ClientID:   ConsoleClientIDCrossDevice,
		Scope:      scopeAllAll,
	})
	if err != nil {
		t.Fatalf("ExchangeToken returned error: %v", err)
	}
	if resp.RefreshToken != "refresh-token" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestConsoleOAuthClientExchangeTokenRetriesRetryableErrors(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&attempts, 1)
		if current < 3 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"slow_down","error_description":"try again later"}`))
			return
		}

		resp := ConsoleTokenResponse{
			AccessToken:  `{"access_key_id":"ak","secret_access_key":"sk","session_token":"st"}`,
			TokenType:    "sts",
			ExpiresIn:    900,
			RefreshToken: "refresh-new",
			Scope:        scopeAllAll,
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
		EndpointURL: server.URL,
		HTTPClient:  server.Client(),
	})

	resp, err := client.ExchangeToken(context.Background(), &ConsoleTokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: "refresh-old",
		ClientID:     ConsoleClientIDSameDevice,
		Scope:        scopeAllAll,
	})
	if err != nil {
		t.Fatalf("ExchangeToken returned error: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
	if resp.RefreshToken != "refresh-new" {
		t.Fatalf("refresh token = %q, want refresh-new", resp.RefreshToken)
	}
}

func TestConsoleOAuthClientExchangeTokenDoesNotRetryNonRetryableErrors(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"bad code"}`))
	}))
	defer server.Close()

	client := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
		EndpointURL: server.URL,
		HTTPClient:  server.Client(),
	})

	_, err := client.ExchangeToken(context.Background(), &ConsoleTokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: "refresh-old",
		ClientID:     ConsoleClientIDSameDevice,
		Scope:        scopeAllAll,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}
