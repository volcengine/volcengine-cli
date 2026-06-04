package cmd

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/manifoldco/promptui"
	"github.com/volcengine/volcengine-cli/util"
)

const ssoAccessTokenRefreshWindow = 5 * time.Minute

var (
	// getSsoConfigFileDir 是 SSO 缓存目录的注入点，生产环境固定使用 util.GetConfigFileDir。
	// 单测会替换为临时目录，避免读写真实用户目录下的 ~/.volcengine。
	getSsoConfigFileDir = util.GetConfigFileDir
	// newOAuthClientForSSO 集中创建 OAuth 客户端，便于业务刷新与登录流程复用同一套构造逻辑。
	newOAuthClientForSSO = func(region string) OAuthClientAPI {
		return NewOAuthClient(&OAuthClientConfig{Region: region})
	}
	// newPortalClientForSSO 集中创建 Portal 客户端，单测可替换后验证业务路径使用的 access token。
	newPortalClientForSSO = func(region string) PortalClientAPI {
		return NewPortalClient(&PortalClientConfig{Region: region})
	}
	// selectSsoAccount/selectSsoRole 是账号与角色交互选择的注入点，生产环境使用 promptui，
	// 单测替换为确定性选择，避免测试阻塞在真实终端交互上。
	selectSsoAccount = promptSelectAccount
	selectSsoRole    = promptSelectRole
	// deviceAuthorizationSleep 是设备码轮询等待的注入点，测试中会置空以避免真实等待。
	deviceAuthorizationSleep = time.Sleep
)

// Sso 持有 SSO 运行时所需的配置与状态。
type Sso struct {
	Profile        *Profile
	SsoSessionName string
	StartURL       string
	Region         string
	UseDeviceCode  bool
	NoBrowser      bool
	Scopes         []string
}

// SSOService 定义对外暴露的 SSO 操作接口。
type SSOService interface {
	SetProfile() error
	Login() error
	Logout() error
	GetAccessToken() (string, error)
	GetRoleCredentials() (*RoleCredentials, error)
}

// 编译期断言：确保 *Sso 实现了 SSOService 接口（缺方法会直接编译失败）。
var _ SSOService = (*Sso)(nil)

// 统一读取/校验配置中的 SSO session。
func (s *Sso) loadSsoSession(cfg *Configure) (*SsoSession, error) {
	if cfg == nil {
		return nil, fmt.Errorf("the configuration file cannot be loaded")
	}
	if strings.TrimSpace(s.SsoSessionName) == "" {
		return nil, fmt.Errorf("the SSO session must be specified")
	}
	session, exists := cfg.SsoSession[s.SsoSessionName]
	if !exists {
		return nil, fmt.Errorf("there is no SSO session named %s in the configuration file", s.SsoSessionName)
	}
	return session, nil
}

// 用 session 中的默认值补全当前 SSO 配置。
func (s *Sso) applySessionDefaults(session *SsoSession) {
	if session == nil {
		return
	}
	if strings.TrimSpace(s.StartURL) == "" {
		s.StartURL = session.StartURL
	}
	if strings.TrimSpace(s.Region) == "" {
		s.Region = session.Region
	}
	if len(s.Scopes) == 0 {
		s.Scopes = session.RegistrationScopes
	}
}

// EnsureValidStsToken 确保 SSO 模式下的 STS Token 有效（过期或缺失则刷新）。
func (s *Sso) EnsureValidStsToken(ctx *Context) error {
	if ctx == nil || ctx.config == nil {
		return fmt.Errorf("failed to refresh stsToken: failed to obtain the config in ctx")
	}
	if s == nil || s.Profile == nil {
		return fmt.Errorf("failed to refresh stsToken: profile is nil")
	}

	if s.SsoSessionName == "" {
		s.SsoSessionName = s.Profile.SsoSessionName
	}
	if s.Region == "" {
		s.Region = s.Profile.Region
	}

	stsToken := strings.TrimSpace(s.Profile.SessionToken)
	expiration := s.Profile.StsExpiration
	if stsToken != "" && expiration > 0 && time.Now().Before(util.UnixTimestampToTime(expiration)) {
		return nil
	}

	ssoSession, err := s.loadSsoSession(ctx.config)
	if err != nil {
		return err
	}
	s.applySessionDefaults(ssoSession)
	if strings.TrimSpace(s.StartURL) == "" {
		return fmt.Errorf("the start URL of SSO session %s is not configured", s.SsoSessionName)
	}

	roleCredentials, err := s.GetRoleCredentials()
	if err != nil {
		return fmt.Errorf("failed to get role credentials: %w", err)
	}

	s.Profile.AccessKey = roleCredentials.AccessKeyID
	s.Profile.SecretKey = roleCredentials.SecretAccessKey
	s.Profile.SessionToken = roleCredentials.SessionToken
	s.Profile.StsExpiration = roleCredentials.Expiration
	ctx.config.Profiles[s.Profile.Name] = s.Profile
	return WriteConfigToFile(ctx.config)
}

