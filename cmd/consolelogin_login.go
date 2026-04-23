package cmd

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/volcengine/volcengine-cli/util"
)

const scopeAllAll = "Console:All:All"

// ConsoleLogin holds runtime state for the volcengine login flow.
type ConsoleLogin struct {
	Profile     string // profile name, default "default"
	Region      string
	Remote      bool   // true = cross-device mode
	EndpointURL string // default "https://signin.volcengine.com"
}

// LoginTokenCache represents the cached login token data persisted to disk.
type LoginTokenCache struct {
	LoginSession string          `json:"login_session"`
	AccessToken  json.RawMessage `json:"access_token"`
	RefreshToken string          `json:"refresh_token,omitempty"`
	IDToken      string          `json:"id_token,omitempty"`
	Scope        string          `json:"scope"`
	ClientID     string          `json:"client_id"`
	EndpointURL  string          `json:"endpoint_url,omitempty"`
	IssuedAt     string          `json:"issued_at"`
	ExpiresIn    int             `json:"expires_in"`
	TokenType    string          `json:"token_type"`
}

// ---------------------------------------------------------------------------
// Login orchestrates the full console login flow.
// ---------------------------------------------------------------------------

func (cl *ConsoleLogin) Login() error {
	// Apply defaults.
	if cl.Profile == "" {
		cl.Profile = "default"
	}

	if cl.EndpointURL == "" {
		cl.EndpointURL = "https://signin.volcengine.com"
	}

	// Load existing profile values from the in-memory runtime config first.
	cfg := runtimeConfig()
	if cfg == nil {
		cfg = &Configure{
			Profiles: make(map[string]*Profile),
		}
		setRuntimeConfig(cfg)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]*Profile)
	}
	if profile := cfg.Profiles[cl.Profile]; profile != nil {
		if cl.Region == "" && profile.Region != "" {
			cl.Region = profile.Region
		}
	}

	// 1. Determine client_id based on mode.
	clientID := ConsoleClientIDSameDevice
	if cl.Remote {
		clientID = ConsoleClientIDCrossDevice
	}

	// 2. Generate PKCE parameters.
	codeVerifier, err := generateCodeVerifier()
	if err != nil {
		return fmt.Errorf("generating code verifier: %w", err)
	}
	codeChallenge := generateCodeChallenge(codeVerifier)

	// 3. Generate state (UUID v4).
	state, err := generateState()
	if err != nil {
		return fmt.Errorf("generating state: %w", err)
	}

	// 4. Create the OAuth client.
	oauthClient := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
		EndpointURL: cl.EndpointURL,
	})

	// 5. Obtain the authorization code and redirect_uri used.
	var authCode string
	var redirectURI string
	if cl.Remote {
		authCode, redirectURI, err = cl.remoteAuthorize(oauthClient, clientID, codeChallenge, state)
	} else {
		authCode, redirectURI, err = cl.localAuthorize(oauthClient, clientID, codeChallenge, state)
	}
	if err != nil {
		return err
	}

	// 6. Exchange authorization code for token.
	tokenResp, err := oauthClient.ExchangeToken(context.Background(), &ConsoleTokenRequest{
		GrantType:    "authorization_code",
		Code:         authCode,
		RedirectURI:  redirectURI,
		ClientID:     clientID,
		Scope:        scopeAllAll,
		CodeVerifier: codeVerifier,
	})
	if err != nil {
		return fmt.Errorf("exchanging authorization code for token: %w", err)
	}

	// 7. Validate STS credentials from access_token.
	if _, err := ParseSTSCredentials(tokenResp.AccessToken); err != nil {
		return fmt.Errorf("parsing STS credentials: %w", err)
	}

	// 8. Extract login_session from id_token.
	loginSession, err := extractLoginSession(tokenResp.IDToken)
	if err != nil {
		return fmt.Errorf("extracting login session from id_token: %w", err)
	}

	// 9. Confirm replacement when the profile is already bound to another login_session.
	profile, exists := cfg.Profiles[cl.Profile]
	if !exists || profile == nil {
		disableSSL := false
		profile = &Profile{
			Name:       cl.Profile,
			DisableSSL: &disableSSL,
		}
	}
	if profile.LoginSession != "" && profile.LoginSession != loginSession {
		confirmed, err := confirmLoginSessionReplacement(os.Stdin, os.Stdout, cl.Profile, profile.LoginSession, loginSession)
		if err != nil {
			return fmt.Errorf("confirming login session replacement: %w", err)
		}
		if !confirmed {
			return fmt.Errorf("login canceled: existing login_session was not replaced")
		}
	}

	// 10. Cache the token to disk.
	accessTokenRaw := json.RawMessage(tokenResp.AccessToken)
	cache := &LoginTokenCache{
		LoginSession: loginSession,
		AccessToken:  accessTokenRaw,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		Scope:        scopeAllAll,
		ClientID:     clientID,
		EndpointURL:  cl.EndpointURL,
		IssuedAt:     time.Now().UTC().Format(time.RFC3339),
		ExpiresIn:    tokenResp.ExpiresIn,
		TokenType:    tokenResp.TokenType,
	}
	if err := writeLoginCache(cache); err != nil {
		return fmt.Errorf("writing login cache: %w", err)
	}

	// 11. Update the CLI config profile.
	profile.Mode = ModeConsoleLogin
	if cl.Region != "" {
		profile.Region = cl.Region
	}
	profile.LoginSession = loginSession

	cfg.Profiles[cl.Profile] = profile
	if cfg.Current == "" {
		cfg.Current = cl.Profile
	}

	if err := WriteConfigToFile(cfg); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	setRuntimeConfig(cfg)

	// 12. Print success message.
	fmt.Println("\nSuccessfully logged in!")
	fmt.Printf("Credentials cached for profile: %s\n", cl.Profile)
	issuedAt, _ := time.Parse(time.RFC3339, cache.IssuedAt)
	expiresAt := issuedAt.Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	fmt.Printf("STS credentials expire at: %s\n", expiresAt.Local().Format("2006-01-02 15:04:05"))
	return nil
}

