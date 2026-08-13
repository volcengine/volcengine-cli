[← 使用指南](4-Usage-zh.md) | 高级用法[(English)](5-Advanced.md)

---

## 高级用法

本文包含自动补全、彩色输出、debug 日志、`--force` 强制泛化调用和常见问题。这些能力不是调用 API 的必需步骤，但能提升日常使用效率和排障效率。

## 自动补全

CLI 支持生成 Bash、Zsh、fish 和 PowerShell 的补全脚本：

```shell
ve completion --help
```

### Bash

临时启用：

```shell
source <(ve completion bash)
```

每次打开 shell 自动启用：

```shell
echo 'source <(ve completion bash)' >> ~/.bashrc
source ~/.bashrc
```

系统级安装：

```shell
ve completion bash > /etc/bash_completion.d/ve
```

Bash 补全依赖 `bash-completion`。安装和检查：

```shell
# CentOS/RHEL
yum install bash-completion

# Debian/Ubuntu
apt-get install bash-completion

# 启用
source /usr/share/bash-completion/bash_completion

# 检查
type _init_completion
```

如果出现 `_get_comp_words_by_ref: command not found`，通常是 `bash-completion` 未安装或未 source。

macOS 使用 Homebrew 时：

```shell
ve completion bash > "$(brew --prefix)/etc/bash_completion.d/ve"
```

### Zsh

如果还没有启用 `compinit`：

```shell
echo "autoload -U compinit; compinit" >> ~/.zshrc
```

安装补全脚本：

```shell
ve completion zsh > "${fpath[1]}/_ve"
```

重新打开 shell，或执行：

```shell
source ~/.zshrc
```

### fish

临时启用：

```shell
ve completion fish | source
```

每次打开 shell 自动启用：

```shell
mkdir -p ~/.config/fish/completions
ve completion fish > ~/.config/fish/completions/ve.fish
```

### PowerShell

临时启用：

```powershell
ve completion powershell | Out-String | Invoke-Expression
```

保存脚本后，可在 PowerShell profile 中 source：

```powershell
ve completion powershell > ve.ps1
```

## 彩色输出

CLI 默认输出 JSON。可以启用彩色显示，便于终端阅读：

```shell
ve enable-color
```

关闭彩色显示：

```shell
ve disable-color
```

这两个命令会修改配置文件中的 `enableColor`。彩色输出影响 `ve configure get`、`ve configure list` 和 API 响应 JSON 的展示，不改变响应内容。

## Debug 日志

CLI debug 日志用于定位配置解析、参数构造和 SDK 调用问题。开启方式是设置环境变量：

```shell
VOLCENGINE_CLI_DEBUG=true ve sts GetCallerIdentity
```

关闭值：

```shell
VOLCENGINE_CLI_DEBUG=false
VOLCENGINE_CLI_DEBUG=0
VOLCENGINE_CLI_DEBUG=off
VOLCENGINE_CLI_DEBUG=no
VOLCENGINE_CLI_DEBUG=
```

其它非空值均视为开启。

开启后日志默认追加写入配置目录下的小时日志文件：

```text
~/.volcengine/logs/YYYYMMDDHH.log
```

例如：

```text
~/.volcengine/logs/2026061814.log
```

同一个小时内多次调用会追加到同一个文件。目录权限为 `0700`，日志文件权限为 `0600`。CLI 会拒绝写入符号链接或多硬链接日志文件，避免 debug 内容被追加到非预期文件。

debug 日志会记录：

- action 开始信息：service、action、version、method、content type。
- client 配置：profile 来源、凭证模式、region、endpoint、endpoint resolver、代理是否配置等。
- 参数构造结果：动态参数名、是否来自 `--body`、脱敏后的输入。
- SDK 请求尝试和请求结果。
- 错误阶段和耗时。

敏感字段会脱敏，例如 AK/SK、token、password、signature、private key 等常见字段。

排查示例：

```shell
VOLCENGINE_CLI_DEBUG=true ve sts GetCallerIdentity --region cn-beijing
tail -n 100 ~/.volcengine/logs/$(date +%Y%m%d%H).log
```

## 自升级（ve upgrade）

行为取决于安装来源：

| 安装来源 | 默认行为 |
|--------|--------|
| **Homebrew**（macOS/Linux；Homebrew/Linuxbrew/Cellar 路径） | 执行 `brew update` 再 `brew upgrade volcengine-cli`（需网络；不支持 `--version`） |
| **npm**（`node_modules/@volcengine/cli`） | 执行 `npm install -g @volcengine/cli@...`（需网络）；不原地替换二进制；失败时打印手动命令并以非 0 退出 |
| **standalone**（Release 解压、源码编译等） | 下载并原地替换当前二进制 |