// SsoTokenCache 保存 SSO 访问令牌及客户端凭据的缓存结构。
type SsoTokenCache struct {
	StartURL              string `json:"start_url"`
	SessionName           string `json:"session_name"`
	AccessToken           string `json:"access_token"`
	ExpiresAt             string `json:"expires_at"`
	ClientId              string `json:"client_id"`
	ClientSecret          string `json:"client_secret"`
	ClientIdIssuedAt      int64  `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt int64  `json:"client_secret_expires_at,omitempty"`
	RefreshToken          string `json:"refresh_token,omitempty"`
	Region                string `json:"region"`
}

// DeviceCodeFetcher 负责基于设备码的 OAuth 授权流程。
type DeviceCodeFetcher struct {
	sso       *Sso
	oauth     OAuthClientAPI
	noBrowser bool
}

// clientRegistrationCache 用于缓存注册后的 OAuth 客户端信息。
type clientRegistrationCache struct {
	ClientName            string `json:"client_name"`
	ClientID              string `json:"client_id"`
	ClientSecret          string `json:"client_secret"`
	ClientIDIssuedAt      int64  `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt int64  `json:"client_secret_expires_at,omitempty"`
}

// 使用临时文件写入后原子替换，避免中断导致缓存损坏。
func writeJSONFileAtomic(path string, perm os.FileMode, payload interface{}) (retErr error) {
	dir := filepath.Dir(path)
	tempFile, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempName := tempFile.Name()
	defer func() {
		if retErr != nil {
			_ = tempFile.Close()
			_ = os.Remove(tempName)
		}
	}()

	if err := tempFile.Chmod(perm); err != nil {
		retErr = fmt.Errorf("failed to set cache file permissions: %w", err)
		return retErr
	}

	encoder := json.NewEncoder(tempFile)
	if err := encoder.Encode(payload); err != nil {
		retErr = fmt.Errorf("failed to write cache file: %w", err)
		return retErr
	}

	if err := tempFile.Close(); err != nil {
		retErr = fmt.Errorf("failed to close cache file: %w", err)
		return retErr
	}

	if err := os.Rename(tempName, path); err != nil {
		removeErr := os.Remove(path)
		if removeErr == nil || os.IsNotExist(removeErr) {
			if err2 := os.Rename(tempName, path); err2 == nil {
				return nil
			}
		}
		retErr = fmt.Errorf("failed to replace cache file: %w", err)
		return retErr
	}

	return nil
}

// tokenCacheFilePath 返回当前会话对应的 token 缓存文件路径。
func (s *Sso) tokenCacheFilePath() (string, error) {
	cacheDir, err := s.getSsoCacheDir()
	if err != nil {
		return "", err
	}
	fileName := s.generateCacheFileName(s.StartURL, s.SsoSessionName)
	return filepath.Join(cacheDir, fileName), nil
}

// readTokenCache 从磁盘读取 token 缓存；不存在时返回 nil。
func (s *Sso) readTokenCache() (*SsoTokenCache, error) {
	filePath, err := s.tokenCacheFilePath()
	if err != nil {
		return nil, err
	}

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to open the cache file: %v", err)
	}

	var token SsoTokenCache
	decodeErr := json.NewDecoder(file).Decode(&token)
	_ = file.Close()

	if decodeErr != nil {
		if errors.Is(decodeErr, io.EOF) {
			return nil, nil
		}
		// 缓存损坏时视为无缓存，并清理该文件。
		_ = os.Remove(filePath)
		return nil, nil
	}
	return &token, nil
}

// tokenExpired 判断 access token 是否过期或不可解析。
func tokenExpired(expiresAt string) bool {
	if expiresAt == "" {
		return true
	}
	expTime, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return true
	}
	return time.Now().After(expTime)
}

// tokenNeedsRefresh 判断 access token 是否需要刷新。
// 业务命令不会等到完全过期才刷新，而是在过期前窗口内提前静默续期。
func tokenNeedsRefresh(expiresAt string) bool {
	if expiresAt == "" {
		return true
	}
	expTime, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return true
	}
	return !time.Now().Add(ssoAccessTokenRefreshWindow).Before(expTime)
}

// clientSecretExpired 判断客户端密钥是否过期（0 表示无过期时间）。
func clientSecretExpired(expiresAt int64) bool {
	if expiresAt == 0 {
		return false
	}
	return time.Now().UnixMilli() >= expiresAt
}

// registrationClientCacheKey 基于关键字段生成客户端缓存键。
func (f *DeviceCodeFetcher) registrationClientCacheKey() (string, error) {
	keyPayload := struct {
		StartURL    string   `json:"start_url"`
		Region      string   `json:"region"`
		Scopes      []string `json:"scopes"`
		SessionName string   `json:"session_name"`
	}{
		StartURL:    f.sso.StartURL,
		Region:      f.sso.Region,
		Scopes:      f.sso.Scopes,
		SessionName: f.sso.SsoSessionName,
	}

	data, err := json.Marshal(keyPayload)
	if err != nil {
		return "", fmt.Errorf("failed to build registration cache key: %w", err)
	}
	sum := sha1.Sum(data)
	return fmt.Sprintf("%x", sum), nil
}

// registrationClientCachePath 返回注册客户端缓存文件路径。
func (f *DeviceCodeFetcher) registrationClientCachePath() (string, error) {
	cacheDir, err := f.sso.getSsoCacheDir()
	if err != nil {
		return "", err
	}
	key, err := f.registrationClientCacheKey()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, key+".json"), nil
}

