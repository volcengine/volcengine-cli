package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"github.com/volcengine/volcengine-go-sdk/volcengine/client"
	"github.com/volcengine/volcengine-go-sdk/volcengine/client/metadata"
	"github.com/volcengine/volcengine-go-sdk/volcengine/credentials"
	"github.com/volcengine/volcengine-go-sdk/volcengine/credentials/clicreds"
	"github.com/volcengine/volcengine-go-sdk/volcengine/defaults"
	"github.com/volcengine/volcengine-go-sdk/volcengine/endpoints"
	"github.com/volcengine/volcengine-go-sdk/volcengine/request"
	"github.com/volcengine/volcengine-go-sdk/volcengine/session"
	"github.com/volcengine/volcengine-go-sdk/volcengine/signer/volc"
	"github.com/volcengine/volcengine-go-sdk/volcengine/volcenginequery"
)

type SdkClient struct {
	Config      *volcengine.Config
	Session     *session.Session
	DebugLogger *DebugLogger
}

type SdkClientInfo struct {
	ServiceName string
	Action      string
	Version     string
	Method      string
	ContentType string
	// Headers are custom HTTP headers from --header Name=Value (excluding Content-Type,
	// which is applied via ContentType with JSON charset normalization).
	Headers []requestHeader
}

// selectInvocationProfile 解析本次调用使用的 profile。
// 优先级：---profile > config.Current > 环境变量（VOLCENGINE_PROFILE / VOLCSTACK_PROFILE）。
// 注意：
//   - Current 为空且无 env 时不会回落到某个“默认 profile”，profile 为 nil，走默认凭证链；
//   - ---profile 指定了不存在的名字时返回 error；
//   - c.config 为 nil 时忽略 profile 相关 flag（与历史 NewSimpleClient 行为一致）。
// 返回值 name/source 供 debug 日志使用；profile 为 nil 表示未选中有效 profile。
func selectInvocationProfile(c *Context) (name, source string, profile *Profile, err error) {
	source = "default-chain"
	if c == nil || c.config == nil {
		return "", source, nil, nil
	}
	name, source = defaultProfileNameWithSource(c.config)
	override := false
	if c.fixedFlags != nil {
		if f := c.fixedFlags.GetByName("profile"); f != nil {
			if v := strings.TrimSpace(f.GetValue()); v != "" {
				name = v
				source = "flag"
				override = true
			}
		}
	}
	if name != "" {
		profile = c.config.Profiles[name]
	}
	if override && profile == nil {
		return name, source, nil, fmt.Errorf("profile %q not found", name)
	}
	return name, source, profile, nil
}

// validateProfileIfSpecified 在指定了 ---profile 时校验该 profile 是否存在。
// 规则与 NewSimpleClient 一致；force 预检在创建 client 之前调用，以便尽早给出明确错误。
func validateProfileIfSpecified(c *Context) error {
	_, _, _, err := selectInvocationProfile(c)
	return err
}

// explicitEndpointFlag 读取 ---endpoint：去空白后非空则返回，否则返回 ""（表示未设置）。
func explicitEndpointFlag(c *Context) string {
	if c == nil || c.fixedFlags == nil {
		return ""
	}
	f := c.fixedFlags.GetByName("endpoint")
	if f == nil {
		return ""
	}
	return strings.TrimSpace(f.GetValue())
}

// isAutoAddressingEndpoint 判断 endpoint 是否为 auto-addressing 特殊值（大小写不敏感）。
// 该值表示走 SDK standard resolver，而不是固定 host。
func isAutoAddressingEndpoint(s string) bool {
	return strings.ToLower(strings.TrimSpace(s)) == "auto-addressing"
}

// endpointSourcesFromProfileAndEnv 读取 ---endpoint 覆盖之前的 host/resolver 来源。
// 有 profile 时：profile 字段优先，字段为空再回落 VOLCENGINE_ENDPOINT / VOLCENGINE_ENDPOINT_RESOLVER；
// 无 profile 时：直接读环境变量。
func endpointSourcesFromProfileAndEnv(profile *Profile) (endpoint, resolver string) {
	if profile != nil {
		endpoint = strings.TrimSpace(profile.Endpoint)
		if endpoint == "" {
			endpoint = strings.TrimSpace(os.Getenv("VOLCENGINE_ENDPOINT"))
		}
		resolver = strings.TrimSpace(profile.EndpointResolver)
		if resolver == "" {
			resolver = strings.TrimSpace(os.Getenv("VOLCENGINE_ENDPOINT_RESOLVER"))
		}
		return endpoint, resolver
	}
	return strings.TrimSpace(os.Getenv("VOLCENGINE_ENDPOINT")),
		strings.TrimSpace(os.Getenv("VOLCENGINE_ENDPOINT_RESOLVER"))
}

