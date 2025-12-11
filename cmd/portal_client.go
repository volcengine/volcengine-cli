package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultPortalRegion 为默认区域标识（当前未启用拼接区域模板的逻辑）。
	defaultPortalRegion = "cn-beijing"
	// defaultPortalTimeout 为默认 HTTP 超时，避免请求长期阻塞。
	defaultPortalTimeout = 30 * time.Second
	// defaultPortalPageSize 为分页查询的默认页大小。
	defaultPortalPageSize = 50
	// portalBaseURLTemplate 为 Portal 服务基础地址模板（已固定为 BOE 环境）。
	// portalBaseURLTemplate     = "https://cloudidentity-portal.%s.volces.com"
	portalBaseURLTemplate = "https://cloudidentity-portal-boe.bytedance.net"
	// Portal API 的路径常量。
	portalListAccountsPath   = "/assignment/accounts"
	portalListAccountRoles   = "/assignment/roles"
	portalGetRoleCredentials = "/federation/credentials"
	// portalAccessTokenHeader 为 Portal 访问令牌在请求头中的名称。
	portalAccessTokenHeader = "x-bd-cloudidentity-bearer-token"
	// portalContentTypeJSON 为 JSON 的 Content-Type 常量（当前请求为 GET 未用到）。
	portalContentTypeJSON = "application/json"
	// portalDefaultAcceptHeader 为默认 Accept 头，要求返回 JSON。
	portalDefaultAcceptHeader = "application/json"
)

// PortalClientConfig 用于配置 Portal 客户端的可选项，比如自定义 BaseURL、HTTPClient 或分页大小。
type PortalClientConfig struct {
	Region          string       // 区域标识（当前逻辑未使用）
	BaseURL         string       // 自定义 Portal 基础地址
	HTTPClient      *http.Client // 自定义 HTTP 客户端（可注入超时、代理等）
	DefaultPageSize int          // 默认分页大小
}

// PortalClient 封装 CloudIdentity Portal API 调用，集中管理 URL、HTTP 客户端和默认分页参数。
type PortalClient struct {
	baseURL            string
	listAccountsURL    string
	listRolesURL       string
	roleCredentialsURL string
	httpClient         *http.Client
	defaultPageSize    int
}

// PortalClientAPI 定义 Portal 客户端对外暴露的方法集合，便于测试或替换实现。
type PortalClientAPI interface {
	ListAccounts(ctx context.Context, req *ListAccountsRequest) (*ListAccountsResponse, error)
	ListAccountRoles(ctx context.Context, req *ListAccountRolesRequest) (*ListAccountRolesResponse, error)
	GetRoleCredentials(ctx context.Context, req *GetRoleCredentialsRequest) (*GetRoleCredentialsResponse, error)
}

// 编译期断言：确保 *PortalClient 实现了 PortalClientAPI 接口（缺方法会直接编译失败）。
var _ PortalClientAPI = (*PortalClient)(nil)

// ResponseMetadata 表示 Portal API 返回的基础元信息。
type ResponseMetadata struct {
	RequestID string `json:"RequestId"`
}

// PortalAPIError 用于承载 Portal API 非 2xx 响应时的结构化错误信息。
type PortalAPIError struct {
	StatusCode int
	RequestID  string
	Message    string
	RawBody    string
}

func (e *PortalAPIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" && e.RequestID != "" {
		return fmt.Sprintf("portal API request failed: %s [status %d, requestId=%s]", e.Message, e.StatusCode, e.RequestID)
	}
	if e.Message != "" {
		return fmt.Sprintf("portal API request failed: %s [status %d]", e.Message, e.StatusCode)
	}
	if e.RequestID != "" {
		return fmt.Sprintf("portal API request failed with status %d (requestId=%s)", e.StatusCode, e.RequestID)
	}
	return fmt.Sprintf("portal API request failed with status %d", e.StatusCode)
}

// AccountInfo 表示 ListAccounts 返回的账号信息。
type AccountInfo struct {
	AccountID   string `json:"AccountId"`
	AccountName string `json:"AccountName"`
}

// ListAccountsRequest 为 ListAccounts 的请求参数封装。
type ListAccountsRequest struct {
	AccessToken string
	PageSize    int
	PageNumber  int
	NextToken   string
}

