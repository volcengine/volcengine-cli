[← Advanced Usage](5-Advanced.md) | MCP Server[(中文)](6-MCP-zh.md) | [Proxy-CLI →](7-ProxyCLI.md)

---

## MCP Server

`ve mcp` exposes API discovery, public documentation search and retrieval, single API calls and, when explicitly enabled, multi-API scripts through the Model Context Protocol. It provides these tools:

- `search_api`: the first step for API discovery. Searches the official public Volcengine API catalog by keywords or task and returns compact records. No cloud credentials are needed. See [API search](#api-search).
- `list_services` / `list_apis`: fallback discovery when `search_api` fails or finds nothing useful. They list every service code supported by the CLI and every supported API of one service.
- `get_api_parameters`: inspect request parameters, types, required fields, and an example.
- `call_api`: call one API supported by the CLI per tool call. Volcengine IAM evaluates the cloud credential, and the local CLI safety policy is checked before credentials are resolved or a request is sent.
- `run_script` (opt-in, stdio only): run a Python script that orchestrates several API calls with loops, branches, pagination, data processing and bounded concurrency. See [run_script](#run_script).
- `search_docs`: search public Volcengine documentation, optionally filter by service code, and return titles, URLs, and text snippets. This tool does not require cloud credentials.
- `fetch_docs`: fetch a Volcengine documentation URL returned by `search_docs`, with Unicode character offsets for paginated reading. This tool does not require cloud credentials.

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

An explicitly named profile that does not exist prevents the server from starting. stdio cannot receive HTTP headers; if no profile or default-chain credential is available, `call_api` and `run_script` return an error while the discovery and documentation tools remain usable.

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

1. Optional `region` argument of `call_api`, or of `call_api(...)` inside `run_script`.
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

<a id="api-search"></a>

## API Search

`search_api` calls the official public endpoint `https://api.volcengine.com/api/common/search/all-new` with an anonymous `GET` and the parameters `Query`, `Type=api`, `Limit` and `Offset`. It sends no cloud credentials, signature, cookies or client ID, and needs no configuration. Requests time out after 15 seconds.

- **Arguments:** `query` (required, up to 1,000 characters), `limit` (1–50, default 10; the endpoint returns at most 50 records per page) and `offset` (default 0). For the next page, pass `NextOffset` as `offset`; it is absent on the last page. The endpoint reports `Total` and does not filter by service.
- **Output:** compact JSON, to save tokens: `{"Total":…,"NextOffset":…,"APIs":[{"action":…,"service":…,"version":…,"FullContent":…,"score":…}]}`. Each record has exactly these five fields, taken unchanged from the official row: `BizInfo.Action`, `BizInfo.ServiceCode`, `BizInfo.Version`, `FullContent` and `Score`. The catalog can return several versions of an action and APIs this CLI does not support; records are returned as published. Rows without a service code and action are skipped.
- **Parameters:** search results are not parameter definitions. Use `get_api_parameters` for the parameters this CLI sends; `call_api` always uses the CLI's built-in version of a service, which `list_apis` and `get_api_parameters` describe.
- **Errors:** an endpoint error (for example HTTP 400 `InvalidParam`) becomes a tool error with the HTTP status, error code, message and `RequestId` (or the `X-Tt-Logid` header), and suggests falling back to `list_services` and `list_apis`. Invalid JSON, a missing `Result` and responses larger than 4 MiB are protocol errors.

<a id="run_script"></a>

## run_script

`run_script` runs a Python script in a separate CPython worker process. The supported way for a script to call Volcengine APIs is the injected `call_api`, which sends structured requests to the CLI over a framed pipe protocol. This is the script contract, not enforced isolation (see the trust boundary below). The CLI executes each request through the same validation, credential, signing and safety-policy path as `call_api`. No Python cloud SDK is used and no `ve` process is started per API.

**Trust boundary.** Locally, the script is trusted code that runs with your OS user's privileges: it can read files, open network connections and start processes. A subprocess, an empty working directory, a minimal environment without `VOLCENGINE_*` credentials and `python -I -S` reduce accidents; they are **not** a sandbox. For this reason `run_script` is disabled by default. It is registered only with `--enable-run-script` (or `VOLCENGINE_MCP_ENABLE_RUN_SCRIPT=true`), and only with the stdio transport, so it is offered only to the local client that started the server. `ve mcp` refuses `--enable-run-script` with SSE or Streamable HTTP, because those would expose host code execution over the network. Isolated execution for hosted or multi-tenant servers needs a real sandbox; the CLI defines the executor interface and protocol for one but does not provide or deploy it.

```shell
ve script runtime install          # only if no local CPython 3.9+ is on PATH
ve mcp --enable-run-script
```

### Script contract

- CPython 3.9 or newer, standard library only. Third-party packages and `pip` are not part of the contract; the ve-managed runtime is CPython 3.12.15. Top-level `await` is allowed.
- `await call_api(service, api, parameters=None, region=None)` calls one API and returns the full response object; the API data is under `response["Result"]`, for example `response["Result"]["Vpcs"]`. Use the same service codes and actions as `call_api`; `parameters` is a dict.
- Failures raise `APIError` with `.code`, `.status`, `.request_id` and `.seq`. `.status` is `rejected` (not sent), `failed` (the API returned an error) or `unknown` (the request may have reached the API, for example after a per-call timeout or cancellation).
- `args` holds the tool's `args` input. Assign a JSON-serializable value to the global `result`. `print()` and stderr output are captured into `output` (64 KiB by default; truncation is flagged). Output cannot corrupt the protocol: the worker moves the protocol to private file descriptors and points stdout at the output stream.
- Use `asyncio.gather` for concurrency. At most `max_concurrency` calls are in flight; extra calls wait.

```python
import asyncio

instances, token = [], ""
while True:
    page = await call_api("ecs", "DescribeInstances", {"MaxResults": 100, "NextToken": token})
    instances += page["Result"]["Instances"]
    token = page["Result"].get("NextToken", "")
    if not token:
        break

types = sorted({i["InstanceTypeId"] for i in instances})
details = await asyncio.gather(*[call_api("ecs", "DescribeInstanceTypes", {"InstanceTypeIds.1": t}) for t in types])
result = {"instances": len(instances), "types": len(details)}
```

### Limits

| Argument | Default | Maximum | Behavior |
| --- | --- | --- | --- |
| `max_calls` | 100 | 1000 | The first call beyond the budget is rejected with `CallLimitExceeded`; any further call ends the run with status `limit_exceeded`. The journal never exceeds `max_calls + 1` entries. |
| `max_concurrency` | 4 | 16 | Calls in flight at once. |
| `call_timeout_seconds` | 30 | 300 | A call without a response in time is `unknown` with `CallTimeout`; the script continues. |
| `timeout_seconds` | 120 | 900 | Wall-clock limit; the run ends with status `timed_out`. |

The code is limited to 256 KiB, the `result` to 1 MiB and each protocol frame to 4 MiB. A successful API response that does not fit in a frame is journaled as `ok` but delivered to the script as a `ResponseTooLarge` error.

### Result and journal

The result has `status` (`succeeded`, `failed`, `cancelled`, `timed_out`, `aborted` or `limit_exceeded`), `result`, `error` (type, message and the script's traceback), `output`, `calls`, `summary`, `binding`, `runtime` and `trust`. Any status other than `succeeded` is returned as a tool error with the full report. `calls` lists every API call in issue order with its status (`ok`, `failed`, `rejected` or `unknown`), error code and message, and `RequestId`. Calls that completed before a failure, cancellation or timeout stay in the journal. **Never rerun a partially executed script;** continue from the journal instead. The one exception is the whole-script rerun the user approves after a `SafetyConfirmationRequired` stop (see below). Once a run stops, no new call is sent: calls still waiting for a `max_concurrency` slot are `rejected`, and calls in flight make no new attempt.

### Safety policy

Every API call made by a script reloads and evaluates the safety policy, so a rule added while the script runs applies to its next call. A `deny` rule rejects the call with `SafetyDenied`. Allowed APIs, including writes, run normally. A script is approved as a whole: without approval, the first call that a `confirm` rule protects is not sent and stops the run (status `aborted`, error code `SafetyConfirmationRequired`), even if the script catches `APIError`. The agent explains what the whole script does, waits for a new user reply approving it, and runs the same script again unchanged with `yes: true` (`--yes` for `ve script run`). That run sends every call that `confirm` rules protect; `deny` rules still apply, and the approval is discarded after the run. Performing the protected operations one by one with `call_api` instead could repeat operations the script already did. Calls in the journal with status `ok` or `unknown` (those that `allow` rules permitted before the stop) may already have changed resources and run again in the approved run, so the agent tells the user about them. `VOLCENGINE_SAFETY_SKIP_CONFIRM` is deliberately not honored for scripts.

### Identity binding

At start, a run binds to the resolved profile, credential source (`profile`, `default-chain` or `request-header`), credential identity and default region (reported as `binding`). Every API call re-resolves credentials exactly as `call_api` does and must match the binding. Same-identity refresh is supported:

- SSO profiles: the SSO session, account and role are bound, and the CLI refreshes the temporary STS keys;
- Console Login profiles: the login session and account are bound, and the login cache refreshes;
- AK profiles: the access key is bound, and the secret key or session token may change;
- role-assuming profiles (`ramrolearn`, `oidc`, `ecsrole`): the source access key and the role target (`RoleTrn`, `AccountId`, `RoleName`, OIDC token file) are bound.

Default-chain and request-header credentials are bound by access key ID. A rotated key cannot be proven to belong to the same principal locally, so it is treated as an identity change. Any identity change rejects the call with `IdentityChanged` before it is sent and ends the run with status `aborted`. A call without a `region` uses the bound default region, even if the profile's region changes during the run.

### Cancellation

With stdio, an MCP `notifications/cancelled` for the `run_script` request cancels the run: in-flight HTTP requests are aborted and journaled as `unknown`, the worker gets a short grace period, then its whole process tree is killed (process group on Unix, Job Object on Windows). Cancellation also follows server shutdown and the run's own time limits. Local `run_script` is not available over the network transports. In stateless Streamable HTTP mode there are no sessions, so a client cancellation notification cannot reach a running request; future hosted adapters must provide their own cancellation path.

### Python runtime

`run_script` never downloads anything, neither at MCP startup nor while running. The interpreter is selected in this order:

1. An explicit interpreter: `--script-python` (`ve mcp`) or `--python` (`ve script run` and `ve script runtime status`), otherwise `VOLCENGINE_SCRIPT_PYTHON`. An invalid explicit interpreter is an error; it never falls back.
2. A local CPython 3.9+ found on `PATH`: `python3`, then `python` (`python.exe`, then `python3.exe` on Windows), checked in `PATH` order with the worker's `-I -S -B` flags. Unusable, older or non-CPython candidates are skipped. Empty and relative `PATH` entries and the Microsoft Store `python.exe` alias are never used.
3. The ve-managed runtime installed by `ve script runtime install`.

If none is usable, the error lists why each local candidate was rejected. The `runtime` field of each result reports the selected `path`, `version` and `source` (`explicit`, `system` or `managed`). With `ve mcp --enable-run-script` the interpreter is selected and validated once at startup; the server fails to start when none is usable. Every `run_script` call reuses that interpreter, even if `PATH` or the managed runtime changes later. If the interpreter is removed, the call fails and the server must be restarted. Without `--enable-run-script` no Python is looked up. `ve script run` selects the interpreter once before running.

- `ve script runtime install` downloads the CPython 3.12.15 `install_only_stripped` archive pinned in the manifest built into `ve`. The manifest is sourced from [python-build-standalone release 20261013](https://github.com/astral-sh/python-build-standalone/releases/tag/20261013). The archive's size and SHA-256 are verified before extraction. The archive is extracted with path, symlink and size checks into `~/.volcengine/script-runtime/`, and the interpreter is validated before activation. Pinned platforms are darwin-arm64, darwin-amd64, linux-arm64 (glibc), linux-amd64 (glibc) and windows-amd64. Other platforms, including musl-based Linux, fail explicitly; use an explicit interpreter there. `--manifest <https-url|file>` (optionally with `--manifest-sha256`) installs from another manifest with the same format; artifact URLs must be HTTPS.
- Local and explicit interpreters are validated but not version-pinned. The managed runtime is pinned to CPython 3.12.15.
- `ve script runtime status` shows the interpreter that would be selected and its source.

### Command line

`ve script run --file script.py [--args '{"key":"value"}' | --args-file args.json]` runs the same contract without MCP and prints the JSON report. It accepts `--profile`, `--region`, `--python`, `--max-calls`, `--max-concurrency`, `--timeout-seconds`, `--call-timeout-seconds` and `--yes` (approves the whole script, like `yes: true`), and exits non-zero unless the status is `succeeded`. Because it executes local code, it requires `VOLCENGINE_SCRIPT_RUN_ENABLED=true` in the environment. A flag would not be enough: an agent that is only allowed to run `ve` commands could add a flag itself. Ctrl-C cancels the run like an MCP cancellation.

### Hosted execution contract

The executor is replaceable. A hosted sandbox adapter implements the same executor interface: it runs the bootstrap worker inside the sandbox and relays its frames to the CLI broker, which keeps credentials, policy and the journal outside the sandbox. Frames are a 4-byte big-endian length followed by a UTF-8 JSON object (protocol version 1):

- broker → worker: `start` (`code`, `args`), `result` (`id`, `seq`, `ok`, `data` or `error`), `cancel`;
- worker → broker: `call` (`id`, `service`, `api`, `region`, `parameters`), `done` (`result`), `error` (`type`, `message`, `traceback`).

User code runs only after `start`.

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

For non-loopback OAuth deployments, the issuer and public base URL must use HTTPS (see [Plain HTTP for development](#plain-http-for-development)). TLS may terminate at a reverse proxy. Protected Resource Metadata is served at the RFC 9728 well-known location derived from the MCP resource path: for the resource `https://mcp.example.com/mcp` the metadata lives at `https://mcp.example.com/.well-known/oauth-protected-resource/mcp`.

### Connecting an authorization server

By default `ve mcp` acts only as a resource server. It **does not issue tokens and provides neither client registration nor a token endpoint** (except in [OAuth proxy](#oauth-proxy-for-a-fixed-oauth-application) mode). Bring your own authorization server (self-hosted or an existing IdP) that meets these requirements:

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

### Authorization server metadata outside the issuer

Some authorization servers serve their metadata on a different host than the token issuer and register clients dynamically, so each client's tokens carry a different audience. `https://ecs.mcp.volcbiz.com` is one: its metadata lives there, while its tokens are issued by `https://signin.volcengine.com`. Point `ve mcp` at the metadata host with `--oauth-authorization-server`:

```shell
ve mcp --transport streamable-http --oauth \
  --oauth-authorization-server https://ecs.mcp.volcbiz.com \
  --host 0.0.0.0 --port 8888 --base-url https://mcp.example.com
```

The server discovers the metadata from `--oauth-authorization-server` and lists it in Protected Resource Metadata, so MCP clients find its `registration_endpoint` and register themselves. `--oauth-issuer` defaults to the metadata `issuer` (if set, it must match). `--oauth-audience` is optional in this mode: without it the audience is not checked, because it differs per registered client. Signature, `iss`, `exp` and `--oauth-scopes` are still checked.

### OAuth proxy for a fixed OAuth application

OAuth applications with one fixed client, such as GitHub or a company SSO, offer neither dynamic client registration nor locally verifiable tokens. With `--oauth-upstream-*`, `ve mcp` acts as the authorization server MCP clients see and proxies the login to that application:

```shell
export VOLCENGINE_MCP_OAUTH_UPSTREAM_CLIENT_SECRET='replace-me'
ve mcp --transport streamable-http --oauth \
  --oauth-upstream-authorize-url https://github.com/login/oauth/authorize \
  --oauth-upstream-token-url https://github.com/login/oauth/access_token \
  --oauth-upstream-userinfo-url https://api.github.com/user \
  --oauth-upstream-client-id <client-id> \
  --oauth-upstream-scopes read:user \
  --host 0.0.0.0 --port 8888 --base-url https://mcp.example.com
```

| Flag | Environment variable | Description |
| --- | --- | --- |
| `--oauth-upstream-authorize-url` | `VOLCENGINE_MCP_OAUTH_UPSTREAM_AUTHORIZE_URL` | Authorize URL of the upstream application (required) |
| `--oauth-upstream-token-url` | `VOLCENGINE_MCP_OAUTH_UPSTREAM_TOKEN_URL` | Token URL of the upstream application (required) |
| `--oauth-upstream-userinfo-url` | `VOLCENGINE_MCP_OAUTH_UPSTREAM_USERINFO_URL` | Userinfo URL that validates access tokens (required) |
| `--oauth-upstream-client-id` | `VOLCENGINE_MCP_OAUTH_UPSTREAM_CLIENT_ID` | Client ID of the upstream application (required) |
| - | `VOLCENGINE_MCP_OAUTH_UPSTREAM_CLIENT_SECRET` | Client secret; never passed as a flag |
| `--oauth-upstream-client-secret-file` | `VOLCENGINE_MCP_OAUTH_UPSTREAM_CLIENT_SECRET_FILE` | File holding the client secret, instead of the variable above |
| `--oauth-upstream-scopes` | `VOLCENGINE_MCP_OAUTH_UPSTREAM_SCOPES` | Scopes requested from the upstream application |
| `--oauth-upstream-token-auth-method` | `VOLCENGINE_MCP_OAUTH_UPSTREAM_TOKEN_AUTH_METHOD` | `client_secret_post` (default) or `client_secret_basic` |
| `--oauth-redirect-uris` | `VOLCENGINE_MCP_OAUTH_REDIRECT_URIS` | Client redirect URIs allowed besides loopback ones (exact match) |
| - | `VOLCENGINE_MCP_OAUTH_STATE_KEY` | Base64 32-byte key that seals state and codes; set the same key on every instance behind a load balancer |

Register `{base-url}/auth/oauth/callback` as the callback URL of the upstream application. The server publishes RFC 8414 metadata and these endpoints under the base URL:

- `/auth/oauth/register`: dynamic client registration for public clients. Registration only checks the redirect URIs: loopback `http(s)` URIs are always allowed, others only when listed in `--oauth-redirect-uris`.
- `/auth/oauth/authorize`: requires PKCE (`S256`), keeps the client's `state`, and redirects to the upstream with the proxy callback.
- `/auth/oauth/callback`: receives the upstream code and returns a sealed code to the client.
- `/auth/oauth/token`: checks the PKCE verifier and redirect URI, then exchanges the code (or a `refresh_token`) at the upstream with the client secret, and returns the upstream access token.

State and codes are encrypted with the state key, so the proxy keeps no login state. Without `VOLCENGINE_MCP_OAUTH_STATE_KEY` each process uses a random key, which invalidates logins in progress on restart. An access token is accepted when the upstream userinfo endpoint accepts it; the answer is cached for 5 minutes, so a revoked token may keep working that long. The proxy cannot be combined with `--oauth-issuer`, `--oauth-authorization-server`, `--oauth-audience` or `--oauth-scopes`.

### Plain HTTP for development

OAuth requires an HTTPS `--base-url` unless it is loopback. For a development host without TLS, `--oauth-allow-insecure-base-url` (or `VOLCENGINE_MCP_OAUTH_ALLOW_INSECURE_BASE_URL=true`) accepts an `http://` base URL and prints a warning on startup. Access tokens then travel unencrypted; do not use it in production.

Protected Resource Metadata and the OAuth proxy endpoints allow cross-origin requests without credentials, so browser-based MCP clients can discover and log in.

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
VOLCENGINE_MCP_OAUTH_AUTHORIZATION_SERVER
VOLCENGINE_MCP_OAUTH_UPSTREAM_AUTHORIZE_URL
VOLCENGINE_MCP_OAUTH_UPSTREAM_TOKEN_URL
VOLCENGINE_MCP_OAUTH_UPSTREAM_USERINFO_URL
VOLCENGINE_MCP_OAUTH_UPSTREAM_CLIENT_ID
VOLCENGINE_MCP_OAUTH_UPSTREAM_CLIENT_SECRET
VOLCENGINE_MCP_OAUTH_UPSTREAM_CLIENT_SECRET_FILE
VOLCENGINE_MCP_OAUTH_UPSTREAM_SCOPES
VOLCENGINE_MCP_OAUTH_UPSTREAM_TOKEN_AUTH_METHOD
VOLCENGINE_MCP_OAUTH_REDIRECT_URIS
VOLCENGINE_MCP_OAUTH_STATE_KEY
VOLCENGINE_MCP_OAUTH_ALLOW_INSECURE_BASE_URL
VOLCENGINE_MCP_ENABLE_RUN_SCRIPT
VOLCENGINE_SCRIPT_PYTHON
VOLCENGINE_REGION
```

The HTTP server binds to `127.0.0.1:8888` by default. Expose it beyond loopback only with an appropriate authentication and TLS deployment.

---

[← Advanced Usage](5-Advanced.md) | MCP Server[(中文)](6-MCP-zh.md) | [Proxy-CLI →](7-ProxyCLI.md)
