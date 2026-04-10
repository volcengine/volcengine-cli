package cmd

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// AuthorizationResult holds the result received from the OAuth callback
type AuthorizationResult struct {
	Code             string
	State            string
	Error            string
	ErrorDescription string
}

// CallbackServer is a local HTTP server that listens for OAuth callbacks
type CallbackServer struct {
	server   *http.Server
	listener net.Listener
	result   chan *AuthorizationResult
	port     int
}

// NewCallbackServer creates a new local callback server bound to 127.0.0.1
// with an OS-assigned random available port.
func NewCallbackServer() (*CallbackServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port

	cs := &CallbackServer{
		listener: listener,
		result:   make(chan *AuthorizationResult, 1),
		port:     port,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", cs.handleCallback)

	cs.server = &http.Server{
		Handler: mux,
	}

	return cs, nil
}

// Port returns the assigned port number.
func (s *CallbackServer) Port() int {
	return s.port
}

// RedirectURI returns the full OAuth redirect URI for this callback server.
func (s *CallbackServer) RedirectURI() string {
	return fmt.Sprintf("http://127.0.0.1:%d/oauth/callback", s.port)
}

// Start begins serving HTTP requests in a background goroutine (non-blocking).
func (s *CallbackServer) Start() {
	go func() {
		if err := s.server.Serve(s.listener); err != nil && err != http.ErrServerClosed {
			s.result <- &AuthorizationResult{
				Error:            "server_error",
				ErrorDescription: fmt.Sprintf("callback server error: %v", err),
			}
		}
	}()
}

// WaitForCallback blocks until the OAuth callback is received or the timeout
// expires. Returns the AuthorizationResult or an error on timeout.
func (s *CallbackServer) WaitForCallback(timeout time.Duration) (*AuthorizationResult, error) {
	select {
	case result := <-s.result:
		return result, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timed out waiting for OAuth callback after %v", timeout)
	}
}

// Shutdown gracefully shuts down the HTTP server.
func (s *CallbackServer) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.server.Shutdown(ctx)
}

// handleCallback processes the OAuth callback request from the browser.
// It extracts the authorization code and state (or error information) from
// the query parameters, delivers the result to the waiting goroutine, and
// returns an HTML page to the browser. Only the first callback is accepted;
// duplicate requests are ignored.
func (s *CallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	code := query.Get("code")
	state := query.Get("state")
	oauthError := query.Get("error")
	errorDescription := query.Get("error_description")

	result := &AuthorizationResult{
		Code:             code,
		State:            state,
		Error:            oauthError,
		ErrorDescription: errorDescription,
	}

	// Deliver the result only once; ignore duplicate callbacks.
	select {
	case s.result <- result:
	default:
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if oauthError != "" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w,
			`<html><body><h2>Authentication failed</h2><p>Error: %s</p><p>%s</p><p>Please return to the terminal.</p></body></html>`,
			oauthError, errorDescription,
		)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w,
		`<html><body><h2>Authentication successful!</h2><p>You can close this page and return to the terminal.</p></body></html>`,
	)
}
