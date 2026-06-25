[← 入门](1-GettingStarted-zh.md) | 认证与登录[(English)](2-Authentication.md) | [配置管理 →](3-Configuration-zh.md)

---

## 认证与登录

火山引擎 CLI 支持配置文件 profile、环境变量默认凭证链、SSO 和 Console Login。认证信息默认写入 `~/.volcengine/config.json`。

## 凭证解析优先级

业务命令创建 SDK Client 时，按以下顺序解析凭证和运行配置：

1. `---profile`：仅对本次调用生效，必须指向已存在的 profile。
2. 当前配置中的 `current` profile。
3. `VOLCENGINE_PROFILE` 或 `VOLCSTACK_PROFILE` 指定的 profile。
4. 默认凭证链：环境变量、OIDC、CLI 配置 Provider、ECS 实例角色等由 SDK 继续解析。

Region 的最终优先级是：

1. `---region`
2. profile 中的 `region`
3. `VOLCENGINE_REGION`

Endpoint 的最终优先级是：

1. `---endpoint`
2. profile 中的 `endpoint`
3. `VOLCENGINE_ENDPOINT`

当 `endpoint-resolver` 或 `VOLCENGINE_ENDPOINT_RESOLVER` 为 `standard` 时，使用 SDK 标准 Endpoint 解析器，此时显式 endpoint 会被忽略。`endpoint` 设置为 `auto-addressing` 时，也会启用标准 Endpoint 解析器。

## 凭证模式

| 模式 | 用途 | 必填字段 |
| --- | --- | --- |
| `ak` | AK/SK 静态凭证，默认模式 | `access-key`, `secret-key` |
| `sso` | SSO 单点登录 | 通过 `ve configure sso` 配置 |
| `console-login` | 控制台 OAuth 登录换取临时 STS 凭证 | 通过 `ve login` 写入 |
| `ramrolearn` | 使用 AK/SK 调 STS AssumeRole | `access-key`, `secret-key`, `role-name`, `account-id` |
| `oidc` | 使用 OIDC Token 换取临时凭证 | `oidc-token-file`, `role-trn` |
| `ecsrole` | ECS 实例角色，通过 IMDS 获取凭证 | `role-name` |

`ve configure set` 会校验当前 mode 的必填字段。修改已有 profile 时，未传的字段会保留原值；新建或修改成功后会把该 profile 设为 current。`ve configure sso` 例外，它写入 SSO profile，但不会自动切换 current。

## 使用 Profile 配置凭证

### AK/SK

```shell
ve configure set --profile prod --mode ak --region cn-beijing --access-key AK --secret-key SK
```

`--mode ak` 可省略：

```shell
ve configure set --profile prod --region cn-beijing --access-key AK --secret-key SK
```

临时凭证可以同时写入 session token：

```shell
ve configure set --profile sts-dev --region cn-beijing \
  --access-key AK --secret-key SK --session-token SESSION_TOKEN
```

### AssumeRole

```shell
ve configure set --profile ram-dev --mode ramrolearn --region cn-beijing \
  --access-key AK --secret-key SK \
  --role-name YourRoleName --account-id 2100000000
```

### OIDC

```shell
ve configure set --profile ci-oidc --mode oidc --region cn-beijing \
  --oidc-token-file /var/run/secrets/oidc-token \
  --role-trn trn:iam::2100000000:role/CIRole
```

### ECS 实例角色

```shell
ve configure set --profile ecs-role --mode ecsrole --region cn-beijing \
  --role-name YourEcsRoleName
```

## Profile 字段说明

```shell
profile: 配置名称。新建或修改 profile 时必填。
mode: 凭证模式。可选 ak, sso, console-login, ramrolearn, oidc, ecsrole；未传时新 profile 默认为 ak。
access-key: AK。
secret-key: SK。
session-token: 临时凭证 Session Token。
region: API 调用地域。configure set 阶段可不填，但 API 调用时必须可解析到 region。
endpoint: 自定义 endpoint；endpoint-resolver 为 standard 时会被忽略。
endpoint-resolver: 设置为 standard 时使用标准 Endpoint 解析器。
http-proxy: SSL 关闭时 SDK 使用的 HTTP 代理。
https-proxy: SDK 使用的 HTTPS 代理。
disable-ssl: 是否禁用 SSL。只在显式传参时写入。
use-dual-stack: 是否启用双栈 endpoint。只在显式传参时写入。
role-name: ramrolearn 和 ecsrole 模式必填。
account-id: ramrolearn 模式必填。
oidc-token-file: oidc 模式必填。
role-trn: oidc 模式必填。
login-session: console-login 模式字段，由 ve login 写入，不建议手动配置。
sso-session: sso 模式字段，由 ve configure sso 写入。
```