func confirmLoginSessionReplacement(input io.Reader, output io.Writer, profileName, currentLoginSession, newLoginSession string) (bool, error) {
	if currentLoginSession == "" || currentLoginSession == newLoginSession {
		return true, nil
	}
	if input == nil {
		return false, fmt.Errorf("nil input reader")
	}
	if output == nil {
		output = io.Discard
	}

	reader := bufio.NewReader(input)
	fmt.Fprintf(output, "Profile %q is currently using login_session %q.\n", profileName, currentLoginSession)
	fmt.Fprintf(output, "The new login would replace it with %q.\n", newLoginSession)
	fmt.Fprint(output, "Replace the existing login_session? [y/N]: ")

	response, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}

	answer := strings.ToLower(strings.TrimSpace(response))
	return answer == "y" || answer == "yes", nil
}

// ---------------------------------------------------------------------------
// localAuthorize runs the browser-based local redirect flow.
// Returns the authorization code and the redirect_uri used.
// ---------------------------------------------------------------------------

func (cl *ConsoleLogin) localAuthorize(
	oauthClient *ConsoleOAuthClient,
	clientID, codeChallenge, state string,
) (string, string, error) {

	// Start the local callback server.
	cbServer, err := NewCallbackServer()
	if err != nil {
		return "", "", fmt.Errorf("starting callback server: %w", err)
	}
	cbServer.Start()
	defer cbServer.Shutdown()

	redirectURI := cbServer.RedirectURI()

	// Build the authorize URL.
	authorizeURL := oauthClient.BuildAuthorizeURL(&AuthorizeParams{
		ClientID:            clientID,
		Scope:               scopeAllAll,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
		State:               state,
		RedirectURI:         redirectURI,
	})

	fmt.Println("Attempting to automatically open the login page in your default browser.")
	fmt.Println("If the browser does not open, open the following URL:")
	fmt.Println(authorizeURL)

	// Best-effort browser open.
	_ = util.OpenBrowser(authorizeURL)

	// Wait for the callback with a 10-minute timeout.
	result, err := cbServer.WaitForCallback(10 * time.Minute)
	if err != nil {
		return "", "", fmt.Errorf("waiting for authorization callback: %w", err)
	}

	// Check for errors in the result.
	if result.Error != "" {
		desc := result.Error
		if result.ErrorDescription != "" {
			desc = fmt.Sprintf("%s: %s", result.Error, result.ErrorDescription)
		}
		return "", "", fmt.Errorf("authorization failed: %s", desc)
	}

	// Validate the state matches.
	if result.State != state {
		return "", "", fmt.Errorf("state mismatch: expected %s, got %s (possible CSRF attack)", state, result.State)
	}

	if result.Code == "" {
		return "", "", fmt.Errorf("authorization callback did not include an authorization code")
	}

	return result.Code, redirectURI, nil
}

// ---------------------------------------------------------------------------
// remoteAuthorize runs the cross-device (manual code entry) flow.
// ---------------------------------------------------------------------------

