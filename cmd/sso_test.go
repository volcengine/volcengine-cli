package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeOAuthClient struct {
	registerResp *RegisterClientResponse
	registerErr  error
	startResp    *StartDeviceAuthorizationResponse
	startErr     error
	refreshResp  *CreateTokenResponse
	refreshErr   error
	deviceResp   *CreateTokenResponse
	deviceErr    error

	registerRequests []RegisterClientRequest
	createRequests   []CreateTokenRequest
	startRequests    []StartDeviceAuthorizationRequest
}

func (f *fakeOAuthClient) RegisterClient(ctx context.Context, req *RegisterClientRequest) (*RegisterClientResponse, error) {
	f.registerRequests = append(f.registerRequests, *req)
	if f.registerErr != nil {
		return nil, f.registerErr
	}
	if f.registerResp != nil {
		return f.registerResp, nil
	}
	return &RegisterClientResponse{
		ClientID:              "registered-client",
		ClientSecret:          "registered-secret",
		ClientSecretExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	}, nil
}

func (f *fakeOAuthClient) CreateToken(ctx context.Context, req *CreateTokenRequest) (*CreateTokenResponse, error) {
	f.createRequests = append(f.createRequests, *req)
	switch req.GrantType {
	case "refresh_token":
		if f.refreshErr != nil {
			return nil, f.refreshErr
		}
		if f.refreshResp != nil {
			return f.refreshResp, nil
		}
		return &CreateTokenResponse{AccessToken: "refreshed-access", RefreshToken: req.RefreshToken, ExpiresIn: 3600}, nil
	case deviceCodeGrantType:
		if f.deviceErr != nil {
			return nil, f.deviceErr
		}
		if f.deviceResp != nil {
			return f.deviceResp, nil
		}
		return &CreateTokenResponse{AccessToken: "device-access", RefreshToken: "device-refresh", ExpiresIn: 3600}, nil
	default:
		return nil, errors.New("unexpected grant type")
	}
}

func (f *fakeOAuthClient) RevokeToken(ctx context.Context, req *RevokeTokenRequest) error {
	return nil
}

func (f *fakeOAuthClient) StartDeviceAuthorization(ctx context.Context, req *StartDeviceAuthorizationRequest) (*StartDeviceAuthorizationResponse, error) {
	f.startRequests = append(f.startRequests, *req)
	if f.startErr != nil {
		return nil, f.startErr
	}
	if f.startResp != nil {
		return f.startResp, nil
	}
	return &StartDeviceAuthorizationResponse{
		DeviceCode:              "device-code",
		UserCode:                "user-code",
		VerificationURIComplete: "https://example.com/verify?user_code=user-code",
		ExpiresIn:               60,
		Interval:                1,
	}, nil
}

type fakePortalClient struct {
	lastAccessToken string
	resp            *GetRoleCredentialsResponse
	err             error
}

func (f *fakePortalClient) ListAccounts(ctx context.Context, req *ListAccountsRequest) (*ListAccountsResponse, error) {
	return nil, errors.New("ListAccounts should not be called")
}

func (f *fakePortalClient) ListAccountRoles(ctx context.Context, req *ListAccountRolesRequest) (*ListAccountRolesResponse, error) {
	return nil, errors.New("ListAccountRoles should not be called")
}

func (f *fakePortalClient) GetRoleCredentials(ctx context.Context, req *GetRoleCredentialsRequest) (*GetRoleCredentialsResponse, error) {
	f.lastAccessToken = req.AccessToken
	if f.err != nil {
		return nil, f.err
	}
	if f.resp != nil {
		return f.resp, nil
	}
	return &GetRoleCredentialsResponse{
		RoleCredentials: RoleCredentials{
			AccessKeyID:     "ak",
			SecretAccessKey: "sk",
			SessionToken:    "session-token",
			Expiration:      time.Now().Add(time.Hour).Unix(),
		},
	}, nil
}

