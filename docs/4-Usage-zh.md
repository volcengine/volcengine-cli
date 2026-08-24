[← 配置管理](3-Configuration-zh.md) | 使用指南[(English)](4-Usage.md) | [高级用法 →](5-Advanced-zh.md)

---

## 使用指南

CLI 的基本调用格式：

```shell
ve <service> <action> [--Param value ...] [--header Name=Value ...] [--body json]
                      [--api-param Name=Value ...]
                      [--profile name] [--region region] [--endpoint endpoint] [--lang language]
                      [--version api-version] [--method GET|POST] [--force]
                      [--output json|table|table-num|text|yaml|off] [--query jmespath]
```

参数分几类：

- **API 业务参数**：双横线 `--Param value`，进入请求体/查询参数（保留名 `body` / `header` / `api-param` 除外）
- **对外系统参数**（放在 Action 后）：`--profile` / `--region` / `--endpoint` / `--lang` / `--version` / `--method` / `--force` / `--output` / `--query`
- **双横线保留控制参数**：`--header`（HTTP 头）、`--body`（JSON 请求体）、仅限 force 的 `--api-param Name=Value`（显式业务参数）；这些控制参数自身**不是**业务参数

API 调用中的系统参数统一使用双横线并放在 Action 后。若当前 Action 暴露了大小写完全相同的业务参数，双横线优先按 API 参数解析。

### 参数前缀规范

CLI 对外只有一种参数前缀：**双横线 `--name`**。系统参数、API 业务参数、保留控制参数（`--header` / `--body` / `--api-param`）一律使用双横线。

- 帮助信息、命令补全、报错文案和文档示例只出现 `--name` 一种写法。
- 冲突判定区分大小写，并以当前 Action 实际暴露的参数为准：`--Region`、`--Lang` 等不同大小写始终是业务参数。
- 当某个 Action 暴露了与系统参数**完全同名**的业务参数时，该 Action 后的双横线归业务参数解析，此时请改用等价的非参数途径传递系统语义（见下方「已知同名冲突」）。

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
| `--output` | 设置 API 响应输出格式：`json`（默认）、`table`、`table-num`、`text`、`yaml`、`off` |
| `--query` | JMESPath 表达式，在格式化前过滤/投影完整响应 JSON（含 `ResponseMetadata` 与 `Result`） |

Action 后如果当前 Action 暴露了大小写完全相同的参数，双横线形式优先按 API 业务参数解析；没有同名冲突时按系统参数解析。

参数名区分大小写，`--Region`、`--Endpoint` 等不同大小写名称始终是 API 参数。

### 双横线保留控制参数

| 参数 | 作用 |
| --- | --- |
| `--header Name=Value` | 追加 HTTP 请求头，**可重复**；不进入请求体。`Content-Type` 优先于元数据；同名多次时以最后一次为准 |
| `--body json` | JSON 请求体（`application/json` 风格）；不能与其他 API 业务参数混用 |
| `--api-param Name=Value` | 显式添加 API 业务参数，**可重复且仅可配合 `--force` 使用**；主要用于未收录接口中名为 `query`、`output` 等与系统参数同名的业务参数 |

```shell
ve sts GetCallerIdentity --header X-Custom-Trace=abc
ve newsvc Act --force --version 2024-01-01 --endpoint open.volcengineapi.com \
  --header Content-Type=application/json \
  --header X-Feature=on \
  --body '{"k":1}'
ve newsvc Search --force --version 2024-01-01 --endpoint open.volcengineapi.com \
  --query 'Result.Items' --output table \
  --api-param 'query=server-side-filter' --api-param 'output=compact'
```

规则补充：

