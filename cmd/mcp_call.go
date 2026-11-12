package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/volcengine/volcengine-go-sdk/volcengine/credentials"
)

type volcengineMCPInvoker struct {
	config          *Configure
	explicitProfile string
	startupRegion   string
	reloadConfig    func() *Configure
}

var mcpClientInitMu sync.Mutex

func newVolcengineMCPInvoker(cfg *Configure, explicitProfile, startupRegion string) (*volcengineMCPInvoker, error) {
	invoker := &volcengineMCPInvoker{
		config:          cfg,
		explicitProfile: strings.TrimSpace(explicitProfile),
		startupRegion:   strings.TrimSpace(startupRegion),
	}
	if _, _, _, err := invoker.resolveConfig(); err != nil {
		return nil, err
	}
	return invoker, nil
}

func newReloadingVolcengineMCPInvoker(cfg *Configure, explicitProfile, startupRegion string) (*volcengineMCPInvoker, error) {
	invoker, err := newVolcengineMCPInvoker(cfg, explicitProfile, startupRegion)
	if err != nil {
		return nil, err
	}
	invoker.reloadConfig = LoadConfig
	return invoker, nil
}

func (i *volcengineMCPInvoker) resolveConfig() (*Configure, string, string, error) {
	cfg := i.config
	if i.reloadConfig != nil {
		if reloaded := i.reloadConfig(); reloaded != nil {
			cfg = reloaded
		}
	}

	profileName := i.explicitProfile
	if profileName != "" {
		if cfg == nil || cfg.Profiles == nil || cfg.Profiles[profileName] == nil {
			return nil, "", "", fmt.Errorf("profile %q not found", profileName)
		}
	} else {
		candidate, _ := defaultProfileNameWithSource(cfg)
		if cfg != nil && cfg.Profiles != nil && cfg.Profiles[candidate] != nil {
			profileName = candidate
		}
	}

	profileRegion := ""
	if profileName != "" {
		profileRegion = cfg.Profiles[profileName].Region
	}
	return cfg, profileName, profileRegion, nil
}

func (i *volcengineMCPInvoker) CallAPI(ctx context.Context, serviceName, action, callRegion string, parameters map[string]interface{}, yes bool) (map[string]interface{}, error) {
	if !rootSupport.IsValidAction(serviceName, action) {
		return nil, fmt.Errorf("unsupported API %s.%s", serviceName, action)
	}
	// 已下线版本直接阻断，不发起请求；MCP 以工具错误返回，不污染业务 JSON。
	if err := rejectDeprecatedServiceCall(serviceName, rootSupport.GetVersion(serviceName)); err != nil {
		return nil, err
	}
	_, policy, err := loadEffectiveSafetyPolicy()
	if err != nil {
		return nil, err
	}
	if err := enforceGivenSafetyPolicy(policy, serviceName, action, yes, false, tr("call the same MCP tool again with yes=true"), safetyDeniedForModel()); err != nil {
		return nil, err
	}
	client, err := i.resolveClient(callRegion, mcpRequestMetadataFromContext(ctx))
	if err != nil {
		return nil, err
	}

	output, err := callAPIWithClientContext(ctx, client.sdk, serviceName, action, parameters, false)
	if err != nil {
		return nil, formatActionError(err)
	}
	if output == nil {
		return map[string]interface{}{}, nil
	}
	return *output, nil
}

// Credential sources reported by resolveClient.
const (
	mcpCredentialSourceProfile      = "profile"
	mcpCredentialSourceDefaultChain = "default-chain"
	mcpCredentialSourceHeader       = "request-header"
)

// mcpResolvedClient is the SDK client for one API request together with the
// profile, credential source and identity it was built from.
type mcpResolvedClient struct {
	sdk              *SdkClient
	profileName      string
	credentialSource string
	identity         string
	region           string
}

