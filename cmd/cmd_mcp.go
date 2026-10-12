package cmd

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ThinkInAIXYZ/go-mcp/pkg"
	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/server"
	"github.com/ThinkInAIXYZ/go-mcp/transport"
	"github.com/spf13/cobra"
	"github.com/volcengine/volcengine-cli/internal/docs"
)

const (
	defaultMCPHost = "127.0.0.1"
	defaultMCPPort = 8888
)

type mcpCommandOptions struct {
	Transport     string
	Profile       string
	Region        string
	Host          string
	Port          int
	BaseURL       string
	OAuth         bool
	OAuthIssuer   string
	OAuthAudience string
	OAuthScopes   []string
}

func init() {
	rootCmd.AddCommand(newMCPCommand())
}

func newMCPCommand() *cobra.Command {
	options := mcpCommandOptions{}
	command := &cobra.Command{
		Use:   "mcp",
		Short: tr("Run the Volcano Engine MCP server"),
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if !command.Flags().Changed("transport") {
				options.Transport = strings.TrimSpace(os.Getenv("VOLCENGINE_MCP_TRANSPORT"))
			}
			if !command.Flags().Changed("host") {
				options.Host = environmentOrDefault("VOLCENGINE_MCP_HOST", defaultMCPHost)
			}
			if !command.Flags().Changed("port") {
				port, err := environmentIntOrDefault("VOLCENGINE_MCP_PORT", defaultMCPPort)
				if err != nil {
					return err
				}
				options.Port = port
			}
			if !command.Flags().Changed("base-url") {
				options.BaseURL = strings.TrimSpace(os.Getenv("VOLCENGINE_MCP_BASE_URL"))
			}
			if !command.Flags().Changed("oauth") {
				oauth, err := environmentBool("VOLCENGINE_MCP_OAUTH", false)
				if err != nil {
					return err
				}
				options.OAuth = oauth
			}
			if !command.Flags().Changed("oauth-issuer") {
				options.OAuthIssuer = strings.TrimSpace(os.Getenv("VOLCENGINE_MCP_OAUTH_ISSUER"))
			}
			if !command.Flags().Changed("oauth-audience") {
				options.OAuthAudience = strings.TrimSpace(os.Getenv("VOLCENGINE_MCP_OAUTH_AUDIENCE"))
			}
			if !command.Flags().Changed("oauth-scopes") {
				options.OAuthScopes = splitMCPScopes(os.Getenv("VOLCENGINE_MCP_OAUTH_SCOPES"))
			}

			transportName, err := resolveMCPTransport(options.Transport, command.Flags().Changed("transport") || options.Transport != "", options.OAuth)
			if err != nil {
				return err
			}
			options.Transport = transportName
			return runMCP(command.Context(), options)
		},
	}
	command.Flags().StringVar(&options.Transport, "transport", "", tr("Transport: stdio, sse, or streamable-http"))
	command.Flags().StringVar(&options.Profile, "profile", "", tr("Use a configured profile"))
	command.Flags().StringVar(&options.Region, "region", "", tr("Set the fallback region"))
	command.Flags().StringVar(&options.Host, "host", defaultMCPHost, tr("HTTP listen host"))
	command.Flags().IntVar(&options.Port, "port", defaultMCPPort, tr("HTTP listen port"))
	command.Flags().StringVar(&options.BaseURL, "base-url", "", tr("Public base URL used in MCP and OAuth metadata"))
	command.Flags().BoolVar(&options.OAuth, "oauth", false, tr("Require OAuth Bearer tokens"))
	command.Flags().StringVar(&options.OAuthIssuer, "oauth-issuer", "", tr("OAuth authorization server issuer"))
	command.Flags().StringVar(&options.OAuthAudience, "oauth-audience", "", tr("Required access token audience"))
	command.Flags().StringSliceVar(&options.OAuthScopes, "oauth-scopes", nil, tr("Required OAuth scopes"))
	command.SetUsageTemplate(mcpUsageTemplate())
	return command
}

func mcpUsageTemplate() string {
	return tr("Usage:") + `
  {{.CommandPath}} [flags]{{if .HasAvailableLocalFlags}}

` + tr("Flags:") + `
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}
`
}