- `Content-Type` 可用 `--header Content-Type=...` 覆盖；带参数形式（如 `application/json; charset=utf-8`）仍按 JSON 处理
- 无元数据且仅有 `--body` 时，默认 `Content-Type=application/json`
- `--header` 与 `--body` 可同时使用；`--header` 不算 flattened 业务参数，不会与 `--body` 冲突
- 不允许覆盖：`Host`、`Authorization`、`Content-Length`（与传输/签名冲突）
- `--api-param` 只按第一个 `=` 拆分；参数名会去除两端空白但保留原始大小写，参数值允许为空。名称重复，或与直接传入的同大小写 `--Name value` 冲突时会明确报错，不会静默覆盖
- `--api-param` 在构造请求前展开，JSON 与 query/form 调用均可使用，控制参数自身不会进入请求
- 保留名：`--header`、`--body`、`--api-param` 不能再作为普通 API 参数名使用

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

### 已知同名冲突

当前已知的精确同名冲突包括：

- `i18nopenapi VideoProjectSuppressionStart` 的业务参数 `--lang`：该 Action 后的 `--lang` 按业务参数解析；需要切换 CLI 显示语言时，请改用环境变量（`LC_ALL` / `LC_MESSAGES` / `LANG`），见「显示语言」
- `insight AgentChat` 的业务参数 `--query`：该 Action 后的 `--query` 按业务参数解析；需要过滤响应时，请保留默认 JSON 输出并在下游处理

其他 Action 若未来暴露同名业务参数，同样遵循「双横线优先业务参数」规则。

对于未收录 Action，CLI 无法通过元数据识别同名冲突，因此 `--query`、`--output` 仍保留其对外系统语义。配合 `--force` 时，可用 `--api-param query=<value>`、`--api-param output=<value>` 显式传递同名业务参数，从而在一次调用中无歧义地同时使用系统值和业务值。该显式入口也可用于已收录 Action 的 `--force` 调用，但不能覆盖直接传入的同大小写业务参数。

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

如果未收录的 query/form 接口包含名为 `query`、`output` 的业务参数，可保留公共参数作为响应控制，并通过仅限 force 的显式入口传业务值：

```shell
ve newservice Search \
  --version 2024-01-01 \
  --endpoint open.volcengineapi.com \
  --force \
  --query 'Result.Items' \
  --output table \
  --api-param 'query=status=active' \
  --api-param 'output='
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

对外系统参数（双横线）：`--profile`、`--region`、`--endpoint`、`--lang`、`--force`、`--version`、`--method`、`--output`、`--query`。
双横线保留控制参数：`--header`、`--body`、`--api-param`（见上文「双横线保留控制参数」）。

## 过滤与输出格式

API 调用成功后，CLI 默认将**完整响应 JSON**（通常含 `ResponseMetadata` 与 `Result`）打印到 stdout。可用系统参数控制展示：

| 参数 | 说明 |
|------|------|
| `--output` | 输出格式：`json`（默认）、`table`、`table-num`、`text`、`yaml`、`off` |
| `--query` | JMESPath 表达式，在格式化**之前**过滤/投影；路径相对完整响应，列表字段多在 `Result.*` 下 |

处理顺序：`原始响应 → [--query] → [--output] → stdout`（先过滤再格式化；字段路径按火山引擎响应 envelope）。

```shell
# 先投影再表格（推荐；用 query 选择要展示的字段）
# table/text 的列序跟随多选哈希的书写顺序：下面写的是 Name、Id、Status，
# 列序就是 Name、Id、Status。
ve ecs DescribeInstances \
  --query 'Result.Instances[*].{Name:InstanceName,Id:InstanceId,Status:Status}' \
  --output table

# 需要行号时用 table-num（在表格最左侧加 # 列，从 1 开始）
ve ecs DescribeInstances \
  --query 'Result.Instances[*].{Name:InstanceName,Id:InstanceId}' \
  --output table-num

# 文本（Tab 分隔，便于 awk/grep；一行一条数据，可直接接 nl 加行号）
ve sts GetCallerIdentity --query 'Result.AccountId' --output text
ve ecs DescribeInstances --query 'Result.Instances[*].{Id:InstanceId}' --output text | nl

# 无 query 时 table 展示完整响应，并把嵌套结构拆成带标题的分区。
ve sts GetCallerIdentity --output table

# YAML
ve sts GetCallerIdentity --output yaml