// loadClientRegistration 从缓存加载客户端注册信息。
func (f *DeviceCodeFetcher) loadClientRegistration() (*RegisterClientResponse, error) {
	filePath, err := f.registrationClientCachePath()
	if err != nil {
		return nil, err
	}

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to open client cache file: %v", err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			fmt.Printf("failed to close the client cache file: %v", err)
		}
	}(file)

	var cached clientRegistrationCache
	if err := json.NewDecoder(file).Decode(&cached); err != nil {
		return nil, fmt.Errorf("failed to read the client cache: %v", err)
	}
	if cached.ClientID == "" || cached.ClientSecret == "" {
		return nil, nil
	}

	return &RegisterClientResponse{
		ClientID:              cached.ClientID,
		ClientSecret:          cached.ClientSecret,
		ClientIDIssuedAt:      cached.ClientIDIssuedAt,
		ClientSecretExpiresAt: cached.ClientSecretExpiresAt,
	}, nil
}

// cacheClientRegistration 将客户端注册信息写入缓存文件。
func (f *DeviceCodeFetcher) cacheClientRegistration(client *RegisterClientResponse, clientName string) error {
	if client == nil || client.ClientID == "" || client.ClientSecret == "" {
		return fmt.Errorf("client registration is empty")
	}
	cacheDir, err := f.sso.getSsoCacheDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return fmt.Errorf("failed to create the cache directory: %v", err)
	}
	_ = os.Chmod(cacheDir, 0700)
	filePath, err := f.registrationClientCachePath()
	if err != nil {
		return err
	}

	cache := clientRegistrationCache{
		ClientName:            clientName,
		ClientID:              client.ClientID,
		ClientSecret:          client.ClientSecret,
		ClientIDIssuedAt:      client.ClientIDIssuedAt,
		ClientSecretExpiresAt: client.ClientSecretExpiresAt,
	}

	return writeJSONFileAtomic(filePath, 0600, cache)
}

// newDeviceCodeFetcher 构建 DeviceCodeFetcher，并注入 OAuth 客户端。
func newDeviceCodeFetcher(s *Sso) *DeviceCodeFetcher {
	return &DeviceCodeFetcher{
		sso:       s,
		oauth:     newOAuthClientForSSO(s.Region),
		noBrowser: s.NoBrowser,
	}
}

// loadCachedToken 读取 SSO token 缓存。
func (f *DeviceCodeFetcher) loadCachedToken() (*SsoTokenCache, error) {
	return f.sso.readTokenCache()
}

// persistClientCredentials 将客户端凭据写入 token 缓存。
func (f *DeviceCodeFetcher) persistClientCredentials(client *RegisterClientResponse, cached *SsoTokenCache) error {
	if client == nil {
		return fmt.Errorf("client registration is empty")
	}
	token := cached
	if token == nil {
		token = &SsoTokenCache{
			StartURL:    f.sso.StartURL,
			SessionName: f.sso.SsoSessionName,
			Region:      f.sso.Region,
		}
	}
	token.ClientId = client.ClientID
	token.ClientSecret = client.ClientSecret
	token.ClientIdIssuedAt = client.ClientIDIssuedAt
	token.ClientSecretExpiresAt = client.ClientSecretExpiresAt
	return f.sso.setAccessTokenToCache(f.sso.StartURL, f.sso.SsoSessionName, token)
}

