package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

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
