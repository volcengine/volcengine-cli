package cmd

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	callbackasset "github.com/volcengine/volcengine-cli/asset/consolelogin"
)

const callbackHTMLPlaceholder = "__CALLBACK_ERROR__"

var (
	callbackTemplateOnce sync.Once
	callbackTemplate     []byte
	callbackTemplateErr  error
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

func logCallbackWarning(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, "Warning: "+format+"\n", args...)
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
			logCallbackWarning("OAuth callback server stopped unexpectedly: %v", err)
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

func loadCallbackTemplate() ([]byte, error) {
	callbackTemplateOnce.Do(func() {
		callbackTemplate, callbackTemplateErr = callbackasset.Asset("callback.html")
		if callbackTemplateErr != nil {
			callbackTemplateErr = fmt.Errorf("failed to load callback html template asset: %w", callbackTemplateErr)
		}
	})

	if callbackTemplateErr != nil {
		return nil, callbackTemplateErr
	}
	return callbackTemplate, nil
}

func jsStringLiteral(value string) string {
	quoted := strconv.Quote(value)
	// Avoid accidentally terminating the script block when error text contains "</script>".
	return strings.ReplaceAll(quoted, "</", "<\\/")
}

func renderCallbackPage(errorMessage string) ([]byte, error) {
	content, err := loadCallbackTemplate()
	if err != nil {
		return nil, err
	}

	return bytes.Replace(content, []byte(callbackHTMLPlaceholder), []byte(jsStringLiteral(errorMessage)), 1), nil
}

func writeFallbackCallbackPage(w http.ResponseWriter, errorMessage string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	if errorMessage != "" {
		_, _ = fmt.Fprintf(
			w,
			`<html><body><h2>Authentication failed</h2><p>Please return to the terminal.</p><p>OAuth error: %s</p></body></html>`,
			html.EscapeString(errorMessage),
		)
		return
	}

	_, _ = fmt.Fprint(w, `<html><body><h2>Authentication successful!</h2><p>You can close this page and return to the terminal.</p></body></html>`)
}

// handleCallback processes the OAuth callback request from the browser.
// It extracts the authorization code and state (or error information) from
// the query parameters, delivers the result to the waiting goroutine, and
// returns an HTML page to the browser. Only the first callback is accepted;
// duplicate requests are ignored.
func (s *CallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		logCallbackWarning("received non-GET OAuth callback request: method=%s path=%s", r.Method, r.URL.Path)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	code := query.Get("code")
	state := query.Get("state")
	errorParam := query.Get("error")
	errorParamUpper := query.Get("Error")
	errorDescription := query.Get("error_description")
	oauthError := errorParam
	if oauthError == "" {
		oauthError = errorParamUpper
	}
	if oauthError == "" {
		oauthError = errorDescription
	}

	if oauthError != "" {
		logCallbackWarning("OAuth callback returned error=%q", oauthError)
	}
	if code == "" && oauthError == "" {
		logCallbackWarning("OAuth callback did not include both code and error; login flow may fail")
	}

	normalizedErrorDescription := errorDescription
	if normalizedErrorDescription == oauthError {
		normalizedErrorDescription = ""
	}

	result := &AuthorizationResult{
		Code:             code,
		State:            state,
		Error:            oauthError,
		ErrorDescription: normalizedErrorDescription,
	}

	// Deliver the result only once; ignore duplicate callbacks.
	select {
	case s.result <- result:
	default:
	}

	errorMessage := ""
	if oauthError != "" {
		errorMessage = oauthError
		if normalizedErrorDescription != "" {
			errorMessage = fmt.Sprintf("%s: %s", oauthError, errorDescription)
		}
	}

	page, err := renderCallbackPage(errorMessage)
	if err != nil {
		logCallbackWarning("failed to render OAuth callback page; fallback page is used: %v", err)
		writeFallbackCallbackPage(w, errorMessage)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(page); err != nil {
		logCallbackWarning("failed to write OAuth callback page response: %v", err)
	}
}
