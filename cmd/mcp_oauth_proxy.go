package cmd

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OAuth proxy routes, relative to the MCP base path. The callback path is
// the one registered with the upstream OAuth application.
const (
	mcpOAuthProxyRegisterPath  = "/auth/oauth/register"
	mcpOAuthProxyAuthorizePath = "/auth/oauth/authorize"
	mcpOAuthProxyCallbackPath  = "/auth/oauth/callback"
	mcpOAuthProxyTokenPath     = "/auth/oauth/token"

	mcpOAuthProxyClientID        = "volcengine-mcp"
	mcpOAuthProxyStateLifetime   = 10 * time.Minute
	mcpOAuthProxyCodeLifetime    = 5 * time.Minute
	mcpOAuthProxyUserinfoTTL     = 5 * time.Minute
	mcpOAuthProxyUserinfoEntries = 10000
	mcpOAuthProxyStateKeyEnv     = "VOLCENGINE_MCP_OAUTH_STATE_KEY"
)

// mcpOAuthUpstreamOptions configure the OAuth proxy for an upstream OAuth 2.0
// application with a fixed client, such as GitHub or a company SSO, whose
// access tokens cannot be verified locally.
type mcpOAuthUpstreamOptions struct {
	AuthorizeURL     string
	TokenURL         string
	UserinfoURL      string
	ClientID         string
	ClientSecret     string
	ClientSecretFile string
	Scopes           []string
	TokenAuthMethod  string
	// RedirectURIs are client redirect URIs allowed besides loopback ones.
	RedirectURIs []string
}

func (o mcpOAuthUpstreamOptions) configured() bool {
	return o.AuthorizeURL != "" || o.TokenURL != "" || o.UserinfoURL != "" || o.ClientID != ""
}

func (o *mcpOAuthUpstreamOptions) validate() error {
	for _, field := range []struct{ value, label string }{
		{o.AuthorizeURL, "--oauth-upstream-authorize-url"}, {o.TokenURL, "--oauth-upstream-token-url"},
		{o.UserinfoURL, "--oauth-upstream-userinfo-url"}, {o.ClientID, "--oauth-upstream-client-id"},
	} {
		if field.value == "" {
			return fmt.Errorf("%s is required for the OAuth proxy", field.label)
		}
	}
	for _, field := range []struct{ value, label string }{
		{o.AuthorizeURL, "--oauth-upstream-authorize-url"}, {o.TokenURL, "--oauth-upstream-token-url"}, {o.UserinfoURL, "--oauth-upstream-userinfo-url"},
	} {
		if err := validateMCPSecureURL(field.value, field.label); err != nil {
			return err
		}
	}
	if o.ClientSecretFile != "" {
		if o.ClientSecret != "" {
			return fmt.Errorf("set the upstream client secret either in VOLCENGINE_MCP_OAUTH_UPSTREAM_CLIENT_SECRET or with --oauth-upstream-client-secret-file")
		}
		data, err := os.ReadFile(o.ClientSecretFile)
		if err != nil {
			return fmt.Errorf("read --oauth-upstream-client-secret-file: %w", err)
		}
		o.ClientSecret = strings.TrimSpace(string(data))
	}
	switch o.TokenAuthMethod {
	case "":
		o.TokenAuthMethod = "client_secret_post"
	case "client_secret_post", "client_secret_basic":
	default:
		return fmt.Errorf("--oauth-upstream-token-auth-method must be client_secret_post or client_secret_basic")
	}
	for _, redirect := range o.RedirectURIs {
		if parsed, err := url.Parse(redirect); err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Fragment != "" {
			return fmt.Errorf("--oauth-redirect-uris: invalid redirect URI %q", redirect)
		}
	}
	return nil
}

