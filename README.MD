## Volcengine Command Line Tools

---
[![Volcengine SKILL & CLI Survey](img_new.png)](https://www.volcengine.com/survey/q/v2/7662656599489085475/q36U8r81/5ffc/channel=doc)

为提高您对火山引擎 SKILL&CLI 的使用体验，邀您参加[有奖调研](https://www.volcengine.com/survey/q/v2/7662656599489085475/q36U8r81/5ffc/channel=doc)

---

中文 | [English](README.EN.MD)

### 特别说明

从 v1.0.20 开始，火山引擎 CLI 命令前缀由 `volcengine-cli` 更新为 `ve`。低版本不受影响；升级到 v1.0.20 及以后版本后，请同步更新脚本中的命令前缀。

### 概述

火山引擎 CLI 是用于调用和管理火山引擎 OpenAPI 的命令行工具。你可以通过它配置凭证、切换 profile、查询服务接口、调用 API，并使用 SSO 或 Console Login 获取临时凭证。

从源码编译需要 Go 1.17 或更高版本；使用 Release 预编译包或 npm 安装时不需要 Go。

### 快速安装

使用 npm 安装：

```shell
npm install -g @volcengine/cli
ve version
```

npm 包要求 Node.js >= 14，安装后提供 `ve` 命令入口。

通过 npm 安装或升级 CLI 时，会自动安装火山引擎的核心 Skill。Skill 安装失败不影响 CLI 本身安装。
若需要暂时跳过 Skill 安装，可设置 `VOLCENGINE_CLI_SKIP_SKILLS=1`。

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
4. [使用指南](docs/4-Usage-zh.md)：服务查询、API 调用、系统参数、JSON body、数组和嵌套参数，以及 `--query` 过滤和 `--output` 输出格式。
5. [高级用法](docs/5-Advanced-zh.md)：自动补全、彩色输出、debug 日志、`--force` 强制泛化调用、常见问题。

### Security and privacy
This project takes security seriously.
For vulnerability reporting and supported versions, see [SECURITY.md](SECURITY.md).
