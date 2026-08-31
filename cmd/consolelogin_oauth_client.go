package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	// defaultConsoleEndpoint is the default Volcengine console sign-in endpoint.
	defaultConsoleEndpoint = "https://signin.volcengine.com"

	// consoleTokenPath is the path appended to the endpoint for the token URL.
	consoleTokenPath = "/authorize/oauth/token"

	// consoleDeviceAuthorizationPath is the path used to start device authorization.
	consoleDeviceAuthorizationPath = "/authorize/oauth/device_authorization"

	// consoleTokenRequestTimeout is the HTTP timeout for console token exchange requests.
	consoleTokenRequestTimeout = 30 * time.Second

	// consoleTokenRetryAttempts is the number of retry attempts for token exchange.
	consoleTokenRetryAttempts = 3

	// ConsoleClientIDSameDevice is the legacy public client ID issued by the
	// removed authorization code flow. Login never mints it again; refresh
	// replays whatever client ID the cache holds, so caches written by older
	// CLI versions keep working. Kept to document that value and to pin it in
	// the compatibility tests.
	ConsoleClientIDSameDevice = "trn:signin:::devtools/same-device"

	// ConsoleClientIDCrossDevice is the public client ID used by device code login.
	ConsoleClientIDCrossDevice = "trn:signin:::devtools/cross-device"
)

// ---------------------------------------------------------------------------
// Error types (independent from SSO OAuthAPIError)
// ---------------------------------------------------------------------------

// ConsoleOAuthErrorResponse represents the error response body from the
// console signin OAuth endpoints. Structure per the signin API spec:
//
//	{
//	    "state": "...",               // echoed back if present in request
//	    "error": "invalid_grant",     // OAuth error code (required)
//	    "error_description": "...",   // human-readable detail (optional)
//	    "error_uri": "..."            // URI for more info (optional)
//	}
type ConsoleOAuthErrorResponse struct {
	State            string `json:"state,omitempty"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
	ErrorURI         string `json:"error_uri,omitempty"`
}

// ConsoleOAuthAPIError wraps a non-2xx response from the console OAuth endpoints.
type ConsoleOAuthAPIError struct {
	StatusCode int
	Response   ConsoleOAuthErrorResponse
	RawBody    string
	RequestID  string // X-Tt-Logid header
}

func (e *ConsoleOAuthAPIError) Error() string {
	if e == nil {
		return ""
	}

	var parts []string

	// Primary error code.
	if e.Response.Error != "" {
		parts = append(parts, e.Response.Error)
	}

	// Human-readable description.
	if e.Response.ErrorDescription != "" {
		parts = append(parts, e.Response.ErrorDescription)
	}

	// Build the message.
	msg := strings.Join(parts, ": ")
	if msg == "" {
		if e.RawBody != "" {
			msg = e.RawBody
		} else {
			msg = "unknown error"
		}
	}

	// Append metadata.
	suffix := fmt.Sprintf("[status %d", e.StatusCode)
	if e.RequestID != "" {
		suffix += ", requestId: " + e.RequestID
	}
	suffix += "]"

	return trf("console oauth request failed: %s %s", msg, suffix)
}

// IsRetryable reports whether the error is transient and the request should be
// retried. Reuses the same HTTP status heuristic as the SSO client.
func (e *ConsoleOAuthAPIError) IsRetryable() bool {
	if e == nil {
		return false
	}
	return e.StatusCode == http.StatusTooManyRequests ||
		e.StatusCode == http.StatusRequestTimeout ||
		e.StatusCode/100 == 5
}

// ---------------------------------------------------------------------------
// Client config & types
// ---------------------------------------------------------------------------

// ConsoleOAuthClientConfig holds configuration for console OAuth client.
type ConsoleOAuthClientConfig struct {
	// EndpointURL is the base URL of the console sign-in service,
	// e.g. "https://signin.volcengine.com". If empty, defaultConsoleEndpoint is used.
	EndpointURL string
	// HTTPClient allows injecting a custom HTTP client (e.g. for proxy or testing).
	HTTPClient *http.Client
}

// ConsoleOAuthClient wraps HTTP calls to the Volcengine console sign-in OAuth endpoints.
// Unlike the existing OAuthClient (which talks to CloudIdentity), this client targets
// signin.volcengine.com and implements the public-client device code flow.
type ConsoleOAuthClient struct {
	endpointURL            string
	tokenURL               string
	deviceAuthorizationURL string
	httpClient             *http.Client
}

// ConsoleTokenRequest represents the token exchange request for console OAuth.
type ConsoleTokenRequest struct {
	GrantType    string // deviceCodeGrantType or "refresh_token"
	ClientID     string
	Scope        string
	RefreshToken string // for refresh_token grant
	DeviceCode   string // for device code grant
}

// ConsoleDeviceAuthorizationRequest represents a device authorization request.
type ConsoleDeviceAuthorizationRequest struct {
	ClientID   string
	Scope      string
	DeviceInfo string
}

// ConsoleDeviceAuthorizationResponse represents a device authorization response.
type ConsoleDeviceAuthorizationResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval,omitempty"`
}

