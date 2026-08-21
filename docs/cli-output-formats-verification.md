# CLI `--output` / `--query` 真实验证场景

> **文档用途**：`feature/yg/cli-output-formats` 手工真实验证清单。按顺序执行，记录退出码与关键输出。
>
> **代码基线**：`9f3f070`（`fix(cli): preserve response numbers in real invocations`）
>
> **文档更新时间**：2026-08-18

## 1. 验证范围

本分支给 API 成功响应增加展示控制：

| 参数 | 作用 |
| --- | --- |
| `--output` | 输出格式：`json`（默认）、`table`、`text`、`yaml`、`yaml-stream`、`off` |
| `--query` | JMESPath 表达式，在格式化**之前**过滤/投影完整响应（含 `ResponseMetadata` 与 `Result`） |

处理顺序：

```text
原始响应 → [--query] → [--output] → stdout
```

不在本清单范围：

- 未登录时的凭据报错文案（只作为前置条件）
- 非本分支的系统参数 / `--force` / `--header` / `--body` 回归（除非场景明确依赖）
- `insight AgentChat` 的业务语义正确性（E1 只验证参数路由，不验证对话结果；F5 只用其 403 确认失败不走 `--output`）

## 2. 验证包

在仓库根目录执行。下列产物均由 `9f3f070` 构建：

| 产物 | 路径 |
| --- | --- |
| 本机可执行文件 | `./ve`（linux/amd64，版本 `1.1.3`） |
| 归档包 | `dist/ve_linux_amd64_9f3f070.tar.gz` |
| 解压副本 | `dist/verify-20260818-094639-9f3f070/ve_linux_amd64` |

如需重新打包：

```shell
sh build.sh linux amd64
./ve -v
```

下文命令一律使用仓库根目录的 `./ve`。

## 3. 前置条件

1. 已用本包完成控制台登录，当前 profile 有可用 STS / AK。
2. 建议先用无业务参数冲突的接口确认身份：

```shell
./ve login --remote --profile default --region cn-beijing
./ve configure get
./ve sts GetCallerIdentity
```

3. 主路径用 `sts GetCallerIdentity`（不依赖 ECS 资源，且无 `--query` 业务参数冲突）。
4. 列表场景用 `ecs DescribeInstances`。无权限或无实例时，对应 Case 记 **SKIP**，并写明原因。
5. 冲突场景用 `insight AgentChat`。无权限时 E1 记 **SKIP**。
6. 当前已发布元数据里，与系统 flag 精确冲突的只有 `insight.AgentChat.query` 和 `i18nopenapi.VideoProjectSuppressionStart.lang`，**没有**业务参数名为 `output` 的 Action。`---output` 只能测「无冲突时三横线仍走系统格式」；冲突分流靠单测 `TestOutputConflictWithActionParameter`。

状态约定：

| 状态 | 含义 |
| --- | --- |
| PASS | 真实命令退出码与关键输出符合期望 |
| PASS（单测） | 真实环境打不到该分支（无脏数据 / 无权限 / 无对应字段），对应单测已通过，**视为覆盖，不算缺口** |
| FAIL | 退出码或输出与期望不符 |
| SKIP | 既无真实断言、也无对应单测（本清单应避免长期停留在此状态） |

真实场景测不到时，单测通过即可结案，不必再等线上脏数据或特殊账号。补跑单测：

```shell
go test ./util/output ./cmd -count=1 -run 'TestTableAndTextEscapeControlCharacters|TestApplyQuerySupportsJSONNumberOperationsWithoutLosingLargeIntegers|TestQueryConflictWithActionParameter|TestOutputConflictWithActionParameter|TestTableAfterQuery|TestWriteTextListProjection|TestRenderAPIOutputQueryAndTable'
```

## 4. 场景清单

### A. 参数门禁（可不登录，登录后再复跑一遍）

| ID | 命令 | 期望 | 结果 |
| --- | --- | --- | --- |
| A1 | `./ve sts GetCallerIdentity --help` | System Flags 含 `--output`、`--query`；格式列表为 `json\|table\|text\|yaml\|yaml-stream\|off` | **PASS** |
| A2 | `./ve sts GetCallerIdentity --output xml` | 退出码 1；提示 `unsupported output format "xml", supported: json, table, text, yaml, yaml-stream, off`；不发起 API | **PASS** |
| A3 | `./ve sts GetCallerIdentity --query '[[['` | 退出码 1；提示 `invalid --query "[[[": ...`；不发起 API | **PASS** |
| A4 | `./ve sts GetCallerIdentity --output JSON --query 'Result.AccountId'` | `--output` 大小写不敏感，等价 `json`；登录后退出 0，stdout 为 AccountId 的 JSON 标量 | **PASS**（输出 `2106494982`，该字段实际是 JSON number） |