// mcpOAuthProxy is the authorization server MCP clients see when ve mcp
// fronts an upstream OAuth application. Clients register dynamically and
// use PKCE against the proxy; the proxy runs the authorization code flow
// with the upstream client and its secret, and hands the upstream access
// token to the client. State and codes are sealed with a server key, so the
// proxy keeps no per-login state.
type mcpOAuthProxy struct {
	options  mcpOAuthUpstreamOptions
	issuer   string
	callback string
	routes   map[string]string
	aead     cipher.AEAD
	client   *http.Client
	now      func() time.Time
	verifier *mcpUserinfoVerifier
}

func newMCPOAuthProxy(options mcpOAuthUpstreamOptions, baseURL string, client *http.Client) (*mcpOAuthProxy, error) {
	if err := options.validate(); err != nil {
		return nil, err
	}
	key, err := mcpOAuthProxyStateKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	issuer := strings.TrimRight(baseURL, "/")
	proxy := &mcpOAuthProxy{
		options: options, issuer: issuer, callback: joinMCPURL(issuer, mcpOAuthProxyCallbackPath),
		aead: aead, client: client, now: time.Now,
	}
	proxy.verifier = &mcpUserinfoVerifier{endpoint: options.UserinfoURL, client: client, now: time.Now, entries: map[[32]byte]mcpUserinfoEntry{}}
	return proxy, nil
}

// mcpOAuthProxyStateKey returns the AES-256 key that seals state and codes.
// Instances behind one load balancer must share it; a single process may
// use a random key, which invalidates logins in progress on restart.
func mcpOAuthProxyStateKey() ([]byte, error) {
	encoded := strings.TrimSpace(os.Getenv(mcpOAuthProxyStateKeyEnv))
	if encoded == "" {
		key := make([]byte, 32)
		_, err := rand.Read(key)
		return key, err
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("%s must be 32 bytes encoded in standard base64", mcpOAuthProxyStateKeyEnv)
	}
	return key, nil
}

// authorizationServerMetadataPath is the RFC 8414 metadata path for the
// proxy issuer.
func (p *mcpOAuthProxy) authorizationServerMetadataPath() string {
	parsed, _ := url.Parse(p.issuer)
	metadataPath := "/.well-known/oauth-authorization-server"
	if issuerPath := strings.Trim(parsed.Path, "/"); issuerPath != "" {
		metadataPath += "/" + issuerPath
	}
	return metadataPath
}

// register adds the proxy endpoints to mux. They are public OAuth
// endpoints that browser-based clients call cross-origin.
func (p *mcpOAuthProxy) register(mux *http.ServeMux, basePath string) {
	handlers := map[string]http.HandlerFunc{
		p.authorizationServerMetadataPath():                p.serveMetadata,
		joinMCPRoute(basePath, mcpOAuthProxyRegisterPath):  p.serveRegister,
		joinMCPRoute(basePath, mcpOAuthProxyAuthorizePath): p.serveAuthorize,
		joinMCPRoute(basePath, mcpOAuthProxyCallbackPath):  p.serveCallback,
		joinMCPRoute(basePath, mcpOAuthProxyTokenPath):     p.serveToken,
	}
	for route, handler := range handlers {
		mux.Handle(route, mcpOAuthPublicMiddleware(handler))
	}
}

// mcpOAuthPublicMiddleware allows cross-origin calls without credentials and
// bounds request bodies.
func mcpOAuthPublicMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, MCP-Protocol-Version")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
		}
		next.ServeHTTP(w, r)
	})
}

func (p *mcpOAuthProxy) endpoint(route string) string {
	return joinMCPURL(p.issuer, route)
}

func (p *mcpOAuthProxy) serveMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeMCPOAuthJSON(w, http.StatusOK, map[string]interface{}{
		"issuer":                                p.issuer,
		"authorization_endpoint":                p.endpoint(mcpOAuthProxyAuthorizePath),
		"token_endpoint":                        p.endpoint(mcpOAuthProxyTokenPath),
		"registration_endpoint":                 p.endpoint(mcpOAuthProxyRegisterPath),
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      append([]string{}, p.options.Scopes...),
	})
}