// ConsoleTokenResponse represents the raw token response from the console OAuth endpoint.
type ConsoleTokenResponse struct {
	AccessToken  string `json:"access_token"` // JSON string containing STS credentials
	TokenType    string `json:"token_type"`   // e.g. "urn:ietf:params:oauth:token-type:access_token_sts"
	ExpiresIn    int    `json:"expires_in"`   // seconds, e.g. 900
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	IDToken      string `json:"id_token"` // JWT
}

// STSCredentials represents the parsed STS credentials extracted from the access_token
// field of ConsoleTokenResponse. The access_token is a JSON-encoded string that must
// be parsed separately.
type STSCredentials struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

// NewConsoleOAuthClient creates a new ConsoleOAuthClient with the given configuration.
// If cfg is nil or EndpointURL is empty, the default endpoint is used.
func NewConsoleOAuthClient(cfg *ConsoleOAuthClientConfig) *ConsoleOAuthClient {
	endpoint := defaultConsoleEndpoint
	if cfg != nil && strings.TrimSpace(cfg.EndpointURL) != "" {
		endpoint = strings.TrimSpace(cfg.EndpointURL)
	}
	endpoint = strings.TrimRight(endpoint, "/")

	client := &http.Client{Timeout: consoleTokenRequestTimeout}
	if cfg != nil && cfg.HTTPClient != nil {
		client = cfg.HTTPClient
	}

	return &ConsoleOAuthClient{
		endpointURL:            endpoint,
		tokenURL:               endpoint + consoleTokenPath,
		deviceAuthorizationURL: endpoint + consoleDeviceAuthorizationPath,
		httpClient:             client,
	}
}

// StartDeviceAuthorization starts the OAuth 2.0 Device Authorization Grant flow.
func (c *ConsoleOAuthClient) StartDeviceAuthorization(
	ctx context.Context,
	req *ConsoleDeviceAuthorizationRequest,
) (*ConsoleDeviceAuthorizationResponse, error) {
	if req == nil {
		return nil, trErrorf("request cannot be nil")
	}
	if strings.TrimSpace(req.ClientID) == "" {
		return nil, trErrorf("client_id is required")
	}

	q := url.Values{}
	q.Set("client_id", req.ClientID)
	if req.Scope != "" {
		q.Set("scope", req.Scope)
	}
	if req.DeviceInfo != "" {
		q.Set("device_info", req.DeviceInfo)
	}

	var authResp ConsoleDeviceAuthorizationResponse
	if err := c.postForm(ctx, c.deviceAuthorizationURL, q, &authResp, consoleTokenRetryAttempts); err != nil {
		return nil, err
	}
	if strings.TrimSpace(authResp.DeviceCode) == "" {
		return nil, trErrorf("device authorization response missing device_code")
	}
	if strings.TrimSpace(authResp.UserCode) == "" {
		return nil, trErrorf("device authorization response missing user_code")
	}
	if strings.TrimSpace(authResp.VerificationURI) == "" {
		return nil, trErrorf("device authorization response missing verification_uri")
	}
	if authResp.ExpiresIn <= 0 {
		return nil, trErrorf("device authorization response has invalid expires_in")
	}

	return &authResp, nil
}

// ---------------------------------------------------------------------------
// ExchangeToken
// ---------------------------------------------------------------------------

// ExchangeToken performs the token exchange by sending a POST request to the token
// endpoint with application/x-www-form-urlencoded body parameters.
//
// For the device code grant: device_code and client_id are required.
//
// For grant_type=refresh_token: refresh_token and client_id are required.
//
// The method uses retry logic (doWithRetry) with up to 3 attempts for transient
// failures. Only errors where ConsoleOAuthAPIError.IsRetryable() returns true
// are retried.
func (c *ConsoleOAuthClient) ExchangeToken(ctx context.Context, req *ConsoleTokenRequest) (*ConsoleTokenResponse, error) {
	return c.exchangeToken(ctx, req, consoleTokenRetryAttempts)
}