func setupSsoTokenTest(t *testing.T) *Sso {
	t.Helper()

	oldConfigDir := getSsoConfigFileDir
	oldOAuthFactory := newOAuthClientForSSO
	oldPortalFactory := newPortalClientForSSO
	oldSleep := deviceAuthorizationSleep

	cacheRoot := t.TempDir()
	getSsoConfigFileDir = func() (string, error) {
		return cacheRoot, nil
	}
	deviceAuthorizationSleep = func(time.Duration) {}
	t.Cleanup(func() {
		getSsoConfigFileDir = oldConfigDir
		newOAuthClientForSSO = oldOAuthFactory
		newPortalClientForSSO = oldPortalFactory
		deviceAuthorizationSleep = oldSleep
	})

	return &Sso{
		Profile: &Profile{
			AccountId: "account-id",
			RoleName:  "role-name",
		},
		SsoSessionName: "test-session",
		StartURL:       "https://example.com/userportal",
		Region:         "cn-beijing",
		UseDeviceCode:  true,
		NoBrowser:      true,
		Scopes:         []string{"cloudidentity:account:access", "offline_access"},
	}
}

func cacheTokenForTest(t *testing.T, sso *Sso, token *SsoTokenCache) {
	t.Helper()
	if token.StartURL == "" {
		token.StartURL = sso.StartURL
	}
	if token.SessionName == "" {
		token.SessionName = sso.SsoSessionName
	}
	if token.Region == "" {
		token.Region = sso.Region
	}
	if err := sso.setAccessTokenToCache(sso.StartURL, sso.SsoSessionName, token); err != nil {
		t.Fatalf("failed to cache token: %v", err)
	}
}

func validClientSecretExpiry() int64 {
	return time.Now().Add(time.Hour).UnixMilli()
}

func expiredClientSecretExpiry() int64 {
	return time.Now().Add(-time.Hour).UnixMilli()
}

func TestGetFreshTokenForLoginIgnoresCachedRefreshToken(t *testing.T) {
	sso := setupSsoTokenTest(t)
	cacheTokenForTest(t, sso, &SsoTokenCache{
		AccessToken:           "cached-access",
		RefreshToken:          "cached-refresh",
		ExpiresAt:             time.Now().Add(time.Hour).Format(time.RFC3339),
		ClientId:              "cached-client",
		ClientSecret:          "cached-secret",
		ClientSecretExpiresAt: validClientSecretExpiry(),
	})
	fakeOAuth := &fakeOAuthClient{
		deviceResp: &CreateTokenResponse{AccessToken: "fresh-login-access", RefreshToken: "fresh-login-refresh", ExpiresIn: 3600},
	}
	newOAuthClientForSSO = func(region string) OAuthClientAPI {
		return fakeOAuth
	}

	token, err := newDeviceCodeFetcher(sso).GetFreshTokenForLogin()
	if err != nil {
		t.Fatalf("GetFreshTokenForLogin() error = %v", err)
	}
	if token.AccessToken != "fresh-login-access" {
		t.Fatalf("access token = %q, want fresh-login-access", token.AccessToken)
	}
	if len(fakeOAuth.startRequests) != 1 {
		t.Fatalf("StartDeviceAuthorization calls = %d, want 1", len(fakeOAuth.startRequests))
	}
	for _, req := range fakeOAuth.createRequests {
		if req.GrantType == "refresh_token" {
			t.Fatalf("login must not use refresh_token grant, got request %#v", req)
		}
	}
}

