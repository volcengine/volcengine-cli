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

切换 current 只影响后续未指定 `---profile` 的业务命令。单次调用时也可以使用运行时覆盖：

```shell
ve ecs DescribeInstances ---profile prod
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
ve ecs DescribeInstances ---profile prod
```

这条命令只在本次调用中使用 `prod`，不会修改 `current`。

### 单次调用覆盖 Region 和 Endpoint

```shell
ve ecs DescribeInstances ---region cn-shanghai
ve sts GetCallerIdentity ---region cn-beijing ---endpoint sts.volcengineapi.com
```

---

[← 认证与登录](2-Authentication-zh.md) | 配置管理[(English)](3-Configuration.md) | [使用指南 →](4-Usage-zh.md)