// ListAccountsResponse 返回账号列表及分页信息。
type ListAccountsResponse struct {
	Total       int
	PageNumber  int
	PageSize    int
	AccountList []AccountInfo
	NextToken   string
	RequestID   string
}

// RoleInfo 表示 ListAccountRoles 返回的角色信息。
type RoleInfo struct {
	AccountID string `json:"AccountId"`
	RoleName  string `json:"RoleName"`
}

// ListAccountRolesRequest 为 ListAccountRoles 的请求参数封装。
type ListAccountRolesRequest struct {
	AccessToken string
	AccountID   string
	PageSize    int
	PageNumber  int
	NextToken   string
}

// ListAccountRolesResponse 返回角色列表及分页信息。
type ListAccountRolesResponse struct {
	Total      int
	PageNumber int
	PageSize   int
	RoleList   []RoleInfo
	NextToken  string
	RequestID  string
}

// RoleCredentials 表示 GetRoleCredentials 返回的临时凭证信息。
type RoleCredentials struct {
	AccessKeyID     string `json:"AccessKeyId"`
	Expiration      int64  `json:"Expiration"`
	SecretAccessKey string `json:"SecretAccessKey"`
	SessionToken    string `json:"sessionToken"`
}

// GetRoleCredentialsRequest 为 GetRoleCredentials 的请求参数封装。
type GetRoleCredentialsRequest struct {
	AccessToken string
	AccountID   string
	RoleName    string
	PageSize    int
	PageNumber  int
}

// GetRoleCredentialsResponse 返回临时凭证及请求 ID。
type GetRoleCredentialsResponse struct {
	RoleCredentials RoleCredentials
	RequestID       string
}

// NewPortalClient 根据配置创建 PortalClient，包含默认值和可选覆盖项。
func NewPortalClient(cfg *PortalClientConfig) *PortalClient {
	/*region := defaultPortalRegion
	if cfg != nil && strings.TrimSpace(cfg.Region) != "" {
		region = strings.TrimSpace(cfg.Region)
	}*/

	//base := fmt.Sprintf(portalBaseURLTemplate, region)
	base := portalBaseURLTemplate
	if cfg != nil && strings.TrimSpace(cfg.BaseURL) != "" {
		base = strings.TrimRight(cfg.BaseURL, "/")
	}
	base = strings.TrimRight(base, "/")

	client := &http.Client{Timeout: defaultPortalTimeout}
	if cfg != nil && cfg.HTTPClient != nil {
		client = cfg.HTTPClient
	}

	pageSize := defaultPortalPageSize
	if cfg != nil && cfg.DefaultPageSize > 0 {
		pageSize = cfg.DefaultPageSize
	}

	return &PortalClient{
		baseURL:            base,
		listAccountsURL:    base + portalListAccountsPath,
		listRolesURL:       base + portalListAccountRoles,
		roleCredentialsURL: base + portalGetRoleCredentials,
		httpClient:         client,
		defaultPageSize:    pageSize,
	}
}

// ListAccounts 调用 ListAccounts API，返回当前访问令牌可见的账号列表。
func (c *PortalClient) ListAccounts(ctx context.Context, req *ListAccountsRequest) (*ListAccountsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}
	token := strings.TrimSpace(req.AccessToken)
	if token == "" {
		return nil, fmt.Errorf("access token is required")
	}

	pageNumber, err := resolvePageNumber(req.PageNumber, req.NextToken)
	if err != nil {
		return nil, err
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = c.defaultPageSize
	}

	q := url.Values{}
	q.Set("page_size", strconv.Itoa(pageSize))
	q.Set("page_number", strconv.Itoa(pageNumber))
	endpoint := c.listAccountsURL + "?" + q.Encode()

	body, err := c.doPortalGet(ctx, token, endpoint)
	if err != nil {
		return nil, err
	}

	env, err := decodePortalEnvelope(body, "ListAccounts")
	if err != nil {
		return nil, err
	}

	var result listAccountsResult
	if len(env.Result) == 0 {
		return nil, fmt.Errorf("ListAccounts succeeded but response was empty")
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to decode ListAccounts result: %w", err)
	}

	nextToken := computeNextToken(result.Total, result.PageNumber, result.PageSize)
	return &ListAccountsResponse{
		Total:       result.Total,
		PageNumber:  result.PageNumber,
		PageSize:    result.PageSize,
		AccountList: result.AccountList,
		NextToken:   nextToken,
		RequestID:   env.ResponseMetadata.RequestID,
	}, nil
}