func TestGetValidTokenForBusinessUsesCachedAccessTokenOutsideRefreshWindow(t *testing.T) {
	sso := setupSsoTokenTest(t)
	cacheTokenForTest(t, sso, &SsoTokenCache{
		AccessToken:           "cached-access",
		RefreshToken:          "cached-refresh",
		ExpiresAt:             time.Now().Add(30 * time.Minute).Format(time.RFC3339),
		ClientId:              "cached-client",
		ClientSecret:          "cached-secret",
		ClientSecretExpiresAt: validClientSecretExpiry(),
	})
	fakeOAuth := &fakeOAuthClient{}
	newOAuthClientForSSO = func(region string) OAuthClientAPI {
		return fakeOAuth
	}

	token, err := newDeviceCodeFetcher(sso).GetValidTokenForBusiness()
	if err != nil {
		t.Fatalf("GetValidTokenForBusiness() error = %v", err)
	}
	if token.AccessToken != "cached-access" {
		t.Fatalf("access token = %q, want cached-access", token.AccessToken)
	}
	if len(fakeOAuth.createRequests) != 0 || len(fakeOAuth.startRequests) != 0 {
		t.Fatalf("business path should reuse cached token without OAuth calls, create=%d start=%d", len(fakeOAuth.createRequests), len(fakeOAuth.startRequests))
	}
}

func TestGetValidTokenForBusinessRefreshesNearExpiryAndPreservesRefreshToken(t *testing.T) {
	sso := setupSsoTokenTest(t)
	cacheTokenForTest(t, sso, &SsoTokenCache{
		AccessToken:           "expiring-access",
		RefreshToken:          "old-refresh",
		ExpiresAt:             time.Now().Add(5 * time.Minute).Format(time.RFC3339),
		ClientId:              "cached-client",
		ClientSecret:          "cached-secret",
		ClientSecretExpiresAt: validClientSecretExpiry(),
	})
	fakeOAuth := &fakeOAuthClient{
		refreshResp: &CreateTokenResponse{AccessToken: "refreshed-access", ExpiresIn: 3600},
	}
	newOAuthClientForSSO = func(region string) OAuthClientAPI {
		return fakeOAuth
	}

	token, err := newDeviceCodeFetcher(sso).GetValidTokenForBusiness()
	if err != nil {
		t.Fatalf("GetValidTokenForBusiness() error = %v", err)
	}
	if token.AccessToken != "refreshed-access" {
		t.Fatalf("access token = %q, want refreshed-access", token.AccessToken)
	}
	if token.RefreshToken != "old-refresh" {
		t.Fatalf("refresh token = %q, want old-refresh", token.RefreshToken)
	}
	if len(fakeOAuth.createRequests) != 1 {
		t.Fatalf("CreateToken calls = %d, want 1", len(fakeOAuth.createRequests))
	}
	req := fakeOAuth.createRequests[0]
	if req.GrantType != "refresh_token" || req.RefreshToken != "old-refresh" {
		t.Fatalf("refresh request = %#v, want refresh_token with old-refresh", req)
	}
	if len(fakeOAuth.startRequests) != 0 {
		t.Fatalf("business refresh must not start device authorization")
	}
}

func TestClientFromTokenCacheRejectsExpiredClient(t *testing.T) {
	client := clientFromTokenCache(&SsoTokenCache{
		ClientId:              "cached-client",
		ClientSecret:          "cached-secret",
		ClientSecretExpiresAt: expiredClientSecretExpiry(),
	})
	if client != nil {
		t.Fatalf("clientFromTokenCache() = %#v, want nil for expired client", client)
	}
}

func TestLoadReusableClientDoesNotReturnExpiredClient(t *testing.T) {
	sso := setupSsoTokenTest(t)
	cacheTokenForTest(t, sso, &SsoTokenCache{
		ClientId:              "cached-client",
		ClientSecret:          "cached-secret",
		ClientSecretExpiresAt: expiredClientSecretExpiry(),
	})
	fakeOAuth := &fakeOAuthClient{}
	newOAuthClientForSSO = func(region string) OAuthClientAPI {
		return fakeOAuth
	}

	client, err := newDeviceCodeFetcher(sso).loadReusableClient(&SsoTokenCache{
		ClientId:              "cached-client",
		ClientSecret:          "cached-secret",
		ClientSecretExpiresAt: expiredClientSecretExpiry(),
	})
	if err != nil {
		t.Fatalf("loadReusableClient() error = %v", err)
	}
	if client != nil {
		t.Fatalf("loadReusableClient() = %#v, want nil when all cached clients are expired", client)
	}
}

