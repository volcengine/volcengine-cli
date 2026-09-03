# Agent 调用者环境变量调研：GitHub Copilot CLI、Kimi Code CLI、Kiro CLI 与 Cline

调研日期：2026-09-03（Asia/Shanghai）

## 结论摘要

这里的“识别”指：`volcengine-cli` 被 Agent 作为 Shell 命令、Git hook、stdio MCP server 或 hook 子进程启动后，能否仅从自身进程环境判断调用来源。它不包括“机器上安装了某个 CLI”或“用户配置过某个模型”。

| 产品与调用路径 | 明确信号 | 结论 | 建议 |
| --- | --- | --- | --- |
| GitHub Copilot CLI → Shell / Git hook | `COPILOT_CLI=1` | 官方明确称其可用于检测 Copilot CLI 子进程；当前 1.0.82 仍存在 | 严格按值等于 `1` 判断，首选 |
| GitHub Copilot CLI → Shell / stdio MCP | `COPILOT_AGENT_SESSION_ID` 非空 | 官方明确注入 Shell 命令与 MCP server；当前 1.0.82 两条路径均存在 | 可作跨路径辅助信号；不要校验值格式 |
| GitHub Copilot CLI → stdio MCP | `COPILOT_CLI=1` | 当前 1.0.82 实测不存在 | 不可用来覆盖 MCP 路径 |
| Kimi Code CLI 0.40.1 → Bash | 无 Kimi 专属调用者变量 | 最新官方二进制的 `!` Shell mode 与模型驱动 `Bash` tool 均已隔离实测；当前源码只继承父环境并覆盖通用非交互变量 | 不应根据任意 `KIMI_*` 猜测 |
| Kimi Code CLI 0.40.1 → stdio MCP | 无 Kimi 专属调用者变量 | 当前源码与官方二进制实测均未注入身份/会话变量 | 暂无可靠 env-only 规则 |
| Kimi Code CLI → plugin hook | `KIMI_CODE_HOME`、`KIMI_PLUGIN_ROOT` | 仅 plugin hook 的范围限定注入，不覆盖普通 Bash/MCP | 不可作为通用 Kimi Agent 识别规则 |
| Kiro CLI 2.21.0 → `!` Shell mode | `KIRO_SESSION_ID=<UUID>` | 官方当前发行物隔离实测；每次 session 重新生成，并覆盖父环境同名输入 | 可作当前 Kiro session 的强实用信号；上游未承诺为稳定接口 |
| Kiro CLI 2.21.0 → Agent `shell` tool | `KIRO_SESSION_ID=<UUID>`、`AWS_EXECUTION_ENV=AmazonQ-For-CLI Version/2.21.0` | 官方当前发行物隔离实测；后者是工具执行路径专门追加的 AWS user-agent 元数据 | 优先认 `KIRO_SESSION_ID`；`AWS_EXECUTION_ENV` 只能校验具体 token，不能只判断存在 |
| Kiro CLI 2.21.0 → 其他启动层/历史版本 | `KIRO_CLI_ACP_CLIENT_NAME`、`KIRO_VERSION` 等 | 历史 issue 的 TUI/hook 路径中出现，但本次直接 CLI Shell 两条路径没有 | 不可作为所有 Kiro CLI 调用的必要条件 |
| Cline VS Code 扩展 4.1.17 → Cline 创建的 Terminal | `CLINE_ACTIVE=true` | 官方 tag 源码的两条建终端路径均显式注入；官方 Marketplace VSIX 的 next/legacy bundle 也均包含同一行为 | 当前最明确的 Cline IDE Shell 信号；严格按值 `true` 判断 |
| Cline CLI 3.0.61 → `run_commands` | `CLINE_CONNECTOR_CLI_LAUNCH=<JSON>` | 官方 npm 二进制隔离实测会主动生成并覆盖父环境伪值；`CLINE_ACTIVE` 不会由 CLI 设置 | 可作当前 Cline CLI/宿主运行时的实用信号；它是 connector 启动元数据，不是公开 caller 契约，也非 standalone CLI 独占 |

面向当前产品代码的结论是：

```text
严格识别 GitHub Copilot CLI 的 Shell 子进程：COPILOT_CLI == "1"

覆盖已验证的 Copilot CLI Shell + MCP 场景：
COPILOT_CLI == "1" OR COPILOT_AGENT_SESSION_ID != ""

Kimi Code CLI：没有可推荐的环境变量规则

Kiro CLI 2.21.0：KIRO_SESSION_ID != "" 是当前发行物两种 Shell 路径均命中的实用规则；
若只识别 Agent shell tool，还可辅助检查 AWS_EXECUTION_ENV 是否包含
"AmazonQ-For-CLI Version/"，但不能只判断 AWS_EXECUTION_ENV 存在

Cline 4.1.17 IDE 扩展：CLINE_ACTIVE == "true" 是当前 Cline 创建终端的直接信号；
Cline CLI 3.0.61 不设置该变量，当前可见的主动注入信号是合法且非空的
CLINE_CONNECTOR_CLI_LAUNCH JSON，但它属于内部 connector 启动元数据
```

第二条会把“处在 Copilot Agent session 中”视为 GitHub Copilot 调用。它比只认 CLI Shell 的范围更大；若未来其他 Copilot host 复用 `COPILOT_AGENT_SESSION_ID`，可能把它们也归为 Copilot。若产品要求严格区分“Copilot CLI”和其他 Copilot host，应按调用通道分别处理，而不是无条件扩大规则。

## 证据等级

### 1. 正式 reference/spec 级公开稳定契约

没有找到任何一方对“所有运行形态下的子进程调用者身份环境变量”作出的正式、带兼容性承诺的 reference/spec 级契约。