// exchangeToken performs the token exchange with a caller-controlled attempt
// count. Device-code polling passes attempts=1 so that RFC 8628 interval /
// slow_down backpressure is driven by the poll loop instead of being swallowed
// by the transport-level retry, and so that slow_down/authorization_pending
// signals arriving with a retryable HTTP status reach the caller unfiltered.
func (c *ConsoleOAuthClient) exchangeToken(ctx context.Context, req *ConsoleTokenRequest, attempts int) (*ConsoleTokenResponse, error) {
	if req == nil {
		return nil, trErrorf("request cannot be nil")
	}
	if strings.TrimSpace(req.GrantType) == "" {
		return nil, trErrorf("grant_type is required")
	}
	if strings.TrimSpace(req.ClientID) == "" {
		return nil, trErrorf("client_id is required")
	}

	q := url.Values{}
	q.Set("grant_type", req.GrantType)
	q.Set("client_id", req.ClientID)

	if req.Scope != "" {
		q.Set("scope", req.Scope)
	}

	switch req.GrantType {
	case "refresh_token":
		if strings.TrimSpace(req.RefreshToken) == "" {
			return nil, trErrorf("refresh_token is required for refresh_token grant")
		}
		q.Set("refresh_token", req.RefreshToken)

	case deviceCodeGrantType:
		if strings.TrimSpace(req.DeviceCode) == "" {
			return nil, trErrorf("device_code is required for device code grant")
		}
		q.Set("device_code", req.DeviceCode)

	default:
		return nil, trErrorf("unsupported grant_type: %s", req.GrantType)
	}

	var tokenResp ConsoleTokenResponse
	if err := c.postForm(ctx, c.tokenURL, q, &tokenResp, attempts); err != nil {
		return nil, err
	}

	if tokenResp.AccessToken == "" && tokenResp.TokenType == "" &&
		tokenResp.RefreshToken == "" && tokenResp.ExpiresIn == 0 {
		return nil, trErrorf("ExchangeToken succeeded but response was empty")
	}

	return &tokenResp, nil
}

func (c *ConsoleOAuthClient) postForm(ctx context.Context, endpoint string, form url.Values, out interface{}, attempts int) error {
	requestBody := form.Encode()

	return doWithRetry(ctx, retryOptions{maxAttempts: attempts}, func() error {
		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(requestBody))
		if reqErr != nil {
			return trErrorf("failed to build request: %w", reqErr)
		}
		httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if customHeaders := os.Getenv("VOLCENGINE_LOGIN_HEADERS"); customHeaders != "" {
			for _, entry := range strings.Split(customHeaders, ";") {
				if idx := strings.Index(entry, "="); idx > 0 {
					httpReq.Header.Set(strings.TrimSpace(entry[:idx]), strings.TrimSpace(entry[idx+1:]))
				}
			}
		}
		resp, doErr := c.httpClient.Do(httpReq)
		if doErr != nil {
			return trErrorf("request failed: %w", doErr)
		}
		defer resp.Body.Close()

		respBytes, readErr := ioutil.ReadAll(resp.Body)
		if readErr != nil {
			return trErrorf("failed to read response: %w", readErr)
		}

		requestID := resp.Header.Get("X-Tt-Logid")

		// ---------- Error handling ----------
		if resp.StatusCode/100 != 2 {
			apiErr := &ConsoleOAuthAPIError{
				StatusCode: resp.StatusCode,
				RequestID:  requestID,
				RawBody:    string(respBytes),
			}

			// Try to parse the structured error response.
			if len(respBytes) > 0 {
				var errResp ConsoleOAuthErrorResponse
				if json.Unmarshal(respBytes, &errResp) == nil && errResp.Error != "" {
					apiErr.Response = errResp
				}
			}

			return apiErr
		}

		// ---------- Success handling ----------
		if len(respBytes) > 0 && out != nil {
			if unmarshalErr := json.Unmarshal(respBytes, out); unmarshalErr != nil {
				return trErrorf(
					"failed to decode oauth response (status %d, requestId: %s): %w",
					resp.StatusCode, requestID, unmarshalErr,
				)
			}
		}

		return nil
	})
}

// ---------------------------------------------------------------------------
// ParseSTSCredentials
// ---------------------------------------------------------------------------

// ParseSTSCredentials parses the JSON-encoded access_token string into an
// STSCredentials struct. The access_token field in ConsoleTokenResponse is not
// a simple bearer token; it is a JSON string containing STS temporary credentials.
func ParseSTSCredentials(accessToken string) (*STSCredentials, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, trErrorf("access_token is empty")
	}

	var creds STSCredentials
	if err := json.Unmarshal([]byte(accessToken), &creds); err != nil {
		return nil, trErrorf("failed to parse STS credentials from access_token: %w", err)
	}

	if creds.AccessKeyID == "" {
		return nil, trErrorf("parsed STS credentials missing access_key_id")
	}
	if creds.SecretAccessKey == "" {
		return nil, trErrorf("parsed STS credentials missing secret_access_key")
	}
	if creds.SessionToken == "" {
		return nil, trErrorf("parsed STS credentials missing session_token")
	}

	return &creds, nil
}