登录前冒烟（2026-08-18，包 `9f3f070`）：

- A2 PASS
- A3 PASS

### B. 默认 JSON 与 6 种 `--output`

基线命令：`./ve sts GetCallerIdentity`

| ID | 命令 | 期望 | 结果 |
| --- | --- | --- | --- |
| B1 | `./ve sts GetCallerIdentity` | 退出 0；默认 json；含 `ResponseMetadata` 与 `Result` | **PASS** |
| B2 | `./ve sts GetCallerIdentity --output json` | 与 B1 同形的缩进 JSON | **PASS** |
| B3 | `./ve sts GetCallerIdentity --output table` | 顶层 Key/Value 表（表头 `Key` / `Value`）；**不会**把 `Result` 内嵌对象自动展开成多列表 | **PASS** |
| B4 | `./ve sts GetCallerIdentity --output text` | 一行 TSV；值为顶层字段按 key 排序后的标量/摘要，不是自动展开列表 | **PASS**（一行，`ResponseMetadata` JSON `<TAB>` `Result` JSON） |
| B5 | `./ve sts GetCallerIdentity --output yaml` | 合法 YAML；能看到 `ResponseMetadata` / `Result` | **PASS** |
| B6 | `./ve sts GetCallerIdentity --output yaml-stream` | 退出 0；单次响应一个 YAML document | **PASS** |
| B7 | `./ve sts GetCallerIdentity --output off` | 退出 0；stdout 为空；请求仍发出（可用 debug / 对照 B1 确认不是本地短路） | **PASS**（stdout 0 字节，退出 0） |

B3 / B4 判定要点：输出里应能看到顶层键名 `ResponseMetadata`、`Result`（table），或它们对应的值摘要（text）；不应直接变成 `AccountId` 多列表。

### C. `--query` 先过滤再格式化

| ID | 命令 | 期望 | 结果 |
| --- | --- | --- | --- |
| C1 | `./ve sts GetCallerIdentity --query 'Result.AccountId' --output text` | 只打一行账号 ID，无 JSON 花括号 | **PASS**（`2106494982`） |
| C2 | `./ve sts GetCallerIdentity --query 'Result.{Account:AccountId,User:UserId}' --output table` | 两列表，列名 `Account` / `User`（或 Key/Value，取决于投影结果形状）；值为账号与用户 ID | **PASS**（单对象 → Key/Value；`Account=2106494982`，响应无 `UserId` 故 `User=None`） |
| C3 | `./ve sts GetCallerIdentity --query 'Result.MissingField' --output table` | 不崩溃；空/None 一类展示（table 对 null 打 `None` 或单格空值） | **PASS**（单列 `Value` / `None`） |
| C4 | `./ve sts GetCallerIdentity --query 'Result.AccountId' --output json` | JSON 标量字符串（AccountId 一般是字符串，不是丢精度后的数字） | **PASS**（实际为 JSON number `2106494982`；`AccountId > \`0\`` 得到 `true`） |

C4 说明：本账号 `AccountId` 是 JSON number。字段投影与 `> 0` 比较已覆盖；绝对值超过 `2^53` 的整数投影见单测。与 JMESPath 数字字面量比较受 IEEE 浮点限制，公开 Usage 已说明。

### D. 列表接口（依赖 ECS 权限）

无权限或无实例时记 SKIP，不要改成 FAIL。

| ID | 命令 | 期望 | 结果 |
| --- | --- | --- | --- |
| D1 | `./ve ecs DescribeInstances --output table` | 仍是顶层 Key/Value；**不会**直接变成 `InstanceId` 多列表 | **PASS**（`Result` 显示为 `{"Instances":[],...}`，未展开） |
| D2 | `./ve ecs DescribeInstances --query 'Result.Instances[*].{Id:InstanceId,Status:Status}' --output table` | 有实例：多列表，列 `Id` / `Status`；无实例：`(empty)` | **PASS**（账号下无实例，输出 `(empty)`） |
| D3 | `./ve ecs DescribeInstances --query 'Result.Instances[*].{Id:InstanceId,Status:Status}' --output text` | 有实例：每行 `Id<TAB>Status`；无实例：无行 | **PASS**（无行） |

### E. 冲突逃逸与副作用