# 只要退出码、不要正文（仍会发起 API 调用）
ve ecs DescribeInstances --output off
```

说明：

- **渲染层不删任何字段**：所有格式都如实展示拿到的数据，因此 `table` / `table-num` / `text` 与 `json` / `yaml` 一样会显示 `ResponseMetadata`（含 `RequestId`），各格式对「有哪些字段」的回答完全一致。`--output` 只决定排版：`table` 把 envelope 放进独立的带标题分区，`text` 以 `RESPONSEMETADATA` 作为行前缀。只想要一部分数据时请用 `--query`：`--query 'Result'` 去掉 envelope，`--query 'ResponseMetadata.RequestId'` 只取请求 ID，`--query '@'` 表示完整响应。
- **嵌套结构分区展示**：`table` 会把嵌套的对象/记录列表拆成带标题的独立分区（标题为字段路径，如 `Result.Instances.Tags[1]`），而不是把一坨 JSON 塞进单元格。嵌套字段**同时保留在主表列中**，单元格显示 `(see section)` 指向对应分区；若同一字段在部分记录里是标量、`null` 或空列表，这些值会照常显示在主表，不会因为别的记录是嵌套结构而丢失。分区编号从 1 开始，与 `table-num` 的 `#` 列对应，便于回溯是哪条记录。父列表只有一条记录时不加编号。纯标量列表（如 `["sg-1","sg-2"]`）仍保留在单元格内。
- **单条记录自动转竖表**：单个对象（以及任何单行结果）渲染成一条横向记录——字段名做表头行、值做一行，与 AWS CLI 一致。仅当**已知终端宽度且**该行超出宽度时，才自动转成 `Field | Value` 两列竖表，避免横向滚屏。多行结果、以及宽度未知时（重定向、管道、探测失败）保持横表。
- **终端宽度自适应**：输出到终端时自动探测宽度，超宽时优先压缩最宽的列并将单元格内容折成多行；不会用省略号丢弃响应值。每列保留最小可读宽度。输出重定向到文件或管道时不折行，保留完整的单行值。
- **列序规则**：使用 `--query` 多选哈希（`{Key:Path,...}`）时，`table` / `table-num` / `text` 的列序与你的书写顺序一致——**对记录列表和单个对象都生效**（单个对象体现为表头列的顺序）。其余情况（无 `--query`、只做路径投影、`merge()` 等无法静态确定列序的表达式、哈希内出现重复 key）按字段名字母序排列。提示与实际字段不完全匹配时整体回落字母序，不会部分生效或丢列。需要固定列序时请显式写多选哈希。
- **行号**：`table-num` 对记录结果加 `#` 列。单个对象是一条记录，编号为 `1`；列表从 1 开始按顺序编号。行号从 1 开始，仅用于人眼定位，不参与数据本身。脚本取值请用 `--output json` / `text`，不要依赖 `#` 列。
- **着色**：`enableColor` 开启且输出到终端时，`table` / `table-num` 的表头和单元格会着色；重定向、管道或设置 `NO_COLOR` 时不着色。着色**不影响**列宽与对齐。
- 不要对 `--output table` 使用 `nl`：`nl` 会把边框和表头一起编号，序号与数据行错位。需要行号请用 `--output table-num`（表格）或 `--output text | nl`（TSV）。
- `table` / `table-num` / `text` 会把换行、Tab 和终端控制字符显示为可见转义，避免响应内容破坏行列结构或触发终端控制序列。
- 为保持稳定的人读输出契约，`table` / `table-num` / `text` 中的布尔值显示为 `True` / `False`；`json` / `yaml` 仍按各自语法输出小写 `true` / `false`。
- 业务参数名与系统 flag 冲突时（如 `insight AgentChat` 的 `--query`），该 Action 后的双横线按业务参数解析，此时无法对该接口使用同名系统参数；详见「已知同名冲突」。
- 空列表：`table` / `table-num` 输出 `(empty)`；`text` 不输出行（便于脚本判断为空）。`--query` 命中缺失/null 时，table/text 输出 `None`。空对象 `{}` 不是空列表：table 只打表头行、没有值行，text 同样没有数据行。若非空列表中的记录全是空对象或空位置数组，table 会为每条记录保留一个 `{}` 或 `[]` 行（`table-num` 仍会编号），text 因没有字段或值可打印而不输出行。
- `text` 输出不区分类型：空列表 `[]` 和空对象 `{}` 都是无行；缺失/null 的 `--query` 路径输出 `None`；字面量字符串 `None` 同样输出为 `None`。需要无歧义的类型或空值判断时请使用 `--output json`。
- **`text` 递归摊平任意响应**：与 AWS CLI 的 text 一致，`text` 绝不输出 JSON 串。不带 `--query` 时整份响应被递归摊平成 TSV：对象的标量字段拼成一行，嵌套的对象/列表各自换行输出、并以大写字段名做前缀（如 `INSTANCELIST\t...`）。对象列表共享一套列集合（缺失字段为 `None`），每个对象一行。更深的位置数组或对象列表投影会递归展开，嵌套空列表或空对象不产生空白行，对象列序仍遵循 `--query` 多选哈希。顶层纯标量列表会拼成一行（Tab 分隔）；需要“每条记录一行”便于接 `nl`、`grep` 时，请用 `--query` 投影成扁平列表。
- `--output off` 仍会发起 API 请求且不写 stdout。它会跳过依赖响应数据的 `--query` 求值，但表达式语法、函数调用和精确数字安全规则仍会在请求前校验。
- **`--query` 错误在发请求前拦截**：语法错误、未知函数名（如 `lenght(@)`）、参数个数错误（如 `length(@, @)`）、表达式不完整（如 `a | [0`），以及违反精确数字安全规则的查询，都会在 API 调用之前报错。错误信息包含原始表达式、指向出错位置的 `^` 标记，以及可操作的修复提示；函数名拼错时会提示最接近的内置函数。非 ASCII 字段名需要用双引号包裹，如 `--query '"实例列表"."数据"'`。
- 通过预检的查询仍可能在对真实响应求值时失败，例如 `Result` 实际是对象，却使用 `starts_with(Result, 'x')`。这类错误发生在 API 调用成功之后，进程退出 1，并提示 `API call succeeded but response output failed`。使用 `--output off` 时会有意跳过这种依赖响应数据的求值。
- API 失败（例如 HTTP 403）把错误打到 stderr，不走 `--output` / `--query`。
- **查询的精确数字语义**：字段选择、投影、过滤、比较（`==` / `!=` / `<` / `>` / `<=` / `>=`）、`contains`、`max` / `min` / `sum` / `avg` / `abs` / `ceil` / `floor` / `to_number` / `sort`，以及按数字的 `max_by` / `min_by` / `sort_by`，全部按精确 JSON 十进制数值计算，例如 ``[?Cpu > `4`]``、``AccountId == `2106494982` `` 和 ``contains(Result.Numbers, `9007199254740993`)``。`1` / `1.0` / `1e0` 等等价写法判为相等，超过 2^53 的整数不会被舍入，投影仍输出原始 JSON token。只有 `avg` 可能舍入，且仅当精确结果是无限循环小数时发生，此时至少保留 34 位有效数字。十进制指数超过 10000 的 token（如 `1e20000`）参与算术会显式报错，而不会静默舍入；这类 token 的比较、排序和 `abs` 仍然精确。
- **YAML 数字**：`--output yaml` 会把响应整数输出为 `!!int` 标量，把小数和指数形式输出为 `!!float` 标量，同时保留原始 JSON 数字 token，包括超长整数、长小数、指数写法和末尾的零。数字不会被静默舍入，长小数也不会转成字符串。YAML 对象的 key 按字母序输出。yaml.v3 编码器对列表的缩进可能与旧版本不同；这只是展示格式变化，解析后的 YAML 数据语义一致，请勿依赖逐字节相同的 YAML 空白。`--query` 的书写顺序只影响 `table` / `table-num` / `text` 的列序，不影响 YAML key 顺序。

---

[← 配置管理](3-Configuration-zh.md) | 使用指南[(English)](4-Usage.md) | [高级用法 →](5-Advanced-zh.md)
