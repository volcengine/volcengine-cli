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

func (i *volcengineMCPInvoker) CallAPI(ctx context.Context, serviceName, action, callRegion string, parameters map[string]interface{}) (map[string]interface{}, error) {
	if !rootSupport.IsValidAction(serviceName, action) {
		return nil, fmt.Errorf("unsupported API %s.%s", serviceName, action)
	}
	callConfig, profileName, profileRegion, err := i.resolveConfig()
	if err != nil {
		return nil, formatActionError(err)
	}
	metadata := mcpRequestMetadataFromContext(ctx)
	region := resolveMCPRegion(callRegion, i.startupRegion, profileRegion, os.Getenv("VOLCENGINE_REGION"))
	callContext := NewContext()
	callContext.SetConfig(callConfig)
	if profileName != "" {
		profileFlag, _ := callContext.fixedFlags.AddByName("profile")
		profileFlag.SetValue(profileName)
	}
	regionFlag, _ := callContext.fixedFlags.AddByName("region")
	regionFlag.SetValue(region)

	mcpClientInitMu.Lock()
	sdk, err := NewSimpleClient(callContext)
	mcpClientInitMu.Unlock()
	if err != nil && profileName == "" && metadata.Credentials != nil {
		sdk, err = newSimpleClient(callContext, staticMCPCredentials(metadata.Credentials))
	}
	if err != nil {
		return nil, formatActionError(err)
	}

	if profileName == "" {
		if _, credentialErr := sdk.Config.Credentials.Get(); credentialErr != nil {
			if metadata.Credentials == nil {
				return nil, formatActionError(credentialErr)
			}
			sdk, err = newSimpleClient(callContext, staticMCPCredentials(metadata.Credentials))
			if err != nil {
				return nil, formatActionError(err)
			}
		}
	}

	output, err := callAPIWithClientContext(ctx, sdk, serviceName, action, parameters, false)
	if err != nil {
		return nil, formatActionError(err)
	}
	if output == nil {
		return map[string]interface{}{}, nil
	}
	return *output, nil
}

func staticMCPCredentials(value *mcpCredentials) *credentials.Credentials {
	return credentials.NewStaticCredentials(value.AccessKeyID, value.SecretAccessKey, value.SessionToken)
}