// registerClient 注册 OAuth 客户端并写入缓存。
func (f *DeviceCodeFetcher) registerClient(ctx context.Context, cached *SsoTokenCache) (*RegisterClientResponse, error) {
	clientName := fmt.Sprintf("volcengine-cli-%s", uuid.NewString())
	resp, err := f.oauth.RegisterClient(ctx, &RegisterClientRequest{
		ClientName: clientName,
		ClientType: "public",
		GrantTypes: []string{deviceCodeGrantType, "refresh_token"},
		Scopes:     f.sso.Scopes,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to register client: %w", err)
	}
	if err := f.cacheClientRegistration(resp, clientName); err != nil {
		return nil, fmt.Errorf("failed to persist client registration: %w", err)
	}
	if err := f.persistClientCredentials(resp, cached); err != nil {
		return nil, fmt.Errorf("failed to cache client credentials: %w", err)
	}
	return resp, nil
}

// clientFromTokenCache 从 token 缓存中还原 OAuth client 注册信息。
// 业务静默刷新只能依赖已有注册信息；这里不创建新 client，避免业务命令产生交互式登录副作用。
func clientFromTokenCache(cached *SsoTokenCache) *RegisterClientResponse {
	if cached == nil || strings.TrimSpace(cached.ClientId) == "" || strings.TrimSpace(cached.ClientSecret) == "" {
		return nil
	}
	if clientSecretExpired(cached.ClientSecretExpiresAt) {
		return nil
	}
	return &RegisterClientResponse{
		ClientID:              cached.ClientId,
		ClientSecret:          cached.ClientSecret,
		ClientIDIssuedAt:      cached.ClientIdIssuedAt,
		ClientSecretExpiresAt: cached.ClientSecretExpiresAt,
	}
}

// loadReusableClient 优先读取独立 client 注册缓存，缺失或过期时再回退到 token 缓存里的 client 信息。
func (f *DeviceCodeFetcher) loadReusableClient(cached *SsoTokenCache) (*RegisterClientResponse, error) {
	client, err := f.loadClientRegistration()
	if err != nil {
		return nil, err
	}
	if client != nil && client.ClientID != "" && client.ClientSecret != "" && !clientSecretExpired(client.ClientSecretExpiresAt) {
		return client, nil
	}
	if cachedClient := clientFromTokenCache(cached); cachedClient != nil {
		return cachedClient, nil
	}
	return nil, nil
}

// ensureClientForInteractiveAuth 返回可用于显式授权流程的 OAuth client。
// 登录/配置命令允许在 client 缺失或过期时重新注册，但不会用 refresh_token 代替用户授权。
func (f *DeviceCodeFetcher) ensureClientForInteractiveAuth(ctx context.Context, cached *SsoTokenCache) (*RegisterClientResponse, error) {
	client, err := f.loadReusableClient(cached)
	if err != nil {
		return nil, err
	}
	if client == nil || client.ClientID == "" || client.ClientSecret == "" || clientSecretExpired(client.ClientSecretExpiresAt) {
		return f.registerClient(ctx, cached)
	}
	if err := f.persistClientCredentials(client, cached); err != nil {
		return nil, err
	}
	return client, nil
}

// loadClientForRefresh 返回业务静默刷新可用的 OAuth client。
// 这里故意不自动注册新 client：注册/授权属于登录语义，业务命令只做 refresh，失败则要求用户显式登录。
func (f *DeviceCodeFetcher) loadClientForRefresh(cached *SsoTokenCache) (*RegisterClientResponse, error) {
	client, err := f.loadReusableClient(cached)
	if err != nil {
		return nil, err
	}
	if client == nil || client.ClientID == "" || client.ClientSecret == "" {
		return nil, fmt.Errorf("SSO access token cannot be refreshed because client credentials are missing; please log in using the `sso login` command")
	}
	if clientSecretExpired(client.ClientSecretExpiresAt) {
		return nil, fmt.Errorf("SSO access token cannot be refreshed because client registration has expired; please log in using the `sso login` command")
	}
	return client, nil
}

// storeToken 将获取的 token 组装为缓存对象并写入磁盘。
func (f *DeviceCodeFetcher) storeToken(resp *CreateTokenResponse, client *RegisterClientResponse) (*SsoTokenCache, error) {
	if client == nil {
		return nil, fmt.Errorf("client registration is required to store token")
	}
	expiresAt := time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second).Format(time.RFC3339)
	token := &SsoTokenCache{
		StartURL:              f.sso.StartURL,
		SessionName:           f.sso.SsoSessionName,
		AccessToken:           resp.AccessToken,
		RefreshToken:          resp.RefreshToken,
		ExpiresAt:             expiresAt,
		ClientId:              client.ClientID,
		ClientSecret:          client.ClientSecret,
		ClientIdIssuedAt:      client.ClientIDIssuedAt,
		ClientSecretExpiresAt: client.ClientSecretExpiresAt,
		Region:                f.sso.Region,
	}
	if err := f.sso.setAccessTokenToCache(f.sso.StartURL, f.sso.SsoSessionName, token); err != nil {
		return nil, err
	}
	return token, nil
}

func (f *DeviceCodeFetcher) createToken(ctx context.Context, grantType string, refreshToken string, deviceCode string, client *RegisterClientResponse) (*CreateTokenResponse, error) {
	if client == nil {
		return nil, fmt.Errorf("client registration is required to create token")
	}
	req := &CreateTokenRequest{
		GrantType:    grantType,
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
	}
	if refreshToken != "" {
		req.RefreshToken = refreshToken
	}
	if deviceCode != "" {
		req.DeviceCode = deviceCode
	}
	return f.oauth.CreateToken(ctx, req)
}

// refreshToken 使用 refresh_token 换取新的 access token。
func (f *DeviceCodeFetcher) refreshToken(ctx context.Context, refreshToken string, client *RegisterClientResponse) (*SsoTokenCache, error) {
	if client == nil {
		return nil, fmt.Errorf("client registration is required to refresh token")
	}
	resp, err := f.createToken(ctx, "refresh_token", refreshToken, "", client)
	if err != nil {
		return nil, err
	}
	// 有些 OAuth 服务会在刷新 access token 时轮换 refresh token。
	// 只有当服务端未返回新的 refresh_token 时，才沿用旧值，避免把新 token 覆盖掉导致下一次静默刷新失败。
	if resp.RefreshToken == "" {
		resp.RefreshToken = refreshToken
	}
	return f.storeToken(resp, client)
}

func oauthErrorCode(err error) (string, bool) {
	var apiErr *OAuthAPIError
	if !errors.As(err, &apiErr) {
		return "", false
	}
	return apiErr.Response.Error, true
}

type createTokenErrorAction struct {
	Retry                bool
	ReRegister           bool
	FallbackToDeviceAuth bool
	Message              string
}