func (cl *ConsoleLogin) remoteAuthorize(
	oauthClient *ConsoleOAuthClient,
	clientID, codeChallenge, state string,
) (string, string, error) {

	// Default redirect_uri for cross-device flow.
	redirectURI := strings.TrimRight(cl.EndpointURL, "/") + "/authorize/oauth/authorize"

	// Build the authorize URL with the default redirect_uri for cross-device flow.
	authorizeURL := oauthClient.BuildAuthorizeURL(&AuthorizeParams{
		ClientID:            clientID,
		Scope:               scopeAllAll,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
		State:               state,
		RedirectURI:         redirectURI,
	})

	fmt.Println("Open the following URL in a browser on any device:")
	fmt.Println()
	fmt.Println(authorizeURL)
	fmt.Println()
	fmt.Println("After completing login, enter the authorization code shown in the browser:")

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Authorization code: ")
	rawInput, err := reader.ReadString('\n')
	if err != nil {
		return "", "", fmt.Errorf("reading authorization code from stdin: %w", err)
	}
	rawInput = strings.TrimSpace(rawInput)
	if rawInput == "" {
		return "", "", fmt.Errorf("authorization code cannot be empty")
	}

	// Base64 decode the input. The browser displays a base64-encoded string
	// containing "code=<authcode>&state=<state>".
	decoded, err := base64.StdEncoding.DecodeString(rawInput)
	if err != nil {
		// Try URL-safe base64 as a fallback.
		decoded, err = base64.URLEncoding.DecodeString(rawInput)
		if err != nil {
			return "", "", fmt.Errorf("base64 decoding authorization response: %w", err)
		}
	}

	// Parse the decoded query string to extract code and state.
	params, err := url.ParseQuery(string(decoded))
	if err != nil {
		return "", "", fmt.Errorf("parsing decoded authorization response: %w", err)
	}

	authCode := params.Get("code")
	if authCode == "" {
		return "", "", fmt.Errorf("decoded authorization response does not contain a \"code\" parameter")
	}

	// Validate the state to prevent CSRF attacks.
	respondedState := params.Get("state")
	if respondedState != state {
		return "", "", fmt.Errorf("state mismatch: expected %s, got %s (possible CSRF attack)", state, respondedState)
	}

	return authCode, redirectURI, nil
}

// ---------------------------------------------------------------------------
// Cache management
// ---------------------------------------------------------------------------

// getLoginCacheDir returns the path to ~/.volcengine/login/cache/, creating
// the directory tree if it does not exist.
func getLoginCacheDir() (string, error) {
	configDir, err := configFileDirFunc()
	if err != nil {
		return "", fmt.Errorf("getting config directory: %w", err)
	}
	cacheDir := filepath.Join(configDir, "login", "cache")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return "", fmt.Errorf("creating cache directory %s: %w", cacheDir, err)
	}
	return cacheDir, nil
}

// loginCacheFilePath returns the full path for a cached token file.
// The filename is the hex-encoded SHA-1 of the loginSession string.
func loginCacheFilePath(loginSession string) (string, error) {
	cacheDir, err := getLoginCacheDir()
	if err != nil {
		return "", err
	}
	h := sha1.New()
	h.Write([]byte(loginSession))
	name := fmt.Sprintf("%x.json", h.Sum(nil))
	return filepath.Join(cacheDir, name), nil
}

// writeLoginCache atomically writes the token cache to disk with 0600
// permissions. It writes to a temporary file first, then renames.
func writeLoginCache(cache *LoginTokenCache) (retErr error) {
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling login cache: %w", err)
	}

	cachePath, err := loginCacheFilePath(cache.LoginSession)
	if err != nil {
		return err
	}

	dir := filepath.Dir(cachePath)
	tmpFile, err := os.CreateTemp(dir, ".tmp-login-cache-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmpFile.Name()
	closed := false
	defer func() {
		if retErr != nil {
			if !closed {
				_ = tmpFile.Close()
			}
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("writing temp cache file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing temp cache file: %w", err)
	}
	closed = true
	if err := os.Chmod(tmpName, 0600); err != nil {
		return fmt.Errorf("setting cache file permissions: %w", err)
	}
	if err := os.Rename(tmpName, cachePath); err != nil {
		return fmt.Errorf("renaming temp cache file: %w", err)
	}
	return nil
}

// readLoginCache reads and parses a cached token file identified by
// loginSession.
func readLoginCache(loginSession string) (*LoginTokenCache, error) {
	cachePath, err := loginCacheFilePath(loginSession)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, fmt.Errorf("reading cache file %s: %w", cachePath, err)
	}

	var cache LoginTokenCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("parsing cache file %s: %w", cachePath, err)
	}
	return &cache, nil
}