- GitHub 的正式 [Copilot CLI 环境变量参考表](https://github.com/github/docs/blob/6f3a3dc662821c85f36a5cf3cf5507d18e269bc6/content/copilot/reference/copilot-cli-reference/cli-command-reference.md#L632-L665) 列的是用户输入的配置变量，没有列 `COPILOT_CLI` 或 `COPILOT_AGENT_SESSION_ID`。
- Kimi Code 的官方 [Environment variables 页面](https://www.kimi.com/code/docs/en/kimi-code-cli/configuration/env-vars.html) 说明环境变量用于少量运行行为，并在同页给出完整分类；其中没有调用者身份或 Agent session 环境变量。对应当前源文见 [env-vars.md](https://github.com/MoonshotAI/kimi-code/blob/26dd6ce6aec4456867af010b55dff92ba58f04ee/docs/en/configuration/env-vars.md#L1-L10)。
- Cline 当前 [CLI Environment Variables](https://docs.cline.bot/cli/cli-reference#environment-variables) 列的是配置、存储、sandbox、hook 与命令权限变量，没有列 `CLINE_ACTIVE` 或 CLI caller 标记。Cline 的历史官方文档曾明确用 `CLINE_ACTIVE` 识别它创建的 IDE 终端，但这不是覆盖 CLI、SDK 和后台执行模式的统一契约。
- Kiro 的官方 [Settings 环境变量表](https://kiro.dev/docs/reference/settings.md#environment-variables) 与 [CLI logging 环境变量表](https://kiro.dev/docs/reference/cli-commands.md#log-files) 只列用户配置项，没有声明 `KIRO_SESSION_ID` 或其他 Shell caller 标识的兼容性契约。

因此，下面的 Copilot 信号应准确表述为“官方公开发布说明中的有意注入行为”，而不是宣称它们属于正式规范。尽管如此，`COPILOT_CLI=1` 的官方文字直接把“检测 Copilot CLI 子进程”作为用途，实际可靠性很高。

### 2. 官方公开发布说明中的有意注入行为

#### `COPILOT_CLI=1`

GitHub Copilot CLI 0.0.421（2026-03-03）的官方 changelog 明确写道：Git hooks 可以通过 `COPILOT_CLI=1` 检测 Copilot CLI 子进程。参见：

- [固定 commit 的 changelog](https://github.com/github/copilot-cli/blob/689d1486a101d9752906f1c6b268eba532f915dc/changelog.md#L3-L11)
- [v0.0.421 release](https://github.com/github/copilot-cli/releases/tag/v0.0.421)

这不是一个“用户先设置、CLI 再读取”的配置变量，而是 CLI 给其执行环境提供的调用来源信号。当前 1.0.82 实测确认普通 Shell 命令仍会收到它。

范围限制：

- 已确认：Copilot CLI 的普通 Shell 命令，以及该命令继续触发的 Git hook/后代进程。
- 未覆盖：当前 1.0.82 的本地 stdio MCP server，实测其中没有 `COPILOT_CLI`。
- 环境变量天然会继续向后代进程传播。因此它准确表达的是“进程祖先链中有 Copilot CLI 的 Shell 执行环境”，不能绝对证明 Copilot CLI 是直接父进程。
- 用户可以手工伪造；嵌套 Agent 也可能把它继续传下去。建议严格判断值为 `1`，不要只判断变量名存在或非空，以减少误报。

#### `COPILOT_AGENT_SESSION_ID`

GitHub Copilot CLI 1.0.29（2026-04-16）的官方 changelog 明确写道，Shell commands 和 MCP servers 都会收到 `COPILOT_AGENT_SESSION_ID`：

- [固定 commit 的 changelog](https://github.com/github/copilot-cli/blob/709b59e3f09291b92a52322bcec4a5c7ee7d591c/changelog.md#L3-L9)
- [v1.0.29 release](https://github.com/github/copilot-cli/releases/tag/v1.0.29)

当前 1.0.82 的 Shell 与 stdio MCP 隔离实测均确认该变量存在。实测值是 UUID 形态，但官方没有承诺格式，所以代码只应判断非空，不应加入 UUID 正则。

它对“当前处在 Copilot Agent session 的子进程里”是强信号，但名字和官方描述没有承诺它永远只由独立 Copilot CLI 设置。因此：

- 要严格识别 CLI Shell，优先 `COPILOT_CLI == "1"`。
- 要覆盖 Copilot CLI 的 Shell 与 MCP 两条已验证路径，可以把 `COPILOT_AGENT_SESSION_ID != ""` 作为 OR 条件。
- 不应记录或上报该变量的原值；识别只需要布尔存在性。

### 3. 源码/发行包中的内部或范围限定变量

以下变量不能替代上述规则：

| 变量 | 为什么不适合作通用调用者识别 |
| --- | --- |
| `COPILOT_CLI_BINARY_VERSION` | 当前 SEA 单文件发行包会设置，源码/非 SEA 启动模式不保证；它描述发行加载方式，不是调用者契约 |
| `COPILOT_RUN_APP` | loader 内部实现细节，可能随打包方式变化 |
| `COPILOT_LOADER_PID` | loader/进程管理元数据，不是公开的调用者标记 |
| `KIMI_CODE_UPDATE_REEXEC` | Kimi 原生更新 swap/re-exec 的防循环内部标志，而且启动后会删除；只覆盖特殊更新路径。源码注释见 [constant/app.ts](https://github.com/MoonshotAI/kimi-code/blob/a3b48a7272880dafb64e4c403006d94dc781d05c/apps/kimi-code/src/constant/app.ts#L58-L64) |
| `KIMI_CODE_DEV_SERVER`、`KIMI_CODE_PLUGIN_MARKETPLACE_FROM_DEV_SERVER` | 开发/测试或 marketplace 路径开关，不存在于正常调用的全部场景 |

GitHub 官方仓库的 [issue #2107](https://github.com/github/copilot-cli/issues/2107#issue-4090204790) 也报告了 `COPILOT_CLI_BINARY_VERSION` 只覆盖 SEA mode、`COPILOT_RUN_APP` 属 loader 内部细节，并请求给 MCP server 增加稳定的 CLI-specific 标记。该 issue 是用户提案而非正式规范或维护者承诺，只作为范围风险的补充证据。

Kimi plugin hook 是另一个必须单独看待的范围限定场景。官方文档说明，plugin hook 子进程会额外收到 `KIMI_CODE_HOME` 和 `KIMI_PLUGIN_ROOT`，见 [Hooks in Plugins](https://github.com/MoonshotAI/kimi-code/blob/a3b48a7272880dafb64e4c403006d94dc781d05c/docs/en/customization/plugins.md#L444-L467)。但这只能说明“处在 Kimi plugin hook 中”，不能覆盖普通 Bash、普通 hook 或 stdio MCP。

### 4. 仅配置/认证用途的变量

这些变量可以在启动 Agent 之前由用户、CI、包管理器或其他工具设置。它们会被子进程继承，但“存在”不等于“调用者是对应 Agent”。

GitHub Copilot CLI 的典型例子：

- `COPILOT_GITHUB_TOKEN`、`GH_TOKEN`、`GITHUB_TOKEN`：认证；官方 README 也把后两者作为用户提供 PAT 的方式，见 [README](https://github.com/github/copilot-cli/blob/be82101e70f0253b57519bebb9cc9d0f6dfb2ed2/README.md#L109-L117)。
- `COPILOT_HOME`：配置与状态目录。
- `COPILOT_MODEL`、`COPILOT_ALLOW_ALL`、`COPILOT_AUTO_UPDATE` 等：模型或行为配置。
- 完整正式列表见 [GitHub Docs](https://github.com/github/docs/blob/6f3a3dc662821c85f36a5cf3cf5507d18e269bc6/content/copilot/reference/copilot-cli-reference/cli-command-reference.md#L632-L665)。

Kimi Code CLI 的典型例子：

- `KIMI_CODE_HOME`、`KIMI_DISABLE_TELEMETRY`：数据目录和遥测开关，见 [Core paths](https://github.com/MoonshotAI/kimi-code/blob/a3b48a7272880dafb64e4c403006d94dc781d05c/docs/en/configuration/env-vars.md#L13-L37)。
- `KIMI_MODEL_*`：临时模型配置，见 [完整变量列表](https://github.com/MoonshotAI/kimi-code/blob/a3b48a7272880dafb64e4c403006d94dc781d05c/docs/en/configuration/env-vars.md#L102-L131)。
- `KIMI_CODE_OAUTH_HOST`、`KIMI_OAUTH_HOST`、`KIMI_CODE_BASE_URL`：OAuth/托管服务重定向，见 [OAuth and managed services](https://github.com/MoonshotAI/kimi-code/blob/a3b48a7272880dafb64e4c403006d94dc781d05c/docs/en/configuration/env-vars.md#L88-L100)。
- `KIMI_CODE_IDENTITY_NAME`、`KIMI_CODE_IDENTITY_SLUG`：用户可配置的展示名、出站 User-Agent product token 和 MCP client name，见 [runtime switches 表](https://github.com/MoonshotAI/kimi-code/blob/a3b48a7272880dafb64e4c403006d94dc781d05c/docs/en/configuration/env-vars.md#L133-L176)。它们是“配置 Kimi 对外自称什么”，不是 Kimi 自动给子进程注入的身份标记，甚至可以被用户改成其他值。

用户给出的官方页面还特别澄清了一组“看起来像 Shell 环境变量”的名称：

- [页首警告](https://www.kimi.com/code/docs/en/kimi-code-cli/configuration/env-vars.html) 明确说 `KIMI_API_KEY`、`ANTHROPIC_API_KEY`、`OPENAI_API_KEY` 不会自动从 Shell 环境读取；`export KIMI_API_KEY=...` 不会给 provider 配置密钥。
- [Provider credential key names 章节](https://www.kimi.com/code/docs/en/kimi-code-cli/configuration/env-vars.html#provider-credential-key-names-written-in-config-toml) 原文明确说，`KIMI_API_KEY`、`KIMI_BASE_URL`、`OPENAI_API_KEY` 等是写在 `config.toml` 的 `[providers.<name>.env]` 表中的键名，“The CLI reads only from the config file, not from `process.env`”。对应当前源文见 [env-vars.md L55-L84](https://github.com/MoonshotAI/kimi-code/blob/26dd6ce6aec4456867af010b55dff92ba58f04ee/docs/en/configuration/env-vars.md#L55-L84)。
- 唯一明示的 Shell 凭据例外是 [`KIMI_MODEL_*`](https://github.com/MoonshotAI/kimi-code/blob/26dd6ce6aec4456867af010b55dff92ba58f04ee/docs/en/configuration/env-vars.md#L102-L131)：它是用户主动传入的临时模型配置通道，不是 CLI 默认向 Bash 子进程注入的调用者标记。

“CLI 会从父进程读哪些配置”与“CLI 会自动给 Bash 子进程新增什么”是两个方向相反的问题。官方环境变量页面描述的主要是前者；要判断 Agent UA，还必须核对 Bash 的 spawn 源码和子进程实测。

因此，不能用“任意 `COPILOT_*` 存在”或“任意 `KIMI_*` 存在”作为识别规则。

### 5. Shell / IDE 集成变量

Kimi Code CLI 当前默认的 v2 Bash 工具只额外覆盖以下通用变量：

```text
NO_COLOR=1
TERM=dumb
GIT_TERMINAL_PROMPT=<父环境值或 0>
SHELL=<实际 shell 路径>
```

当前 HEAD 源码见 [bashTool.ts](https://github.com/MoonshotAI/kimi-code/blob/26dd6ce6aec4456867af010b55dff92ba58f04ee/packages/agent-core-v2/src/agent/tools/os/bash/bashTool.ts#L157-L173)。底层进程服务把这些覆盖项合并到 `process.env`，见 [hostProcessService.ts](https://github.com/MoonshotAI/kimi-code/blob/26dd6ce6aec4456867af010b55dff92ba58f04ee/packages/agent-core-v2/src/os/backends/node-local/hostProcessService.ts#L18-L40)。这些变量被大量终端、CI 与自动化工具使用，没有 Kimi 特异性。

Kimi 官方文档还说明它只读取、不修改 `TERM_PROGRAM`、`TERM`、`TMUX` 等系统变量来探测终端能力，见 [System environment variables](https://github.com/MoonshotAI/kimi-code/blob/a3b48a7272880dafb64e4c403006d94dc781d05c/docs/en/configuration/env-vars.md#L190-L203)。因此：

- `TERM_PROGRAM=vscode` 只说明终端来自 VS Code，不能证明 Copilot CLI 或 Kimi Code CLI 是调用者。
- Zed、JetBrains、VS Code 等 IDE 的环境变量识别的是承载 Agent 的客户端/终端；在 ACP 路径中，命令还可能由 ACP client 自己执行，更不能把 IDE 标记等同于 Kimi 标记。
- `SHELL`、`TERM`、`NO_COLOR`、`GIT_TERMINAL_PROMPT` 都不应进入 Agent 分类规则。

## GitHub Copilot CLI 深入结论

### Shell 与 MCP 的注入范围不同

GitHub 官方文档规定，本地/stdio MCP server 默认只自动继承 `PATH`，其他用户环境变量需要显式配置，见 [Adding MCP servers](https://github.com/github/docs/blob/6f3a3dc662821c85f36a5cf3cf5507d18e269bc6/content/copilot/how-tos/copilot-cli/customize-copilot/add-mcp-servers.md#L41-L50)。CLI 仍会专门给 MCP 注入少数运行时变量，例如官方已公布的 `COPILOT_AGENT_SESSION_ID`。

这解释了当前 1.0.82 的实测差异：

| 变量 | 普通 Shell | stdio MCP |
| --- | --- | --- |
| `COPILOT_CLI=1` | 有 | 无 |
| `COPILOT_AGENT_SESSION_ID` | 有 | 有 |
| `COPILOT_CLI_BINARY_VERSION` | 有（SEA 包） | 有（SEA 包） |

所以，如果 `volcengine-cli` 目前只会被 Agent 通过 Shell 执行，`COPILOT_CLI=1` 是最干净的规则；如果未来也会被 Copilot CLI 作为 MCP server 拉起，必须额外考虑 `COPILOT_AGENT_SESSION_ID` 或改用 MCP 协议内的客户端信息。

### 1.0.82 隔离实测

测试使用 GitHub 官方 [v1.0.82 release](https://github.com/github/copilot-cli/releases/tag/v1.0.82) 的 darwin-arm64 artifact。下载文件 SHA-256 为 `c03bfc58c4402ec31d09ca11c379e1c17e95c3441f48009d721bf556fcc2c42e`。

Shell 测试在 Copilot CLI 中执行：

```sh
!env | grep '^COPILOT_' | sort
```

与识别相关的脱敏输出：

```text
COPILOT_AGENT_SESSION_ID=<redacted UUID>
COPILOT_CLI=1
COPILOT_CLI_BINARY_VERSION=1.0.82
```

MCP 测试将一个临时 stdio server 配置为 `/bin/sh -c 'env | sort > <temp-file>; sleep ...'`，并在隔离环境中启动 Copilot CLI。与识别相关的脱敏输出包括：

```text
COPILOT_AGENT_SESSION_ID=<redacted UUID>
COPILOT_CLI_BINARY_VERSION=1.0.82
COPILOT_HOME=<isolated path>
COPILOT_LOADER_PID=<pid>
COPILOT_MODEL=<test model>
```

输出中没有 `COPILOT_CLI`。重复运行时 session ID 会变化，进一步证明它是 session 级值；这不构成 UUID 格式的兼容性承诺。

## Kimi Code CLI 深入结论

### 先区分新旧官方仓库

当前推荐产品是 [`MoonshotAI/kimi-code`](https://github.com/MoonshotAI/kimi-code)，不是只看 legacy [`MoonshotAI/kimi-cli`](https://github.com/MoonshotAI/kimi-cli)。legacy 仓库自己的 README 已声明正在迁移到新一代 Kimi Code CLI，见 [迁移说明](https://github.com/MoonshotAI/kimi-cli/blob/86f136422a0aae6b217ea49e7ea1d2e8a1defcd2/README.md#L9-L14)。

主结论以新仓库和当前官方最新版 0.40.1 为准：

- 二次核查时的官方仓库 HEAD：[`26dd6ce6aec4456867af010b55dff92ba58f04ee`](https://github.com/MoonshotAI/kimi-code/commit/26dd6ce6aec4456867af010b55dff92ba58f04ee)
- HEAD 包版本：[`apps/kimi-code/package.json`](https://github.com/MoonshotAI/kimi-code/blob/26dd6ce6aec4456867af010b55dff92ba58f04ee/apps/kimi-code/package.json#L1-L4)，仍为 `0.40.1`
- 官方 [`/latest`](https://code.kimi.com/kimi-code/latest) 返回 `0.40.1`
- 官方二进制 manifest：[0.40.1 manifest](https://code.kimi.com/kimi-code/binaries/0.40.1/manifest.json)

### 官方环境变量文档不是 Bash 注入契约

用户给出的 [Kimi Code CLI 官方环境变量页](https://www.kimi.com/code/docs/en/kimi-code-cli/configuration/env-vars.html#provider-credential-key-names-written-in-config-toml) 对 provider credential key names 的表述非常明确：

> The key names below are not read directly from the shell ... The CLI reads only from the config file, not from `process.env`.

所以，`KIMI_API_KEY`、`KIMI_BASE_URL`、`ANTHROPIC_API_KEY`、`OPENAI_API_KEY` 等名称不是调用者标记，甚至默认都不是 Shell 输入配置；它们是 `config.toml` 的 `[providers.<name>.env]` 键名。官方文档另外明确的 `KIMI_MODEL_*` 是用户主动设置的临时 provider/model 通道。这两类都不能证明 Kimi Code CLI 会在执行 Bash 时自动注入它们。

这里的“不从 Shell 读取”精确含义是“不把该变量用作 provider 配置”，不是 CLI 会主动从 OS 环境删除该名称。如果用户自己 `export KIMI_API_KEY=...`，由于 Bash spawn 会继承完整父 `process.env`，它仍可能被普通继承到子进程；这是输入的传播，不是 Kimi 的默认注入，因而也不能用于识别调用者。

### 普通 Bash 路径没有 Kimi 调用者标记

默认 v2 Bash 工具把 `NO_COLOR`、`TERM`、`GIT_TERMINAL_PROMPT`、`SHELL` 作为覆盖项传给底层进程服务，没有加入 `KIMI_CODE`、`KIMI_AGENT_SESSION_ID` 或类似标记：

- [Bash 组装环境](https://github.com/MoonshotAI/kimi-code/blob/26dd6ce6aec4456867af010b55dff92ba58f04ee/packages/agent-core-v2/src/agent/tools/os/bash/bashTool.ts#L157-L173)
- [底层合并父环境与覆盖项](https://github.com/MoonshotAI/kimi-code/blob/26dd6ce6aec4456867af010b55dff92ba58f04ee/packages/agent-core-v2/src/os/backends/node-local/hostProcessService.ts#L18-L40)

legacy engine 的新仓库实现也同样只合并父环境与通用非交互变量，见 [agent-core Bash implementation](https://github.com/MoonshotAI/kimi-code/blob/a3b48a7272880dafb64e4c403006d94dc781d05c/packages/agent-core/src/tools/builtin/shell/bash.ts#L273-L298)。

TUI 的 `!` Shell mode 不是另一套随意的 shell runner：当前 v2 源码中 [`AgentShellCommandService.run`](https://github.com/MoonshotAI/kimi-code/blob/26dd6ce6aec4456867af010b55dff92ba58f04ee/packages/agent-core-v2/src/agent/shellCommand/shellCommandService.ts#L90-L118) 会取得注册的 `Bash` tool，对同一个 `Bash` execution 调用 `resolveExecution` 和 `execute`。因此，用 `!env` 直接实测能覆盖真实 Bash tool 的 spawn 环境，而不需要依赖真实云端模型是否恰好发起工具调用。

### stdio MCP 路径也没有 Kimi 调用者标记

v2 stdio MCP transport 启动 server 时只传 `mergeStdioEnv(config.env)`，见 [client-stdio.ts 的 spawn](https://github.com/MoonshotAI/kimi-code/blob/a3b48a7272880dafb64e4c403006d94dc781d05c/packages/agent-core-v2/src/mcpCore/client-stdio.ts#L180-L203)。`mergeStdioEnv` 只合并父环境、MCP 配置中的 `env` 和代理变量，没有身份/session 注入，见 [同文件实现](https://github.com/MoonshotAI/kimi-code/blob/a3b48a7272880dafb64e4c403006d94dc781d05c/packages/agent-core-v2/src/mcpCore/client-stdio.ts#L292-L303)。

MCP 协议握手本身有非环境变量的客户端身份：默认 client name 是 `kimi-code`，见 [client-shared.ts](https://github.com/MoonshotAI/kimi-code/blob/a3b48a7272880dafb64e4c403006d94dc781d05c/packages/agent-core-v2/src/mcpCore/client-shared.ts#L1-L7)。但 `KIMI_CODE_IDENTITY_SLUG` 可以改变 MCP client name，所以消费方应按 MCP 协议和自身兼容策略判断，不能把它转化为 env 规则。

### 容易误认成环境变量的 Kimi session 信息

`KIMI_SESSION_ID` 确实出现在源码，但它是 Skill 文本模板占位符：Skill 加载时用 session ID 替换字符串 `${KIMI_SESSION_ID}`，见 [registry.ts](https://github.com/MoonshotAI/kimi-code/blob/a3b48a7272880dafb64e4c403006d94dc781d05c/packages/agent-core-v2/src/features/skill/catalog/registry.ts#L154-L173)。它没有被写入 OS 子进程环境，不能用 `os.Getenv("KIMI_SESSION_ID")` 检测。

Hooks 也有明确的非 env 元数据。官方 hook stdin JSON 包含：

```json
{
  "session_id": "session_abc",
  "client_type": "kimi_code_cli"
}
```

见 [Hooks / Event Data Format](https://github.com/MoonshotAI/kimi-code/blob/a3b48a7272880dafb64e4c403006d94dc781d05c/docs/en/customization/hooks.md#L57-L71)。若调用点本来就是 Kimi hook，解析 stdin payload 比猜测环境变量可靠；但这不适用于普通 Bash 命令。

### 0.40.1 官方二进制与真实 Bash 隔离实测

二次核查直接从官方发行服务解析版本、manifest 和 darwin-arm64 二进制：

```sh
version="$(curl -fsSL https://code.kimi.com/kimi-code/latest)"
curl -fsSL "https://code.kimi.com/kimi-code/binaries/${version}/manifest.json"
curl -fsSL -o kimi-code-darwin-arm64 \
  "https://code.kimi.com/kimi-code/binaries/${version}/kimi-code-darwin-arm64"
shasum -a 256 kimi-code-darwin-arm64
./kimi-code-darwin-arm64 --version
```

实际返回：

```text
version: 0.40.1
manifest darwin-arm64 checksum: cdee8d0c3a1a994a7eb30a7ad95e93bf27e0f0ab3d126121518c68f6b2fe05e7
downloaded SHA-256:            cdee8d0c3a1a994a7eb30a7ad95e93bf27e0f0ab3d126121518c68f6b2fe05e7
binary --version:              0.40.1
```

#### 无 `KIMI_*` 输入的 `!` Shell mode

模型、本地 dummy provider 地址和测试 key 只写入隔离 `HOME` 下的 `~/.kimi-code/config.toml`，没有作为环境变量传入。用 `env -i` 启动官方二进制：

```sh
env -i \
  HOME=<isolated-home> \
  PATH=/usr/bin:/bin \
  SHELL=/bin/sh \
  TERM=xterm-256color \
  ./kimi-code-darwin-arm64
```

在 TUI 中执行真实 Shell mode：

```sh
!env | sort
```

并与同一输入集合的基线比较：

```sh
env -i HOME=<isolated-home> PATH=/usr/bin:/bin \
  SHELL=/bin/sh TERM=xterm-256color \
  /bin/bash -c 'env | sort'
```

子进程的关键脱敏输出是：

```text
GIT_TERMINAL_PROMPT=0
HOME=<isolated-home>
NO_COLOR=1
PATH=<CLI-enriched PATH>
SHELL=/bin/bash
TERM=dumb
```

完整输出中还有 `PWD`、`OLDPWD`、`SHLVL`、`_` 和 macOS 的 `__CF_USER_TEXT_ENCODING`；这些是 shell/平台运行时元数据。与基线相比，Kimi Bash 源码明确覆盖的是 `GIT_TERMINAL_PROMPT=0`、`NO_COLOR=1`、`TERM=dumb` 和实际 shell 路径；`PATH` 在 CLI 启动期间被补全。最关键的结果是：**子进程中没有任何 `KIMI_*` 变量**。

#### 本地 mock provider 驱动的模型 `Bash` tool

为排除 `!` Shell mode 与模型工具调用之间的路径差异，又用本地 OpenAI-compatible SSE mock provider 返回一次真实工具调用：

```json
{"name":"Bash","arguments":"{\"command\":\"env | sort\"}"}
```

CLI 仍由 `env -i` 启动，本次故意传入 `KIMI_MODEL_*` 以连接本地 mock，并传入 `KIMI_PARENT_SENTINEL=present` 来验证父环境继承：

```sh
env -i \
  HOME=<isolated-home> PATH=/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin \
  SHELL=/bin/zsh KIMI_CODE_HOME=<isolated-state> \
  KIMI_DISABLE_TELEMETRY=1 KIMI_CODE_NO_AUTO_UPDATE=1 \
  KIMI_MODEL_NAME=env-test KIMI_MODEL_API_KEY=<redacted> \
  KIMI_MODEL_PROVIDER_TYPE=openai \
  KIMI_MODEL_BASE_URL=http://127.0.0.1:18080/v1 \
  KIMI_PARENT_SENTINEL=present \
  ./kimi-code-darwin-arm64 -p \
  'Use the Bash tool exactly once to run env | sort, then stop.' \
  --output-format stream-json
```

返回流中确认实际执行了 `Bash` tool，工具结果里的 `KIMI_*` 只有上述显式输入项，`KIMI_PARENT_SENTINEL=present` 也被原样继承。除通用 Bash 覆盖项外，没有新增 `KIMI_CODE_CLI`、`KIMI_SESSION_ID`、`KIMI_AGENT_SESSION_ID` 或任何其他 `KIMI_*` 变量。这与“父 `process.env` + 四个通用覆盖项”的源码完全一致。

### 0.40.1 stdio MCP 隔离实测

此前的 MCP 对照测试通过官方 [`install.sh`](https://code.kimi.com/kimi-code/install.sh) 在隔离的 `HOME`、`KIMI_CODE_HOME`、`KIMI_INSTALL_DIR` 下安装并校验同一 darwin-arm64 0.40.1 发行版。

在隔离的 `KIMI_CODE_HOME/mcp.json` 中配置临时 stdio server：

```text
command = /bin/sh
args = [-c, "env | sort > <temp-file>; sleep 2"]
```

以 `env -i` 只传入以下必要/测试变量后运行 `kimi -p`：

```text
HOME
PATH
SHELL
KIMI_CODE_HOME
KIMI_DISABLE_TELEMETRY=1
KIMI_CODE_NO_AUTO_UPDATE=1
KIMI_MODEL_NAME=test-model
KIMI_MODEL_API_KEY=<redacted test value>
KIMI_MODEL_BASE_URL=http://127.0.0.1:9/v1
```

MCP 子进程除 `PATH` 被运行时补全外，只看到上述环境，没有新增任何 Kimi 调用者、CLI presence 或 Agent session 标记。该结果和前述源码一致。

### legacy `MoonshotAI/kimi-cli`

legacy 1.50.0 同样没有发现专属子进程身份变量：

- Shell 先复制 `os.environ`，再只补 `GIT_TERMINAL_PROMPT`，见 [subprocess_env.py](https://github.com/MoonshotAI/kimi-cli/blob/86f136422a0aae6b217ea49e7ea1d2e8a1defcd2/src/kimi_cli/utils/subprocess_env.py#L23-L52) 和 [get_noninteractive_env](https://github.com/MoonshotAI/kimi-cli/blob/86f136422a0aae6b217ea49e7ea1d2e8a1defcd2/src/kimi_cli/utils/subprocess_env.py#L55-L73)。
- Shell 执行时只覆盖 `SHELL`，见 [tools/shell/__init__.py](https://github.com/MoonshotAI/kimi-cli/blob/86f136422a0aae6b217ea49e7ea1d2e8a1defcd2/src/kimi_cli/tools/shell/__init__.py#L221-L241)。
- `${KIMI_NOW}`、`${KIMI_WORK_DIR}`、`${KIMI_AGENTS_MD}` 等是 system prompt 模板参数，不是 OS 环境变量，见 [官方文档](https://github.com/MoonshotAI/kimi-cli/blob/86f136422a0aae6b217ea49e7ea1d2e8a1defcd2/docs/en/customization/agents.md#L72-L104)。

## Kiro CLI 深入结论

### 官方安装入口与发行物审计

Kiro 的官方 [安装文档](https://kiro.dev/docs/getting-started/installation.md) 指定 macOS/Linux 使用：

```sh
curl -fsSL https://cli.kiro.dev/install | bash
```

2026-09-03 读取到的安装脚本快照是 16,864 bytes，SHA-256 为
`91a21bfa05cd7b58601cb83e0f1f187a9d0084726e5b824d4a4cf60306250908`。官方 S3 返回了
version ID，因此可以用[固定脚本版本](https://cli.kiro.dev/install?versionId=vP_k48Sk0OofBgSL0vTxduF7mCmg3vTU)复核。本次逐段审计确认脚本会：

1. 从 `https://prod.download.cli.kiro.dev/<channel>/latest/manifest.json` 解析当前包；
2. 下载平台对应发行物；
3. 从 manifest 读取该文件的 SHA-256，并用本机 `shasum -a 256`/`sha256sum` 比对；
4. macOS 安装目标是 `/Applications/Kiro CLI.app`，Linux 安装目标是 `~/.local/bin`。

当前 stable manifest 的 S3 version ID 是 `6z4iRZDE3mFWGewxUO50pekfUMnWNeqn`，可从
[固定 manifest 版本](https://prod.download.cli.kiro.dev/stable/latest/manifest.json?versionId=6z4iRZDE3mFWGewxUO50pekfUMnWNeqn)
复核以下发行信息：

```text
version: 2.21.0
macOS target: universal-apple-darwin
download: 2.21.0/Kiro CLI.dmg
size: 1049634043 bytes
manifest SHA-256: baffdad5d3cdfb4886baf418803104b1ee79e5bb15415f63e49f1c855f01510f
```

本次实际下载使用官方 `latest` URL；其内容与[版本化 2.21.0 URL](https://prod.download.cli.kiro.dev/stable/2.21.0/Kiro%20CLI.dmg)
对应。下载后实算 SHA-256 为：

```text
baffdad5d3cdfb4886baf418803104b1ee79e5bb15415f63e49f1c855f01510f
```

与 manifest 完全一致。包内 `kiro-cli --version` 和 `kiro-cli-chat --version` 均返回 `2.21.0`，也与
[官方 2.21.0 changelog](https://kiro.dev/changelog/cli/2-21/) 一致。macOS manifest 只发布含 CLI
可执行文件的 DMG，没有独立 macOS headless archive；本次只读挂载 DMG 后，直接运行
`Contents/MacOS/kiro-cli` 和 `Contents/MacOS/kiro-cli-chat`，没有安装或启动桌面应用。

两个 CLI 可执行文件的签名均验证通过，签发者为：

```text
Developer ID Application: AMZN Mobile LLC (94KV3E626L)
TeamIdentifier=94KV3E626L
```

整个 bundle 也通过 macOS Gatekeeper，状态为 `accepted`、`Notarized Developer ID`。官方 installer
公开执行的是 manifest SHA-256 校验，没有另外公开 detached signature/GPG 验证步骤；macOS 包本身另有 Apple
代码签名与 stapled notarization ticket。

### 当前源码公开边界

Kiro 官方 GitHub 仓库明确说明它是反馈与 issue tracker，产品源码不托管在其中，见
[固定 README](https://github.com/kirodotdev/Kiro/blob/bfe7ff30d9b21e583566c844d75e3dd2b572e2c7/README.md#about-this-repository)。
Amazon Q Developer CLI 的最后公开源码也说明 Amazon Q CLI 已演进为闭源 Kiro CLI，见
[固定 README](https://github.com/aws/amazon-q-developer-cli/blob/15cc8f3cd18c4272925ce1c7053268eedff1ea0a/README.md)。
所以旧源码只能解释产品血缘，不能替代 Kiro 2.21.0 的当前发行物实测。

官方 [Settings 环境变量表](https://kiro.dev/docs/reference/settings.md#environment-variables) 和
[CLI logging 文档](https://kiro.dev/docs/reference/cli-commands.md#log-files) 列出的变量包括：

```text
KIRO_HOME
KIRO_LOG_LEVEL
KIRO_LOG_NO_COLOR
KIRO_ASCII_MODE
KIRO_ACP_RECORD_PATH
KIRO_CLI_TOOL_SEARCH_MATCHING_THRESHOLD
NO_COLOR
```

它们是用户设置的路径、日志、显示或调试配置，不是“当前进程由 Kiro Agent 启动”的身份标识。官方文档没有把
`KIRO_SESSION_ID` 或其他变量声明为兼容性稳定的 Shell caller contract，也没有类似
`KIRO_CLI=1` 的公开契约。因此下面的结论是对当前官方 2.21.0 发行物的实用检测结论，不能表述成上游长期 API 承诺。

### 2.21.0 `!` Shell mode 隔离实测

`!` Shell mode 是 Kiro CLI 的本地 shell escape，不需要模型生成工具调用。为避免使用真实账户或模型凭据，
以 dummy `KIRO_API_KEY` 只绕过登录初始化；在发出任何模型请求前直接执行本地 `!env`。用户实际入口
`kiro-cli` 的有效启动环境为：

```sh
env -i \
  HOME=<isolated-home> \
  PATH=/usr/bin:/bin \
  SHELL=/bin/zsh \
  TERM=xterm-256color \
  KIRO_API_KEY=<dummy-local-test-value> \
  <official-dmg>/Contents/MacOS/kiro-cli chat --classic
```

进入官方 CLI 后实际执行：

```sh
!env | sort
```

完整输出如下（仅把临时目录和 session UUID 抽象化）：

```text
HOME=<isolated-home>
KIRO_API_KEY=<dummy-local-test-value>
KIRO_SESSION_ID=<new UUID>
LOGNAME=root
OLDPWD=<isolated-workdir>
PATH=/usr/bin:/bin
PWD=<isolated-workdir>
SHELL=/bin/zsh
SHLVL=1
TERM=xterm-256color
_=/usr/bin/env
__CF_USER_TEXT_ENCODING=0x1F5:0x19:0x34
```

与父环境相比，唯一 Kiro 身份/会话候选是自动新增的 `KIRO_SESSION_ID`。其余 `PWD`、`OLDPWD`、
`SHLVL`、`_`、`LOGNAME` 和 macOS 的 `__CF_USER_TEXT_ENCODING` 是 Shell/平台运行时变量；
`KIRO_API_KEY` 是明确传入的测试值。换一个全新隔离 HOME 重复运行，仍会注入 `KIRO_SESSION_ID`，且
UUID 与第一次不同。本测试仅执行 CLI binary/`kiro-cli-chat`，没有启动包内 `kiro_cli_desktop` 或任何图形应用。

### 2.21.0 Agent `shell` tool 隔离实测

为了在无真实账户/模型凭据时继续验证模型通常触发的 Agent Shell 路径，使用官方发行二进制自身包含的
`KIRO_TEST_MODE` 与本地 `KIRO_MOCK_CHAT_RESPONSE`。这两个变量以及
`KIRO_DISABLE_TELEMETRY` 是测试命令显式输入，不是识别候选。本地 mock response 返回一次真实的内置工具调用：

```json
{
  "tool_use_id": "env-test-1",
  "name": "execute_bash",
  "args": { "command": "/usr/bin/env | /usr/bin/sort" }
}
```

CLI 输出明确标记 `(using tool: shell)`，命令成功执行。完整环境是：

```text
AWS_EXECUTION_ENV=AmazonQ-For-CLI Version/2.21.0
HOME=<isolated-home>
KIRO_DISABLE_TELEMETRY=1
KIRO_MOCK_CHAT_RESPONSE=<local-response.json>
KIRO_SESSION_ID=<new UUID>
KIRO_TEST_MODE=1
PATH=/usr/bin:/bin
PWD=<isolated-workdir>
SHELL=/bin/zsh
SHLVL=1
TERM=xterm-256color
_=/usr/bin/env
__CF_USER_TEXT_ENCODING=0x1F5:0x19:0x34
```

这里有两个由当前 Kiro CLI 自动加入的信号：

- `KIRO_SESSION_ID=<UUID>`：session 级标识；两次独立运行得到不同 UUID。
- `AWS_EXECUTION_ENV=AmazonQ-For-CLI Version/2.21.0`：只在 Agent `shell` tool 路径新增，`!` Shell mode
  中没有。它保留了产品从 Amazon Q CLI 演进而来的历史命名。

额外做了污染测试：分别在 `!` Shell 和 Agent `shell` 测试的父环境故意传入
`KIRO_SESSION_ID=parent-fake`；Agent `shell` 测试还传入
`AWS_EXECUTION_ENV=ParentSentinel`。两条路径中的 `KIRO_SESSION_ID` 都被换成新 UUID，Agent
`shell` 子进程的关键结果是：

```text
KIRO_SESSION_ID=<new UUID, parent-fake 被覆盖>
AWS_EXECUTION_ENV=ParentSentinel AmazonQ-For-CLI Version/2.21.0
```

这说明当前 2.21.0 会在 session 建立时重新生成 `KIRO_SESSION_ID`，并在内置 Agent Shell 路径给已有
`AWS_EXECUTION_ENV` 追加自己的 token。最后公开的 Amazon Q CLI 源码与后者完全一致：

- [常量与 token 格式](https://github.com/aws/amazon-q-developer-cli/blob/15cc8f3cd18c4272925ce1c7053268eedff1ea0a/crates/chat-cli/src/cli/chat/consts.rs#L30-L34)
- [保留父值并追加 token](https://github.com/aws/amazon-q-developer-cli/blob/15cc8f3cd18c4272925ce1c7053268eedff1ea0a/crates/chat-cli/src/cli/chat/tools/mod.rs#L515-L543)
- [传给 Unix Shell 子进程](https://github.com/aws/amazon-q-developer-cli/blob/15cc8f3cd18c4272925ce1c7053268eedff1ea0a/crates/chat-cli/src/cli/chat/tools/execute/unix.rs#L28-L48)

旧源码与当前实测相互印证，但对当前版本的直接证据仍是上面的官方 2.21.0 发行物运行结果。

### 哪些变量可用，哪些不能用

| 变量/规则 | 分类 | 当前证据与风险 |
| --- | --- | --- |
| `KIRO_SESSION_ID` 非空 | 当前强实用信号 | 2.21.0 的 `!` 与 Agent Shell 两条路径都有；父环境伪值会被覆盖；但官方未承诺长期兼容，而且它表达 Kiro session，不保证能区分 CLI 与未来其他 Kiro surface |
| `AWS_EXECUTION_ENV` 包含 `AmazonQ-For-CLI Version/` | Agent Shell 辅助信号 | 2.21.0 Agent Shell 主动追加；`!` 路径没有；旧 Amazon Q CLI 也会命中，所以它不是 Kiro CLI 独占信号 |
| 只判断 `AWS_EXECUTION_ENV` 存在 | 不可用 | AWS Lambda 等官方运行时也定义该通用变量，见 [AWS Lambda 文档](https://docs.aws.amazon.com/lambda/latest/dg/configuration-envvars.html#configuration-envvars-runtime) |
| `KIRO_CLI_ACP_CLIENT_NAME`、`KIRO_VERSION` | 范围限定/历史信号 | 官方 tracker 中 2.11.0 TUI/hook 路径曾出现，但本次 2.21.0 直接 CLI 的两个 Shell 路径没有；不能作为必要条件 |
| `KIRO_HOME`、`KIRO_LOG_*`、`KIRO_ASCII_MODE`、`KIRO_ACP_RECORD_PATH` | 用户配置 | 用户可在 Kiro 之外设置；不能证明调用来源 |
| `KIRO_TEST_MODE`、`KIRO_MOCK_CHAT_RESPONSE` | 内部测试输入 | 本次为了无凭据验证显式传入；正常 session 不应依赖，更不能用于产品识别 |

历史行为可在官方 tracker 交叉核对：

- [#8790](https://github.com/kirodotdev/Kiro/issues/8790) 报告 2.4.0 Shell 继承过超大的 `KIRO_FEED_JSON`；维护者后续认为该冗余变量不应传播，说明 feed/rollout 内部变量不是稳定标识。
- [#8790 的 2.11.0 后续实测](https://github.com/kirodotdev/Kiro/issues/8790#issuecomment-5042659312) 列出过 `KIRO_VERSION`、`KIRO_SESSION_ID`、`KIRO_CLI_ACP_CLIENT_NAME=kiro-tui` 和 `AWS_EXECUTION_ENV` 等。
- [#8430 的 2.16.0 hook 实测](https://github.com/kirodotdev/Kiro/issues/8430#issuecomment-5178556793) 显示 hook 子进程收到 `KIRO_SESSION_ID` 与 `KIRO_VERSION`。

这些历史 issue 说明变量集合会随版本和路径改变，也支持只选择当前两条 Shell 实测都存在的
`KIRO_SESSION_ID`，而不把内部 rollout/feed/version/ACP 变量打包成宽泛的“任一命中”规则。

## Cline 深入结论

### 官方文档：默认 IDE 终端有明确标识

Cline 官方仓库此前承载的文档明确写道，Cline 会给它打开以执行命令的终端设置环境变量，并把
`CLINE_ACTIVE` 定义为“shell 在 Cline 下运行时非空”。同一页给出的验证命令是：

```sh
echo "$CLINE_ACTIVE"
```

预期应输出非空值。固定证据见：

- [官方文档源码第 1–14 行](https://github.com/cline/cline/blob/d7716a514d574d3d0c605af5ccdc33837588acd8/docs/features/customization/disable-terminal-pagers.mdx#L1-L14)
- [官方验证步骤第 39–47 行](https://github.com/cline/cline/blob/d7716a514d574d3d0c605af5ccdc33837588acd8/docs/features/customization/disable-terminal-pagers.mdx#L39-L47)

该页随后随 [New Cline Docs 重构](https://github.com/cline/cline/commit/e884699d249b55496f18425276e4c001634a539c)
从仓库删除；当前 CLI 环境变量参考表也没有列 `CLINE_ACTIVE`。因此准确表述是：历史官方文档明确给出过
这一检测方法，当前 4.1.17 实现仍保留；但上游没有承诺它是跨所有 Cline surface 的长期公共 API。

### VS Code 扩展 4.1.17 源码与正式 VSIX

核验的当前稳定扩展是 `4.1.17`，官方 tag `v4.1.17` 指向 commit
[`833be95cfbb75fffc9343639efcbe05a45a4d2a7`](https://github.com/cline/cline/commit/833be95cfbb75fffc9343639efcbe05a45a4d2a7)。
两条创建 Cline terminal 的路径都通过 VS Code `TerminalOptions.env` 明确设置：

```text
CLINE_ACTIVE=true
```

- [TerminalRegistry 创建受管终端](https://github.com/cline/cline/blob/v4.1.17/apps/vscode/src/hosts/vscode/terminal/VscodeTerminalRegistry.ts#L25-L44)
- [Host bridge 创建终端并执行命令](https://github.com/cline/cline/blob/v4.1.17/apps/vscode/src/hosts/vscode/hostbridge/workspace/executeCommandInTerminal.ts#L14-L30)
- [VS Code 官方 `TerminalOptions.env` 语义](https://code.visualstudio.com/api/references/vscode-api#TerminalOptions)

`TerminalOptions.env` 是传给 terminal process 的环境变量映射，所以它不是仅留在扩展内部的元数据；
命令及通常的后代进程都会继承该值。当前默认执行模式也是 `vscodeTerminal`，见
[`state-keys.ts`](https://github.com/cline/cline/blob/v4.1.17/apps/vscode/src/shared/storage/state-keys.ts#L76-L81)。

本次还从 Visual Studio Marketplace 官方固定地址下载
[`saoudrizwan.claude-dev` 4.1.17 VSIX](https://marketplace.visualstudio.com/_apis/public/gallery/publishers/saoudrizwan/vsextensions/claude-dev/4.1.17/vspackage)：

```text
transport 文件 SHA-256: dd083edc4914b45771137ccddaaad6405995cecd616c118a99ac5a4d73783d69
解压后 VSIX SHA-256: 82875472744ded4a360e22c726bb6101962ecf0a178aa8efe84a5c7ea728d2f5
manifest: saoudrizwan.claude-dev 4.1.17
```

VSIX 的 `next/dist/extension.js` 与 `legacy/dist/extension.js` 都各有两条
`env:{CLINE_ACTIVE:"true"}` 的对应发布代码；并已在隔离的 Cursor user-data/extensions 目录安装，
`--list-extensions --show-versions` 返回 `saoudrizwan.claude-dev@4.1.17`。这排除了“源码有、正式包没有”的情况。

但扩展还有可选的 `backgroundExec`：官方源码说明 foreground 使用 VS Code terminal，background 使用
`child_process.spawn`；后台 executor 只额外设置 `SHELL`，没有设置 `CLINE_ACTIVE`，见
[`vscode-run-commands-tool.ts`](https://github.com/cline/cline/blob/v4.1.17/apps/vscode/src/sdk/vscode-run-commands-tool.ts#L541-L576)。
所以 `CLINE_ACTIVE=true` 能正向确认 Cline 创建的 IDE 终端，但变量缺失不能反向证明“不是 Cline”。

### Cline CLI 3.0.61：真实 Bash 隔离实测

官方安装文档当前指定 `npm install -g cline`，见
[Installing Cline](https://docs.cline.bot/getting-started/installing-cline#cli)。本次在隔离目录安装官方
`cline@3.0.61`；`cline --version` 返回 `3.0.61`。npm 为当前 darwin-arm64 选择
`@cline/cli-darwin-arm64@3.0.61`，二进制 SHA-256 为：

```text
cf9f28dad04bc21e3e554e4fc7dda615d913845e70b2ecaa62462025b8f46c0d
```

测试以 `env -i` 清空父环境，只传入隔离配置、`PATH`、`HOME`、`SHELL`、VCR 测试开关和
`CLINE_PARENT_SENTINEL=present`。使用官方二进制自带的 VCR playback，令模型响应确定调用真实
`run_commands` 工具执行：

```sh
/usr/bin/env | /usr/bin/sort
```

运行结果明确显示工具成功，关键输出是：

```text
CLINE_CONNECTOR_CLI_LAUNCH={"launcher":"<official platform binary>","connectArgsPrefix":["connect"],"cwd":"<workspace>"}
CLINE_PARENT_SENTINEL=present
SHELL=/bin/bash
SHLVL=1
```

输出中没有由 CLI 新增的 `CLINE_ACTIVE`、`CLINE_SESSION_ID` 或 `CLINE_VERSION`。污染复测先在父环境传入：

```text
CLINE_ACTIVE=parent-fake
CLINE_CONNECTOR_CLI_LAUNCH=parent-fake
```

真实 Bash 中 `CLINE_ACTIVE` 仍是 `parent-fake`，而 `CLINE_CONNECTOR_CLI_LAUNCH` 被换成当前 CLI
生成的合法 JSON。这证明 CLI 主动设置的是后者，并未把 `CLINE_ACTIVE` 作为 CLI 标记。

源码与实测一致：CLI entrypoint 调用
[`setConnectorCliLaunchSpec`](https://github.com/cline/cline/blob/v4.1.17/apps/cli/src/index.ts#L32-L45)
写入 connector 启动规格，SDK Bash executor 再以
[`{ ...process.env, ...config.env }`](https://github.com/cline/cline/blob/v4.1.17/sdk/packages/core/src/extensions/tools/executors/bash.ts#L673-L682)
启动命令。

`CLINE_CONNECTOR_CLI_LAUNCH` 是当前发行物很强的 Cline 运行时实用信号，但仍有三个边界：

- 它的公开语义不是 caller identity，而是供 connector/hub 重新启动 CLI 的内部规格。
- 官方 CLI 环境变量参考没有列它，不能视为长期兼容承诺。
- Cline Hub、desktop/menubar 等宿主也会设置同一规格；它能说明 Cline 运行时祖先链，不能严格证明
  直接父进程一定是 standalone CLI。

正常经过 npm Node wrapper 的另一路径还可能出现 `CLINE_WRAPPER_PATH`，见
[`apps/cli/bin/cline`](https://github.com/cline/cline/blob/3501d4b0e23eca73039bafc72fc9ba59fb9e4710/apps/cli/bin/cline#L19-L24)。
但平台包也声明同名 `cline` bin；本次 npm 安装生成的 `.bin/cline` 实际直接指向平台二进制，真实命令中
没有 `CLINE_WRAPPER_PATH`。因此它不应作为 CLI 的必要条件。

### Cline 的可落地判断

```text
严格识别 Cline VS Code 默认终端：CLINE_ACTIVE == "true"

覆盖当前 Cline CLI/部分 Cline 宿主：
CLINE_CONNECTOR_CLI_LAUNCH 是可解析的非空 JSON，且 launcher、connectArgsPrefix、cwd 字段合法

不要使用：任意 CLINE_* 非空、TERM_PROGRAM=vscode、任意 VSCODE_*、CLAUDE_CODE*
```

第一条来自 Cline 明确设置并有历史官方文档支持，最适合当前“Cline IDE 调用 Bash”的需求；第二条只能作为
当前发行实现的扩展覆盖。若产品要求一个能覆盖 IDE foreground/background、CLI、SDK 与直接平台二进制的
统一稳定规则，应推动上游提供新的公开 caller/session 契约，而不是把内部配置变量拼成宽泛 OR 条件。

## 误判与漏判模型

| 规则 | 主要误判 | 主要漏判 |
| --- | --- | --- |
| `COPILOT_CLI == "1"` | 用户伪造；嵌套/后代进程继承，把外层 Copilot 当成直接调用者 | 当前 stdio MCP 路径 |
| `COPILOT_AGENT_SESSION_ID != ""` | 未来其他 Copilot host 复用；用户伪造；后代继承 | 1.0.29 以前的 CLI，或主动清洗 env 的中间层 |
| 任意 `COPILOT_*` 非空 | 用户配置、认证、loader 变量大量误报 | 无法形成可靠语义 |
| 任意 `KIMI_*` 非空 | 模型、认证、调试、路径、遥测开关大量误报 | 默认 Kimi Code 会话通常没有任何这些变量 |
| `KIRO_SESSION_ID` 非空 | 用户仍可伪造；嵌套 Agent/后代进程会继承；可能无法区分 Kiro CLI 与其他 Kiro surface | 未来版本若更名/清洗；不是上游公开稳定契约 |
| `AWS_EXECUTION_ENV` 存在 | AWS Lambda、其他 AWS 工具与用户配置都会误报 | Kiro `!` Shell mode 没有该变量 |
| `AWS_EXECUTION_ENV` 包含 `AmazonQ-For-CLI Version/` | 旧 Amazon Q CLI 同样命中；后代进程继承 | Kiro `!` Shell mode；未来 Kiro 若移除 legacy token |
| `CLINE_ACTIVE == "true"` | 用户伪造；嵌套/后代进程继承，把外层 Cline terminal 当成直接调用者 | Cline IDE `backgroundExec`、CLI、SDK、被清洗的环境 |
| `CLINE_CONNECTOR_CLI_LAUNCH` 是合法 JSON | Cline Hub、desktop/menubar 等宿主也会设置；用户伪造；后代继承 | IDE 默认 terminal、未来内部实现改名或清洗 |
| `CLINE_WRAPPER_PATH` 非空 | 用户设置或其他包装器继承；只能说明经过特定 npm wrapper | 平台包同名 bin 直连二进制、源码/SDK/IDE 路径 |
| 任意 `CLINE_*` 非空 | API key、配置目录、日志、sandbox、测试和内部状态大量误报 | 没有统一变量的 background/SDK 路径 |
| `TERM_PROGRAM` / IDE 变量 | 把宿主终端或编辑器误认为 Agent | 非对应 IDE、远程/ACP 执行路径 |
| 进程名/父 PID | 包装器、shell、容器、进程重托管 | 父进程退出、跨平台限制、权限限制 |

## 最终建议

1. 若本次只增加 GitHub Copilot CLI 的 Agent User-Agent 识别，采用 `COPILOT_CLI == "1"`；如果明确要覆盖 stdio MCP，再 OR `COPILOT_AGENT_SESSION_ID != ""`，并在注释中说明范围扩大。
2. 不为 Kimi Code CLI 增加 `KIMI_CODE_HOME`、`KIMI_MODEL_*`、`KIMI_DISABLE_TELEMETRY` 或任意 `KIMI_*` 的猜测规则。当前证据表明这些只能造成高误报/高漏报。
3. 若必须支持 Kimi env-only 识别，最稳妥的方向是向上游请求类似 `KIMI_CODE_CLI=1` 与 `KIMI_AGENT_SESSION_ID` 的明确子进程契约，并分别规定 Bash、hook、stdio MCP、ACP client-executed terminal 的传播范围。
4. 如果集成通道允许，优先使用结构化来源信息：Kimi hook 的 `client_type`、MCP initialize 的 `clientInfo`，都比从通用环境变量推断更清晰。
5. 对 Kiro CLI 2.21.0，若产品接受“当前发行物实测、非公开稳定契约”，可用 `KIRO_SESSION_ID != ""` 识别处于 Kiro session 的 Shell/后代进程。不要使用任意 `KIRO_*` 规则；若只想辅助确认 Agent `shell` tool，可再检查 `AWS_EXECUTION_ENV` 是否包含 `AmazonQ-For-CLI Version/`，但不能只判断该变量存在。
6. 若产品必须严格区分 Kiro CLI 与 Kiro IDE/其他 Kiro surface，仅靠 `KIRO_SESSION_ID` 证据不足；应推动上游提供类似 `KIRO_CLI=1` 的明确契约，或通过 ACP/MCP 的结构化 client metadata 区分。
7. 对 Cline VS Code 扩展默认 terminal 模式，采用 `CLINE_ACTIVE == "true"`。这是当前最明确、历史官方文档也直接给出过的检测方法；注释中应注明它表达的是“Cline 创建的终端”，并且不覆盖 `backgroundExec`。
8. 若明确还要覆盖当前 Cline CLI，可把“`CLINE_CONNECTOR_CLI_LAUNCH` 是字段合法的非空 JSON”作为第二条实用规则，但不要把它表述成 standalone CLI 独占或官方稳定 API。不要依赖 `CLINE_WRAPPER_PATH`，也不要用任意 `CLINE_*` 存在来识别。
9. 如果产品必须跨 Cline IDE foreground/background、CLI、SDK 与直接平台二进制统一识别，当前没有可靠的 env-only 方案；应推动上游公开统一 caller/session 标记。
