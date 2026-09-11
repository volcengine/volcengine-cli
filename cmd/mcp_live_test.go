package cmd

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	mcpclient "github.com/ThinkInAIXYZ/go-mcp/client"
	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/transport"
)

func TestMCPLiveProtocols(t *testing.T) {
	if os.Getenv("VOLCENGINE_MCP_STDIO_LIVE_HELPER") == "1" {
		invoker, err := newReloadingVolcengineMCPInvoker(runtimeConfig(), os.Getenv("VOLCENGINE_MCP_LIVE_PROFILE"), "")
		if err != nil {
			t.Fatalf("create live invoker: %v", err)
		}
		if err := runMCPStdio(invoker); err != nil {
			t.Fatalf("run live stdio server: %v", err)
		}
		return
	}
	if os.Getenv("VOLCENGINE_MCP_LIVE_TEST") != "1" {
		t.Skip("set VOLCENGINE_MCP_LIVE_TEST=1 to call sts.GetCallerIdentity with the active CLI credentials")
	}
	liveProfile := os.Getenv("VOLCENGINE_MCP_LIVE_PROFILE")

	t.Run("stdio", func(t *testing.T) {
		clientTransport, err := transport.NewStdioClientTransport(
			os.Args[0], []string{"-test.run=^TestMCPLiveProtocols$"},
			transport.WithStdioClientOptionEnv("VOLCENGINE_MCP_STDIO_LIVE_HELPER=1"),
		)
		if err != nil {
			t.Fatalf("create stdio transport: %v", err)
		}
		client, err := mcpclient.NewClient(clientTransport, mcpclient.WithInitTimeout(20*time.Second))
		if err != nil {
			t.Fatalf("create stdio client: %v", err)
		}
		callLiveMCPAPI(t, client)
		if err := client.Close(); err != nil {
			t.Fatalf("close stdio client: %v", err)
		}
	})

	for _, transportName := range []string{mcpTransportSSE, mcpTransportStreamableHTTP} {
		t.Run(transportName, func(t *testing.T) {
			invoker, err := newReloadingVolcengineMCPInvoker(runtimeConfig(), liveProfile, "")
			if err != nil {
				t.Fatalf("create live invoker: %v", err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			baseURL := "http://" + listener.Addr().String()
			ctx, cancel := context.WithCancel(context.Background())
			serverDone := make(chan error, 1)
			go func() {
				serverDone <- runMCPHTTP(ctx, listener, baseURL, mcpCommandOptions{Transport: transportName}, invoker)
			}()

			var clientTransport transport.ClientTransport
			switch transportName {
			case mcpTransportSSE:
				clientTransport, err = transport.NewSSEClientTransport(baseURL + "/sse")
			case mcpTransportStreamableHTTP:
				clientTransport, err = transport.NewStreamableHTTPClientTransport(baseURL + "/mcp")
			}
			if err != nil {
				cancel()
				t.Fatalf("create %s transport: %v", transportName, err)
			}
			client, err := mcpclient.NewClient(clientTransport, mcpclient.WithInitTimeout(20*time.Second))
			if err != nil {
				cancel()
				t.Fatalf("create %s client: %v", transportName, err)
			}
			callLiveMCPAPI(t, client)
			if err := client.Close(); err != nil {
				t.Fatalf("close %s client: %v", transportName, err)
			}
			cancel()
			select {
			case err := <-serverDone:
				if err != nil {
					t.Fatalf("stop %s server: %v", transportName, err)
				}
			case <-time.After(15 * time.Second):
				t.Fatalf("timed out stopping %s server", transportName)
			}
		})
	}
}

func callLiveMCPAPI(t *testing.T, client *mcpclient.Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := client.CallTool(ctx, &protocol.CallToolRequest{
		Name: "call_api",
		Arguments: map[string]interface{}{
			"service": "sts", "api": "GetCallerIdentity", "parameters": map[string]interface{}{},
		},
	})
	if err != nil {
		t.Fatalf("call sts.GetCallerIdentity: %v", err)
	}
	if result.IsError {
		var messages []string
		for _, content := range result.Content {
			if text, ok := content.(*protocol.TextContent); ok {
				messages = append(messages, text.Text)
			}
		}
		t.Fatalf("sts.GetCallerIdentity returned an MCP tool error: %s", strings.Join(messages, "; "))
	}
}
