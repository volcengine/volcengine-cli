[← 认证与登录](2-Authentication-zh.md) | 配置管理[(English)](3-Configuration.md) | [使用指南 →](4-Usage-zh.md)

---

## 配置管理

CLI 的 profile 和 SSO session 默认保存在 `~/.volcengine/config.json`。配置文件会以 `0600` 权限写入，配置目录会以 `0700` 权限创建。

本文只介绍 profile 的查看、切换、修改和删除。认证方式本身见 [认证与登录](2-Authentication-zh.md)。

## 配置文件结构

配置文件包含：

- `current`：当前默认 profile 名称。
- `profiles`：profile 映射。
- `sso-session`：SSO session 映射。
- `enableColor`：是否启用彩色 JSON 输出，见 [高级用法](5-Advanced-zh.md)。

示意：

```json
{
    "current": "prod",
    "profiles": {
        "prod": {
            "name": "prod",
            "mode": "ak",
            "access-key": "AK",
            "secret-key": "SK",
            "region": "cn-beijing"
        }
    },
    "enableColor": false,
    "sso-session": {}
}
```

不要手动编辑敏感字段，优先使用 CLI 命令写入。

## 查看当前 Profile

```shell
ve configure get
```

未指定 `--profile` 时，命令展示 current profile：

```shell
no profile name specified, show current profile: [prod]
```

## 查看指定 Profile

```shell
ve configure get --profile prod
```

如果 profile 不存在，命令会输出一个空 profile 对象，不会自动创建配置。

## 列出所有 Profile

```shell
ve configure list
```

输出会先显示 current：

```shell
*** current profile: prod ***
```

然后逐个输出配置文件中的 profile。

## 切换 Current Profile

```shell
ve configure profile --profile prod
```

`--profile` 必填。指定 profile 不存在时，current 不会变化并返回错误。

切换 current 只影响后续未指定 `--profile` 的业务命令。单次调用时也可以使用运行时覆盖：

```shell
ve ecs DescribeInstances --profile prod
```

## 新建或修改 Profile

```shell
ve configure set --profile prod --region cn-beijing --access-key AK --secret-key SK
```

行为说明：

- `--profile` 必填。
- profile 不存在时新建，默认 mode 为 `ak`。
- profile 已存在时，只更新本次传入的非空字段；未传字段保留原值。
- `--disable-ssl` 和 `--use-dual-stack` 只有显式传参时才写入。
- 新建或修改成功后，会把该 profile 设置为 current。
- `region` 在 `configure set` 阶段不是强制项，但 API 调用时必须能解析到 region。

修改 region：

```shell
ve configure set --profile prod --region cn-shanghai
```

修改 endpoint：

```shell
ve configure set --profile prod --endpoint ecs.cn-beijing.volcengineapi.com
```

使用标准 Endpoint 解析器：

```shell
ve configure set --profile prod --endpoint-resolver standard
```

配置代理：

```shell
ve configure set --profile prod --https-proxy http://127.0.0.1:7890
```

配置双栈：

```shell
ve configure set --profile prod --use-dual-stack
```

禁用 SSL：

```shell
ve configure set --profile prod --disable-ssl
```

## 删除 Profile

```shell
ve configure delete --profile prod
```

`--profile` 必填。若删除的是 current profile，CLI 会从剩余 profile 中选择一个作为新的 current；如果没有剩余 profile，current 会置空。

