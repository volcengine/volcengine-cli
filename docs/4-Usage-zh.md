[← 配置管理](3-Configuration-zh.md) | 使用指南[(English)](4-Usage.md) | [高级用法 →](5-Advanced-zh.md)

---

## 使用指南

CLI 的基本调用格式：

```shell
ve <service> <action> [--Param value ...] [---profile name] [---region region] [---endpoint endpoint]
```

其中 `--Param value` 是 API 参数，`---profile` / `---region` / `---endpoint` 是 CLI 固定参数。

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
---region cn-beijing
```

不要写成 `--Param=value` 或 `---region=cn-beijing`。

## CLI 固定参数

固定参数使用三横线 `---`，不会和 API 的双横线参数冲突：

| 参数 | 作用 |
| --- | --- |
| `---profile` | 本次调用使用指定 profile，不修改 current |
| `---region` | 本次调用覆盖 region |
| `---endpoint` | 本次调用覆盖 endpoint，并清空 endpoint resolver |

示例：

```shell
# 使用指定 profile
ve ecs DescribeInstances ---profile prod

# 使用指定 profile 并覆盖 region
ve ecs DescribeInstances ---profile prod ---region ap-southeast-1

# 只覆盖 region
ve ecs DescribeInstances ---region cn-shanghai

# 调用 STS 时临时指定 endpoint
ve sts GetCallerIdentity ---region cn-beijing ---endpoint sts.volcengineapi.com
```

如果 `---profile` 指向不存在的 profile，会直接报错。

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

## 常用调用场景

使用默认 profile：

```shell
ve ecs DescribeInstances
```

使用非默认 profile：

```shell
ve ecs DescribeInstances ---profile prod
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

ve ecs DescribeInstances ---profile ci-oidc
```

使用 ECS 实例角色 profile：

```shell
ve configure set --profile ecs-role --mode ecsrole --region cn-beijing --role-name MyRole
ve ecs DescribeInstances ---profile ecs-role
```

## 错误提示

缺少凭证时：

```text
credentials not configured, please run 've login' or 've configure set', or set VOLCENGINE_ACCESS_KEY and VOLCENGINE_SECRET_KEY environment variables
```

缺少 region 时：

```text
region not set, please set it via profile, ---region flag, or VOLCENGINE_REGION environment variable
```

固定参数不支持时：

```text
---debug is not supported, supported fixed flags: ---profile, ---region, ---endpoint
```

当前支持的固定参数只有 `---profile`、`---region`、`---endpoint`。

---

[← 配置管理](3-Configuration-zh.md) | 使用指南[(English)](4-Usage.md) | [高级用法 →](5-Advanced-zh.md)
