[← 配置管理](3-Configuration-zh.md) | 使用指南[(English)](4-Usage.md) | [高级用法 →](5-Advanced-zh.md)

---

## 使用指南

CLI 的基本调用格式：

```shell
ve <service> <action> [--Param value ...] [--header Name=Value ...] [--body json]
                      [--profile name] [--region region] [--endpoint endpoint] [--lang language]
                      [--version api-version] [--method GET|POST] [--force]
```

参数分几类：

- **API 业务参数**：双横线 `--Param value`，进入请求体/查询参数（保留名 `body` / `header` 除外）
- **对外系统参数**（放在 Action 后）：`--profile` / `--region` / `--endpoint` / `--lang` / `--version` / `--method` / `--force`
- **双横线保留控制参数**：`--header`（HTTP 头）、`--body`（JSON 请求体）；**不是**业务参数

API 调用中的系统参数统一使用双横线并放在 Action 后。若当前 Action 暴露了大小写完全相同的业务参数，双横线优先按 API 参数解析。

## 查看服务和接口

查看支持的服务：

```shell
ve --help
```

查看某个服务下的接口：

```shell
ve ecs --help
```

查看某个接口的参数：

```shell
ve ecs DescribeInstances --help
```

默认 `-h` / `--help` 使用简洁模式，只展示参数名称、类型和是否必填，不加载完整参数语料。需要查看参数描述和示例时，使用详细模式：

```shell
ve ecs DescribeInstances -h --detail
ve ecs DescribeInstances --help --detail
```

单独使用 `--detail` 不会触发帮助。

查看版本：

```shell
ve version
ve -v
```

## 调用 API

无参数调用：

```shell
ve sts GetCallerIdentity
```

带参数调用：

```shell
ve ecs DescribeInstances --InstanceIds.1 i-1234567890abcdef0
```

多个参数：

```shell
ve rds_mysql ListDBInstanceIPLists --InstanceId mysql-xxxxxx --GroupName default
```

参数名和值使用空格分隔。当前 CLI 参数语法是：

```shell
--Param value
--region cn-beijing
```

不要写成 `--Param=value`、`--region=cn-beijing` 或 `--lang=ZH`。参数名称和值之间必须使用空格。

## CLI 系统参数

系统参数对外统一使用双横线：

| 参数 | 作用 |
| --- | --- |
| `--profile` | 本次调用使用指定 profile，不修改 current |
| `--region` | 本次调用覆盖 region |
| `--endpoint` | 本次调用覆盖 endpoint，并清空 endpoint resolver |
| `--lang` | 设置本次调用中 CLI 自有帮助、提示和错误的显示语言 |
| `--version` | 指定本次调用的 **API 版本**；未指定时使用内置元数据中的 service 版本（与根命令 `ve -v` / `ve --version` / `ve version` 的 CLI 二进制版本无关） |
| `--force` | 跳过 service/action 元数据校验，强制调用未收录或新发布的接口；**未收录 service** 须提供 `--version` 与固定 endpoint（`--endpoint` 或非 standard 下的 profile/`VOLCENGINE_ENDPOINT`）；已收录 service 可回落元数据。纯开关：只写 `--force`，不要写 `--force true` |
| `--method` | 指定 HTTP 方法（`GET`/`POST`）；正常路径与 `--force` 路径规则一致：显式值优先，否则用 action 元数据，均无则默认 `GET` |

Action 后如果当前 Action 暴露了大小写完全相同的参数，双横线形式优先按 API 业务参数解析；没有同名冲突时按系统参数解析。

参数名区分大小写，`--Region`、`--Endpoint` 等不同大小写名称始终是 API 参数。

### 双横线保留控制参数

| 参数 | 作用 |
| --- | --- |
| `--header Name=Value` | 追加 HTTP 请求头，**可重复**；不进入请求体。`Content-Type` 优先于元数据；同名多次时以最后一次为准 |
| `--body json` | JSON 请求体（`application/json` 风格）；不能与其他 API 业务参数混用 |

```shell
ve sts GetCallerIdentity --header X-Custom-Trace=abc
ve newsvc Act --force --version 2024-01-01 --endpoint open.volcengineapi.com \
  --header Content-Type=application/json \
  --header X-Feature=on \
  --body '{"k":1}'
```

规则补充：

- `Content-Type` 可用 `--header Content-Type=...` 覆盖；带参数形式（如 `application/json; charset=utf-8`）仍按 JSON 处理
- 无元数据且仅有 `--body` 时，默认 `Content-Type=application/json`
- `--header` 与 `--body` 可同时使用；`--header` 不算 flattened 业务参数，不会与 `--body` 冲突
- 不允许覆盖：`Host`、`Authorization`、`Content-Length`（与传输/签名冲突）
- 保留名：`--header`、`--body` 不能再作为普通 API 参数名使用

示例：

```shell
# 使用指定 profile
ve ecs DescribeInstances --profile prod

# 使用指定 profile 并覆盖 region
ve ecs DescribeInstances --profile prod --region ap-southeast-1

# 只覆盖 region
ve ecs DescribeInstances --region cn-shanghai

# 调用 STS 时临时指定 endpoint
ve sts GetCallerIdentity --region cn-beijing --endpoint sts.volcengineapi.com
```

如果 `--profile` 指向不存在的 profile，会直接报错。

当前唯一的精确同名冲突是 `i18nopenapi VideoProjectSuppressionStart` 的业务参数 `--lang`，因此该 Action 后的双横线 `--lang` 按业务参数解析。

