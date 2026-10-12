[← Advanced Usage](5-Advanced.md) | MCP Server[(中文)](6-MCP-zh.md) | [Proxy-CLI →](7-ProxyCLI.md)

---

## MCP Server

`ve mcp` exposes the CLI service metadata and API caller through the Model Context Protocol. It provides four tools:

- `list_services`: list every service and service code supported by the CLI.
- `list_apis`: list every supported API for a given service code.
- `get_api_parameters`: inspect request parameters, types, required fields, and an example.
- `call_api`: call any API supported by the CLI. Volcengine IAM evaluates the cloud credential, and the local CLI safety policy is checked before credentials are resolved or a request is sent.

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

1. Optional `region` argument of `call_api`.
2. Startup `--region`.
3. Region in the selected profile.
4. `VOLCENGINE_REGION`.
5. `cn-beijing`.

Unlike regular CLI API commands, MCP calls use `cn-beijing` when no region is configured.

<a id="safety-policy"></a>

## Safety Policy

Every `call_api` request reloads and evaluates `~/.volcengine/safety-policy.json` and its environment overrides. A `deny` match returns a tool error without sending the cloud request. A `confirm` match also returns a tool error instructing the agent to explain the operation and ask the user for approval. After explicit approval, repeat the same tool call with `yes: true`:

```json
{
  "service": "ecs",
  "api": "UpdateInstance",
  "parameters": {"InstanceId": "i-xxxxx"},
  "yes": true
}
```

`yes` is optional and defaults to `false`. It skips only `confirm` and cannot bypass `deny`. Omit it on each new operation. Only after a confirmation error, briefly explain the target and impact, ask to continue, then end the turn and wait for a new user reply. After approval, internally set `yes: true` for one unchanged retry without asking the user to handle this parameter. Discard approval after the attempt, even on failure. Single-use approval is agent guidance, not enforced by this boolean. See [Configuration: Safety Policy](3-Configuration.md#safety-policy).

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

For non-loopback OAuth deployments, the issuer and public base URL must use HTTPS. TLS may terminate at a reverse proxy. Protected Resource Metadata is served at the RFC 9728 well-known location derived from the MCP resource path: for the resource `https://mcp.example.com/mcp` the metadata lives at `https://mcp.example.com/.well-known/oauth-protected-resource/mcp`.

### Connecting an authorization server

`ve mcp` acts only as a resource server. It **does not issue tokens and provides neither client registration nor a token endpoint**. Bring your own authorization server (self-hosted or an existing IdP) that meets these requirements:

1. Publish Authorization Server Metadata. On startup the server tries `/.well-known/oauth-authorization-server` derived from `{issuer}`, then `{issuer}/.well-known/openid-configuration`; startup fails if neither responds. The metadata `issuer` must match `--oauth-issuer` exactly and must include `jwks_uri`.
2. Publish a JWKS at `jwks_uri`. Only RSA and EC (P-256/P-384/P-521) signing keys are supported, and every JWK must carry a `kid`.
3. Issue JWT access tokens signed with `RS256/RS384/RS512` or `ES256/ES384/ES512` such that:
   - `iss` equals `--oauth-issuer`;
   - `aud` contains `--oauth-audience`;
   - `exp` is present (validated with 30 seconds of clock skew);
   - when `--oauth-scopes` is set, every requested scope appears in the token `scope` (space-delimited string) or `scp` (array); a single missing scope is rejected.

The grant type is up to the authorization server; `ve mcp` does not constrain it. Use `client_credentials` for service-to-service calls, or authorization code with PKCE for MCP clients acting on behalf of an end user. With `client_credentials`:

```shell
# 1. Obtain an access token; audience and scope must match the ve mcp configuration
TOKEN=$(curl -s -X POST https://auth.example.com/token \
  -u "$CLIENT_ID:$CLIENT_SECRET" \
  -d grant_type=client_credentials \
  -d audience=volcengine-mcp \
  -d scope=mcp:use | jq -r .access_token)

# 2. Call the MCP endpoint with the token
curl -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' \
  https://mcp.example.com/mcp
```

When authentication fails, the status code and `WWW-Authenticate` header identify the cause:

| Status | `error` | Typical cause |
| --- | --- | --- |
| 401 | `invalid_token` | Missing Bearer header, bad signature, `iss`/`aud` mismatch, or expired token |
| 403 | `insufficient_scope` | Valid token that lacks a scope required by `--oauth-scopes` |

Both responses carry `resource_metadata` so clients can discover the authorization server.

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

[← Advanced Usage](5-Advanced.md) | MCP Server[(中文)](6-MCP-zh.md) | [Proxy-CLI →](7-ProxyCLI.md)