// resolveClientEndpoint 按 NewSimpleClient 完整优先级解析最终 host/resolver：
//
//	显式 ---endpoint（非空时清空 resolver）
//	> profile/env 的 resolver + host
//	> （由调用方再交给 classifyEndpoint / SDK 默认解析）
func resolveClientEndpoint(c *Context, profile *Profile) (endpoint, resolver string) {
	endpoint, resolver = endpointSourcesFromProfileAndEnv(profile)
	if ep := explicitEndpointFlag(c); ep != "" {
		return ep, ""
	}
	return endpoint, resolver
}

// endpointMode 表示 resolveClientEndpoint 结果在 SDK 上的落地方式。
// NewSimpleClient 应用配置与 force 未收录 service 的固定 host 预检共用此分类。
type endpointMode int

const (
	// endpointModeSDKDefault：无固定 host、无 standard resolver，交给 SDK 按 service+region 解析。
	endpointModeSDKDefault endpointMode = iota
	// endpointModeStandardResolver：使用 standard endpoint resolver（含 auto-addressing）。
	endpointModeStandardResolver
	// endpointModeFixedHost：使用显式固定 host（WithEndpoint）。
	endpointModeFixedHost
)

// classifyEndpoint 解释 resolveClientEndpoint 得到的 host+resolver，输出唯一落地模式。
// 规则：
//   - resolver=standard → standard（忽略 host）；
//   - host 为空 → SDK 默认；
//   - host 为 auto-addressing → standard；
//   - 其它非空 host → 固定 host。
// 必须同时被 NewSimpleClient 与 hasEffectiveFixedEndpoint 使用，避免双源漂移。
func classifyEndpoint(endpoint, resolver string) endpointMode {
	if strings.ToLower(strings.TrimSpace(resolver)) == "standard" {
		return endpointModeStandardResolver
	}
	if strings.TrimSpace(endpoint) == "" {
		return endpointModeSDKDefault
	}
	if isAutoAddressingEndpoint(endpoint) {
		return endpointModeStandardResolver
	}
	return endpointModeFixedHost
}

// hasEffectiveFixedEndpoint 判断按 NewSimpleClient 规则最终是否会落到固定 host。
// 供 force 未收录 service 预检使用；内部依赖 classifyEndpoint，不得再写第二套判断。
// 若 profile 选择报错则返回 false（validateForceCall 会先校验 profile 并返回明确错误）。
func hasEffectiveFixedEndpoint(c *Context) bool {
	_, _, profile, err := selectInvocationProfile(c)
	if err != nil {
		// validateForceCall 会先校验 profile；其它调用方视为“无固定 host”。
		return false
	}
	endpoint, resolver := resolveClientEndpoint(c, profile)
	return classifyEndpoint(endpoint, resolver) == endpointModeFixedHost
}