// redirectAllowed accepts loopback http(s) redirect URIs, as native and
// local clients use, and the exact URIs configured with --oauth-redirect-uris.
func (p *mcpOAuthProxy) redirectAllowed(redirect string) bool {
	parsed, err := url.Parse(redirect)
	if err != nil || parsed.Host == "" || parsed.Fragment != "" {
		return false
	}
	if (parsed.Scheme == "http" || parsed.Scheme == "https") && isLoopbackMCPHostname(parsed.Hostname()) {
		return true
	}
	for _, allowed := range p.options.RedirectURIs {
		if redirect == allowed {
			return true
		}
	}
	return false
}

func (p *mcpOAuthProxy) serveRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var request map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "the registration request is not a JSON object")
		return
	}
	redirects, _ := request["redirect_uris"].([]interface{})
	if len(redirects) == 0 {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris is required")
		return
	}
	for _, value := range redirects {
		redirect, _ := value.(string)
		if !p.redirectAllowed(redirect) {
			writeMCPOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", fmt.Sprintf("redirect URI %q is not allowed", redirect))
			return
		}
	}
	// The proxy accepts every client with an allowed redirect URI, so a
	// registration only echoes the client metadata.
	response := map[string]interface{}{
		"client_id": mcpOAuthProxyClientID, "client_id_issued_at": p.now().Unix(),
		"redirect_uris": redirects, "token_endpoint_auth_method": "none",
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
	}
	if name, ok := request["client_name"].(string); ok {
		response["client_name"] = name
	}
	writeMCPOAuthJSON(w, http.StatusCreated, response)
}

// mcpOAuthProxyGrant is sealed into the upstream state and into the code
// handed to the client.
type mcpOAuthProxyGrant struct {
	RedirectURI   string `json:"r"`
	ClientState   string `json:"s,omitempty"`
	CodeChallenge string `json:"c"`
	UpstreamCode  string `json:"u,omitempty"`
	Expires       int64  `json:"e"`
}

func (p *mcpOAuthProxy) serveAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	redirect := query.Get("redirect_uri")
	if !p.redirectAllowed(redirect) {
		// Never redirect to a URI that is not allowed.
		http.Error(w, "redirect_uri is missing or not allowed", http.StatusBadRequest)
		return
	}
	fail := func(code, description string) {
		redirectMCPOAuthClient(w, r, redirect, url.Values{"error": {code}, "error_description": {description}, "state": {query.Get("state")}})
	}
	if query.Get("response_type") != "code" {
		fail("unsupported_response_type", "response_type must be code")
		return
	}
	if query.Get("code_challenge") == "" || query.Get("code_challenge_method") != "S256" {
		fail("invalid_request", "PKCE with code_challenge_method S256 is required")
		return
	}
	state, err := p.seal("state", mcpOAuthProxyGrant{
		RedirectURI: redirect, ClientState: query.Get("state"), CodeChallenge: query.Get("code_challenge"),
		Expires: p.now().Add(mcpOAuthProxyStateLifetime).Unix(),
	})
	if err != nil {
		fail("server_error", "could not start the authorization")
		return
	}
	upstream, _ := url.Parse(p.options.AuthorizeURL)
	values := upstream.Query()
	values.Set("response_type", "code")
	values.Set("client_id", p.options.ClientID)
	values.Set("redirect_uri", p.callback)
	values.Set("state", state)
	if len(p.options.Scopes) > 0 {
		values.Set("scope", strings.Join(p.options.Scopes, " "))
	}
	upstream.RawQuery = values.Encode()
	http.Redirect(w, r, upstream.String(), http.StatusFound)
}