func classifyCreateTokenError(err error) (createTokenErrorAction, bool) {
	code, ok := oauthErrorCode(err)
	if !ok {
		return createTokenErrorAction{}, false
	}
	switch code {
	case "authorization_pending":
		return createTokenErrorAction{Retry: true}, true
	case "invalid_device_code", "expired_token":
		return createTokenErrorAction{Message: "device code is invalid or expired; please retry login"}, true
	case "invalid_token":
		return createTokenErrorAction{
			FallbackToDeviceAuth: true,
			Message:              "token is invalid; please retry login",
		}, true
	case "invalid_request":
		return createTokenErrorAction{Message: "token request parameters are invalid"}, true
	case "invalid_client":
		return createTokenErrorAction{
			ReRegister: true,
			Message:    "client registration is invalid; please retry login",
		}, true
	case "unsupported_grant_type":
		return createTokenErrorAction{Message: "token grant type is not supported"}, true
	case "server_error":
		return createTokenErrorAction{Message: "server error while requesting token"}, true
	default:
		return createTokenErrorAction{Message: fmt.Sprintf("unknown error: %s", code)}, false
	}
}

// performDeviceAuthorization 发起设备码授权流程并轮询获取 token。
func (f *DeviceCodeFetcher) performDeviceAuthorization(ctx context.Context, client *RegisterClientResponse) (*SsoTokenCache, error) {
	if client == nil {
		return nil, fmt.Errorf("client registration is required to start device authorization")
	}

	authResp, err := f.oauth.StartDeviceAuthorization(ctx, &StartDeviceAuthorizationRequest{
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
		Scopes:       f.sso.Scopes,
		PortalUrl:    f.sso.StartURL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start device authorization: %w", err)
	}

	verificationURIComplete := authResp.VerificationURIComplete
	if verificationURIComplete == "" && authResp.VerificationURI != "" && authResp.UserCode != "" {
		verificationURIComplete = fmt.Sprintf("%s?user_code=%s", authResp.VerificationURI, authResp.UserCode)
	}

	if verificationURIComplete == "" {
		return nil, fmt.Errorf("failed to start device authorization: verificationURI is empty")
	}

	if f.noBrowser {
		if verificationURIComplete != "" {
			fmt.Printf("To authorize, open the following URL in your browser:\n\n%s\n", verificationURIComplete)
		}
	} else {
		if verificationURIComplete != "" {
			fmt.Printf("Attempting to open your default browser.\n")
			fmt.Printf("If the browser does not open or you want to authorize from another device, open the following URL:\n\n%s\n", verificationURIComplete)
			if err := util.OpenBrowser(verificationURIComplete); err != nil {
				fmt.Printf("Failed to open the browser automatically: %v\n", err)
			}
		}
	}

	interval := time.Duration(authResp.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	expiresIn := time.Duration(authResp.ExpiresIn) * time.Second
	deadline := time.Now().Add(expiresIn)

	fmt.Printf("Please complete authorization promptly to avoid timeout. This device code expires in %d seconds.\n", authResp.ExpiresIn)

	// 轮询直到授权完成或设备码过期。
	for time.Now().Before(deadline) {
		deviceAuthorizationSleep(interval)

		tokenResp, err := f.createToken(ctx, deviceCodeGrantType, "", authResp.DeviceCode, client)
		if err != nil {
			if action, ok := classifyCreateTokenError(err); ok {
				if action.Retry {
					continue
				}
				if action.Message != "" {
					return nil, errors.New(action.Message)
				}
			}
			return nil, fmt.Errorf("failed to poll access token: %w", err)
		}

		return f.storeToken(tokenResp, client)
	}

	return nil, fmt.Errorf("authorization has timed out. Please try again")
}

// GetToken 协调设备码流程、refresh token 刷新及缓存复用。
// 该方法保留给 configure sso 等交互式流程使用：它可以复用缓存、尝试 refresh，并在必要时回退到设备码授权。
func (f *DeviceCodeFetcher) GetToken() (*SsoTokenCache, error) {
	ctx := context.Background()

	cached, err := f.loadCachedToken()
	if err != nil {
		return nil, err
	}
	if cached != nil && cached.AccessToken != "" && !tokenExpired(cached.ExpiresAt) {
		return cached, nil
	}

	client, err := f.ensureClientForInteractiveAuth(ctx, cached)
	if err != nil {
		return nil, err
	}

	if cached != nil && cached.RefreshToken != "" {
		token, err := f.refreshToken(ctx, cached.RefreshToken, client)
		if err == nil {
			return token, nil
		}
		if action, ok := classifyCreateTokenError(err); ok {
			if action.ReRegister {
				client, err = f.registerClient(ctx, cached)
				if err != nil {
					return nil, err
				}
				return f.performDeviceAuthorization(ctx, client)
			}
			if action.FallbackToDeviceAuth {
				return f.performDeviceAuthorization(ctx, client)
			}
			if action.Message != "" {
				return nil, errors.New(action.Message)
			}
		}
		return nil, err
	}

	return f.performDeviceAuthorization(ctx, client)
}

// GetFreshTokenForLogin 执行显式登录授权。
// 无论缓存 access token 是否有效，也不会用 refresh_token 静默完成登录。
func (f *DeviceCodeFetcher) GetFreshTokenForLogin() (*SsoTokenCache, error) {
	ctx := context.Background()
	cached, err := f.loadCachedToken()
	if err != nil {
		return nil, err
	}
	client, err := f.ensureClientForInteractiveAuth(ctx, cached)
	if err != nil {
		return nil, err
	}
	return f.performDeviceAuthorization(ctx, client)
}

// GetValidTokenForBusiness 返回业务命令可用的 access token 缓存。
// 业务命令只允许静默 refresh，不允许回退到设备码授权，避免普通 API 调用突然打开浏览器或阻塞等待用户授权。
func (f *DeviceCodeFetcher) GetValidTokenForBusiness() (*SsoTokenCache, error) {
	ctx := context.Background()
	cached, err := f.loadCachedToken()
	if err != nil {
		return nil, err
	}
	if cached == nil || strings.TrimSpace(cached.AccessToken) == "" {
		return nil, fmt.Errorf("no cached access token found; please log in using the `sso login` command")
	}
	if !tokenNeedsRefresh(cached.ExpiresAt) {
		return cached, nil
	}
	if strings.TrimSpace(cached.RefreshToken) == "" {
		return nil, fmt.Errorf("SSO access token cannot be refreshed because refresh token is missing; please log in using the `sso login` command")
	}
	client, err := f.loadClientForRefresh(cached)
	if err != nil {
		return nil, err
	}
	token, err := f.refreshToken(ctx, cached.RefreshToken, client)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh SSO access token; please log in using the `sso login` command: %w", err)
	}
	return token, nil
}

// SetProfile 通过 SSO 登录并写入配置文件。
func (s *Sso) SetProfile() error {
	if !s.UseDeviceCode {
		return fmt.Errorf("currently, only device code authentication is supported")
	}

	fetcher := newDeviceCodeFetcher(s)
	token, err := fetcher.GetToken()
	if err != nil {
		return fmt.Errorf("failed to obtain the access token: %v", err)
	}

	accountId, roleName, err := s.chooseAccountAndRole(token)
	if err != nil {
		return fmt.Errorf("failed to select the account and role: %v", err)
	}

	s.Profile.Mode = ModeSSO
	s.Profile.SsoSessionName = s.SsoSessionName
	s.Profile.AccountId = accountId
	s.Profile.RoleName = roleName
	s.Profile.Region = s.Region
	// 重新选择 SSO 账号或角色后，旧 STS 临时凭证已经不再可信。
	// 如果不清空，后续业务命令会在过期前继续复用旧身份，导致配置已变更但调用仍落到旧账号/角色。
	clearSsoProfileTemporaryCredentials(s.Profile)
	s.Profile.DisableSSL = new(bool)
	*s.Profile.DisableSSL = false
	if s.Profile.Name == "" {
		s.Profile.Name = fmt.Sprintf("%s-%s", roleName, accountId)
	}

	cfg := ctx.config
	if cfg == nil {
		cfg = &Configure{
			Profiles: make(map[string]*Profile),
		}
	}

	cfg.Profiles[s.Profile.Name] = s.Profile

	if err := WriteConfigToFile(cfg); err != nil {
		return err
	}
	fmt.Printf("SSO profile [%s] has been configured successfully\n", s.Profile.Name)
	return nil
}

// setAccessTokenToCache 将 token 缓存写入到指定会话文件。
func (s *Sso) setAccessTokenToCache(startURL, sessionName string, token *SsoTokenCache) error {
	cacheDir, err := s.getSsoCacheDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return fmt.Errorf("failed to create the cache directory: %v", err)
	}
	_ = os.Chmod(cacheDir, 0700)

	fileName := s.generateCacheFileName(startURL, sessionName)
	filePath := filepath.Join(cacheDir, fileName)

	return writeJSONFileAtomic(filePath, 0600, token)
}