// resolveClient reloads configuration and builds the SDK client for one API
// request. Client construction stays serialized by mcpClientInitMu: for SSO
// and Console Login profiles NewSimpleClient lets the CLI refresh the STS or
// login cache, mutating the shared config's profile map and writing
// config.json or the login cache, which must not race between concurrent
// MCP requests or script calls.
func (i *volcengineMCPInvoker) resolveClient(callRegion string, metadata mcpRequestMetadata) (*mcpResolvedClient, error) {
	callConfig, profileName, profileRegion, err := i.resolveConfig()
	if err != nil {
		return nil, formatActionError(err)
	}
	region := resolveMCPRegion(callRegion, i.startupRegion, profileRegion, os.Getenv("VOLCENGINE_REGION"))
	callContext := NewContext()
	callContext.SetConfig(callConfig)
	if profileName != "" {
		profileFlag, _ := callContext.fixedFlags.AddByName("profile")
		profileFlag.SetValue(profileName)
	}
	regionFlag, _ := callContext.fixedFlags.AddByName("region")
	regionFlag.SetValue(region)

	resolved := &mcpResolvedClient{profileName: profileName, region: region, credentialSource: mcpCredentialSourceDefaultChain}
	if profileName != "" {
		resolved.credentialSource = mcpCredentialSourceProfile
		resolved.identity = profileCredentialIdentity(callConfig.Profiles[profileName])
	}
	mcpClientInitMu.Lock()
	sdk, err := NewSimpleClient(callContext)
	mcpClientInitMu.Unlock()
	if err != nil && profileName == "" && metadata.Credentials != nil {
		sdk, err = newSimpleClient(callContext, staticMCPCredentials(metadata.Credentials))
		resolved.credentialSource = mcpCredentialSourceHeader
	}
	if err != nil {
		return nil, formatActionError(err)
	}

	if profileName == "" {
		value, credentialErr := sdk.Config.Credentials.Get()
		if credentialErr != nil {
			if metadata.Credentials == nil {
				return nil, formatActionError(credentialErr)
			}
			sdk, err = newSimpleClient(callContext, staticMCPCredentials(metadata.Credentials))
			if err != nil {
				return nil, formatActionError(err)
			}
			resolved.credentialSource = mcpCredentialSourceHeader
		}
		switch resolved.credentialSource {
		case mcpCredentialSourceHeader:
			resolved.identity = "access-key:" + metadata.Credentials.AccessKeyID
		default:
			resolved.identity = defaultChainCredentialIdentity(value)
		}
	}
	resolved.sdk = sdk
	return resolved, nil
}

// profileCredentialIdentity returns the identity a profile's credentials
// act as: the profile, its mode and every role or session target field.
// Only values the CLI refreshes for that same target are excluded: the
// secret key, session token and expiration, and, for SSO and Console Login,
// the temporary access key the CLI writes back after refreshing.
func profileCredentialIdentity(profile *Profile) string {
	if profile == nil {
		return ""
	}
	mode := strings.ToLower(strings.TrimSpace(profile.Mode))
	accessKey := profile.AccessKey
	if mode == ModeSSO || mode == ModeConsoleLogin {
		accessKey = ""
	}
	return strings.Join([]string{
		"profile", profile.Name, mode, accessKey, profile.AccountId, profile.RoleName, profile.RoleTrn,
		profile.SsoSessionName, profile.LoginSession, profile.OidcTokenFile,
	}, "\x00")
}

// defaultChainCredentialIdentity identifies credentials from the SDK default
// chain or request headers by access key ID. A rotated key cannot be tied to
// the same principal locally, so it fails closed as an identity change.
func defaultChainCredentialIdentity(value credentials.Value) string {
	return "default-chain:" + value.ProviderName + ":" + value.AccessKeyID
}

func staticMCPCredentials(value *mcpCredentials) *credentials.Credentials {
	return credentials.NewStaticCredentials(value.AccessKeyID, value.SecretAccessKey, value.SessionToken)
}