func (p *mcpOAuthProxy) serveCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	var grant mcpOAuthProxyGrant
	if err := p.open("state", query.Get("state"), &grant); err != nil {
		http.Error(w, "the authorization state is invalid or expired; start the login again", http.StatusBadRequest)
		return
	}
	back := url.Values{}
	if grant.ClientState != "" {
		back.Set("state", grant.ClientState)
	}
	if upstreamError := query.Get("error"); upstreamError != "" {
		back.Set("error", upstreamError)
		if description := query.Get("error_description"); description != "" {
			back.Set("error_description", description)
		}
		redirectMCPOAuthClient(w, r, grant.RedirectURI, back)
		return
	}
	if query.Get("code") == "" {
		back.Set("error", "server_error")
		back.Set("error_description", "the upstream authorization server returned no code")
		redirectMCPOAuthClient(w, r, grant.RedirectURI, back)
		return
	}
	grant.UpstreamCode = query.Get("code")
	grant.ClientState = ""
	grant.Expires = p.now().Add(mcpOAuthProxyCodeLifetime).Unix()
	code, err := p.seal("code", grant)
	if err != nil {
		back.Set("error", "server_error")
		redirectMCPOAuthClient(w, r, grant.RedirectURI, back)
		return
	}
	back.Set("code", code)
	redirectMCPOAuthClient(w, r, grant.RedirectURI, back)
}

func (p *mcpOAuthProxy) serveToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_request", "the token request must be form encoded")
		return
	}
	upstream := url.Values{}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		var grant mcpOAuthProxyGrant
		if err := p.open("code", r.PostForm.Get("code"), &grant); err != nil {
			writeMCPOAuthError(w, http.StatusBadRequest, "invalid_grant", "the authorization code is invalid or expired")
			return
		}
		if r.PostForm.Get("redirect_uri") != grant.RedirectURI {
			writeMCPOAuthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match the authorization request")
			return
		}
		digest := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(digest[:])), []byte(grant.CodeChallenge)) != 1 {
			writeMCPOAuthError(w, http.StatusBadRequest, "invalid_grant", "code_verifier does not match the code challenge")
			return
		}
		upstream.Set("grant_type", "authorization_code")
		upstream.Set("code", grant.UpstreamCode)
		upstream.Set("redirect_uri", p.callback)
	case "refresh_token":
		if r.PostForm.Get("refresh_token") == "" {
			writeMCPOAuthError(w, http.StatusBadRequest, "invalid_request", "refresh_token is required")
			return
		}
		upstream.Set("grant_type", "refresh_token")
		upstream.Set("refresh_token", r.PostForm.Get("refresh_token"))
	default:
		writeMCPOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
		return
	}
	token, status, err := p.exchange(r.Context(), upstream)
	if err != nil {
		writeMCPOAuthError(w, status, "invalid_grant", err.Error())
		return
	}
	writeMCPOAuthJSON(w, http.StatusOK, token)
}

