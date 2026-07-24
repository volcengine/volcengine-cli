package cmd

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const mcpOAuthMetadataPath = "/.well-known/oauth-protected-resource"

type mcpTokenClaims struct {
	Subject  string
	ClientID string
	Scopes   []string
}

type mcpTokenVerifier interface {
	Verify(string) (*mcpTokenClaims, error)
}

type mcpTokenClaimsContextKey struct{}

func mcpTokenClaimsFromContext(ctx context.Context) (*mcpTokenClaims, bool) {
	claims, ok := ctx.Value(mcpTokenClaimsContextKey{}).(*mcpTokenClaims)
	return claims, ok
}

func mcpOAuthMiddleware(verifier mcpTokenVerifier, resourceMetadataURL string, requiredScopes []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authorization := strings.TrimSpace(r.Header.Get("Authorization"))
			parts := strings.Fields(authorization)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
				writeMCPBearerError(w, http.StatusUnauthorized, "invalid_token", resourceMetadataURL, requiredScopes)
				return
			}
			claims, err := verifier.Verify(parts[1])
			if err != nil {
				writeMCPBearerError(w, http.StatusUnauthorized, "invalid_token", resourceMetadataURL, requiredScopes)
				return
			}
			if !hasMCPScopes(claims.Scopes, requiredScopes) {
				writeMCPBearerError(w, http.StatusForbidden, "insufficient_scope", resourceMetadataURL, requiredScopes)
				return
			}
			ctx := context.WithValue(r.Context(), mcpTokenClaimsContextKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeMCPBearerError(w http.ResponseWriter, status int, code, resourceMetadataURL string, scopes []string) {
	challenge := `Bearer error="` + mcpOAuthQuotedValue(code) + `"`
	if resourceMetadataURL != "" {
		challenge += `, resource_metadata="` + mcpOAuthQuotedValue(resourceMetadataURL) + `"`
	}
	if len(scopes) > 0 {
		challenge += `, scope="` + mcpOAuthQuotedValue(strings.Join(scopes, " ")) + `"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

func mcpOAuthQuotedValue(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func hasMCPScopes(actual, required []string) bool {
	if len(required) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(actual))
	for _, scope := range actual {
		set[scope] = struct{}{}
	}
	for _, scope := range required {
		if _, ok := set[scope]; !ok {
			return false
		}
	}
	return true
}

type mcpAuthorizationServerMetadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint,omitempty"`
	TokenEndpoint         string   `json:"token_endpoint,omitempty"`
	JWKSURI               string   `json:"jwks_uri"`
	ScopesSupported       []string `json:"scopes_supported,omitempty"`
}

type mcpProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

type mcpOAuthConfig struct {
	Issuer   string
	Audience string
	Resource string
	Client   *http.Client
}

func newMCPJWTVerifier(config mcpOAuthConfig) (*mcpJWTVerifier, mcpAuthorizationServerMetadata, error) {
	config.Issuer = strings.TrimSpace(config.Issuer)
	config.Audience = strings.TrimSpace(config.Audience)
	config.Resource = strings.TrimRight(strings.TrimSpace(config.Resource), "/")
	if config.Issuer == "" {
		return nil, mcpAuthorizationServerMetadata{}, fmt.Errorf("--oauth-issuer is required when OAuth is enabled")
	}
	if config.Audience == "" {
		return nil, mcpAuthorizationServerMetadata{}, fmt.Errorf("--oauth-audience is required when OAuth is enabled")
	}
	if config.Resource == "" {
		return nil, mcpAuthorizationServerMetadata{}, fmt.Errorf("--base-url is required when OAuth is enabled")
	}
	if config.Client == nil {
		config.Client = &http.Client{Timeout: 10 * time.Second}
	}

	metadata, err := discoverMCPAuthorizationServer(config.Client, config.Issuer)
	if err != nil {
		return nil, mcpAuthorizationServerMetadata{}, err
	}
	if metadata.Issuer != config.Issuer {
		return nil, mcpAuthorizationServerMetadata{}, fmt.Errorf("authorization server issuer mismatch")
	}
	if metadata.JWKSURI == "" {
		return nil, mcpAuthorizationServerMetadata{}, fmt.Errorf("authorization server metadata does not provide jwks_uri")
	}
	if err := validateMCPSecureURL(metadata.JWKSURI, "authorization server jwks_uri"); err != nil {
		return nil, mcpAuthorizationServerMetadata{}, err
	}
	verifier := &mcpJWTVerifier{
		issuer: config.Issuer, audience: config.Audience,
		jwksURI: metadata.JWKSURI, client: config.Client,
	}
	if err := verifier.refreshKeys(); err != nil {
		return nil, mcpAuthorizationServerMetadata{}, err
	}
	return verifier, metadata, nil
}

func discoverMCPAuthorizationServer(client *http.Client, issuer string) (mcpAuthorizationServerMetadata, error) {
	candidates, err := mcpDiscoveryURLs(issuer)
	if err != nil {
		return mcpAuthorizationServerMetadata{}, err
	}
	var lastErr error
	for _, candidate := range candidates {
		var metadata mcpAuthorizationServerMetadata
		if err := getMCPJSON(client, candidate, &metadata); err != nil {
			lastErr = err
			continue
		}
		return metadata, nil
	}
	return mcpAuthorizationServerMetadata{}, fmt.Errorf("OAuth authorization server discovery failed: %w", lastErr)
}

func mcpDiscoveryURLs(issuer string) ([]string, error) {
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid OAuth issuer URL")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackMCPHostname(parsed.Hostname())) {
		return nil, fmt.Errorf("OAuth issuer must use HTTPS unless it is loopback")
	}
	issuerPath := strings.Trim(parsed.Path, "/")
	rfc8414 := *parsed
	rfc8414.RawQuery = ""
	rfc8414.Fragment = ""
	rfc8414.RawPath = ""
	rfc8414.Path = "/.well-known/oauth-authorization-server"
	if issuerPath != "" {
		rfc8414.Path += "/" + issuerPath
	}
	oidc := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	return []string{rfc8414.String(), oidc}, nil
}

func validateMCPSecureURL(value, label string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("%s must be an absolute HTTP(S) URL", label)
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackMCPHostname(parsed.Hostname())) {
		return fmt.Errorf("%s must use HTTPS unless it is loopback", label)
	}
	return nil
}

