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
	Config  *volcengine.Config
	Session *session.Session
}

type SdkClientInfo struct {
	ServiceName string
	Action      string
	Version     string
	Method      string
	ContentType string
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
		disableSSl       bool
		useDualStack     bool
	)
	if ctx == nil || ctx.fixedFlags == nil {
		return nil, fmt.Errorf("invalid context for creating sdk client")
	}
	var currentProfile *Profile
	profileName := ""
	if ctx.config != nil {
		// ---profile 运行时覆盖当前 profile
		profileName = ctx.config.Current
		overrideProfile := false
		if f := ctx.fixedFlags.GetByName("profile"); f != nil && f.GetValue() != "" {
			profileName = f.GetValue()
			overrideProfile = true
		}
		currentProfile = ctx.config.Profiles[profileName]
		if overrideProfile && currentProfile == nil {
			return nil, fmt.Errorf("profile %q not found", profileName)
		}
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
		endpoint = currentProfile.Endpoint
		endpointResolver = currentProfile.EndpointResolver
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
		endpoint = os.Getenv("VOLCENGINE_ENDPOINT")
		endpointResolver = os.Getenv("VOLCENGINE_ENDPOINT_RESOLVER")
		ssl := os.Getenv("VOLCENGINE_DISABLE_SSL")
		if ssl == "true" || ssl == "false" {
			disableSSl, _ = strconv.ParseBool(ssl)
		}
		dualStack := os.Getenv("VOLCENGINE_USE_DUALSTACK")
		if dualStack == "true" || dualStack == "false" {
			useDualStack, _ = strconv.ParseBool(dualStack)
		}
	}

	// ---region 运行时覆盖 region
	if f := ctx.fixedFlags.GetByName("region"); f != nil && f.GetValue() != "" {
		region = f.GetValue()
	}

	if region == "" {
		return nil, fmt.Errorf("region not set, please set it via profile, ---region flag, or VOLCENGINE_REGION environment variable")
	}

	config := volcengine.NewConfig().
		WithRegion(region).
		WithCredentials(creds).
		WithDisableSSL(disableSSl)

	resolverValue := strings.ToLower(strings.TrimSpace(endpointResolver))
	switch resolverValue {
	case "standard":
		config.WithEndpointResolver(endpoints.NewStandardEndpointResolver())
	default:
		if endpoint != "" {
			if strings.ToLower(strings.TrimSpace(endpoint)) == "auto-addressing" {
				config.WithEndpointResolver(endpoints.NewStandardEndpointResolver())
			} else {
				config.WithEndpoint(endpoint)
			}
		}
	}

	if useDualStack {
		config.WithUseDualStack(true)
	}

	sess, _ := session.NewSession(config)

	return &SdkClient{
		Config:  config,
		Session: sess,
	}, nil
}

func (s *SdkClient) initClient(svc string, version string) *client.Client {
	config := s.Session.ClientConfig(svc)
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

	return c
}

func (s *SdkClient) CallSdk(info SdkClientInfo, input interface{}) (output *map[string]interface{}, err error) {
	c := s.initClient(info.ServiceName, info.Version)
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
	if strings.ToLower(info.ContentType) == "application/json" {
		req.HTTPRequest.Header.Set("Content-Type", "application/json; charset=utf-8")
	} else if info.ContentType != "" {
		req.HTTPRequest.Header.Set("Content-Type", info.ContentType)
	}
	err = req.Send()
	return output, err
}