删除 profile 不会删除 SSO session，也不会删除 Console Login 的全局缓存目录。Console Login 缓存清理见 [认证与登录](2-Authentication-zh.md#console-logout)。

## 安全策略（Safety Policy）

安全策略是 `ve` 在客户端执行的 OpenAPI 操作防护，覆盖普通调用、`--force` 调用和 MCP `call_api`。它与火山引擎 IAM 互补，不能替代云端鉴权。`ve arkcli` 和 `ve tosutil` 属于参数原样透传的代理 CLI，不在拦截范围内。

安全策略默认关闭，独立保存在 `~/.volcengine/safety-policy.json`，文件权限为 `0600`。可执行：

```shell
ve configure safety-policy enable
ve configure safety-policy add --pattern '*:Delete*' --action deny
ve configure safety-policy add --pattern '*:Update*' --action confirm
ve configure safety-policy list
```

规则动作：

- `allow`：显式允许命中的操作。通过 JSON 文件或 `VOLCENGINE_SAFETY_POLICY_RULES` 配置；`add` 命令有意只接受防护动作。
- `deny`：在本地拒绝，不发送 API 请求。
- `confirm`：发送请求前必须获得明确确认。
- `forbid`：`confirm` 的别名。

规则格式为 `service:action`，匹配不区分大小写，支持 `*` 通配符，并采用第一条匹配优先（first-match-wins）。未命中任何规则时默认允许。规则按命令写法匹配，不会自动展开别名，因此具体别名需要分别覆盖。

```json
{
  "enabled": true,
  "rules": [
    {"pattern": "ecs:Describe*", "action": "allow"},
    {"pattern": "ecs:*", "action": "deny"}
  ]
}
```

交互式终端命中 `confirm` 时会提示输入 `yes`。Agent 每次先不带 `--yes` 调用，仅遇确认拦截（未发送 API 请求）才简要说明目标和影响，询问是否继续，然后结束当前轮次，等待用户的新回复。确认后由 Agent 内部添加 `--yes` 或 `-y`，按原参数重试一次，无需用户理解或操作此参数。重试后即丢弃批准，即使失败；下次操作重新从不带批准参数开始。这是 Agent 行为约束，不是 CLI 强制的一次性令牌。以下为获批后的重试示例：

```shell
ve ecs UpdateInstance --InstanceId i-xxxxx --yes
```

`--yes` 只能跳过 `confirm`，不能绕过 `deny`。MCP 客户端在重试 `call_api` 时传入 `yes: true`，详见 [MCP 服务](6-MCP-zh.md#安全策略)。

环境变量优先级高于配置文件：

| 变量 | 说明 |
| --- | --- |
| `VOLCENGINE_SAFETY_POLICY_ENABLED` | 覆盖 `enabled`（`true`/`false`，也接受 `1`/`0` 等 Go 布尔写法） |
| `VOLCENGINE_SAFETY_POLICY_RULES` | 用逗号分隔的 `pattern=action` 完整替换文件规则 |
| `VOLCENGINE_SAFETY_SKIP_CONFIRM` | 将当前进程的确认视为已批准（`1`、`true`、`yes` 或 `on`） |

示例：

```shell
export VOLCENGINE_SAFETY_POLICY_ENABLED=true
export VOLCENGINE_SAFETY_POLICY_RULES='ecs:Describe*=allow,*:Delete*=deny,*:Update*=confirm'
```

策略文件损坏或环境变量格式非法时采用 fail-closed：CLI 返回配置错误，不发送 API 请求。

## 配置选择示例

### 多环境切换

```shell
ve configure set --profile dev --region cn-beijing --access-key DEV_AK --secret-key DEV_SK
ve configure set --profile prod --region cn-beijing --access-key PROD_AK --secret-key PROD_SK

ve configure profile --profile dev
ve ecs DescribeInstances

ve configure profile --profile prod
ve ecs DescribeInstances
```

### 单次调用覆盖 Profile

```shell
ve configure profile --profile dev
ve ecs DescribeInstances --profile prod
```

这条命令只在本次调用中使用 `prod`，不会修改 `current`。

### 单次调用覆盖 Region 和 Endpoint

```shell
ve ecs DescribeInstances --region cn-shanghai
ve sts GetCallerIdentity --region cn-beijing --endpoint sts.volcengineapi.com
```

---

[← 认证与登录](2-Authentication-zh.md) | 配置管理[(English)](3-Configuration.md) | [使用指南 →](4-Usage-zh.md)
