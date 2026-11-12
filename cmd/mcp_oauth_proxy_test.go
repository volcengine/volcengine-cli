package cmd

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeOAuthUpstream is a GitHub-like OAuth application: opaque access
// tokens, a form-encoded token request with the client secret, and a
// userinfo endpoint.
func fakeOAuthUpstream(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = r.ParseForm()
			if r.PostForm.Get("client_id") != "upstream-client" || r.PostForm.Get("client_secret") != "upstream-secret" || r.PostForm.Get("code") != "upstream-code" ||
				!strings.HasSuffix(r.PostForm.Get("redirect_uri"), mcpOAuthProxyCallbackPath) || r.PostForm.Get("code_verifier") != "" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"error":"bad_verification_code"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"gho_opaque","token_type":"bearer","scope":"read:user"}`))
		case "/user":
			if r.Header.Get("Authorization") != "Bearer gho_opaque" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"login":"octocat","id":1}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestMCPOAuthProxyFlow(t *testing.T) {
	upstream := fakeOAuthUpstream(t)
	defer upstream.Close()
	proxy, err := newMCPOAuthProxy(mcpOAuthUpstreamOptions{
		AuthorizeURL: upstream.URL + "/authorize?allow_signup=false", TokenURL: upstream.URL + "/token", UserinfoURL: upstream.URL + "/user",
		ClientID: "upstream-client", ClientSecret: "upstream-secret", Scopes: []string{"read:user"},
	}, "http://127.0.0.1:8888", upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	proxy.register(mux, "")
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	server := httptest.NewServer(mux)
	defer server.Close()

	// Discovery and dynamic registration.
	response, err := http.Get(server.URL + "/.well-known/oauth-authorization-server")
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]interface{}
	_ = json.NewDecoder(response.Body).Decode(&metadata)
	response.Body.Close()
	if metadata["issuer"] != "http://127.0.0.1:8888" || metadata["registration_endpoint"] != "http://127.0.0.1:8888/auth/oauth/register" || response.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("metadata = %v", metadata)
	}
	clientRedirect := "http://localhost:6274/oauth/callback"
	register := func(redirect string) int {
		body, _ := json.Marshal(map[string]interface{}{"redirect_uris": []string{redirect}, "client_name": "inspector"})
		response, err := http.Post(server.URL+mcpOAuthProxyRegisterPath, "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}
	if register(clientRedirect) != http.StatusCreated || register("https://evil.example.com/cb") != http.StatusBadRequest {
		t.Fatal("registration does not enforce allowed redirect URIs")
	}

	// Authorize: the client is sent to the upstream with the proxy callback.
	verifier := "a-verifier-with-enough-entropy-0123456789"
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	query := url.Values{"response_type": {"code"}, "client_id": {mcpOAuthProxyClientID}, "redirect_uri": {clientRedirect},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {"client-state"}}
	response, err = noRedirect.Get(server.URL + mcpOAuthProxyAuthorizePath + "?" + query.Encode())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	location, _ := url.Parse(response.Header.Get("Location"))
	if response.StatusCode != http.StatusFound || !strings.HasPrefix(location.String(), upstream.URL+"/authorize") ||
		location.Query().Get("client_id") != "upstream-client" || location.Query().Get("redirect_uri") != "http://127.0.0.1:8888/auth/oauth/callback" ||
		location.Query().Get("scope") != "read:user" || location.Query().Get("allow_signup") != "false" || location.Query().Get("code_challenge") != "" {
		t.Fatalf("authorize redirect = %d %s", response.StatusCode, location)
	}
	query.Set("redirect_uri", "https://evil.example.com/cb")
	if response, _ = noRedirect.Get(server.URL + mcpOAuthProxyAuthorizePath + "?" + query.Encode()); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("authorize with a foreign redirect = %d", response.StatusCode)
	}

	// Callback: the upstream code is sealed and the client state kept.
	upstreamState := location.Query().Get("state")
	response, err = noRedirect.Get(server.URL + mcpOAuthProxyCallbackPath + "?" + url.Values{"code": {"upstream-code"}, "state": {upstreamState}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	back, _ := url.Parse(response.Header.Get("Location"))
	if !strings.HasPrefix(back.String(), clientRedirect) || back.Query().Get("state") != "client-state" || back.Query().Get("code") == "" || back.Query().Get("code") == "upstream-code" {
		t.Fatalf("callback redirect = %s", back)
	}
	if response, _ = noRedirect.Get(server.URL + mcpOAuthProxyCallbackPath + "?code=x&state=forged"); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("callback with a forged state = %d", response.StatusCode)
	}

	// Token: PKCE is checked by the proxy, the secret is added upstream.
	exchange := func(code, codeVerifier string) (int, map[string]interface{}) {
		form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {clientRedirect}, "code_verifier": {codeVerifier}, "client_id": {mcpOAuthProxyClientID}}
		response, err := http.PostForm(server.URL+mcpOAuthProxyTokenPath, form)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var body map[string]interface{}
		_ = json.NewDecoder(response.Body).Decode(&body)
		return response.StatusCode, body
	}
	if status, body := exchange(back.Query().Get("code"), "wrong-verifier"); status != http.StatusBadRequest || body["error"] != "invalid_grant" {
		t.Fatalf("wrong verifier = %d %v", status, body)
	}
	status, token := exchange(back.Query().Get("code"), verifier)
	if status != http.StatusOK || token["access_token"] != "gho_opaque" || token["token_type"] != "Bearer" || token["scope"] != "read:user" {
		t.Fatalf("token = %d %v", status, token)
	}

	// The opaque token is validated through userinfo.
	claims, err := proxy.verifier.Verify("gho_opaque")
	if err != nil || claims.Subject != "1" {
		t.Fatalf("Verify() = %+v, %v", claims, err)
	}
	if _, err := proxy.verifier.Verify("gho_revoked"); err == nil {
		t.Fatal("expected a token the upstream rejects to be invalid")
	}
}

func TestMCPOAuthProxyRequiresUpstreamSettings(t *testing.T) {
	_, err := newMCPOAuthProxy(mcpOAuthUpstreamOptions{AuthorizeURL: "https://github.com/login/oauth/authorize"}, "http://127.0.0.1:8888", nil)
	if err == nil || !strings.Contains(err.Error(), "--oauth-upstream-token-url") {
		t.Fatalf("error = %v", err)
	}
}
