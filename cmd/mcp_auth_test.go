package cmd

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type staticMCPTokenVerifier struct {
	claims *mcpTokenClaims
	err    error
}

func TestMCPJWTVerifierDiscoversJWKSAndValidatesClaims(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"issuer": issuer, "jwks_uri": strings.TrimRight(issuer, "/") + "/jwks"})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"keys": []interface{}{map[string]interface{}{
				"kty": "RSA", "kid": "test-key", "use": "sig",
				"n": base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	issuer = server.URL + "/"

	verifier, _, err := newMCPJWTVerifier(mcpOAuthConfig{
		Issuer: issuer, Audience: "volcengine-mcp", Resource: "https://mcp.example.com/mcp", Client: server.Client(),
	})
	if err != nil {
		t.Fatalf("newMCPJWTVerifier() error = %v", err)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, mcpJWTClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: issuer, Subject: "user-1", Audience: jwt.ClaimStrings{"volcengine-mcp"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)), IssuedAt: jwt.NewNumericDate(time.Now()),
		},
		Scope: "mcp:use profile",
	})
	token.Header["kid"] = "test-key"
	signed, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	claims, err := verifier.Verify(signed)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if claims.Subject != "user-1" || !hasMCPScopes(claims.Scopes, []string{"mcp:use"}) {
		t.Fatalf("unexpected claims: %#v", claims)
	}

	wrongAudience := jwt.NewWithClaims(jwt.SigningMethodRS256, mcpJWTClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: issuer, Subject: "user-1", Audience: jwt.ClaimStrings{"another-audience"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	wrongAudience.Header["kid"] = "test-key"
	wrongAudienceToken, err := wrongAudience.SignedString(privateKey)
	if err != nil {
		t.Fatalf("sign wrong-audience token: %v", err)
	}
	if _, err := verifier.Verify(wrongAudienceToken); err == nil {
		t.Fatal("expected a token with the wrong audience to be rejected")
	}
}

func (v staticMCPTokenVerifier) Verify(_ string) (*mcpTokenClaims, error) {
	return v.claims, v.err
}

func TestMCPOAuthMiddlewareRequiresBearerToken(t *testing.T) {
	handler := mcpOAuthMiddleware(staticMCPTokenVerifier{claims: &mcpTokenClaims{Subject: "user"}}, "https://mcp.example.com/.well-known/oauth-protected-resource", nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if challenge := recorder.Header().Get("WWW-Authenticate"); !strings.Contains(challenge, `resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"`) {
		t.Fatalf("unexpected challenge: %q", challenge)
	}
}

func TestMCPOAuthMiddlewareAcceptsVerifiedBearerToken(t *testing.T) {
	handler := mcpOAuthMiddleware(staticMCPTokenVerifier{claims: &mcpTokenClaims{Subject: "user", Scopes: []string{"mcp:use"}}}, "https://mcp.example.com/.well-known/oauth-protected-resource", []string{"mcp:use"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := mcpTokenClaimsFromContext(r.Context())
		if !ok || claims.Subject != "user" {
			t.Fatalf("claims missing from context: %#v", claims)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestMCPOAuthMiddlewareRejectsMissingScope(t *testing.T) {
	handler := mcpOAuthMiddleware(staticMCPTokenVerifier{claims: &mcpTokenClaims{Subject: "user", Scopes: []string{"profile"}}}, "https://mcp.example.com/.well-known/oauth-protected-resource", []string{"mcp:use"})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestMCPOAuthQuotedValueEscapesQuoteAndBackslash(t *testing.T) {
	if got, want := mcpOAuthQuotedValue(`a"b\c`), `a\"b\\c`; got != want {
		t.Fatalf("mcpOAuthQuotedValue() = %q, want %q", got, want)
	}
}

func TestMCPDiscoveryURLDoesNotDoubleEncodeIssuerPath(t *testing.T) {
	urls, err := mcpDiscoveryURLs("https://auth.example.com/tenant%20one")
	if err != nil {
		t.Fatalf("mcpDiscoveryURLs() error = %v", err)
	}
	want := "https://auth.example.com/.well-known/oauth-authorization-server/tenant%20one"
	if urls[0] != want {
		t.Fatalf("discovery URL = %q, want %q", urls[0], want)
	}
}