// NewSimpleClient creates an SDK client with credential resolution:
//  1. If a profile is configured:
//     a. SSO mode: CLI refreshes STS credentials (EnsureValidStsToken), then delegates to SDK CliProvider.
//     b. Console Login mode: CLI refreshes the login cache, then delegates to SDK CliProvider.
//     c. Other modes: directly delegates to SDK CliProvider for credential resolution.
//  2. If no profile is configured, use the SDK default credential chain (Env → OIDC → CliProvider → EcsRole).
func NewSimpleClient(ctx *Context) (*SdkClient, error) {
	var (
		creds            *credentials.Credentials
		region, endpoint string
		endpointResolver string
		httpProxy        string
		httpsProxy       string
		disableSSl       bool
		useDualStack     bool
	)
	if ctx == nil || ctx.fixedFlags == nil {
		return nil, fmt.Errorf("invalid context for creating sdk client")
	}

	profileName, profileSource, currentProfile, err := selectInvocationProfile(ctx)
	if err != nil {
		return nil, err
	}

	if currentProfile != nil {
		// SSO 模式：CLI 负责刷新凭证并写回 config.json，再交给 SDK CliProvider 读取
		if strings.ToLower(strings.TrimSpace(currentProfile.Mode)) == ModeSSO {
			sso := &Sso{
				Profile:        currentProfile,
				SsoSessionName: currentProfile.SsoSessionName,
				Region:         currentProfile.Region,
			}
			if err := sso.EnsureValidStsToken(ctx); err != nil {
				return nil, err
			}
		}

		if strings.ToLower(strings.TrimSpace(currentProfile.Mode)) == ModeConsoleLogin {
			// Console Login 模式：CLI 负责刷新 login cache，再交给 SDK CliProvider 读取
			_, err := EnsureValidLoginToken(ctx.config, profileName)
			if err != nil {
				return nil, err
			}
		}

		// 所有模式统一委托 SDK CliProvider 解析凭证
		creds = clicreds.NewCliCredentials("", profileName)

		region = currentProfile.Region
		if region == "" {
			region = os.Getenv("VOLCENGINE_REGION")
		}
		httpProxy = currentProfile.HTTPProxy
		httpsProxy = currentProfile.HTTPSProxy
		if currentProfile.DisableSSL != nil {
			disableSSl = *currentProfile.DisableSSL
		}
		if currentProfile.UseDualStack != nil {
			useDualStack = *currentProfile.UseDualStack
		}
	} else {
		// 禁用默认凭证链
		if os.Getenv("VOLCENGINE_DISABLE_DEFAULT_CREDENTIALS") == "true" {
			return nil, fmt.Errorf("no profile configured and default credential chain is disabled (VOLCENGINE_DISABLE_DEFAULT_CREDENTIALS=true)")
		}

		// 无 profile，使用 SDK 默认凭证链（Env → OIDC → CliProvider → EcsRole）
		creds = defaults.NewDefaultCredentialProvider()

		region = os.Getenv("VOLCENGINE_REGION")
		ssl := os.Getenv("VOLCENGINE_DISABLE_SSL")
		if ssl == "true" || ssl == "false" {
			disableSSl, _ = strconv.ParseBool(ssl)
		}
		dualStack := os.Getenv("VOLCENGINE_USE_DUALSTACK")
		if dualStack == "true" || dualStack == "false" {
			useDualStack, _ = strconv.ParseBool(dualStack)
		}
	}

	// --region 运行时覆盖 region
	if f := ctx.fixedFlags.GetByName("region"); f != nil && f.GetValue() != "" {
		region = f.GetValue()
	}

	// endpoint 优先级：--endpoint / ---endpoint（清空 resolver）> profile/env resolver/host > SDK 默认
	endpoint, endpointResolver = resolveClientEndpoint(ctx, currentProfile)
	mode := classifyEndpoint(endpoint, endpointResolver)

	if region == "" {
		if currentProfile == nil && !hasLocalCredentialSignal() {
			return nil, fmt.Errorf("credentials not configured, please run 've login' or 've configure set', or set VOLCENGINE_ACCESS_KEY and VOLCENGINE_SECRET_KEY environment variables")
		}
		return nil, fmt.Errorf("region not set, please set it via profile, --region flag, or VOLCENGINE_REGION environment variable")
	}

	config := volcengine.NewConfig().
		WithRegion(region).
		WithCredentials(creds).
		WithDisableSSL(disableSSl)

	// endpoint 应用规则与 classifyEndpoint / hasEffectiveFixedEndpoint 共用同一解释。
	switch mode {
	case endpointModeStandardResolver:
		config.WithEndpointResolver(endpoints.NewStandardEndpointResolver())
	case endpointModeFixedHost:
		config.WithEndpoint(endpoint)
	}

	if useDualStack {
		config.WithUseDualStack(true)
	}
	if httpProxy != "" {
		config.WithHTTPProxy(httpProxy)
	}
	if httpsProxy != "" {
		config.WithHTTPSProxy(httpsProxy)
	}

	debugEndpoint := endpoint
	if mode == endpointModeStandardResolver {
		debugEndpoint = "standard-resolver"
	}
	debugLogClientConfig(ctx, debugClientConfig{
		ProfileName:          profileName,
		ProfileSource:        profileSource,
		CredentialMode:       debugCredentialMode(currentProfile),
		Region:               region,
		Endpoint:             debugEndpoint,
		EndpointResolver:     endpointResolver,
		DisableSSL:           disableSSl,
		UseDualStack:         useDualStack,
		HTTPProxyConfigured:  httpProxy != "",
		HTTPSProxyConfigured: httpsProxy != "",
	})

	sess, _ := session.NewSession(config)

	return &SdkClient{
		Config:      config,
		Session:     sess,
		DebugLogger: debugLoggerFromContext(ctx),
	}, nil
}

func hasLocalCredentialSignal() bool {
	if os.Getenv("VOLCENGINE_ACCESS_KEY") != "" || os.Getenv("VOLCSTACK_ACCESS_KEY_ID") != "" || os.Getenv("VOLCSTACK_ACCESS_KEY") != "" {
		return true
	}
	if os.Getenv("VOLCENGINE_OIDC_TOKEN_FILE") != "" || os.Getenv("VOLCENGINE_OIDC_ROLE_TRN") != "" {
		return true
	}
	if os.Getenv("VOLCENGINE_PROFILE") != "" || os.Getenv("VOLCSTACK_PROFILE") != "" {
		return true
	}
	if os.Getenv("VOLCENGINE_ECS_METADATA") != "" {
		return true
	}
	if os.Getenv("VOLCSTACK_CONTAINER_CREDENTIALS_FULL_URI") != "" {
		return true
	}
	return false
}