// ListAccountRoles 调用 ListAccountRoles API，返回指定账号下当前令牌可用的角色列表。
func (c *PortalClient) ListAccountRoles(ctx context.Context, req *ListAccountRolesRequest) (*ListAccountRolesResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}
	token := strings.TrimSpace(req.AccessToken)
	if token == "" {
		return nil, fmt.Errorf("access token is required")
	}
	if strings.TrimSpace(req.AccountID) == "" {
		return nil, fmt.Errorf("accountId is required")
	}

	pageNumber, err := resolvePageNumber(req.PageNumber, req.NextToken)
	if err != nil {
		return nil, err
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = c.defaultPageSize
	}

	q := url.Values{}
	q.Set("account_id", req.AccountID)
	q.Set("page_size", strconv.Itoa(pageSize))
	q.Set("page_number", strconv.Itoa(pageNumber))
	endpoint := c.listRolesURL + "?" + q.Encode()

	body, err := c.doPortalGet(ctx, token, endpoint)
	if err != nil {
		return nil, err
	}

	env, err := decodePortalEnvelope(body, "ListAccountRoles")
	if err != nil {
		return nil, err
	}

	var result listAccountRolesResult
	if len(env.Result) == 0 {
		return nil, fmt.Errorf("ListAccountRoles succeeded but response was empty")
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to decode ListAccountRoles result: %w", err)
	}

	nextToken := computeNextToken(result.Total, result.PageNumber, result.PageSize)
	return &ListAccountRolesResponse{
		Total:      result.Total,
		PageNumber: result.PageNumber,
		PageSize:   result.PageSize,
		RoleList:   result.RoleList,
		NextToken:  nextToken,
		RequestID:  env.ResponseMetadata.RequestID,
	}, nil
}

// GetRoleCredentials 使用 Portal 访问令牌换取指定账号和角色的临时凭证。
func (c *PortalClient) GetRoleCredentials(ctx context.Context, req *GetRoleCredentialsRequest) (*GetRoleCredentialsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}
	token := strings.TrimSpace(req.AccessToken)
	if token == "" {
		return nil, fmt.Errorf("access token is required")
	}
	if strings.TrimSpace(req.AccountID) == "" {
		return nil, fmt.Errorf("accountId is required")
	}
	if strings.TrimSpace(req.RoleName) == "" {
		return nil, fmt.Errorf("roleName is required")
	}

	q := url.Values{}

	q.Set("account_id", req.AccountID)
	q.Set("role_name", req.RoleName)
	endpoint := c.roleCredentialsURL + "?" + q.Encode()

	body, err := c.doPortalGet(ctx, token, endpoint)
	if err != nil {
		return nil, err
	}

	env, err := decodePortalEnvelope(body, "GetRoleCredentials")
	if err != nil {
		return nil, err
	}

	var result getRoleCredentialsAPIResult
	if len(env.Result) == 0 {
		return nil, fmt.Errorf("GetRoleCredentials succeeded but response was empty")
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to decode GetRoleCredentials result: %w", err)
	}

	return &GetRoleCredentialsResponse{
		RoleCredentials: result.RoleCredentials,
		RequestID:       env.ResponseMetadata.RequestID,
	}, nil
}