func runMCP(parent context.Context, options mcpCommandOptions) error {
	invoker, err := newReloadingVolcengineMCPInvoker(runtimeConfig(), options.Profile, options.Region)
	if err != nil {
		return err
	}
	if options.Transport == mcpTransportStdio {
		return runMCPStdio(invoker)
	}
	if options.Port < 0 || options.Port > 65535 {
		return fmt.Errorf("MCP port must be between 0 and 65535")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(options.Host, strconv.Itoa(options.Port)))
	if err != nil {
		return fmt.Errorf("listen for MCP: %w", err)
	}
	defer listener.Close()

	baseURL, err := resolveMCPBaseURL(options.BaseURL, options.Host, listener.Addr())
	if err != nil {
		return err
	}
	if options.OAuth && options.BaseURL == "" && isWildcardMCPHost(options.Host) {
		return fmt.Errorf("--base-url is required with OAuth when listening on a wildcard host")
	}
	if options.OAuth {
		parsedBaseURL, _ := url.Parse(baseURL)
		if parsedBaseURL.Scheme != "https" && !(parsedBaseURL.Scheme == "http" && isLoopbackMCPHostname(parsedBaseURL.Hostname())) {
			return fmt.Errorf("--base-url must use HTTPS with OAuth unless it is loopback")
		}
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runMCPHTTP(ctx, listener, baseURL, options, invoker)
}

func runMCPStdio(invoker mcpAPIInvoker) error {
	serverTransport := transport.NewStdioServerTransport(transport.WithStdioServerOptionLogger(quietMCPLogger{}))
	mcpServer, err := newMCPServer(serverTransport, invoker)
	if err != nil {
		return err
	}
	return mcpServer.Run()
}

func runMCPHTTP(ctx context.Context, listener net.Listener, baseURL string, options mcpCommandOptions, invoker mcpAPIInvoker) error {
	mux := http.NewServeMux()
	var serverTransport transport.ServerTransport
	endpoint := ""
	base, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid MCP base URL: %w", err)
	}
	basePath := base.Path

	switch options.Transport {
	case mcpTransportSSE:
		endpoint = "/sse"
		endpointRoute := joinMCPRoute(basePath, endpoint)
		messageRoute := joinMCPRoute(basePath, "/message")
		messageEndpoint := joinMCPURL(baseURL, "/message")
		createdTransport, handler, err := transport.NewSSEServerTransportAndHandler(messageEndpoint, transport.WithSSEServerTransportAndHandlerOptionLogger(quietMCPLogger{}))
		if err != nil {
			return err
		}
		serverTransport = createdTransport
		mux.Handle(endpointRoute, handler.HandleSSE())
		mux.Handle(messageRoute, handler.HandleMessage())
	case mcpTransportStreamableHTTP:
		endpoint = "/mcp"
		endpointRoute := joinMCPRoute(basePath, endpoint)
		createdTransport, handler, err := transport.NewStreamableHTTPServerTransportAndHandler(
			transport.WithStreamableHTTPServerTransportAndHandlerOptionLogger(quietMCPLogger{}),
			transport.WithStreamableHTTPServerTransportAndHandlerOptionStateMode(transport.Stateless),
		)
		if err != nil {
			return err
		}
		serverTransport = createdTransport
		mux.Handle(endpointRoute, handler.HandleMCP())
	default:
		return fmt.Errorf("HTTP server cannot use transport %q", options.Transport)
	}

	mcpServer, err := newMCPServer(serverTransport, invoker)
	if err != nil {
		return err
	}

	resource := joinMCPURL(baseURL, endpoint)
	metadataURL, metadataPath, err := mcpProtectedResourceMetadataLocation(resource)
	if err != nil {
		return err
	}
	var oauthMiddleware func(http.Handler) http.Handler
	if options.OAuth {
		verifier, authorizationServerMetadata, err := newMCPJWTVerifier(mcpOAuthConfig{
			Issuer: options.OAuthIssuer, Audience: options.OAuthAudience,
			Resource: resource,
		})
		if err != nil {
			return err
		}
		oauthMiddleware = mcpOAuthMiddleware(verifier, metadataURL, options.OAuthScopes)
		mux.Handle(metadataPath, mcpProtectedResourceMetadataHandler(mcpProtectedResourceMetadata{
			Resource: resource, AuthorizationServers: []string{authorizationServerMetadata.Issuer},
			ScopesSupported: options.OAuthScopes, BearerMethodsSupported: []string{"header"},
		}))
	}

	securedMux := http.NewServeMux()
	for _, route := range []string{joinMCPRoute(basePath, endpoint), joinMCPRoute(basePath, "/message")} {
		if route == joinMCPRoute(basePath, "/message") && options.Transport != mcpTransportSSE {
			continue
		}
		handler := http.Handler(mux)
		handler = mcpRequestMetadataMiddleware(handler)
		if oauthMiddleware != nil {
			handler = oauthMiddleware(handler)
		}
		handler = mcpHTTPSecurityMiddleware(handler, baseURL)
		securedMux.Handle(route, handler)
	}
	if options.OAuth {
		securedMux.Handle(metadataPath, mcpHTTPSecurityMiddleware(mux, baseURL))
	}

	httpServer := &http.Server{
		Handler:           securedMux,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 * 1024,
	}
	transportErr := make(chan error, 1)
	go func() { transportErr <- mcpServer.Run() }()
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.Serve(listener) }()
	fmt.Fprintf(os.Stderr, "Volcano Engine MCP server listening at %s\n", resource)

	var result error
	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			result = err
		}
	case err := <-transportErr:
		if err != nil {
			result = err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpShutdownErr := make(chan error, 1)
	go func() { httpShutdownErr <- httpServer.Shutdown(shutdownCtx) }()
	if err := mcpServer.Shutdown(shutdownCtx); result == nil && err != nil {
		result = err
	}
	if err := <-httpShutdownErr; result == nil && err != nil {
		result = err
	}
	return result
}