func defaultProfileName(cfg *Configure) string {
	name, _ := defaultProfileNameWithSource(cfg)
	return name
}

func defaultProfileNameWithSource(cfg *Configure) (string, string) {
	if cfg != nil && cfg.Current != "" {
		return cfg.Current, "current"
	}
	if profile := os.Getenv("VOLCENGINE_PROFILE"); profile != "" {
		return profile, "env:VOLCENGINE_PROFILE"
	}
	if profile := os.Getenv("VOLCSTACK_PROFILE"); profile != "" {
		return profile, "env:VOLCSTACK_PROFILE"
	}
	return "", "default-chain"
}

type debugClientConfig struct {
	ProfileName          string
	ProfileSource        string
	CredentialMode       string
	Region               string
	Endpoint             string
	EndpointResolver     string
	DisableSSL           bool
	UseDualStack         bool
	HTTPProxyConfigured  bool
	HTTPSProxyConfigured bool
}

func debugCredentialMode(profile *Profile) string {
	if profile == nil {
		return "default-chain"
	}
	mode := strings.ToLower(strings.TrimSpace(profile.Mode))
	if mode == "" {
		return ModeAK
	}
	return mode
}

func debugLogClientConfig(ctx *Context, info debugClientConfig) {
	logger := debugLoggerFromContext(ctx)
	if logger == nil || !logger.Enabled() {
		return
	}
	logger.Printf("client_config profile_source=%s profile=%s credential_mode=%s region=%s endpoint=%s endpoint_resolver=%s disable_ssl=%t use_dual_stack=%t http_proxy_configured=%t https_proxy_configured=%t",
		info.ProfileSource,
		info.ProfileName,
		info.CredentialMode,
		info.Region,
		info.Endpoint,
		info.EndpointResolver,
		info.DisableSSL,
		info.UseDualStack,
		info.HTTPProxyConfigured,
		info.HTTPSProxyConfigured,
	)
}

func (s *SdkClient) initClient(svc string, version string) (*client.Client, error) {
	if s == nil || s.Session == nil {
		return nil, fmt.Errorf("failed to initialize SDK client for service %q: session is not configured", svc)
	}
	config := s.Session.ClientConfig(svc)
	// SDK 的 ClientConfig 会吞掉 endpoint resolver 错误并返回零值配置。
	// 调用方可能传入 SDK 尚未收录的 service，必须在解引用前转成可读错误，避免 CLI panic。
	if config.Config == nil {
		return nil, fmt.Errorf("failed to initialize SDK client for service %q: endpoint or service configuration could not be resolved", svc)
	}
	c := client.New(
		*config.Config,
		metadata.ClientInfo{
			ServiceName:   svc,
			ServiceID:     svc,
			SigningName:   config.SigningName,
			SigningRegion: config.SigningRegion,
			Endpoint:      config.Endpoint,
			APIVersion:    version,
		},
		config.Handlers,
	)

	c.Handlers.Build.PushBackNamed(clientVersionAndUserAgentHandler)
	c.Handlers.Sign.PushBackNamed(volc.SignRequestHandler)
	c.Handlers.Build.PushBackNamed(volcenginequery.BuildHandler)
	c.Handlers.Unmarshal.PushBackNamed(volcenginequery.UnmarshalHandler)
	c.Handlers.UnmarshalMeta.PushBackNamed(volcenginequery.UnmarshalMetaHandler)
	c.Handlers.UnmarshalError.PushBackNamed(volcenginequery.UnmarshalErrorHandler)
	s.addDebugRequestAttemptHandler(c)

	return c, nil
}

func (s *SdkClient) CallSdk(info SdkClientInfo, input interface{}) (output *map[string]interface{}, err error) {
	c, err := s.initClient(info.ServiceName, info.Version)
	if err != nil {
		return nil, err
	}
	op := &request.Operation{
		Name:       info.Action,
		HTTPMethod: strings.ToUpper(info.Method),
		HTTPPath:   "/",
	}
	if input == nil {
		input = &map[string]interface{}{}
	}
	output = &map[string]interface{}{}
	req := c.NewRequest(op, input, output)
	if isJSONContentType(info.ContentType) {
		// Normalize JSON Content-Type; parameters from the user (charset, etc.) are dropped
		// in favor of a single canonical value the SDK path expects.
		req.HTTPRequest.Header.Set("Content-Type", "application/json; charset=utf-8")
	} else if strings.TrimSpace(info.ContentType) != "" {
		req.HTTPRequest.Header.Set("Content-Type", info.ContentType)
	}
	for _, h := range info.Headers {
		if strings.EqualFold(h.Name, "Content-Type") {
			// Already applied via ContentType (with JSON charset normalization when needed).
			continue
		}
		req.HTTPRequest.Header.Set(h.Name, h.Value)
	}
	err = req.Send()
	return output, err
}