// doPortalGet 封装 Portal GET 请求：构造请求头、发起请求并处理非 2xx 错误。
func (c *PortalClient) doPortalGet(ctx context.Context, token string, fullURL string) ([]byte, error) {
	var result []byte
	err := doWithRetry(ctx, retryOptions{maxAttempts: 3}, func() error {
		body, err := c.doPortalGetOnce(ctx, token, fullURL)
		if err != nil {
			return err
		}
		result = body
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *PortalClient) doPortalGetOnce(ctx context.Context, token string, fullURL string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Accept", portalDefaultAcceptHeader)
	req.Header.Set(portalAccessTokenHeader, token)
	req.Header.Set("x-tt-env", "boe_ci_cli")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode/100 != 2 {
		return nil, parsePortalAPIError(resp.StatusCode, body)
	}

	return body, nil
}

// parsePortalAPIError 解析非 2xx 响应，尽量从 ResponseMetadata 中提取结构化错误信息。
func parsePortalAPIError(statusCode int, body []byte) error {
	var parsed portalErrorResponse
	if len(body) > 0 {
		_ = json.Unmarshal(body, &parsed)
	}

	if apiErr := portalErrorFromMetadata(statusCode, parsed.ResponseMetadata, body); apiErr != nil {
		return apiErr
	}
	msg := strings.TrimSpace(string(body))
	return &PortalAPIError{
		StatusCode: statusCode,
		RequestID:  parsed.ResponseMetadata.RequestID,
		Message:    msg,
		RawBody:    string(body),
	}
}

// resolvePageNumber 根据显式 PageNumber 或 NextToken 推导实际页码。
func resolvePageNumber(pageNumber int, nextToken string) (int, error) {
	if pageNumber > 0 {
		return pageNumber, nil
	}
	if strings.TrimSpace(nextToken) == "" {
		return 1, nil
	}
	page, err := strconv.Atoi(strings.TrimSpace(nextToken))
	if err != nil || page < 1 {
		return 0, fmt.Errorf("invalid NextToken %q: expect a positive integer page number", nextToken)
	}
	return page, nil
}

// computeNextToken 根据总数、页号、页大小计算下一页的 token（空字符串表示无下一页）。
func computeNextToken(total, pageNumber, pageSize int) string {
	if pageSize <= 0 || pageNumber <= 0 {
		return ""
	}
	if total > pageNumber*pageSize {
		return strconv.Itoa(pageNumber + 1)
	}
	return ""
}

// listAccountsResult 为 ListAccounts 的内部解包结构。
type listAccountsResult struct {
	Total       int           `json:"Total"`
	PageNumber  int           `json:"PageNumber"`
	PageSize    int           `json:"PageSize"`
	AccountList []AccountInfo `json:"AccountList"`
}

// listAccountRolesResult 为 ListAccountRoles 的内部解包结构。
type listAccountRolesResult struct {
	Total      int        `json:"Total"`
	PageNumber int        `json:"PageNumber"`
	PageSize   int        `json:"PageSize"`
	RoleList   []RoleInfo `json:"RoleList"`
}

// getRoleCredentialsAPIResult 为 GetRoleCredentials 的内部解包结构。
type getRoleCredentialsAPIResult struct {
	RoleCredentials RoleCredentials `json:"RoleCredentials"`
}

// portalErrorResponse 对应 Portal 错误响应的最外层结构。
type portalErrorResponse struct {
	ResponseMetadata portalResponseMetadata `json:"ResponseMetadata"`
}

// portalResponseError 表示 Portal API 的错误字段。
type portalResponseError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

// portalResponseMetadata 表示 Portal API 返回的元信息字段。
type portalResponseMetadata struct {
	RequestID string               `json:"RequestId"`
	Action    string               `json:"Action"`
	Service   string               `json:"Service"`
	Region    string               `json:"Region"`
	Error     *portalResponseError `json:"Error"`
}

// portalEnvelope 用于统一解析包含 ResponseMetadata 与 Result 的响应体。
type portalEnvelope struct {
	ResponseMetadata portalResponseMetadata `json:"ResponseMetadata"`
	Result           json.RawMessage        `json:"Result"`
}

// decodePortalEnvelope 解包响应体并做基础错误检查。
func decodePortalEnvelope(body []byte, action string) (*portalEnvelope, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("%s succeeded but response was empty", action)
	}
	var env portalEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("failed to decode %s response: %w", action, err)
	}
	if apiErr := portalErrorFromMetadata(http.StatusOK, env.ResponseMetadata, body); apiErr != nil {
		return nil, apiErr
	}
	return &env, nil
}

// portalErrorFromMetadata 将 ResponseMetadata 中的 Error 转换为 PortalAPIError。
func portalErrorFromMetadata(statusCode int, meta portalResponseMetadata, body []byte) error {
	if meta.Error == nil {
		return nil
	}
	msg := strings.TrimSpace(meta.Error.Message)
	code := strings.TrimSpace(meta.Error.Code)
	if code != "" && msg != "" {
		msg = fmt.Sprintf("%s: %s", code, msg)
	} else if code != "" && msg == "" {
		msg = code
	}
	return &PortalAPIError{
		StatusCode: statusCode,
		RequestID:  meta.RequestID,
		Message:    msg,
		RawBody:    string(body),
	}
}