```shell
ve upgrade              # 按安装来源：brew 委托 / npm 委托 / standalone 自升级
ve upgrade --yes        # standalone 原地升级跳过确认（不会代替包管理器升级）
ve upgrade --version 1.0.49
#   standalone：原地安装指定版本（必须高于当前版本；禁止降级）
#   npm：执行 npm install -g @volcengine/cli@1.0.49（目标低于当前时拒绝；失败时打印手动命令）
#   Homebrew：报错（请用 brew 管理版本）
```

standalone 只会安装**高于**当前运行版本的版本：未指定 `--version` 时升到 latest（manifest 滞后也不会回退）；指定 `--version` 时也必须比当前新。若需要更旧的版本，请从官方 Release 页重新下载安装。

standalone 升级流程：从官方 CDN（`https://cloudcache.volccdn.com/ve`）下载对应平台 zip 和校验文件，校验 SHA256，再原子替换当前可执行文件；失败会保留/回滚到旧版本。任一 CDN 产物不可用时回退 GitHub Releases。Windows 上会由临时 helper 在当前进程退出后完成替换，并通过同一 stdout/stderr 输出最终结果。

### 版本检测与升级提醒

执行任意 `ve` 命令时，CLI 可能在后台启动一次轻量版本检测（默认 24 小时最多一次，网络超时约 1.5s）。命令退出不会等待仍在进行的检测；若缓存或已完成的检测发现新版本，则向 **stderr** 输出提示（建议命令会按安装来源区分），**不会**写入 stdout，以免影响管道解析。

升级提醒按 **当前运行版本 + 本地自然日** 节流：同一 `current` 版本当天最多提醒一次；用户升级后 `current` 变化，即使当天仍可再提醒一次（例如 1.50→1.51 后 latest 已是 1.52）。状态记在检测缓存的 `noticed_at` / `noticed_current`。关闭检测时提醒一并关闭。

相关环境变量：

| 变量 | 说明 |
|-----|-----|
| `VOLCENGINE_CLI_DISABLE_UPDATE_CHECK=1` | 关闭后台版本检测与提醒 |
| `VOLCENGINE_CLI_UPDATE_CHECK_TTL_HOURS` | 检测缓存 TTL（小时），默认 24 |
| `VOLCENGINE_CLI_DOWNLOAD_BASE_URL` | 覆盖下载基址（默认 CDN） |
| `VOLCENGINE_CLI_INSTALL_METHOD` | 覆盖安装来源识别：`standalone` / `npm` / `homebrew` |

检测缓存文件：`~/.volcengine/cli/version_check.json`。

<a id="force-invocation"></a>

## 强制泛化调用 (`--force`)

CLI 内置了部分云产品的元数据，正常调用时会校验 service 和 action 是否存在。若产品或接口尚未收录、或元数据版本滞后，直接调用可能报 `unsupported action` 或 `unknown command`。此时可使用 `--force` 跳过 service/action 校验，强制发起 RPC 调用。

### 适用场景

- 调用 **metadata 中尚未收录** 的 service
- 调用 **已收录 service 下的新 action**
- 使用 **非内置元数据版本** 的 API（通过 `--version` 指定）

未知 API 参数在正常模式下已可透传；`--force` 主要解决的是 **service / action / API 版本** 层面的限制。

### 固定参数要求

| 参数 | 是否必填 | 说明 |
| --- | --- | --- |
| `--force` | 是 | 纯开关型参数，出现即表示启用；不接受 `true`/`false` 等后续赋值 |
| `--version` | 视 service | **未收录 service 时必填**；**已收录 service 可省略**，回落内置元数据版本。也可用于覆盖元数据中的 API 版本 |
| `--endpoint` | 视 service | 与正常调用相同：`--endpoint` > `endpoint-resolver=standard` > profile/env endpoint >（已收录）按 service+region 解析。**未收录**需要最终生效的**固定 host**（`endpoint-resolver=standard` 或 `auto-addressing` 不够） |
| `--method` | 否 | HTTP 方法：`GET` 或 `POST`；正常路径与 force 路径规则一致：显式值优先 → action 元数据 → 默认 `GET` |
| `--region` | 视凭证配置 | 与正常调用相同，必须能解析到 region |
| `--header` | 否 | **双横线保留控制参数**，`Name=Value`，可重复；自定义 HTTP 头。`Content-Type` 覆盖元数据；不进请求体。禁止 `Host`/`Authorization`/`Content-Length` |

注意：