// exchange calls the upstream token endpoint with the proxy's client
// credentials and returns the token response for the client.
func (p *mcpOAuthProxy) exchange(ctx context.Context, values url.Values) (map[string]interface{}, int, error) {
	values.Set("client_id", p.options.ClientID)
	if p.options.ClientSecret != "" && p.options.TokenAuthMethod == "client_secret_post" {
		values.Set("client_secret", p.options.ClientSecret)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.options.TokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	if p.options.ClientSecret != "" && p.options.TokenAuthMethod == "client_secret_basic" {
		request.SetBasicAuth(url.QueryEscape(p.options.ClientID), url.QueryEscape(p.options.ClientSecret))
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("the upstream token endpoint failed: %v", err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	document := map[string]interface{}{}
	if mediaType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type")); mediaType == "application/x-www-form-urlencoded" || mediaType == "text/plain" {
		// Some providers answer in form encoding despite Accept.
		form, _ := url.ParseQuery(string(data))
		for key := range form {
			document[key] = form.Get(key)
		}
	} else if err := json.Unmarshal(data, &document); err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("the upstream token endpoint returned HTTP %d without a JSON body", response.StatusCode)
	}
	if upstreamError, _ := document["error"].(string); upstreamError != "" || response.StatusCode != http.StatusOK {
		description, _ := document["error_description"].(string)
		if upstreamError == "" {
			upstreamError = "HTTP " + strconv.Itoa(response.StatusCode)
		}
		return nil, http.StatusBadRequest, fmt.Errorf("the upstream token endpoint rejected the grant: %s %s", upstreamError, description)
	}
	accessToken, _ := document["access_token"].(string)
	if accessToken == "" {
		return nil, http.StatusBadGateway, errors.New("the upstream token endpoint returned no access_token")
	}
	token := map[string]interface{}{"access_token": accessToken, "token_type": "Bearer"}
	for _, key := range []string{"expires_in", "refresh_token", "scope"} {
		if value, ok := document[key]; ok && value != "" {
			if key == "expires_in" {
				if text, isText := value.(string); isText {
					if seconds, err := strconv.Atoi(text); err == nil {
						value = seconds
					}
				}
			}
			token[key] = value
		}
	}
	return token, http.StatusOK, nil
}

func (p *mcpOAuthProxy) seal(kind string, grant mcpOAuthProxyGrant) (string, error) {
	plaintext, err := json.Marshal(grant)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, p.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(p.aead.Seal(nonce, nonce, plaintext, []byte(kind))), nil
}

func (p *mcpOAuthProxy) open(kind, sealed string, grant *mcpOAuthProxyGrant) error {
	data, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil || len(data) < p.aead.NonceSize() {
		return errors.New("malformed")
	}
	plaintext, err := p.aead.Open(nil, data[:p.aead.NonceSize()], data[p.aead.NonceSize():], []byte(kind))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(plaintext, grant); err != nil {
		return err
	}
	if p.now().Unix() > grant.Expires {
		return errors.New("expired")
	}
	return nil
}

func redirectMCPOAuthClient(w http.ResponseWriter, r *http.Request, redirect string, values url.Values) {
	target, _ := url.Parse(redirect)
	query := target.Query()
	for key, value := range values {
		if len(value) > 0 && value[0] != "" {
			query[key] = value
		}
	}
	target.RawQuery = query.Encode()
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func writeMCPOAuthJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeMCPOAuthError(w http.ResponseWriter, status int, code, description string) {
	writeMCPOAuthJSON(w, status, map[string]string{"error": code, "error_description": description})
}

// mcpUserinfoVerifier accepts an access token when the upstream userinfo
// endpoint accepts it, caching the answer briefly so each MCP request does
// not call the upstream.
type mcpUserinfoVerifier struct {
	endpoint string
	client   *http.Client
	now      func() time.Time

	mu      sync.Mutex
	entries map[[32]byte]mcpUserinfoEntry
}

type mcpUserinfoEntry struct {
	claims  *mcpTokenClaims
	expires time.Time
}

func (v *mcpUserinfoVerifier) Verify(token string) (*mcpTokenClaims, error) {
	key := sha256.Sum256([]byte(token))
	v.mu.Lock()
	entry, ok := v.entries[key]
	v.mu.Unlock()
	if ok && v.now().Before(entry.expires) {
		return entry.claims, nil
	}
	request, err := http.NewRequest(http.MethodGet, v.endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", clientUserAgent(os.LookupEnv))
	response, err := v.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("userinfo request failed: %w", err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 256*1024))
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo returned HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var document map[string]interface{}
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("userinfo returned no JSON object")
	}
	claims := &mcpTokenClaims{}
	for _, name := range []string{"sub", "id", "login", "username", "email"} {
		if value, ok := document[name]; ok && value != nil && fmt.Sprint(value) != "" {
			claims.Subject = fmt.Sprint(value)
			break
		}
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("userinfo identifies no user")
	}
	v.mu.Lock()
	if len(v.entries) >= mcpOAuthProxyUserinfoEntries {
		now := v.now()
		for cached, value := range v.entries {
			if !now.Before(value.expires) {
				delete(v.entries, cached)
			}
		}
		if len(v.entries) >= mcpOAuthProxyUserinfoEntries {
			v.entries = map[[32]byte]mcpUserinfoEntry{}
		}
	}
	v.entries[key] = mcpUserinfoEntry{claims: claims, expires: v.now().Add(mcpOAuthProxyUserinfoTTL)}
	v.mu.Unlock()
	return claims, nil
}