func newMCPServer(serverTransport transport.ServerTransport, invoker mcpAPIInvoker) (*server.Server, error) {
	mcpServer, err := server.NewServer(
		serverTransport,
		server.WithServerInfo(protocol.Implementation{Name: "volcengine-cli", Version: clientVersion}),
		server.WithCapabilities(protocol.ServerCapabilities{Tools: &protocol.ToolsCapability{}}),
		server.WithLogger(quietMCPLogger{}),
	)
	if err != nil {
		return nil, err
	}
	registerMCPTools(mcpServerRegistrar{server: mcpServer}, rootSupport, invoker, docs.New(docsUserAgent(os.LookupEnv)))
	return mcpServer, nil
}

func mcpHTTPSecurityMiddleware(next http.Handler, baseURL string) http.Handler {
	parsedBase, _ := url.Parse(baseURL)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
			parsedOrigin, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(parsedOrigin.Scheme, parsedBase.Scheme) || !strings.EqualFold(parsedOrigin.Host, parsedBase.Host) {
				http.Error(w, "origin is not allowed", http.StatusForbidden)
				return
			}
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 4*1024*1024)
		}
		next.ServeHTTP(w, r)
	})
}

func resolveMCPBaseURL(configured, listenHost string, address net.Addr) (string, error) {
	if configured != "" {
		parsed, err := url.Parse(strings.TrimRight(configured, "/"))
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", fmt.Errorf("--base-url must be an absolute HTTP(S) URL")
		}
		return strings.TrimRight(parsed.String(), "/"), nil
	}
	port := ""
	if tcpAddress, ok := address.(*net.TCPAddr); ok {
		port = strconv.Itoa(tcpAddress.Port)
	} else {
		_, port, _ = net.SplitHostPort(address.String())
	}
	host := listenHost
	if host == "" {
		host = defaultMCPHost
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

func joinMCPURL(baseURL, route string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(route, "/")
	}
	parsed.Path = path.Join(parsed.Path, route)
	return parsed.String()
}

func joinMCPRoute(basePath, route string) string {
	joined := path.Join("/", basePath, route)
	if !strings.HasPrefix(joined, "/") {
		return "/" + joined
	}
	return joined
}

func mcpProtectedResourceMetadataLocation(resource string) (string, string, error) {
	parsed, err := url.Parse(resource)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("invalid MCP resource URL")
	}
	resourcePath := strings.Trim(parsed.Path, "/")
	metadataPath := mcpOAuthMetadataPath
	if resourcePath != "" {
		metadataPath += "/" + resourcePath
	}
	metadata := &url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: metadataPath}
	return metadata.String(), metadataPath, nil
}

func environmentOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func environmentIntOrDefault(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return parsed, nil
}

func environmentBool(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return parsed, nil
}

func splitMCPScopes(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
}

func isWildcardMCPHost(host string) bool {
	host = strings.Trim(host, "[]")
	return host == "" || host == "0.0.0.0" || host == "::"
}

func isLoopbackMCPHostname(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type quietMCPLogger struct{}

func (quietMCPLogger) Debugf(string, ...interface{}) {}
func (quietMCPLogger) Infof(string, ...interface{})  {}
func (quietMCPLogger) Warnf(string, ...interface{})  {}
func (quietMCPLogger) Errorf(string, ...interface{}) {}

var _ pkg.Logger = quietMCPLogger{}