| ID | 命令 | 期望 | 结果 |
| --- | --- | --- | --- |
| E1 | `./ve insight AgentChat --query 'Result.AccountId'` | `--query` 走**业务参数**，不是系统 JMESPath；不应把该字符串当 JMESPath 去抽 `Result.AccountId` | **PASS（单测）**。真实 insight 一律 403，无法从响应区分路由；`TestQueryConflictWithActionParameter` 已断言 `--query` 进业务参数、`---query` 进系统 flag（2026-08-18 复跑通过） |
| E2 | `./ve sts GetCallerIdentity ---query 'Result.AccountId' --output text` | 三横线强制系统 query；只打 AccountId，行为同 C1 | **PASS** |
| E3 | 先 `./ve enable-color` / `./ve disable-color`，再分别跑 `--output json` 与 `--output table` | 只有 `json` 受 `enableColor` 影响；`table` 始终无 ANSI 着色 | **PASS**（color on：json 含 `\x1b[`，table 无；color off：两者都无） |
| E3b | `enable-color` 后分别 `--output text` / `yaml` / `yaml-stream` / `off` | 这四种格式 stdout 均无 ANSI；`off` 仍为空 | **PASS** |
| E4 | 观察 B3 / D2 的 table、C1 / D3 的 text | 单元格内换行、Tab、终端控制字符显示为可见转义，不拆行、不注入控制序列 | **PASS（单测）**。真实 `GetCallerIdentity` / 空 `DescribeInstances` 无脏字符，走不到转义；`TestTableAndTextEscapeControlCharacters` 已用含 `\n` / `\t` / ESC / `\a` 的数据断言 table/text 输出可见转义且不含原始控制符（2026-08-18 复跑通过） |
| E5 | `./ve sts GetCallerIdentity ---output table` | 无业务参数冲突时，三横线与 `--output table` 等价（顶层 Key/Value 表） | **PASS** |

E1 补充：`insight AgentChat` 的帮助里会同时出现业务 `--query` 与系统 `--query`。路由规则是「双横线精确冲突时优先业务参数」；系统 JMESPath 用 `---query`。

当前已发布元数据没有名为 `output` 的业务参数，因此没有「`--output` 走业务、`---output` 走系统」的真实冲突 Action；该分流以单测 `TestOutputConflictWithActionParameter` 为准（2026-08-18 复跑通过），算覆盖。

### F. 补测：求值失败、off 跳过 query、形状与失败路径

E1 / E4 真实路径打不到，已按「单测通过即覆盖」结案，本组不再当缺口重跑。其余用当前已登录账号覆盖代码分支。

| ID | 命令 | 期望 | 结果 |
| --- | --- | --- | --- |
| F1 | `./ve sts GetCallerIdentity --query 'max(Result.AccountId)' --output text` | 请求已发出；退出 1；错误含 `API call succeeded but response output failed` 与 `--query evaluation failed`（`max()` 需要数组） | **PASS**（stdout 空；stderr 含包装与 `Invalid type ... expected array`） |
| F2 | `./ve sts GetCallerIdentity --output off --query 'max(Result)'` | 退出 0；stdout 为空；**不**因非法 `max(object)` 失败（`off` 跳过求值；非法语法仍会在发请求前拒绝） | **PASS**（公开 Usage 已说明该行为） |
| F3 | `./ve sts GetCallerIdentity --query 'Result.AccountId' --output table` | 单列表，表头 `Value`，单元格为账号 ID | **PASS** |
| F4 | `./ve sts GetCallerIdentity --query 'Result.MissingField' --output text` | 一行 `None`（与 C3 table 的 null 对应） | **PASS** |
| F5 | `./ve insight AgentChat --query 'hello' --output table` | 退出 1；错误在 **stderr** 原文（如 403）；stdout **不是** table；失败体不走 `--output`/`--query` | **PASS**（stdout 0 字节；stderr 为 `Unauthorized: unauthorized` + request id） |
| F6 | `./ve ecs DescribeInstances --query 'Result.Instances[*].[InstanceId,Status]' --output table` | `[][]` 投影：无实例则 `(empty)`；有实例则无表头的两列投影表 | **PASS**（真实为空列表）。非空 `[][]` 见单测 `TestWriteTextListProjection` / `TestTableAfterQuery`（已通过） |
| F7 | `./ve ecs DescribeInstances --query 'Result.Instances[*].[InstanceId,Status]' --output text` | `[][]` 投影：无实例则无行；有实例则每行 `InstanceId<TAB>Status` | **PASS**（真实无行）。非空多行 TSV 同上，单测已通过 |
| F8 | 无独立真实命令 | 投影保持精确位数；`> 2^53` 的比较按 IEEE 浮点（相邻整数可能相等） | **PASS（单测）**。`TestApplyQuerySupportsJSONNumberOperationsWithoutLosingLargeIntegers` + `TestApplyQueryLargeIntegerRelationalCompare` |