## 使用环境变量

如果没有可用 profile，CLI 会使用 SDK 默认凭证链。最常见的是 AK/SK 环境变量：

```shell
export VOLCENGINE_ACCESS_KEY=AK
export VOLCENGINE_SECRET_KEY=SK
export VOLCENGINE_REGION=cn-beijing

# 可选：临时凭证
export VOLCENGINE_SESSION_TOKEN=SESSION_TOKEN

# 可选：Endpoint 配置
export VOLCENGINE_ENDPOINT=open.volcengineapi.com
export VOLCENGINE_ENDPOINT_RESOLVER=standard

# 可选：网络配置
export VOLCENGINE_DISABLE_SSL=false
export VOLCENGINE_USE_DUALSTACK=false
```

OIDC 环境变量：

```shell
export VOLCENGINE_OIDC_TOKEN_FILE=/path/to/oidc/token
export VOLCENGINE_OIDC_ROLE_TRN=trn:iam::2100000000:role/YourRoleName
export VOLCENGINE_REGION=cn-beijing
```

如果要确保只使用显式配置的 profile，禁用默认凭证链：

```shell
export VOLCENGINE_DISABLE_DEFAULT_CREDENTIALS=true
```

设置后，如果没有活跃 profile，CLI 会直接报错，不再尝试环境变量或 IMDS。

## SSO 登录

SSO 配置分两层：

- `sso-session`：企业 SSO 入口，保存 Start URL、Region、Scopes。
- SSO profile：某个账号和角色的绑定，保存 `mode=sso`、`sso-session-name`、`account-id`、`role-name`、`region` 等字段。

### 快速上手

```shell
# 1. 创建 SSO 会话。registration-scopes 可省略
ve configure sso-session --name my-sso \
  --start-url https://{custom}.volccloudidentity.com/userportal \
  --region cn-beijing

# 2. 创建 SSO profile，完成设备码授权，并选择 account 和 role
ve configure sso --profile my-dev --sso-session my-sso

# 3. 切换当前默认 profile
ve configure profile --profile my-dev

# 4. 使用该 profile 调用 API
ve sts GetCallerIdentity
```

`ve configure sso` 不会自动切换 current profile。跳过第 3 步时，业务命令仍会使用原 current profile。

### 命令关系

| 命令 | 什么时候用 | 主要作用 | 是否切换 current |
| --- | --- | --- | --- |
| `ve configure sso-session` | 每个 SSO 入口通常配置一次 | 保存 Start URL、Region、Scopes，可被多个 SSO profile 复用 | 否 |
| `ve configure sso` | 每个 account + role 组合配置一次 | 关联 SSO 会话，首次授权，选择账号和角色，写入 SSO profile | 否 |
| `ve configure profile --profile NAME` | 需要业务命令默认使用某个 profile | 切换 current profile | 是 |
| `ve sso login` | 被提示重新登录，或主动刷新 SSO 登录态 | 重新执行设备码授权并缓存 access token | 否 |
| `ve sso logout` | 退出某个或全部 SSO 会话 | 撤销缓存令牌，删除 token cache，清理 STS 临时凭证 | 否 |

### 配置 SSO Session

```shell
ve configure sso-session --name my-sso \
  --start-url https://{custom}.volccloudidentity.com/userportal \
  --region cn-beijing \
  --registration-scopes cloudidentity:account:access,offline_access
```

参数说明：

```shell
name: SSO 会话名称；未提供时进入交互式选择/创建模式。
start-url: SSO Start URL，通常是用户登录链接加 /userportal 后缀。
region: SSO 区域，默认 cn-beijing。
registration-scopes: 逗号分隔的 scope 列表；默认 cloudidentity:account:access,offline_access。
```

Scopes 只允许 `cloudidentity:account:access` 和 `offline_access`，CLI 会 trim、去重并校验。编辑已有 session 时，Start URL、Region、Scopes 会回填默认值，直接回车可沿用。

### 配置 SSO Profile

```shell
ve configure sso --profile my-dev --sso-session my-sso
```

无图形界面的服务器可禁用自动打开浏览器：

```shell
ve configure sso --profile my-dev --sso-session my-sso --no-browser
```

如果 `--profile` 为空，交互流程允许直接回车，默认使用 `{sso-role-name}-{sso-account-id}`。如果 `--sso-session` 指定的 session 不存在，命令会引导创建。