// chooseAccountAndRole 交互式选择账号与角色。
func (s *Sso) chooseAccountAndRole(token *SsoTokenCache) (string, string, error) {
	if token == nil || strings.TrimSpace(token.AccessToken) == "" {
		return "", "", fmt.Errorf("access token is empty, please login again")
	}

	var client PortalClientAPI = newPortalClientForSSO(s.Region)
	ctx := context.Background()

	accounts, err := s.fetchAllAccounts(ctx, client, token.AccessToken)
	if err != nil {
		return "", "", err
	}
	if len(accounts) == 0 {
		return "", "", fmt.Errorf("no available accounts found for the current user")
	}

	account, err := selectSsoAccount(accounts)
	if err != nil {
		return "", "", err
	}

	roles, err := s.fetchAllRoles(ctx, client, token.AccessToken, account.AccountID)
	if err != nil {
		return "", "", err
	}
	if len(roles) == 0 {
		return "", "", fmt.Errorf("no roles available under account %s", account.AccountID)
	}

	role, err := selectSsoRole(roles)
	if err != nil {
		return "", "", err
	}

	return account.AccountID, role.RoleName, nil
}

// GetRoleCredentials 获取当前 profile 对应角色的临时凭证。
func (s *Sso) GetRoleCredentials() (*RoleCredentials, error) {
	accessToken, err := s.GetValidAccessToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	var client PortalClientAPI = newPortalClientForSSO(s.Region)
	ctx := context.Background()
	resp, err := client.GetRoleCredentials(ctx, &GetRoleCredentialsRequest{
		AccessToken: accessToken,
		AccountID:   s.Profile.AccountId,
		RoleName:    s.Profile.RoleName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get role credentials: %w", err)
	}

	return &resp.RoleCredentials, nil
}

// fetchAllAccounts 拉取全部账号（分页遍历）。
func (s *Sso) fetchAllAccounts(ctx context.Context, client PortalClientAPI, accessToken string) ([]AccountInfo, error) {
	var (
		accounts  []AccountInfo
		nextToken string
	)

	for {
		resp, err := client.ListAccounts(ctx, &ListAccountsRequest{
			AccessToken: accessToken,
			NextToken:   nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list accounts: %w", err)
		}
		accounts = append(accounts, resp.AccountList...)
		if strings.TrimSpace(resp.NextToken) == "" {
			break
		}
		nextToken = resp.NextToken
	}
	return accounts, nil
}

// fetchAllRoles 拉取指定账号下全部角色（分页遍历）。
func (s *Sso) fetchAllRoles(ctx context.Context, client PortalClientAPI, accessToken, accountID string) ([]RoleInfo, error) {
	var (
		roles     []RoleInfo
		nextToken string
	)

	for {
		resp, err := client.ListAccountRoles(ctx, &ListAccountRolesRequest{
			AccessToken: accessToken,
			AccountID:   accountID,
			NextToken:   nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list roles for account %s: %w", accountID, err)
		}
		roles = append(roles, resp.RoleList...)
		if strings.TrimSpace(resp.NextToken) == "" {
			break
		}
		nextToken = resp.NextToken
	}
	return roles, nil
}

// promptSelectAccount 提供可搜索的账号选择界面。
func promptSelectAccount(accounts []AccountInfo) (AccountInfo, error) {
	searcher := func(input string, index int) bool {
		if index < 0 || index >= len(accounts) {
			return false
		}
		target := accounts[index]
		content := strings.ToLower(target.AccountName + " " + target.AccountID)
		input = strings.TrimSpace(strings.ToLower(input))
		if input == "" {
			return true
		}
		return strings.Contains(content, input)
	}

	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "> {{ .AccountName | cyan }} ({{ .AccountID | faint }})",
		Inactive: "  {{ .AccountName | faint }} ({{ .AccountID | faint }})",
		Selected: "[*] {{ .AccountName }} ({{ .AccountID }})",
		Details: `
--------- Account ----------
Name:   {{ .AccountName }}
ID:     {{ .AccountID }}`,
	}

	sel := promptui.Select{
		Label:             "Select account (type to filter, Enter to choose)",
		Items:             accounts,
		Templates:         templates,
		Searcher:          searcher,
		StartInSearchMode: true,
		Size:              10,
	}

	idx, _, err := sel.Run()
	if err != nil {
		return AccountInfo{}, err
	}
	return accounts[idx], nil
}

// promptSelectRole 提供可搜索的角色选择界面。
func promptSelectRole(roles []RoleInfo) (RoleInfo, error) {
	searcher := func(input string, index int) bool {
		if index < 0 || index >= len(roles) {
			return false
		}
		target := roles[index]
		content := strings.ToLower(target.RoleName + " " + target.AccountID)
		input = strings.TrimSpace(strings.ToLower(input))
		if input == "" {
			return true
		}
		return strings.Contains(content, input)
	}

	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "> {{ .RoleName | cyan }} ({{ .AccountID | faint }})",
		Inactive: "  {{ .RoleName | faint }} ({{ .AccountID | faint }})",
		Selected: "[*] {{ .RoleName }} ({{ .AccountID }})",
		Details: `
--------- Role ----------
Name:    {{ .RoleName }}
Account: {{ .AccountID }}`,
	}

	sel := promptui.Select{
		Label:             "Select role (type to filter, Enter to choose)",
		Items:             roles,
		Templates:         templates,
		Searcher:          searcher,
		StartInSearchMode: true,
		Size:              10,
	}

	idx, _, err := sel.Run()
	if err != nil {
		return RoleInfo{}, err
	}
	return roles[idx], nil
}

// getSsoCacheDir 返回 SSO 缓存目录。
func (s *Sso) getSsoCacheDir() (string, error) {
	configDir, err := getSsoConfigFileDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(configDir, "sso", "cache"), nil
}

// generateCacheFileName 生成缓存文件名（哈希化 startURL + sessionName）。
func (s *Sso) generateCacheFileName(startURL, sessionName string) string {
	payload := struct {
		StartURL    string `json:"start_url"`
		SessionName string `json:"session_name"`
	}{
		StartURL:    startURL,
		SessionName: sessionName,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte(startURL + "\n" + sessionName)
	}
	hash := sha1.Sum(data)
	return fmt.Sprintf("%x.json", hash)
}

// GetAccessToken 从缓存获取有效的 access token。
func (s *Sso) GetAccessToken() (string, error) {
	tokenCache, err := s.readTokenCache()
	if err != nil {
		return "", fmt.Errorf("failed to read access token cache: %w", err)
	}
	if tokenCache == nil || strings.TrimSpace(tokenCache.AccessToken) == "" {
		return "", fmt.Errorf("no cached access token found; please log in using the `sso login` command")
	}

	expTime, err := time.Parse(time.RFC3339, tokenCache.ExpiresAt)
	if err != nil {
		return "", fmt.Errorf("failed to parse access token expiry: %w", err)
	}
	if time.Now().After(expTime) {
		return "", fmt.Errorf("your access token has expired. Please log in again using the `sso login` command")
	}

	return tokenCache.AccessToken, nil
}

// GetValidAccessToken 获取业务命令可用的 access token。
// access token 未进入刷新窗口时直接复用；过期或即将过期时仅尝试 refresh_token 静默续期。
func (s *Sso) GetValidAccessToken() (string, error) {
	fetcher := newDeviceCodeFetcher(s)
	tokenCache, err := fetcher.GetValidTokenForBusiness()
	if err != nil {
		return "", err
	}
	return tokenCache.AccessToken, nil
}

// Login 执行 SSO 登录并写入缓存。
func (s *Sso) Login() error {
	if !s.UseDeviceCode {
		return fmt.Errorf("currently, only device code authentication is supported")
	}
	if strings.TrimSpace(s.SsoSessionName) == "" {
		return fmt.Errorf("the SSO information is incomplete. Please configure the profile first")
	}

	config := ctx.config
	ssoSession, err := s.loadSsoSession(config)
	if err != nil {
		return err
	}

	s.applySessionDefaults(ssoSession)

	if strings.TrimSpace(s.StartURL) == "" {
		return fmt.Errorf("the start URL of SSO session %s is not configured", s.SsoSessionName)
	}
	if strings.TrimSpace(s.Region) == "" {
		return fmt.Errorf("the SSO information is incomplete. Please configure the profile first")
	}

	fetcher := newDeviceCodeFetcher(s)
	if _, err := fetcher.GetFreshTokenForLogin(); err != nil {
		return fmt.Errorf("failed to obtain the access token: %v", err)
	}
	return nil
}

// Logout 撤销缓存 token 并清理本地凭据。
func (s *Sso) Logout() error {
	cfg := ctx.config
	ssoSession, err := s.loadSsoSession(cfg)
	if err != nil {
		return err
	}
	s.applySessionDefaults(ssoSession)
	if strings.TrimSpace(s.StartURL) == "" {
		return fmt.Errorf("the sign-in URL of SSO session %s is not configured", s.SsoSessionName)
	}

	tokenCache, err := s.readTokenCache()
	if err != nil {
		return err
	}

	if tokenCache == nil {
		// 没有本地 token 缓存，仍需清理 profile 中的临时凭据。
		return s.clearProfileStsCredentials(cfg)
	}

	if err := s.revokeCachedToken(tokenCache); err != nil {
		return err
	}

	if err := s.clearCachedToken(tokenCache); err != nil {
		return err
	}

	if err := s.clearProfileStsCredentials(cfg); err != nil {
		return err
	}

	return nil
}

// revokeCachedToken 仅撤销 refresh token；access token 无需 revoke。
func (s *Sso) revokeCachedToken(tokenCache *SsoTokenCache) error {
	if tokenCache == nil {
		return fmt.Errorf("token cache is empty")
	}
	clientID := strings.TrimSpace(tokenCache.ClientId)
	clientSecret := strings.TrimSpace(tokenCache.ClientSecret)
	if clientID == "" || clientSecret == "" {
		return fmt.Errorf("client credentials are missing in the cache, please login first")
	}

	token := strings.TrimSpace(tokenCache.RefreshToken)
	if token == "" {
		return nil
	}

	var oauthClient OAuthClientAPI = NewOAuthClient(&OAuthClientConfig{Region: s.Region})
	return oauthClient.RevokeToken(context.Background(), &RevokeTokenRequest{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Token:        token,
	})
}

// clearCachedToken 删除 token 缓存文件。
func (s *Sso) clearCachedToken(tokenCache *SsoTokenCache) error {
	if tokenCache == nil {
		return fmt.Errorf("token cache is empty")
	}
	filePath, err := s.tokenCacheFilePath()
	if err != nil {
		return err
	}
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove token cache file: %v", err)
	}
	return nil
}

