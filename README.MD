## Volcengine Command Line Tools

---
[![Volcengine SKILL & CLI Survey](img_new.png)](https://www.volcengine.com/survey/q/v2/7662656599489085475/q36U8r81/5ffc/channel=doc)

为提高您对火山引擎 SKILL&CLI 的使用体验，邀您参加[有奖调研](https://www.volcengine.com/survey/q/v2/7662656599489085475/q36U8r81/5ffc/channel=doc)

---

中文 | [English](README.EN.MD)

### 特别说明

从 v1.0.20 开始，火山引擎 CLI 命令前缀由 `volcengine-cli` 更新为 `ve`。低版本不受影响；升级到 v1.0.20 及以后版本后，请同步更新脚本中的命令前缀。

从 v1.1.3 开始，`ve` 才支持安装和管理 Skill（`ve skills` 系列命令，以及 npm 安装时自动安装核心 Skill）。v1.1.3 之前的版本没有该能力，请先升级到 v1.1.3 及以上版本再使用。

### 概述

火山引擎 CLI（`ve`）是火山引擎的统一命令行入口，面向开发者、自动化脚本和 AI Agent。它提供通用 OpenAPI 调用、凭证与 profile 管理、SSO 和 Console Login，并通过 `ve arkcli`、`ve tosutil` 接入产品 CLI，负责按需安装和托管升级；也提供 Skill 管理和 MCP 服务，便于 Agent 使用火山引擎能力。

查询和管理云资源的通用 API 使用 `ve <service> <action>`；使用方舟或对象存储的产品工具时，使用 `ve arkcli <命令>` 或 `ve tosutil <命令>`。产品 CLI 保留自己的命令、认证配置、参数和输出规则，具体用法见下文[代理 CLI](#代理-cli)。

从源码编译需要 Go 1.18 或更高版本；使用 Release 预编译包或 npm 安装时不需要 Go。

### 快速安装

使用 npm 安装：

```shell
npm install -g @volcengine/cli
ve version
```

npm 包要求 Node.js >= 14，安装后提供 `ve` 命令入口。

从 v1.1.3 开始，通过 npm 安装或升级 CLI 时，会自动安装火山引擎的核心 Skill；v1.1.3 之前的版本不会安装 Skill。
Skill 安装失败不影响 CLI 本身安装。若需要暂时跳过 Skill 安装，可设置 `VOLCENGINE_CLI_SKIP_SKILLS=1`。

也可以从 Release 下载：

1. 打开 <https://github.com/volcengine/volcengine-cli/releases> 获取最新版本。
2. 下载对应操作系统和架构的压缩包。
3. 解压得到 `ve`，Windows 下为 `ve.exe`。

或从源码编译：

```shell
sh build.sh
```

将 `ve` 放到 PATH 中：

```shell
sudo cp ve /usr/local/bin
ve version
```

### 升级到最新版本

```shell
ve upgrade
```

- **standalone / Release**：下载并原地替换当前二进制
- **npm**：委托执行 `npm install -g @volcengine/cli@latest`（失败时会提示同一条手动命令）
- **Homebrew**：依次执行 `brew update` 与 `brew upgrade volcengine-cli`

### Skill 管理

> Skill 安装与管理能力从 **v1.1.3** 开始提供。请先执行 `ve version` 确认版本；低于 v1.1.3 时，先通过 `ve upgrade` 或 `npm install -g @volcengine/cli@latest` 升级。

核心 Skill 安装到 `~/.agents/skills` 和 `~/.claude/skills`；检测到 OpenClaw、Hermes Agent 或 Trae 时，
也会安装到对应的 Skill 目录。Codex、Cursor 和 OpenCode 可直接读取 `~/.agents/skills`。完整 Skill 清单和使用介绍请参阅
[`volcengine-skills`](https://github.com/volcengine/volcengine-skills#skills)。

```shell
ve skills install
ve skills update
ve skills uninstall
```

- `install`：安装尚未安装的核心 Skill。
- `update`：更新到最新版本；本地缺失或修改过的核心 Skill 会恢复为最新官方内容。尚未安装时会直接完成安装。
- `uninstall`：卸载由 `ve` 安装的核心 Skill，但保留本地修改过的内容。

### 代理 CLI

`ve` 可以把 `arkcli` 和 `tosutil` 作为透明的二级命令运行：

```shell
ve arkcli --help
ve tosutil version
```

TOS 工具的标准入口为 `ve tosutil`；此前的 `ve toscli` 写法保留为兼容别名。

各工具的命令、认证、配置和参数说明请参阅官方使用文档：

| 工具 | `ve` 调用入口 | 官方使用文档 |
| --- | --- | --- |
| Ark CLI | `ve arkcli <命令> [参数...]` | [Ark CLI 使用说明](https://github.com/volcengine/ark-cli#readme) |
| tosutil | `ve tosutil <命令> [参数...]` | [tosutil 使用指南](https://www.volcengine.com/docs/6349/148775) |

使用文档中的示例时，将命令开头的 `arkcli` 替换为 `ve arkcli`，将 `tosutil` 替换为 `ve tosutil`，后续参数保持原样。可通过 `ve arkcli --help` 和 `ve tosutil help` 查看当前安装版本的帮助；参数语法和输出格式以对应工具为准。

`ve` 原生 API 的 `--profile`、`--region`、`--endpoint` 不能放在代理命令前；子 CLI 支持的参数应放在 `arkcli` 或 `tosutil` 后，由子 CLI 解释。登录会话刷新只针对 `ve` 当前 Console Login 缓存，不会自动将 `ve` 的全部认证或 profile 配置转换为子 CLI 配置。

`arkcli` 或 `tosutil` 后面的参数不会由 `ve` 解析，而是原样交给对应 CLI。转发前，`ve` 会检查当前 profile；若当前 profile 使用 Console Login、存在登录会话且临时 STS 凭证即将过期，会尝试刷新凭证。未登录或刷新失败不会阻止转发，子 CLI 可自行处理登录；帮助和版本命令跳过登录会话刷新。Unix 上 `ve` 随后通过 `exec` 原地替换为目标 CLI，不保留代理父进程；Windows 上使用最薄的等待转发层。

查找顺序是系统 `PATH`，然后是 `ve` 托管目录。托管目录默认为 `~/.volcengine/proxy-cli/bin`，可通过 `VOLCENGINE_CLI_PROXY_PATH` 覆盖。两处都没有对应命令时，首次调用会从独立 manifest `https://cloudcache.volccdn.com/ve/proxycli/manifest.json` 自动安装；manifest 不可用时使用代码内嵌的同 CDN 固定版本清单，不访问 GitHub。默认目录的安装状态记录在 `~/.volcengine/proxy-cli/install-state.json`；自定义托管目录使用独立的路径标识状态文件，互不覆盖。状态损坏或缺失时按未安装处理并重新安装。

托管版本沿用 `ve` 的后台更新提醒模式：普通调用最多每 24 小时通过独立短生命周期 helper 检查一次 manifest，helper 不阻塞目标 CLI，也不受 Unix `exec` 交接影响。发现新版本后在下一次调用的 stderr 提示，由用户决定是否执行 `ve proxycli upgrade`。系统 `PATH` 上的外部安装不参与检查；`VOLCENGINE_CLI_DISABLE_UPDATE_CHECK=1` 会同时关闭 ve 和托管代理 CLI 的后台检查。

```shell
ve proxycli upgrade                 # 安装或升级全部托管代理 CLI
ve proxycli upgrade arkcli          # 只处理 arkcli
ve proxycli upgrade tosutil --yes    # 只处理 tosutil，并跳过确认
```

系统 `PATH` 上的外部安装由原包管理器负责，`ve proxycli upgrade` 不会覆盖。npm 安装 `ve` 时默认不安装代理 CLI；如需同时预装，设置 `VOLCENGINE_CLI_INSTALL_PROXY_CLIS=1`。

### 快速开始

```shell
ve configure set --profile default --region cn-beijing --access-key AK --secret-key SK
ve sts GetCallerIdentity
```

也可以使用环境变量：

```shell
export VOLCENGINE_ACCESS_KEY=AK
export VOLCENGINE_SECRET_KEY=SK
export VOLCENGINE_REGION=cn-beijing
ve sts GetCallerIdentity
```

### 文档

1. [入门](docs/1-GettingStarted-zh.md)：安装、源码编译、PATH 配置、第一次调用 API。
2. [认证与登录](docs/2-Authentication-zh.md)：AK/SK、默认凭证链、OIDC、ECS 实例角色、SSO、Console Login。
3. [配置管理](docs/3-Configuration-zh.md)：profile 查看、列表、切换、修改、删除。
4. [使用指南](docs/4-Usage-zh.md)：原生 API 与产品 CLI 调用、官方产品文档入口、系统参数、JSON body、数组和嵌套参数，以及 `--query` 过滤和 `--output` 输出格式。
5. [高级用法](docs/5-Advanced-zh.md)：自动补全、彩色输出、debug 日志、`--force` 强制泛化调用、常见问题。
6. [MCP 服务](docs/6-MCP-zh.md)：stdio、SSE、Streamable HTTP、OAuth、Header 凭证与 Region。

### Security and privacy
This project takes security seriously.
For vulnerability reporting and supported versions, see [SECURITY.md](SECURITY.md).