func TestGetValidTokenForBusinessRequiresLoginWhenRefreshUnavailable(t *testing.T) {
	tests := []struct {
		name  string
		token *SsoTokenCache
		oauth *fakeOAuthClient
	}{
		{
			name: "missing refresh token",
			token: &SsoTokenCache{
				AccessToken:           "expired-access",
				ExpiresAt:             time.Now().Add(-time.Minute).Format(time.RFC3339),
				ClientId:              "cached-client",
				ClientSecret:          "cached-secret",
				ClientSecretExpiresAt: validClientSecretExpiry(),
			},
			oauth: &fakeOAuthClient{},
		},
		{
			name: "expired client registration",
			token: &SsoTokenCache{
				AccessToken:           "expired-access",
				RefreshToken:          "refresh-token",
				ExpiresAt:             time.Now().Add(-time.Minute).Format(time.RFC3339),
				ClientId:              "cached-client",
				ClientSecret:          "cached-secret",
				ClientSecretExpiresAt: expiredClientSecretExpiry(),
			},
			oauth: &fakeOAuthClient{},
		},
		{
			name: "refresh request failed",
			token: &SsoTokenCache{
				AccessToken:           "expired-access",
				RefreshToken:          "refresh-token",
				ExpiresAt:             time.Now().Add(-time.Minute).Format(time.RFC3339),
				ClientId:              "cached-client",
				ClientSecret:          "cached-secret",
				ClientSecretExpiresAt: validClientSecretExpiry(),
			},
			oauth: &fakeOAuthClient{refreshErr: errors.New("invalid refresh token")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sso := setupSsoTokenTest(t)
			cacheTokenForTest(t, sso, tt.token)
			newOAuthClientForSSO = func(region string) OAuthClientAPI {
				return tt.oauth
			}

			_, err := newDeviceCodeFetcher(sso).GetValidTokenForBusiness()
			if err == nil {
				t.Fatalf("GetValidTokenForBusiness() error = nil, want login guidance")
			}
			if !strings.Contains(err.Error(), "sso login") {
				t.Fatalf("error = %q, want sso login guidance", err.Error())
			}
			if len(tt.oauth.startRequests) != 0 {
				t.Fatalf("business refresh failure must not start device authorization")
			}
		})
	}
}

func TestGetRoleCredentialsRefreshesAccessTokenBeforeFetchingCredentials(t *testing.T) {
	sso := setupSsoTokenTest(t)
	cacheTokenForTest(t, sso, &SsoTokenCache{
		AccessToken:           "expiring-access",
		RefreshToken:          "refresh-token",
		ExpiresAt:             time.Now().Add(time.Minute).Format(time.RFC3339),
		ClientId:              "cached-client",
		ClientSecret:          "cached-secret",
		ClientSecretExpiresAt: validClientSecretExpiry(),
	})
	fakeOAuth := &fakeOAuthClient{
		refreshResp: &CreateTokenResponse{AccessToken: "refreshed-access", RefreshToken: "refresh-token", ExpiresIn: 3600},
	}
	fakePortal := &fakePortalClient{}
	newOAuthClientForSSO = func(region string) OAuthClientAPI {
		return fakeOAuth
	}
	newPortalClientForSSO = func(region string) PortalClientAPI {
		return fakePortal
	}

	credentials, err := sso.GetRoleCredentials()
	if err != nil {
		t.Fatalf("GetRoleCredentials() error = %v", err)
	}
	if credentials.AccessKeyID != "ak" {
		t.Fatalf("AccessKeyID = %q, want ak", credentials.AccessKeyID)
	}
	if fakePortal.lastAccessToken != "refreshed-access" {
		t.Fatalf("portal access token = %q, want refreshed-access", fakePortal.lastAccessToken)
	}
}
