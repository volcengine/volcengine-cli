[← 高级用法](5-Advanced-zh.md) | MCP 服务[(English)](6-MCP.md) | [Proxy-CLI →](7-ProxyCLI-zh.md)

---

## MCP 服务

`ve mcp` 通过 Model Context Protocol 暴露 API 检索、公开文档检索与读取、单次 API 调用，以及显式开启后的多 API 脚本编排能力，提供以下 Tool：

- `search_api`：API 发现的首选工具。按关键词或任务描述检索火山引擎官方公开 API 目录，返回精简记录，无需云账号凭证。详见 [API 检索](#api-检索)。
- `list_services` / `list_apis`：`search_api` 失败或没有找到合适结果时的兜底方式，分别列出 CLI 支持的全部服务（service code），以及某个服务支持的全部 API。
- `get_api_parameters`：查询请求参数、类型、必填项和示例。
- `call_api`：每次 Tool 调用执行一个 CLI 支持的 API。火山引擎 IAM 对云凭证进行权限判断；本地 CLI 安全策略会在解析凭证和发送请求之前执行检查。
- `run_script`（需显式开启，仅 stdio）：运行 Python 脚本编排多个 API 调用，支持循环、分支、分页、数据处理和有上限的并发。详见 [run_script](#run_script)。
- `search_docs`：搜索公开的火山引擎文档，可按 service code 过滤，并返回标题、URL 和正文摘要。该工具无需云账号凭证。
- `fetch_docs`：读取 `search_docs` 返回的火山引擎文档 URL，支持通过 Unicode 字符索引分段读取正文。该工具无需云账号凭证。

## 传输协议

默认使用 stdio：

```shell
ve mcp
ve mcp --profile prod
```

启动 Streamable HTTP：

```shell
ve mcp --transport streamable-http --host 127.0.0.1 --port 8888
```

端点为 `http://127.0.0.1:8888/mcp`。

启动兼容的 HTTP+SSE：

```shell
ve mcp --transport sse --host 127.0.0.1 --port 8888
```

SSE 和消息端点分别为 `/sse` 与 `/message`。`--base-url` 可以包含路径前缀，并用于生成返回给客户端的端点地址。

启用 OAuth 且未显式指定传输协议时，自动使用 Streamable HTTP：

```shell
ve mcp --oauth \
  --oauth-issuer https://auth.example.com \
  --oauth-audience volcengine-mcp \
  --base-url https://mcp.example.com
```

stdio 没有 HTTP Bearer 认证层，因此 `--oauth` 与 `--transport stdio` 同时使用会直接报错。

## 云凭证

每次 `call_api` 按以下顺序解析云凭证：

1. 显式指定的 `--profile`。
2. CLI 配置中的 current profile。
3. `VOLCENGINE_PROFILE` 或 `VOLCSTACK_PROFILE`。
4. 火山引擎 SDK 默认凭证链。
5. HTTP 请求的 `X-Volcengine-Credentials`。

每次调用前都会重新读取 CLI 配置，因此在 MCP 服务运行期间执行 `ve login`、切换 current profile 或更新 profile 后，不需要重启 MCP 服务，下一次 `call_api` 会使用最新配置。显式 `--profile` 固定的是 profile 名称，其登录态和凭证内容仍会热更新。

显式指定但不存在的 profile 会导致服务启动失败。stdio 无法接收 HTTP Header；没有 profile 或默认链凭证时，`call_api` 和 `run_script` 返回错误，但检索和文档 Tool 仍可使用。

凭证 Header 是一个标准 Base64 编码的 JSON 对象：

```json
{"AccessKeyId":"AK","SecretAccessKey":"SK","SessionToken":"可选的 STS Token"}
```

生成示例：

```shell
printf '%s' '{"AccessKeyId":"AK","SecretAccessKey":"SK"}' | base64
```

将结果放入 `X-Volcengine-Credentials`。该值仅对当前请求有效，不会缓存、记录到日志或写入环境变量。`Authorization: Bearer ...` 仅用于 OAuth Access Token。设计上不支持自动读取 `/var/run/secrets/iam/credential` 中的 veFaaS IAM 凭证。

## Region

MCP API 调用按以下顺序解析 Region：

1. `call_api` 的非必填 `region` 参数，或 `run_script` 中 `call_api(...)` 的 `region` 参数。
2. 启动参数 `--region`。
3. 选中 profile 的 Region。
4. `VOLCENGINE_REGION`。
5. `cn-beijing`。

与普通 CLI API 命令不同，MCP 调用在没有任何 Region 配置时使用 `cn-beijing`，不会报错。

<a id="安全策略"></a>

## 安全策略

每次 `call_api` 都会重新加载并检查 `~/.volcengine/safety-policy.json` 及其环境变量覆盖。命中 `deny` 时返回 Tool 错误，不发送云端请求；命中 `confirm` 时同样返回 Tool 错误，要求 Agent 说明操作并征求用户确认。用户明确同意后，使用 `yes: true` 重试同一个 Tool 调用：

```json
{
  "service": "ecs",
  "api": "UpdateInstance",
  "parameters": {"InstanceId": "i-xxxxx"},
  "yes": true
}
```

`yes` 为非必填参数，默认 `false`，只能跳过 `confirm`，不能绕过 `deny`。每次新操作先不传此参数，仅遇确认拦截才简要说明目标和影响，询问是否继续，然后结束当前轮次并等待用户的新回复。确认后由 Agent 内部传入 `yes: true`，按原参数重试一次，无需用户理解或操作此参数。重试后即丢弃批准，即使失败。仅限一次是 Agent 行为约束，不是布尔参数强制保证。详见[配置管理：安全策略](3-Configuration-zh.md#安全策略safety-policy)。

<a id="api-检索"></a>

## API 检索

`search_api` 以匿名 `GET` 请求调用官方公开接口 `https://api.volcengine.com/api/common/search/all-new`，参数为 `Query`、`Type=api`、`Limit` 和 `Offset`。不发送云凭证、签名、Cookie 或客户端 ID，也无需任何配置。请求超时时间为 15 秒。

- **参数**：`query`（必填，最多 1,000 个字符）、`limit`（1–50，默认 10；接口每页最多返回 50 条）和 `offset`（默认 0）。翻页时把返回的 `NextOffset` 作为下一次的 `offset`；最后一页不返回 `NextOffset`。接口会返回 `Total`，不支持按服务过滤。
- **输出**：为节省 token，返回紧凑 JSON：`{"Total":…,"NextOffset":…,"APIs":[{"action":…,"service":…,"version":…,"FullContent":…,"score":…}]}`。每条记录只包含这五个字段，原样取自官方结果行的 `BizInfo.Action`、`BizInfo.ServiceCode`、`BizInfo.Version`、`FullContent` 和 `Score`。目录可能返回同一 action 的多个版本，也可能返回本 CLI 不支持的 API；记录按官方结果原样返回。缺少 service code 或 action 的行会被跳过。
- **参数**：检索结果不是参数定义。请使用 `get_api_parameters` 查询本 CLI 发送的参数；`call_api` 始终使用 CLI 内置的服务版本，即 `list_apis` 和 `get_api_parameters` 所描述的版本。
- **错误**：接口错误（例如 HTTP 400 `InvalidParam`）会转为 Tool 错误，包含 HTTP 状态码、错误码、错误信息和 `RequestId`（或 `X-Tt-Logid` Header），并提示改用 `list_services` 和 `list_apis` 兜底。响应不是合法 JSON、缺少 `Result` 或超过 4 MiB 时视为协议错误。

<a id="run_script"></a>

## run_script

`run_script` 在独立的 CPython worker 进程中运行 Python 脚本。脚本调用火山引擎 API 的受支持方式是注入的 `call_api`：`call_api` 通过分帧管道协议向 CLI 发送结构化请求，CLI 对每个请求复用 `call_api` 的参数校验、凭证、签名和安全策略流程。不依赖 Python 云 SDK，也不会为每个 API 启动 `ve` 进程。这是脚本约定，而不是强制隔离（见下方信任边界）。

**信任边界。**在本地，脚本是以当前系统用户权限运行的受信任代码：可以读取文件、建立网络连接和启动进程。子进程、空工作目录、不含 `VOLCENGINE_*` 凭证的最小环境变量和 `python -I -S` 只能减少误用，**不是**沙箱。因此 `run_script` 默认关闭。只有指定 `--enable-run-script`（或 `VOLCENGINE_MCP_ENABLE_RUN_SCRIPT=true`）且使用 stdio 传输时才会注册，只提供给启动该服务的本地客户端。在 SSE 或 Streamable HTTP 下使用 `--enable-run-script` 会被拒绝，因为这会通过网络暴露本机代码执行能力。托管或多租户场景下的隔离执行需要真正的沙箱；CLI 定义了对应的执行器接口和协议，但不提供或部署沙箱服务。

```shell
ve script runtime install          # 仅当 PATH 中没有本地 CPython 3.9+ 时需要
ve mcp --enable-run-script
```

### 脚本约定

- CPython 3.9 及以上版本，仅限标准库。第三方包和 `pip` 不属于约定范围；CLI 管理的 Python 运行时为 CPython 3.12.15。支持顶层 `await`。
- `await call_api(service, api, parameters=None, region=None)` 调用一个 API 并返回响应对象。service code 和 action 与 `call_api` 相同，`parameters` 为 dict。
- 失败时抛出 `APIError`，包含 `.code`、`.status`、`.request_id` 和 `.seq`。`.status` 为 `rejected`（未发送）、`failed`（API 返回错误）或 `unknown`（请求可能已到达 API，例如单次调用超时或取消后）。
- 全局变量 `args` 为 Tool 的 `args` 输入。将可 JSON 序列化的值赋给全局变量 `result`。`print()` 和 stderr 输出会被收集到 `output`（默认 64 KiB，截断时会标记）。输出不会破坏协议：worker 会把协议迁移到私有文件描述符，并把 stdout 指向输出流。
- 使用 `asyncio.gather` 实现并发。同时进行中的调用最多为 `max_concurrency` 个，超出的调用会排队等待。

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

### 限制

| 参数 | 默认值 | 最大值 | 行为 |
| --- | --- | --- | --- |
| `max_calls` | 100 | 1000 | 超出预算的第一个调用以 `CallLimitExceeded` 拒绝；之后的任何调用都会结束运行，状态为 `limit_exceeded`。调用日志最多 `max_calls + 1` 条。 |
| `max_concurrency` | 4 | 16 | 同时进行中的调用数。 |
| `call_timeout_seconds` | 30 | 300 | 超时未返回的调用记为 `unknown`（`CallTimeout`），脚本继续执行。 |
| `timeout_seconds` | 120 | 900 | 总时长上限，超时后运行结束，状态为 `timed_out`。 |

代码上限 256 KiB，`result` 上限 1 MiB，每个协议帧上限 4 MiB。成功的 API 响应如果超过单帧上限，调用日志记为 `ok`，但交付给脚本的是 `ResponseTooLarge` 错误。

### 结果与调用日志

结果包含 `status`（`succeeded`、`failed`、`cancelled`、`timed_out`、`aborted` 或 `limit_exceeded`）、`result`、`error`（类型、信息和脚本自身的 traceback）、`output`、`calls`、`summary`、`binding`、`runtime` 和 `trust`。`succeeded` 以外的状态均以 Tool 错误返回，同时附带完整报告。`calls` 按发起顺序记录每个 API 调用的状态（`ok`、`failed`、`rejected` 或 `unknown`）、错误码、错误信息和 `RequestId`。失败、取消或超时之前已完成的调用会保留在日志中。**不要重新运行已部分执行的脚本**，应基于调用日志继续处理。

### 安全策略

脚本发起的每个 API 调用都会重新加载并评估安全策略，运行期间新增的规则会作用于下一个调用。`deny` 规则以 `SafetyDenied` 拒绝该调用。`confirm` 规则以 `SafetyConfirmationRequired` 拒绝该调用。脚本没有批准输入，脚本也刻意不遵循 `VOLCENGINE_SAFETY_SKIP_CONFIRM`。允许的 API（包括写操作）正常执行。如需执行受 `confirm` 规则保护的操作，Agent 应说明目标和影响，等待用户的新回复批准，然后仅对该操作使用 `call_api` 并设置 `yes: true` 执行一次。目前不支持在运行中的脚本内进行可恢复的逐操作审批。

### 身份绑定

运行开始时，会绑定解析出的 profile、凭证来源（`profile`、`default-chain` 或 `request-header`）、凭证身份和默认 Region（以 `binding` 返回）。每个 API 调用都按 `call_api` 的方式重新解析凭证，且必须与绑定一致。支持以下同一身份的凭证刷新：

- SSO profile：绑定 SSO session、账号和角色，CLI 刷新临时 STS 密钥；
- Console Login profile：绑定 login session 和账号，登录缓存刷新；
- AK profile：绑定 AccessKey，SecretKey 或 SessionToken 可以变化；
- 角色类 profile（`ramrolearn`、`oidc`、`ecsrole`）：绑定源 AccessKey 和角色目标（`RoleTrn`、`AccountId`、`RoleName`、OIDC token 文件）。

默认凭证链和请求 Header 凭证按 AccessKey ID 绑定。本地无法证明轮换后的密钥属于同一主体，因此密钥轮换按身份变化处理。任何身份变化都会在发送前以 `IdentityChanged` 拒绝该调用，并结束运行，状态为 `aborted`。未指定 `region` 的调用使用绑定的默认 Region，即使运行期间 profile 的 Region 发生变化。

### 取消

使用 stdio 时，针对 `run_script` 请求的 MCP `notifications/cancelled` 会取消运行：进行中的 HTTP 请求被中止并记为 `unknown`，worker 获得短暂的宽限时间后，其整个进程树会被终止（Unix 使用进程组，Windows 使用 Job Object）。服务关闭和运行自身的时间限制同样会触发取消。本地 `run_script` 不在网络传输中提供。Streamable HTTP 无状态模式没有会话，客户端的取消通知无法送达正在执行的请求；未来的托管适配器需要提供自己的取消通道。

### Python 运行时

`run_script` 在 MCP 启动时和运行期间都不会下载任何内容。解释器按以下顺序选择：

1. 显式指定的解释器：`--script-python`（`ve mcp`）或 `--python`（`ve script run` 和 `ve script runtime status`），其次是 `VOLCENGINE_SCRIPT_PYTHON`。显式指定的解释器无效时直接报错，不会回退。
2. `PATH` 中的本地 CPython 3.9+：先 `python3` 后 `python`（Windows 上先 `python.exe` 后 `python3.exe`），按 `PATH` 顺序使用 worker 的 `-I -S -B` 参数校验。不可用、版本过低或非 CPython 的候选会被跳过。空的和相对的 `PATH` 条目以及 Microsoft Store 的 `python.exe` 别名不会被使用。
3. 通过 `ve script runtime install` 安装的 ve 管理的运行时。

都不可用时，错误信息会列出每个本地候选被拒绝的原因。每次结果的 `runtime` 字段会给出选中的 `path`、`version` 和 `source`（`explicit`、`system` 或 `managed`）。使用 `ve mcp --enable-run-script` 时，解释器在启动时选择并校验一次；没有可用解释器时服务无法启动。之后每次 `run_script` 调用都复用该解释器，即使 `PATH` 或 ve 管理的运行时随后发生变化。如果该解释器被删除，调用会报错，需要重启服务。未指定 `--enable-run-script` 时不会查找 Python。`ve script run` 在运行前选择一次解释器。

- `ve script runtime install` 下载 `ve` 内置清单中固定的 CPython 3.12.15 `install_only_stripped` 包，清单来源为 [python-build-standalone 发布版本 20261013](https://github.com/astral-sh/python-build-standalone/releases/tag/20261013)。解压前会校验包的大小和 SHA-256，解压时检查路径、符号链接和大小，解压到 `~/.volcengine/script-runtime/`，并在启用前校验解释器。已固定的平台为 darwin-arm64、darwin-amd64、linux-arm64（glibc）、linux-amd64（glibc）和 windows-amd64。其他平台（包括基于 musl 的 Linux）会明确报错，请改用指定的解释器。`--manifest <https 地址|文件>`（可选 `--manifest-sha256`）可以从格式相同的其他清单安装；制品地址必须为 HTTPS。
- 本地和显式指定的解释器会被校验，但版本不固定。ve 管理的运行时固定为 CPython 3.12.15。
- `ve script runtime status` 显示将被选中的解释器及其来源。

### 命令行

`ve script run --file script.py [--args '{"key":"value"}']` 在不经过 MCP 的情况下按相同约定运行脚本，并输出 JSON 报告。支持 `--profile`、`--region`、`--python`、`--max-calls`、`--max-concurrency`、`--timeout-seconds` 和 `--call-timeout-seconds`；状态不是 `succeeded` 时以非零码退出。由于它会执行本地代码，环境中必须设置 `VOLCENGINE_SCRIPT_RUN_ENABLED=true`。只用命令行参数不够：仅被允许执行 `ve` 命令的 Agent 可以自行添加参数。Ctrl-C 会像 MCP 取消一样取消运行。

### 托管执行约定

执行器可以替换。托管沙箱适配器实现相同的执行器接口：在沙箱中运行 bootstrap worker，并把它的帧转发给 CLI broker，凭证、策略和调用日志都保留在沙箱之外。帧格式为 4 字节大端长度加一个 UTF-8 JSON 对象（协议版本 1）：

- broker → worker：`start`（`code`、`args`）、`result`（`id`、`seq`、`ok`、`data` 或 `error`）、`cancel`；
- worker → broker：`call`（`id`、`service`、`api`、`region`、`parameters`）、`done`（`result`）、`error`（`type`、`message`、`traceback`）。

用户代码只会在收到 `start` 之后运行。

## OAuth Resource Server

OAuth 只控制谁可以访问 MCP，不提供火山引擎云凭证。服务实现了 Bearer 认证、OAuth Authorization Server Metadata 发现、Protected Resource Metadata、基于授权服务器 JWKS 的 JWT 验签，以及 issuer、audience、过期时间和 scope 校验。

```shell
ve mcp --oauth \
  --oauth-issuer https://auth.example.com \
  --oauth-audience volcengine-mcp \
  --oauth-scopes mcp:use \
  --host 0.0.0.0 \
  --port 8888 \
  --base-url https://mcp.example.com
```

非回环地址部署 OAuth 时，issuer 和公开 base URL 必须使用 HTTPS；TLS 可以由反向代理终止。Protected Resource Metadata 按 RFC 9728 使用由 MCP Resource 路径派生的 well-known 地址：Resource 为 `https://mcp.example.com/mcp` 时，元数据地址是 `https://mcp.example.com/.well-known/oauth-protected-resource/mcp`。

### 接入授权服务器

`ve mcp` 只承担 Resource Server 的职责，**不签发 Token，也不提供客户端注册和 Token Endpoint**。授权服务器需要自行准备（自建，或使用已有 IdP），并满足以下要求：

1. 暴露 Authorization Server Metadata。启动时会依次尝试 `{issuer}` 派生的 `/.well-known/oauth-authorization-server` 和 `{issuer}/.well-known/openid-configuration`，两者都取不到即启动失败。元数据中的 `issuer` 必须与 `--oauth-issuer` 完全一致，且必须包含 `jwks_uri`。
2. 通过 `jwks_uri` 暴露 JWKS。仅支持 RSA 与 EC（P-256/P-384/P-521）签名密钥，且每个 JWK 必须带 `kid`。
3. 签发 JWT 格式的 Access Token，算法为 `RS256/RS384/RS512` 或 `ES256/ES384/ES512`，并满足：
   - `iss` 等于 `--oauth-issuer`；
   - `aud` 包含 `--oauth-audience`；
   - `exp` 必填（校验允许 30 秒时钟偏差）；
   - 若配置了 `--oauth-scopes`，其中每一项都必须出现在 Token 的 `scope`（空格分隔字符串）或 `scp`（数组）中，缺一即拒绝。

Grant type 由授权服务器决定，`ve mcp` 不作限制：服务间调用可用 `client_credentials`，代表最终用户的 MCP 客户端可用 authorization code + PKCE。以 `client_credentials` 为例：

```shell
# 1. 从授权服务器换取 Access Token，audience 与 scope 必须和 ve mcp 的配置对齐
TOKEN=$(curl -s -X POST https://auth.example.com/token \
  -u "$CLIENT_ID:$CLIENT_SECRET" \
  -d grant_type=client_credentials \
  -d audience=volcengine-mcp \
  -d scope=mcp:use | jq -r .access_token)

# 2. 携带 Token 访问 MCP 端点
curl -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' \
  https://mcp.example.com/mcp
```

排查认证失败时，响应状态码和 `WWW-Authenticate` 头可以区分原因：

| 状态码 | `error` | 常见原因 |
| --- | --- | --- |
| 401 | `invalid_token` | 缺少 Bearer 头、签名验不过、`iss`/`aud` 不匹配、Token 已过期 |
| 403 | `insufficient_scope` | Token 有效但缺少 `--oauth-scopes` 要求的 scope |

两种响应都会带上 `resource_metadata`，客户端可据此发现授权服务器地址。

支持的环境变量：

```text
VOLCENGINE_MCP_TRANSPORT
VOLCENGINE_MCP_HOST
VOLCENGINE_MCP_PORT
VOLCENGINE_MCP_BASE_URL
VOLCENGINE_MCP_OAUTH
VOLCENGINE_MCP_OAUTH_ISSUER
VOLCENGINE_MCP_OAUTH_AUDIENCE
VOLCENGINE_MCP_OAUTH_SCOPES
VOLCENGINE_MCP_ENABLE_RUN_SCRIPT
VOLCENGINE_SCRIPT_PYTHON
VOLCENGINE_REGION
```

HTTP 服务默认只监听 `127.0.0.1:8888`。只有在正确部署认证和 TLS 后才应对外暴露。

---

[← 高级用法](5-Advanced-zh.md) | MCP 服务[(English)](6-MCP.md) | [Proxy-CLI →](7-ProxyCLI-zh.md)
