package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

const (
	defaultMCPRegion = "cn-beijing"

	mcpCredentialsHeader = "X-Volcengine-Credentials"
	mcpRegionHeader      = "X-Volcengine-Region"

	mcpTransportStdio          = "stdio"
	mcpTransportSSE            = "sse"
	mcpTransportStreamableHTTP = "streamable-http"
)

type mcpCredentials struct {
	AccessKeyID     string `json:"AccessKeyId"`
	SecretAccessKey string `json:"SecretAccessKey"`
	SessionToken    string `json:"SessionToken,omitempty"`
}

type mcpRequestMetadata struct {
	Credentials *mcpCredentials
	Region      string
}

type mcpRequestMetadataContextKey struct{}

var mcpRegionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,127}$`)

func resolveMCPRegion(headerRegion, startupRegion, profileRegion, environmentRegion string) string {
	for _, region := range []string{headerRegion, startupRegion, profileRegion, environmentRegion} {
		if region = strings.TrimSpace(region); region != "" {
			return region
		}
	}
	return defaultMCPRegion
}

func parseMCPRequestMetadata(header http.Header) (mcpRequestMetadata, error) {
	metadata := mcpRequestMetadata{Region: strings.TrimSpace(header.Get(mcpRegionHeader))}
	if metadata.Region != "" && !mcpRegionPattern.MatchString(metadata.Region) {
		return mcpRequestMetadata{}, fmt.Errorf("%s contains an invalid region", mcpRegionHeader)
	}
	encoded := strings.TrimSpace(header.Get(mcpCredentialsHeader))
	if encoded == "" {
		return metadata, nil
	}
	if len(encoded) > 16*1024 {
		return mcpRequestMetadata{}, fmt.Errorf("%s is too large", mcpCredentialsHeader)
	}

	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return mcpRequestMetadata{}, fmt.Errorf("%s must be base64-encoded JSON", mcpCredentialsHeader)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var credential mcpCredentials
	if err := decoder.Decode(&credential); err != nil {
		return mcpRequestMetadata{}, fmt.Errorf("%s must contain valid credential JSON", mcpCredentialsHeader)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return mcpRequestMetadata{}, fmt.Errorf("%s must contain one JSON object", mcpCredentialsHeader)
	}
	credential.AccessKeyID = strings.TrimSpace(credential.AccessKeyID)
	credential.SecretAccessKey = strings.TrimSpace(credential.SecretAccessKey)
	credential.SessionToken = strings.TrimSpace(credential.SessionToken)
	if credential.AccessKeyID == "" || credential.SecretAccessKey == "" {
		return mcpRequestMetadata{}, fmt.Errorf("%s requires AccessKeyId and SecretAccessKey", mcpCredentialsHeader)
	}
	metadata.Credentials = &credential
	return metadata, nil
}

func mcpRequestMetadataMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadata, err := parseMCPRequestMetadata(r.Header)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx := context.WithValue(r.Context(), mcpRequestMetadataContextKey{}, metadata)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func mcpRequestMetadataFromContext(ctx context.Context) mcpRequestMetadata {
	metadata, _ := ctx.Value(mcpRequestMetadataContextKey{}).(mcpRequestMetadata)
	return metadata
}

func resolveMCPTransport(value string, explicitlySet, oauth bool) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		if oauth && !explicitlySet {
			return mcpTransportStreamableHTTP, nil
		}
		value = mcpTransportStdio
	}
	switch value {
	case mcpTransportStdio, mcpTransportSSE, mcpTransportStreamableHTTP:
	default:
		return "", fmt.Errorf("unsupported MCP transport %q; use stdio, sse, or streamable-http", value)
	}
	if oauth && value == mcpTransportStdio {
		return "", fmt.Errorf("OAuth is not supported with the stdio transport")
	}
	return value, nil
}
