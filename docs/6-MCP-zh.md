[← 高级用法](5-Advanced-zh.md) | MCP 服务[(English)](6-MCP.md)

---

## MCP 服务

`ve mcp` 通过 Model Context Protocol 暴露 CLI 的服务元数据和 API 调用能力，提供四个 Tool：

- `list_services`：列出 CLI 支持的全部服务及其 service code。
- `list_apis`：传入 service code，列出该服务支持的全部 API。
- `get_api_parameters`：查询请求参数、类型、必填项和示例。
- `call_api`：调用 CLI 支持的任意 API。MCP 服务不维护 API 白名单，最终由请求使用的火山引擎 IAM 凭证进行权限判断。

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

显式指定但不存在的 profile 会导致服务启动失败。stdio 无法接收 HTTP Header；没有 profile 或默认链凭证时，`call_api` 返回错误，但三个元数据 Tool 仍可使用。

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

1. 请求 Header `X-Volcengine-Region`。
2. 启动参数 `--region`。
3. 选中 profile 的 Region。
4. `VOLCENGINE_REGION`。
5. `cn-beijing`。

与普通 CLI API 命令不同，MCP 调用在没有任何 Region 配置时使用 `cn-beijing`，不会报错。

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

非回环地址部署 OAuth 时，issuer 和公开 base URL 必须使用 HTTPS；TLS 可以由反向代理终止。Protected Resource Metadata 按 RFC 9728 使用由 MCP Resource 路径派生的 well-known 地址。

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
VOLCENGINE_REGION
```

HTTP 服务默认只监听 `127.0.0.1:8888`。只有在正确部署认证和 TLS 后才应对外暴露。

---

[← 高级用法](5-Advanced-zh.md) | MCP 服务[(English)](6-MCP.md)
