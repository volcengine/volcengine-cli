# Console Login 设备码登录设计

## 背景与目标

现有 `ve login` 支持两种 OAuth 2.0 Authorization Code + PKCE 流程：

- 默认模式：当前设备打开浏览器，通过本地 callback 接收授权码。
- `--remote`：远程主机不启动 callback，用户在其他设备完成登录并把授权码粘贴回终端。

设备码登录用于 SSH、容器和其他浏览器能力受限的环境。CLI 获取 `device_code` 与 `user_code` 后展示给用户，并自动轮询 Token 端点，不要求用户复制授权结果。

本次实现采用以下参数语义：

- `--use-device-code` 选择 OAuth 2.0 Device Authorization Grant。
- `--no-browser` 只控制设备码模式是否自动打开浏览器。
- `--remote` 保持原有跨设备授权码语义。

## 命令接口

```shell
# 授权码 + PKCE，本机 callback
ve login

# 授权码 + PKCE，跨设备粘贴授权码
ve login --remote

# 设备码，默认尝试打开浏览器，同时始终打印 URL 和用户码
ve login --use-device-code

# 设备码，不尝试打开浏览器
ve login --use-device-code --no-browser
```

参数约束：

- `--remote` 与 `--use-device-code` 互斥。
- `--no-browser` 必须与 `--use-device-code` 同时使用。
- 参数组合在网络请求、浏览器启动和配置写入前校验。
- `--endpoint-url` 继续覆盖整个 Signin 服务地址。
- `VOLCENGINE_LOGIN_HEADERS` 继续向 Signin OAuth POST 请求注入联调 Header。

## 协议

### 固定客户端配置

```text
client_id=trn:signin:::devtools/cross-device
scope=Console:All:All
device_info=Volcengine CLI
```

该客户端是公开客户端，不发送 `client_secret`。

### 发起设备认证

```http
POST {endpoint-url}/authorize/oauth/device_authorization
Content-Type: application/x-www-form-urlencoded

client_id=trn:signin:::devtools/cross-device
scope=Console:All:All
device_info=Volcengine+CLI
```

CLI 要求响应包含：

- `device_code`
- `user_code`
- `verification_uri`
- 正数 `expires_in`

`verification_uri_complete` 与 `interval` 为可选字段。`interval` 缺失或无效时默认 5 秒。

### 轮询 Token

```http
POST {endpoint-url}/authorize/oauth/token
Content-Type: application/x-www-form-urlencoded

client_id=trn:signin:::devtools/cross-device
scope=Console:All:All
grant_type=urn:ietf:params:oauth:grant-type:device_code
device_code=<device_code>
```

首次轮询前等待一个 interval。状态处理：

| OAuth 错误 | 行为 |
|-|-|
| `authorization_pending` | 保持当前 interval，继续轮询 |
| `slow_down` | 当前及后续 interval 增加 5 秒 |
| `access_denied` | 立即终止并提示用户拒绝授权 |
| `expired_token` | 立即终止并提示重新登录 |
| `invalid_device_code` | 立即终止并提示重新登录 |
| 其他 4xx | 返回原始 OAuth 错误、描述和 request ID |

408、429、5xx 与网络错误沿用 Console OAuth 客户端的有限重试。到达 `expires_in` 截止时间后停止轮询，不写缓存或 profile。

## 浏览器交互

设备认证响应成功后，终端始终打印：

- `verification_uri`
- `user_code`
- 有效期
- 可用时打印 `verification_uri_complete`

未传 `--no-browser` 时，CLI 优先打开 `verification_uri_complete`，否则打开 `verification_uri`。浏览器启动是 best-effort；启动失败只打印警告，登录流程继续。

终端信息不能依赖浏览器启动结果。当前 `util.OpenBrowser` 异步启动系统命令，只能判断启动命令是否成功，不能确认页面是否真正打开。

## 代码结构与兼容性

`ConsoleOAuthClient` 增加设备认证 URL、请求/响应类型和设备码 grant。设备认证与 Token 交换共用表单 POST 方法，从而统一：

- `application/x-www-form-urlencoded`
- `VOLCENGINE_LOGIN_HEADERS`
- OAuth 错误解析
- `X-Tt-Logid`
- 传输层重试

`ConsoleLogin` 在初始 Token 获取阶段选择授权码或设备码流程。成功后统一复用现有逻辑：

- 解析 STS `access_token`
- 从 `id_token.trn` 提取 `login_session`
- 确认是否替换已有 session
- 写入 `LoginTokenCache`
- 更新 `console-login` profile

设备码缓存继续保存 cross-device client ID、`Console:All:All`、Endpoint、access token、refresh token 和 ID token。不缓存 `device_code`、`user_code`，缓存格式无需迁移，旧缓存继续可读和刷新。

## 测试与联调

单元测试覆盖：

- 参数互斥和依赖关系。
- 设备认证及 Token 表单字段，不发送 `client_secret`。
- `VOLCENGINE_LOGIN_HEADERS` 同时注入两个 POST 请求。
- 浏览器默认打开、`--no-browser`、启动失败回退。
- `authorization_pending`、`slow_down`、拒绝、过期、无效设备码和超时。
- 登录成功后的 profile 与缓存字段。
- 默认授权码和 `--remote` 回归。
- 中英文消息目录完整性。

BOE 联调命令：

```shell
VOLCENGINE_LOGIN_HEADERS='x-tt-env=boe_device_code;x-real-ip=<test-ip>' \
ve login \
  --use-device-code \
  --no-browser \
  --endpoint-url https://signin-api-boe.byted.org \
  --profile device-code-boe \
  --region cn-beijing
```

登录后验证：

```shell
ve sts GetCallerIdentity --profile device-code-boe
```

接入文档标注 2026-08-15 可线上接入。正式发布前需要使用默认生产 Endpoint 完成一次设备认证、Token 获取、业务 API 调用和 refresh token 验证。
