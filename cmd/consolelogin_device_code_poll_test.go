package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeviceCodeAuthorizeDoublesIntervalAfterHTTPTimeout(t *testing.T) {
	var tokenAttempts int32
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
			attempt := atomic.AddInt32(&tokenAttempts, 1)
			if attempt == 1 {
				// Sleep longer than the injected client timeout so Do()
				// fails as a timeout. Do not block on r.Context(): some
				// Client.Timeout paths leave that context open, and
				// httptest.Server.Close would then deadlock.
				time.Sleep(80 * time.Millisecond)
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
		HTTPClient:  &http.Client{Timeout: 40 * time.Millisecond},
	})
	resp, err := (&ConsoleLogin{UseDeviceCode: true, NoBrowser: true}).deviceCodeAuthorize(context.Background(), client)
	if err != nil {
		t.Fatalf("deviceCodeAuthorize returned error: %v", err)
	}
	if resp.RefreshToken != "refresh-token" {
		t.Fatalf("response = %+v", resp)
	}
	if attempts := atomic.LoadInt32(&tokenAttempts); attempts != 2 {
		t.Fatalf("token attempts = %d, want 2", attempts)
	}
	if len(waits) != 2 || waits[0] != 5*time.Second || waits[1] != 10*time.Second {
		t.Fatalf("waits = %v, want [5s 10s]", waits)
	}
}

func TestNextConsoleDeviceCodePollInterval(t *testing.T) {
	tests := []struct {
		current time.Duration
		want    time.Duration
	}{
		{current: 0, want: 10 * time.Second},
		{current: time.Second, want: 2 * time.Second},
		{current: 5 * time.Second, want: 10 * time.Second},
		{current: 10 * time.Second, want: 20 * time.Second},
		{current: 16 * time.Second, want: consoleDeviceCodeMaxPollInterval},
		{current: 20 * time.Second, want: consoleDeviceCodeMaxPollInterval},
		{current: consoleDeviceCodeMaxPollInterval, want: consoleDeviceCodeMaxPollInterval},
		{current: consoleDeviceCodeMaxPollInterval + time.Second, want: consoleDeviceCodeMaxPollInterval},
	}
	for _, tt := range tests {
		got := nextConsoleDeviceCodePollInterval(tt.current)
		if got != tt.want {
			t.Fatalf("nextConsoleDeviceCodePollInterval(%v) = %v, want %v", tt.current, got, tt.want)
		}
	}
}

func TestIsConsoleDeviceCodePollTimeout(t *testing.T) {
	timeoutURLErr := &url.Error{
		Op:  "Post",
		URL: "https://example.com/token",
		Err: context.DeadlineExceeded,
	}
	canceledURLErr := &url.Error{
		Op:  "Post",
		URL: "https://example.com/token",
		Err: context.Canceled,
	}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "canceled", err: context.Canceled, want: false},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: true},
		{name: "wrapped request timeout", err: trErrorf("request failed: %w", timeoutURLErr), want: true},
		{name: "wrapped canceled request", err: trErrorf("request failed: %w", canceledURLErr), want: false},
		{name: "net timeout", err: trErrorf("request failed: %w", timeoutError{}), want: true},
		{name: "decode error", err: trErrorf("failed to decode oauth response: %w", errors.New("invalid character")), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConsoleDeviceCodePollTimeout(tt.err)
			if got != tt.want {
				t.Fatalf("isConsoleDeviceCodePollTimeout(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestDeviceCodePollControlHandleTokenError(t *testing.T) {
	timeoutErr := trErrorf("request failed: %w", context.DeadlineExceeded)
	pendingErr := &ConsoleOAuthAPIError{Response: ConsoleOAuthErrorResponse{Error: "authorization_pending"}}
	slowDownErr := &ConsoleOAuthAPIError{Response: ConsoleOAuthErrorResponse{Error: "slow_down"}}
	deniedErr := &ConsoleOAuthAPIError{Response: ConsoleOAuthErrorResponse{Error: "access_denied"}}
	serverErr := &ConsoleOAuthAPIError{Response: ConsoleOAuthErrorResponse{Error: "server_error"}}
	unknownErr := &ConsoleOAuthAPIError{Response: ConsoleOAuthErrorResponse{Error: "invalid_client"}}
	decodeErr := errors.New("gateway down")

	poll := newDeviceCodePollControl(5)
	if err := poll.handleTokenError(pendingErr); err != nil {
		t.Fatalf("pending: %v", err)
	}
	if poll.interval != 5*time.Second || poll.transientErrors != 0 {
		t.Fatalf("pending state = %+v", poll)
	}

	if err := poll.handleTokenError(slowDownErr); err != nil {
		t.Fatalf("slow_down: %v", err)
	}
	if poll.interval != 10*time.Second {
		t.Fatalf("after slow_down interval = %v, want 10s", poll.interval)
	}

	if err := poll.handleTokenError(timeoutErr); err != nil {
		t.Fatalf("timeout: %v", err)
	}
	if poll.interval != 20*time.Second || poll.transientErrors != 1 {
		t.Fatalf("after timeout state = %+v", poll)
	}

	if err := poll.handleTokenError(decodeErr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if poll.interval != 20*time.Second || poll.transientErrors != 2 {
		t.Fatalf("after decode state = %+v", poll)
	}

	if err := poll.handleTokenError(serverErr); err != nil {
		t.Fatalf("server_error: %v", err)
	}
	if poll.interval != 20*time.Second || poll.transientErrors != 3 {
		t.Fatalf("after server_error state = %+v", poll)
	}

	if err := poll.handleTokenError(pendingErr); err != nil {
		t.Fatalf("pending reset: %v", err)
	}
	if poll.transientErrors != 0 || poll.interval != 20*time.Second {
		t.Fatalf("pending should reset budget but keep interval: %+v", poll)
	}

	if err := poll.handleTokenError(deniedErr); err == nil || !strings.Contains(err.Error(), "was denied") {
		t.Fatalf("denied = %v", err)
	}
	if err := poll.handleTokenError(unknownErr); err == nil || !strings.Contains(err.Error(), "invalid_client") {
		t.Fatalf("unknown = %v", err)
	}
}

func TestDeviceCodePollControlAbortsAfterTransientBudget(t *testing.T) {
	poll := newDeviceCodePollControl(5)
	err := errors.New("blip")
	for i := 0; i < consoleDeviceCodeMaxTransientErrors; i++ {
		if got := poll.handleTokenError(err); got != nil {
			t.Fatalf("attempt %d: unexpected abort: %v", i+1, got)
		}
	}
	if got := poll.handleTokenError(err); got == nil || !strings.Contains(got.Error(), "polling device authorization token") {
		t.Fatalf("budget abort = %v", got)
	}
	if poll.interval != 5*time.Second {
		t.Fatalf("non-timeout budget abort changed interval to %v", poll.interval)
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

var _ net.Error = timeoutError{}
