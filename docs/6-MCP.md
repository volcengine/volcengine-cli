[← Advanced Usage](5-Advanced.md) | MCP Server[(中文)](6-MCP-zh.md)

---

## MCP Server

`ve mcp` exposes the CLI service metadata and API caller through the Model Context Protocol. It provides four tools:

- `list_services`: list every service and service code supported by the CLI.
- `list_apis`: list every supported API for a given service code.
- `get_api_parameters`: inspect request parameters, types, required fields, and an example.
- `call_api`: call any API supported by the CLI. The MCP server does not maintain an API allowlist; Volcengine IAM evaluates the cloud credential used by the request.

## Transports

stdio is the default:

```shell
ve mcp
ve mcp --profile prod
```

Run Streamable HTTP:

```shell
ve mcp --transport streamable-http --host 127.0.0.1 --port 8888
```

The endpoint is `http://127.0.0.1:8888/mcp`.

Run legacy HTTP+SSE:

```shell
ve mcp --transport sse --host 127.0.0.1 --port 8888
```

The SSE and message endpoints are `/sse` and `/message`. `--base-url` may include a path prefix and is used in the endpoint announced to clients.

Enabling OAuth without explicitly choosing a transport selects Streamable HTTP:

```shell
ve mcp --oauth \
  --oauth-issuer https://auth.example.com \
  --oauth-audience volcengine-mcp \
  --base-url https://mcp.example.com
```

`--oauth` with `--transport stdio` is rejected because stdio has no HTTP Bearer authentication layer.

## Cloud Credentials

Cloud credentials are resolved for each `call_api` request in this order:

1. Explicit `--profile`.
2. The current profile in CLI configuration.
3. `VOLCENGINE_PROFILE` or `VOLCSTACK_PROFILE`.
4. The Volcengine SDK default credential chain.
5. `X-Volcengine-Credentials` on the HTTP request.

The CLI configuration is reloaded before every call. Running `ve login`, changing the current profile, or updating a profile while the MCP server is running takes effect on the next `call_api` without a server restart. An explicit `--profile` pins the profile name while its login state and credential values remain hot-reloadable.

An explicitly named profile that does not exist prevents the server from starting. stdio cannot receive HTTP headers; if no profile or default-chain credential is available, `call_api` returns an error while the three metadata tools remain usable.

The credential header is standard Base64 containing one JSON object:

```json
{"AccessKeyId":"AK","SecretAccessKey":"SK","SessionToken":"optional STS token"}
```

Example generation:

```shell
printf '%s' '{"AccessKeyId":"AK","SecretAccessKey":"SK"}' | base64
```

Send the result as `X-Volcengine-Credentials`. The value is request-scoped and is not cached, logged, or written to the environment. Use `Authorization: Bearer ...` only for OAuth access tokens. The automatic veFaaS IAM credential file under `/var/run/secrets/iam/credential` is intentionally not supported.

## Region

Region resolution order for MCP API calls is:

1. `X-Volcengine-Region` request header.
2. Startup `--region`.
3. Region in the selected profile.
4. `VOLCENGINE_REGION`.
5. `cn-beijing`.

Unlike regular CLI API commands, MCP calls use `cn-beijing` when no region is configured.

## OAuth Resource Server

OAuth controls who may use the MCP endpoint; it does not provide Volcengine cloud credentials. The server implements Bearer authentication, OAuth Authorization Server Metadata discovery, Protected Resource Metadata, JWT verification through the authorization server JWKS, and issuer/audience/expiration/scope validation.

```shell
ve mcp --oauth \
  --oauth-issuer https://auth.example.com \
  --oauth-audience volcengine-mcp \
  --oauth-scopes mcp:use \
  --host 0.0.0.0 \
  --port 8888 \
  --base-url https://mcp.example.com
```

For non-loopback OAuth deployments, the issuer and public base URL must use HTTPS. TLS may terminate at a reverse proxy. Protected Resource Metadata is served at the RFC 9728 well-known location derived from the MCP resource path.

Supported environment variables:

```text
VOLCENGINE_MCP_TRANSPORT
VOLCENGINE_MCP_HOST
VOLCENGINE_MCP_PORT
VOLCENGINE_MCP_BASE_URL
VOLCENGINE_MCP_OAUTH
VOLCENGINE_MCP_OAUTH_ISSUER
VOLCENGINE_MCP_OAUTH_AUDIENCE
VOLCENGINE_MCP_OAUTH_SCOPES
VOLCENGINE_REGION
```

The HTTP server binds to `127.0.0.1:8888` by default. Expose it beyond loopback only with an appropriate authentication and TLS deployment.

---

[← Advanced Usage](5-Advanced.md) | MCP Server[(中文)](6-MCP-zh.md)