// extractLoginSession decodes the JWT payload (second segment) and returns
// the "sub" claim which serves as the login_session identifier.
// No signature verification is performed because the token is obtained
// directly from the trusted signin server over TLS.
func extractLoginSession(idToken string) (string, error) {
	if idToken == "" {
		return "", fmt.Errorf("id_token is empty")
	}

	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("id_token does not have a valid JWT structure")
	}

	payload := parts[1]
	// JWT base64url encoding may lack padding; add it back.
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("base64-decoding JWT payload: %w", err)
	}

	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return "", fmt.Errorf("parsing JWT payload JSON: %w", err)
	}
	if claims.Sub == "" {
		return "", fmt.Errorf("id_token JWT payload does not contain a \"sub\" claim")
	}
	return claims.Sub, nil
}

// ---------------------------------------------------------------------------
// EnsureValidLoginToken checks the cached login token for the given profile,
// refreshes it if expired, and returns usable STS credentials. It is
// intended to be called by sdk_client.go before making API calls.
// ---------------------------------------------------------------------------

func EnsureValidLoginToken(cfg *Configure, profileName string) (*STSCredentials, error) {
	if cfg == nil || cfg.Profiles == nil {
		return nil, fmt.Errorf("no configuration loaded")
	}

	// 1. Look up profile and its login_session.
	profile, ok := cfg.Profiles[profileName]
	if !ok || profile == nil {
		return nil, fmt.Errorf("profile %q not found in config", profileName)
	}
	if profile.LoginSession == "" {
		return nil, fmt.Errorf("profile %q does not have a login_session; run 've login' first", profileName)
	}

	// 2. Read the token cache.
	cache, err := readLoginCache(profile.LoginSession)
	if err != nil {
		return nil, fmt.Errorf("no active session. Please run 've login' first")
	}

	// 3. Parse STS credentials from the cached access_token.
	//    access_token in the cache is stored as json.RawMessage.
	//    It could be a JSON string (quoted) or a JSON object.
	var accessTokenStr string
	if err := json.Unmarshal(cache.AccessToken, &accessTokenStr); err != nil {
		// Not a JSON string; use the raw bytes directly.
		accessTokenStr = string(cache.AccessToken)
	}

	creds, err := ParseSTSCredentials(accessTokenStr)
	if err != nil {
		return nil, fmt.Errorf("parsing cached STS credentials: %w", err)
	}

	// 4. Check whether the credentials have expired using issued_at + expires_in.
	issuedAt, err := time.Parse(time.RFC3339, cache.IssuedAt)
	if err != nil {
		return nil, fmt.Errorf("parsing issued_at %q: %w", cache.IssuedAt, err)
	}
	expiration := issuedAt.Add(time.Duration(cache.ExpiresIn) * time.Second)

	// 5. If still valid (with a 60-second safety buffer), return immediately.
	if time.Now().UTC().Before(expiration.Add(-60 * time.Second)) {
		return creds, nil
	}

	// 6. Token expired; attempt refresh.
	if cache.RefreshToken == "" {
		return nil, fmt.Errorf(
			"no refresh token available. Session expired. Please run 've login' to re-authenticate",
		)
	}

	endpointURL := cache.EndpointURL
	if endpointURL == "" {
		endpointURL = defaultConsoleEndpoint
	}

	oauthClient := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
		EndpointURL: endpointURL,
	})

	tokenResp, err := oauthClient.ExchangeToken(context.Background(), &ConsoleTokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: cache.RefreshToken,
		ClientID:     cache.ClientID,
		Scope:        cache.Scope,
	})
	if err != nil {
		return nil, fmt.Errorf(
			"failed to refresh session token. Please run 've login' to re-authenticate. %w", err,
		)
	}

	// 7. Parse new credentials.
	newCreds, err := ParseSTSCredentials(tokenResp.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("parsing refreshed STS credentials: %w", err)
	}

	// 8. Update the cache on disk.
	cache.AccessToken = json.RawMessage(tokenResp.AccessToken)
	if tokenResp.RefreshToken != "" {
		cache.RefreshToken = tokenResp.RefreshToken
	}
	if tokenResp.IDToken != "" {
		cache.IDToken = tokenResp.IDToken
	}
	cache.IssuedAt = time.Now().UTC().Format(time.RFC3339)
	cache.ExpiresIn = tokenResp.ExpiresIn
	cache.TokenType = tokenResp.TokenType

	if err := writeLoginCache(cache); err != nil {
		// Non-fatal: credentials are still valid in memory.
		fmt.Fprintf(os.Stderr, "Warning: failed to update login cache: %v\n", err)
	}

	return newCreds, nil
}