### 显示语言

使用 `--lang EN` 显示英文，使用 `--lang ZH` 显示简体中文。同时支持 `en-US`、`en_US`、`zh-CN`、`zh_CN`、`zh-Hans` 等语言码。不支持的值统一回退英文。

未传 `--lang` 时，CLI 依次读取 `LC_ALL`、`LC_MESSAGES`、`LANG`，均无法识别时回退英文。显式参数优先级最高，且不会写入配置文件。

```shell
ve sts GetCallerIdentity --lang ZH --help
ve ecs DescribeInstances --lang EN --help
ve login --lang zh-CN
```

语言选择只影响 CLI 自己生成的文案，不翻译或修改 API 响应体和服务端返回内容。

## JSON 参数

对于 query/form 风格 API，参数值如果是 JSON object 或 JSON array，CLI 会尝试解析成 JSON：

```shell
ve rds_mysql ModifyDBInstanceIPList \
  --InstanceId mysql-xxxxxx \
  --GroupName default \
  --IPList '["10.20.30.40","50.60.70.80"]'
```

字符串类型参数会按字符串保留，不会因为内容看起来像 JSON 就强行解析。

## application/json 请求

对于 `ContentType` 为 `application/json` 的接口，可以直接传 `--body`：

```shell
ve rds_mysql ModifyDBInstanceIPList \
  --body '{"InstanceId":"mysql-xxxxxx","GroupName":"default","IPList":["10.20.30.40","50.60.70.80"]}'
```

`--body` 必须是 JSON object 或 JSON array。它不能和展开参数混用：

```shell
# 错误：--body 不能和其它 API 参数同时使用
ve rds_mysql ModifyDBInstanceIPList --body '{"InstanceId":"mysql-xxxxxx"}' --GroupName default
```

application/json 接口也支持把参数展开为 dotted key，CLI 会根据 metadata 组装嵌套 JSON：

```shell
ve some_service SomeJsonAction \
  --Name demo \
  --Ports.1 80 \
  --Ports.2 443 \
  --Tags.1.Key env \
  --Tags.1.Value prod
```

数组下标从 1 开始，且必须连续。`0`、负数、跳号都会报错。

## 数组和嵌套参数

数组参数常见写法：

```shell
ve ecs DescribeInstances --InstanceIds.1 i-123 --InstanceIds.2 i-456
```

对象数组写法：

```shell
ve some_service SomeAction \
  --Filters.1.Key InstanceType \
  --Filters.1.Values.1 ecs.g1.large \
  --Filters.1.Values.2 ecs.g2.large
```

对于 application/json 接口，CLI 会把上面的 dotted key 还原成嵌套对象和数组。对于非 JSON 接口，CLI 保持 dotted key 行为，由服务端/API 层处理。

## 未知参数

CLI 允许未知 API 参数透传给服务端/API 层处理。除非参数路径本身不合法，CLI 不会仅因为 metadata 中没有某个参数就拦截。

示例：

```shell
ve ecs DescribeInstances --NewServerSideParam value
```

这对服务端新增参数、metadata 尚未更新的场景有用。

## 未收录 service / action

CLI 会校验 service 和 action 是否在内置元数据中。若调用的 **service 或 action 尚未收录**，需使用 `--force` 跳过校验；未收录 service 还须指定 `--version`，并提供**固定** endpoint（`--endpoint`，或未启用 `endpoint-resolver=standard` 时的 profile / `VOLCENGINE_ENDPOINT`），因为 CLI 无法从元数据中解析接入地址。已收录 service 在 force 模式下可省略这些覆盖参数并使用元数据与正常 endpoint 规则。详见 [高级用法：强制泛化调用](5-Advanced-zh.md#force-invocation)。

```shell
ve newservice DescribeNewResource \
  --version 2024-01-01 \
  --endpoint open.volcengineapi.com \
  --SomeParam value \
  --force
```

## 常用调用场景

使用默认 profile：

```shell
ve ecs DescribeInstances
```

使用非默认 profile：

```shell
ve ecs DescribeInstances --profile prod
```

使用环境变量默认凭证链：

```shell
export VOLCENGINE_ACCESS_KEY=AK
export VOLCENGINE_SECRET_KEY=SK
export VOLCENGINE_REGION=cn-beijing
ve ecs DescribeInstances
```

使用 OIDC profile：

```shell
ve configure set --profile ci-oidc --mode oidc --region cn-beijing \
  --oidc-token-file /var/run/secrets/oidc-token \
  --role-trn trn:iam::2100000000:role/CIRole

ve ecs DescribeInstances --profile ci-oidc
```

使用 ECS 实例角色 profile：

```shell
ve configure set --profile ecs-role --mode ecsrole --region cn-beijing --role-name MyRole
ve ecs DescribeInstances --profile ecs-role
```

## 错误提示

缺少凭证时：

```text
credentials not configured, please run 've login' or 've configure set', or set VOLCENGINE_ACCESS_KEY and VOLCENGINE_SECRET_KEY environment variables
```

缺少 region 时：

```text
region not set, please set it via profile, --region flag, or VOLCENGINE_REGION environment variable
```

对外系统参数（双横线）：`--profile`、`--region`、`--endpoint`、`--lang`、`--force`、`--version`、`--method`。
双横线保留控制参数：`--header`、`--body`（见上文「双横线保留控制参数」）。

---

[← 配置管理](3-Configuration-zh.md) | 使用指南[(English)](4-Usage.md) | [高级用法 →](5-Advanced-zh.md)
