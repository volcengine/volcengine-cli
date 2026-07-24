package cmd

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestResolveMCPRegionPriority(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		startup string
		profile string
		env     string
		want    string
	}{
		{name: "header", header: "ap-southeast-1", startup: "cn-shanghai", profile: "cn-guangzhou", env: "cn-north-1", want: "ap-southeast-1"},
		{name: "startup", startup: "cn-shanghai", profile: "cn-guangzhou", env: "cn-north-1", want: "cn-shanghai"},
		{name: "profile", profile: "cn-guangzhou", env: "cn-north-1", want: "cn-guangzhou"},
		{name: "environment", env: "cn-north-1", want: "cn-north-1"},
		{name: "default", want: defaultMCPRegion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveMCPRegion(tt.header, tt.startup, tt.profile, tt.env); got != tt.want {
				t.Fatalf("resolveMCPRegion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMCPCommandUsageDoesNotShowServiceActionPlaceholders(t *testing.T) {
	parent := &cobra.Command{Use: "ve"}
	parent.SetUsageTemplate(rootUsageTemplate())
	command := newMCPCommand()
	parent.AddCommand(command)

	usage := command.UsageString()
	if strings.Contains(usage, "[service]") || strings.Contains(usage, "[action]") || strings.Contains(usage, "[params]") {
		t.Fatalf("MCP usage contains API command placeholders:\n%s", usage)
	}
}

func TestParseMCPRequestMetadata(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte(`{"AccessKeyId":"ak","SecretAccessKey":"sk","SessionToken":"token"}`))
	header := make(http.Header)
	header.Set(mcpCredentialsHeader, payload)
	header.Set(mcpRegionHeader, "ap-southeast-1")

	metadata, err := parseMCPRequestMetadata(header)
	if err != nil {
		t.Fatalf("parseMCPRequestMetadata() error = %v", err)
	}
	if metadata.Region != "ap-southeast-1" {
		t.Fatalf("Region = %q", metadata.Region)
	}
	if metadata.Credentials == nil || metadata.Credentials.AccessKeyID != "ak" || metadata.Credentials.SecretAccessKey != "sk" || metadata.Credentials.SessionToken != "token" {
		t.Fatalf("unexpected credentials: %#v", metadata.Credentials)
	}
}

func TestParseMCPRequestMetadataRejectsPartialCredentials(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte(`{"AccessKeyId":"ak"}`))
	header := make(http.Header)
	header.Set(mcpCredentialsHeader, payload)

	if _, err := parseMCPRequestMetadata(header); err == nil {
		t.Fatal("expected partial credentials to be rejected")
	}
}

func TestResolveMCPTransport(t *testing.T) {
	if got, err := resolveMCPTransport("", false, false); err != nil || got != mcpTransportStdio {
		t.Fatalf("default transport = %q, err = %v", got, err)
	}
	if got, err := resolveMCPTransport("", false, true); err != nil || got != mcpTransportStreamableHTTP {
		t.Fatalf("oauth transport = %q, err = %v", got, err)
	}
	if _, err := resolveMCPTransport(mcpTransportStdio, true, true); err == nil {
		t.Fatal("expected stdio with OAuth to fail")
	}
}