func getMCPJSON(client *http.Client, endpoint string, target interface{}) error {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4*1024))
		return fmt.Errorf("%s returned HTTP %d", endpoint, response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1024*1024))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", endpoint, err)
	}
	return nil
}

type mcpJWTClaims struct {
	jwt.RegisteredClaims
	Scope    string   `json:"scope,omitempty"`
	SCP      []string `json:"scp,omitempty"`
	ClientID string   `json:"client_id,omitempty"`
}

type mcpJWTVerifier struct {
	issuer   string
	audience string
	jwksURI  string
	client   *http.Client

	mu   sync.RWMutex
	keys map[string]interface{}
}

func (v *mcpJWTVerifier) Verify(rawToken string) (*mcpTokenClaims, error) {
	claims := &mcpJWTClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(30*time.Second),
	)
	keyFunction := func(token *jwt.Token) (interface{}, error) {
		kid, _ := token.Header["kid"].(string)
		key, ok := v.lookupKey(kid)
		if !ok {
			if err := v.refreshKeys(); err != nil {
				return nil, err
			}
			key, ok = v.lookupKey(kid)
		}
		if !ok {
			return nil, fmt.Errorf("JWT signing key not found")
		}
		return key, nil
	}
	parsed, err := parser.ParseWithClaims(rawToken, claims, keyFunction)
	if err != nil || !parsed.Valid {
		return nil, fmt.Errorf("invalid access token")
	}
	scopes := append([]string(nil), claims.SCP...)
	scopes = append(scopes, strings.Fields(claims.Scope)...)
	sort.Strings(scopes)
	return &mcpTokenClaims{Subject: claims.Subject, ClientID: claims.ClientID, Scopes: scopes}, nil
}

func (v *mcpJWTVerifier) lookupKey(kid string) (interface{}, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if kid != "" {
		key, ok := v.keys[kid]
		return key, ok
	}
	if len(v.keys) == 1 {
		for _, key := range v.keys {
			return key, true
		}
	}
	return nil, false
}

func (v *mcpJWTVerifier) refreshKeys() error {
	var set struct {
		Keys []mcpJSONWebKey `json:"keys"`
	}
	if err := getMCPJSON(v.client, v.jwksURI, &set); err != nil {
		return fmt.Errorf("load OAuth JWKS: %w", err)
	}
	keys := make(map[string]interface{}, len(set.Keys))
	for _, jwk := range set.Keys {
		key, err := jwk.publicKey()
		if err != nil || jwk.Kid == "" {
			continue
		}
		keys[jwk.Kid] = key
	}
	if len(keys) == 0 {
		return fmt.Errorf("OAuth JWKS contains no supported signing keys")
	}
	v.mu.Lock()
	v.keys = keys
	v.mu.Unlock()
	return nil
}

type mcpJSONWebKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use,omitempty"`
	N   string `json:"n,omitempty"`
	E   string `json:"e,omitempty"`
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
}

func (j mcpJSONWebKey) publicKey() (interface{}, error) {
	if j.Use != "" && j.Use != "sig" {
		return nil, fmt.Errorf("JWK is not a signing key")
	}
	switch j.Kty {
	case "RSA":
		n, err := base64.RawURLEncoding.DecodeString(j.N)
		if err != nil {
			return nil, err
		}
		e, err := base64.RawURLEncoding.DecodeString(j.E)
		if err != nil || len(e) == 0 || len(e) > 4 {
			return nil, fmt.Errorf("invalid RSA exponent")
		}
		exponent := 0
		for _, value := range e {
			exponent = exponent<<8 + int(value)
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exponent}, nil
	case "EC":
		var curve elliptic.Curve
		switch j.Crv {
		case "P-256":
			curve = elliptic.P256()
		case "P-384":
			curve = elliptic.P384()
		case "P-521":
			curve = elliptic.P521()
		default:
			return nil, fmt.Errorf("unsupported EC curve")
		}
		x, err := base64.RawURLEncoding.DecodeString(j.X)
		if err != nil {
			return nil, err
		}
		y, err := base64.RawURLEncoding.DecodeString(j.Y)
		if err != nil {
			return nil, err
		}
		key := &ecdsa.PublicKey{Curve: curve, X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		if !curve.IsOnCurve(key.X, key.Y) {
			return nil, fmt.Errorf("invalid EC key")
		}
		return key, nil
	default:
		return nil, fmt.Errorf("unsupported JWK type")
	}
}

func mcpProtectedResourceMetadataHandler(metadata mcpProtectedResourceMetadata) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_ = json.NewEncoder(w).Encode(metadata)
	})
}