- `--version` 指 **OpenAPI 接口版本**，不是 CLI 工具版本。查看 CLI 版本请用 `ve version` 或 `ve -v`。
- **endpoint 解析与是否 `--force` 无关**，与正常调用同一套规则。
- 未收录 service 没有元数据可推 host：需要固定 host（`--endpoint`，或 profile/`VOLCENGINE_ENDPOINT` 在**未**设置 `endpoint-resolver=standard` 时）。仅配置 `endpoint-resolver=standard` / `auto-addressing` 不够。
- 已收录 service 在 force 模式下可不传 `--version`，行为与正常路径一致（如 `ve sts GetCallerIdentity --force`）。
- `--method` 在正常路径与 force 路径使用相同解析顺序：显式 `--method` 覆盖元数据；未指定时优先用已收录 action 的 Method；均无则默认 `GET`（不因 `--force` 而改变）。
- 对外系统参数统一用 **双横线** `--`（含 `--force` / `--version` / `--method`）。HTTP 头/JSON body 用双横线保留控制参数 **`--header` / `--body`**（见 [使用指南](4-Usage-zh.md#双横线保留控制参数)）。
- `--force` 是**纯开关**：只写 `--force` 本身。不要写 `--force true` 或 `--force false`；其后的 token 会被当成位置参数（常被误当成 action 名）。

### 示例

常规元数据校验调用：

```shell
ve rds_mysql ModifyDBInstanceIPList \
  --InstanceId mysql-xxxxxx \
  --GroupName default \
  --IPList '["10.20.30.40"]'
```

强制调用未收录 service 的新接口：

```shell
ve newservice DescribeNewResource \
  --version 2024-01-01 \
  --endpoint open.volcengineapi.com \
  --SomeParam value \
  --force
```

已知 service、未知 action（可省略 `--version`，回落 service 元数据版本）：

```shell
ve sts SomeNewAction \
  --region cn-beijing \
  --Param1 value \
  --force
```

已知 service、已知 action，仅跳过校验：

```shell
ve sts GetCallerIdentity --region cn-beijing --force
```

指定 endpoint 并覆盖 API 版本：

```shell
ve ecs DescribeInstances \
  --version 2024-01-01 \
  --endpoint ecs.cn-beijing.volcengineapi.com \
  --region cn-beijing \
  --force
```

### 未收录 service 的帮助

`ve <unknown-service> -h` 或只输入 service 名时，会输出 force 调用说明，而不是报错：

```shell
ve newservice -h
ve newservice
```

### 常见错误

默认展示英文；使用 `--lang ZH`（或中文 locale）时，force 相关报错会显示为简体中文。英文示例如下：

未收录 service 缺少 `--version`：

```text
--version is required when using --force for service "newservice"
```

未收录 service 且无可用**固定** endpoint（未传 `--endpoint`，或仅有 `endpoint-resolver=standard` / `auto-addressing`）：

```text
endpoint is required for unlisted service "newservice": set --endpoint, or configure endpoint in the profile / VOLCENGINE_ENDPOINT (endpoint-resolver=standard alone is not enough)
```

未收录 service 且未加 `--force`：

```text
unknown service "newservice": use --force with --version, and a fixed endpoint via --endpoint or profile/VOLCENGINE_ENDPOINT (endpoint-resolver=standard alone is not enough)
```

误把 `--force` 写成带值开关（`true` 会被当成位置参数 / action）：

```text
# 错误：不要在 --force 后跟 true/false
ve newservice true --version 2024-01-01 --endpoint open.volcengineapi.com --force true

# 正确
ve newservice DescribeNewResource --version 2024-01-01 --endpoint open.volcengineapi.com --force
```

## 常见问题

### 如何开启 debug？

debug 不是 CLI 系统参数，当前只通过 `VOLCENGINE_CLI_DEBUG` 环境变量开启：

```shell
VOLCENGINE_CLI_DEBUG=true ve sts GetCallerIdentity
```

对外系统参数：

```text
--profile, --region, --endpoint, --lang, --force, --version, --method
```

双横线保留控制参数：

```text
--header, --body
```

### 为什么提示缺少 region？

API 调用时必须能解析到 region。设置方式按优先级为：

1. `--region`
2. profile 中的 `region`
3. `VOLCENGINE_REGION`

示例：

```shell
ve sts GetCallerIdentity --region cn-beijing
```

或：

```shell
ve configure set --profile prod --region cn-beijing
```

### 为什么配置了环境变量但没有生效？

如果当前存在 current profile，CLI 会优先使用 profile。环境变量默认凭证链主要在没有活跃 profile 时使用。

可以临时指定 profile：

```shell
ve sts GetCallerIdentity --profile prod
```

也可以切换 current：

```shell
ve configure profile --profile prod
```

### 为什么 SSO 配置完成后仍用旧账号？

`ve configure sso` 只写入 SSO profile，不会切换 current。执行：

```shell
ve configure profile --profile my-dev
```

### 无图形界面怎么登录？

SSO：

```shell
ve configure sso --profile my-dev --sso-session my-sso --no-browser
ve sso login --sso-session my-sso --no-browser
```

Console Login：

```shell
ve login --profile dev --region cn-beijing --remote
```

### JSON body 报 `json format error` 怎么办？

`--body` 只接受 JSON object 或 JSON array。检查引号和 shell 转义：

```shell
ve rds_mysql ModifyDBInstanceIPList \
  --body '{"InstanceId":"mysql-xxxxxx","GroupName":"default","IPList":["10.20.30.40"]}'
```

不要把 `--body` 和其它 API 参数混用。

---

[← 使用指南](4-Usage-zh.md) | 高级用法[(English)](5-Advanced.md)