### 日常自动刷新

当前 profile 为 SSO 模式时，业务命令会自动检查并刷新 STS 临时凭证：

- `session-token` 未过期时直接复用。
- STS 缺失或过期时，使用缓存的 SSO access token 和 profile 中的 `account-id` / `role-name` 换取新 STS，并写回 profile。
- SSO access token 过期或接近过期时，只会尝试用 refresh token 静默刷新，不会在业务命令中自动打开浏览器。
- 缓存缺失、refresh token 缺失、客户端注册过期或刷新失败时，会提示执行 `ve sso login`。

### SSO Login

```shell
ve sso login --profile my-dev
ve sso login --sso-session my-sso
ve sso login
```

`ve sso login` 会显式重新登录 SSO，每次执行都会重新走设备码授权，不会用已有 refresh token 静默换取 access token。

可选参数：

```shell
--profile: 指定 SSO profile；必须存在、类型为 sso，并配置了 sso-session。
--sso-session: 指定 SSO session；必须存在且有效。
--no-browser: 禁止自动打开浏览器。
```

不传 `--profile` 和 `--sso-session` 时：没有 session 会报错；只有一个 session 时直接使用；多个 session 时进入可搜索的交互选择。

### SSO Logout

```shell
ve sso logout --sso-session my-sso
ve sso logout
```

不指定 session 时：没有 session 会报错；只有一个 session 时直接登出；多个 session 时进入交互选择，并支持选择 “All SSO sessions” 批量登出。

登出会：

- 撤销该 SSO session 缓存的 refresh token。
- 删除该 SSO session 的 token cache。
- 清理关联 SSO profile 中的 `access-key`、`secret-key`、`session-token`、`sts-expiration`。

登出不会删除 SSO profile、不会删除 sso-session 配置，也不会清除 profile 中的 `account-id` / `role-name`。

## Console Login

Console Login 通过火山引擎控制台完成 OAuth 2.0 + PKCE 登录，并把临时 STS 凭证缓存到本地。

```shell
# 使用 default profile 登录，未指定 region 时会提示输入
ve login

# 指定 profile 和 region
ve login -p dev -r cn-beijing

# 无浏览器、远程服务器或容器环境使用跨设备登录
ve login -p dev -r cn-beijing --remote
```

参数说明：

```shell
--profile, -p: profile 名称，默认 default。
--region, -r: 地域；未指定时会提示输入，直接回车使用 cn-beijing。
--remote: 跨设备登录；按终端输出 URL 在浏览器完成登录，并把授权码粘贴回终端。
--endpoint-url: 登录服务地址，默认 https://signin.volcengine.com，通常无需修改。
```

登录成功后，profile 会写为 `console-login` 模式，并记录 `login-session`。使用非 `default` profile 登录后，业务命令不会自动切换 profile，需要执行：

```shell
ve configure profile --profile dev
```

完整流程：

```shell
ve login --profile dev --region cn-beijing
ve configure profile --profile dev
ve sts GetCallerIdentity
ve logout --profile dev
```

## Console Logout

```shell
# 登出 default profile
ve logout

# 登出指定 profile
ve logout -p dev

# 登出当前配置中的所有 console-login profile
ve logout --all
```

`ve logout` 只清理本地登录状态：删除缓存凭证并清除 profile 中的 `login-session`。它不会删除 profile，也不会向服务端发起请求。

注意：

- 不带 `--profile` 时只处理 `default` profile，不会自动按 current 登出。
- 只适用于 `console-login` profile；AK/SSO profile 不受影响。
- `--all` 会忽略 `--profile`，清理所有 `console-login` profile。

## 常见问题

**执行完 `ve configure sso` 后业务命令还在使用旧账号怎么办？**

执行 `ve configure profile --profile NAME` 切换 current。`configure sso` 只写入 profile，不会自动切换 current。

**什么时候需要执行 `ve sso login`？**

首次 `ve configure sso` 已完成授权。日常业务命令会自动复用或静默刷新凭证；只有 CLI 提示重新登录，或者你想主动刷新 SSO 登录态时，再执行 `ve sso login`。

**没有图形界面的机器怎么登录？**

SSO 使用 `--no-browser`；Console Login 使用 `--remote`。

**Scopes 应该怎么填？**

通常可以省略，默认使用 `cloudidentity:account:access,offline_access`。

---

[← 入门](1-GettingStarted-zh.md) | 认证与登录[(English)](2-Authentication.md) | [配置管理 →](3-Configuration-zh.md)