// clearProfileStsCredentials 清理当前会话相关 profile 的临时凭据。
func (s *Sso) clearProfileStsCredentials(cfg *Configure) error {
	if cfg == nil {
		return fmt.Errorf("the configuration file cannot be loaded")
	}
	updated := false
	for name, profile := range cfg.Profiles {
		if profile == nil || profile.Mode != ModeSSO || profile.SsoSessionName != s.SsoSessionName {
			continue
		}
		clearSsoProfileTemporaryCredentials(profile)
		cfg.Profiles[name] = profile
		updated = true
	}
	if !updated {
		return nil
	}
	return WriteConfigToFile(cfg)
}

// clearSsoProfileTemporaryCredentials 仅清理 SSO profile 中可重新换取的 STS 临时凭据。
//
// AccountId 与 RoleName 是用户在 configure sso 阶段选择并写入的长期绑定信息，
// 后续业务命令刷新 STS 时还需要它们调用 GetRoleCredentials。logout 若清掉这两个字段，
// 再次执行普通业务命令会因缺少 accountId/roleName 无法换取新的 STS。
func clearSsoProfileTemporaryCredentials(profile *Profile) {
	if profile == nil {
		return
	}

	profile.AccessKey = ""
	profile.SecretKey = ""
	profile.SessionToken = ""
	profile.StsExpiration = 0
}
