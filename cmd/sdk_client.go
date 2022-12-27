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

func NewSimpleClient(ctx *Context) (*SdkClient, error) {
	var (
		ak, sk, sessionToken, region, endpoint string
		disableSSl                             bool
	)

	// first try to get ak/sk/region from config file
	var currentProfile *Profile
	if ctx.config != nil {
		if currentProfile = ctx.config.Profiles[ctx.config.Current]; currentProfile != nil {
			ak = currentProfile.AccessKey
			sk = currentProfile.SecretKey
			region = currentProfile.Region
			sessionToken = currentProfile.SessionToken
			endpoint = currentProfile.Endpoint
			disableSSl = *currentProfile.DisableSSL

			if ak == "" {
				return nil, fmt.Errorf("profile AccessKey not set")
			}
			if sk == "" {
				return nil, fmt.Errorf("profile SecretKey not set")
			}
			if region == "" {
				return nil, fmt.Errorf("profile Region not set")
			}
		}
	}

	// if cannot get from config file, try to get from export variable
	if currentProfile == nil {
		ak = os.Getenv("VOLCENGINE_ACCESS_KEY")
		sk = os.Getenv("VOLCENGINE_SECRET_KEY")
		region = os.Getenv("VOLCENGINE_REGION")
		endpoint = os.Getenv("VOLCENGINE_ENDPOINT")
		sessionToken = os.Getenv("VOLCENGINE_SESSION_TOKEN")
		ssl := os.Getenv("VOLCENGINE_DISABLE_SSL")
		if ssl == "true" || ssl == "false" {
			disableSSl, _ = strconv.ParseBool(ssl)
		}

		if ak == "" {
			return nil, fmt.Errorf("VOLCENGINE_ACCESS_KEY not set")
		}
		if sk == "" {
			return nil, fmt.Errorf("VOLCENGINE_SECRET_KEY not set")
		}
		if region == "" {
			return nil, fmt.Errorf("VOLCENGINE_REGION not set")
		}
		if endpoint == "" {
			return nil, fmt.Errorf("VOLCENGINE_ENDPOINT not set")
		}
	}

	config := volcengine.NewConfig().
		WithRegion(region).
		WithCredentials(credentials.NewStaticCredentials(ak, sk, sessionToken)).
		WithDisableSSL(disableSSl)

	if endpoint == "" {
		return nil, fmt.Errorf("endpoint is empty, please set")
	}
	config.WithEndpoint(endpoint)

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
	}
	err = req.Send()
	return output, err
}