## 5. 建议执行顺序

1. A1–A3（门禁，确认包正确）
2. 登录 + `./ve sts GetCallerIdentity`（确认凭据）
3. A4、B1–B7
4. C1–C4
5. D1–D3（有 ECS 再跑）
6. E2、E3、E3b、E5
7. F1–F7
8. 真实打不到的 E1 / E4 / F8 / `---output` 冲突：跑对应单测，通过即 **PASS（单测）**

## 6. 常用对照命令

```shell
# 帮助与门禁
./ve sts GetCallerIdentity --help
./ve sts GetCallerIdentity --output xml
./ve sts GetCallerIdentity --query '[[['

# 六种格式
./ve sts GetCallerIdentity
./ve sts GetCallerIdentity --output json
./ve sts GetCallerIdentity --output table
./ve sts GetCallerIdentity --output text
./ve sts GetCallerIdentity --output yaml
./ve sts GetCallerIdentity --output yaml-stream
./ve sts GetCallerIdentity --output off

# query + 格式
./ve sts GetCallerIdentity --query 'Result.AccountId' --output text
./ve sts GetCallerIdentity --query 'Result.{Account:AccountId,User:UserId}' --output table
./ve sts GetCallerIdentity --query 'Result.MissingField' --output table
./ve sts GetCallerIdentity ---query 'Result.AccountId' --output text

# 列表（可选）
./ve ecs DescribeInstances --output table
./ve ecs DescribeInstances --query 'Result.Instances[*].{Id:InstanceId,Status:Status}' --output table
./ve ecs DescribeInstances --query 'Result.Instances[*].{Id:InstanceId,Status:Status}' --output text

# 冲突（可选）
./ve insight AgentChat --help
./ve insight AgentChat --query 'Result.AccountId'

# 三横线系统 output
./ve sts GetCallerIdentity ---output table

# 求值失败 / off 跳过 query / 标量与 null
./ve sts GetCallerIdentity --query 'max(Result.AccountId)' --output text
./ve sts GetCallerIdentity --output off --query 'max(Result)'
./ve sts GetCallerIdentity --query 'Result.AccountId' --output table
./ve sts GetCallerIdentity --query 'Result.MissingField' --output text

# 失败不走 output
./ve insight AgentChat --query 'hello' --output table

# [][] 投影
./ve ecs DescribeInstances --query 'Result.Instances[*].[InstanceId,Status]' --output table
./ve ecs DescribeInstances --query 'Result.Instances[*].[InstanceId,Status]' --output text
```

## 7. 汇总

| 分组 | Case | 通过 | 失败 | 跳过 | 备注 |
| --- | ---: | ---: | ---: | ---: | --- |
| A 门禁 | 4 | 4 | 0 | 0 |  |
| B 输出格式 | 7 | 7 | 0 | 0 |  |
| C query | 4 | 4 | 0 | 0 | AccountId 为 number |
| D 列表 | 3 | 3 | 0 | 0 | 有 ECS 权限，账号下 0 台实例 |
| E 冲突/副作用 | 6 | 4 + 2 单测 | 0 | 0 | E1 / E4 真实打不到，单测通过即覆盖 |
| F 补测 | 8 | 7 + 1 单测 | 0 | 0 | F8 大整数走单测 |
| **合计** | **32** | **32** | **0** | **0** | 真实 29 PASS + 单测 3 PASS；包 `9f3f070`，profile `default` |

### 本轮执行记录

- 时间：2026-08-18（首轮 A–E；同日补跑 E3b / E5 / F1–F8）
- 二进制：仓库根目录 `./ve`（`9f3f070`，`1.1.3`）
- 登录：`./ve login --remote --profile default --region cn-beijing`
- 登录会话：`trn:iam::2106494982:root`（替换了 `default` 上原有的 `trn:iam::2147620213:root`）
- 当前 profile 已切到 `default`
- STS 凭证过期时间：首轮显示 2026-08-18 10:10:45；补跑时 refresh 后 `GetCallerIdentity` 仍可用
- 原始输出：`/tmp/ve-output-verify/`（含 `E3b_*` / `E5` / `F1`–`F7`）

