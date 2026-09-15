# skills-setup

> **语言**: [English](./README.md) · 中文

一条命令，快捷安装火山引擎命令行工具（`ve`）及核心 agent skills：

```bash
npx -y @volcengine/skills-setup
```

Ark CLI 通过 `ve arkcli` 代理命令使用。

> **前置依赖**：Node.js **>= 20**。当 `ve <= 1.1.2`、需要走兼容链路时，还需要
> `PATH` 上有 `unzip`（或 `tar`）用于解压 skill bundle。详见
> [环境要求](#环境要求)。

## 1. 它做什么

`skills-setup` 做三件事，每一步都是**幂等的**（可安全重复运行）：

1. **检测**：扫描 `PATH`，判断 `ve` 是否已安装。
2. **按需安装**：缺失时通过 npm 安装 `ve`，始终固定为 `@latest`
   （已存在则跳过；用 `--update` 可强制升级到最新版）。
3. **安装或更新 agent skills**：若最初未检测到 `ve`，安装
   `@volcengine/cli` 时会一并安装 skill，不再执行单独的 skill 安装；若已有
   `ve`，再按版本选择新版管理命令或旧版兼容链路。

## 2.1 安装了哪些内容

| 类别 | 内容 | 来源 | 落地位置 |
|---|---|---|---|
| CLI 工具 | `ve` | `@volcengine/cli`（npm） | npm 全局 bin（`--local` → 项目 `node_modules/.bin`） |
| Agent Skills | `volcengine/volcengine-skills` 的核心 skill | `ve skills update`；旧版兼容链路使用预打包 **bundle zip** | 目标 agent 的 skills 目录 |

> 兼容链路的 bundle 里**只含** `skills/core` 下的 `volcengine-cli`、
> `volcengine-troubleshooting`、`volcengine-knowledge-search` 和
> `volcengine-find-skills`。

从 skills-setup **0.9.0** 开始，CLI 安装和升级仅处理 `ve`，不再单独安装
`@volcengine/ark-cli`。**ve >= 1.1.9** 提供 `ve arkcli` 代理入口，首次调用时
复用已有的 Ark CLI，或按需下载托管二进制。已有旧版 `ve` 时，可先升级再使用：

```bash
npx -y @volcengine/skills-setup --update
ve arkcli --help
```

**skill 安装规则**：

1. 启动时不存在 `ve`：通过 npm 安装 `@volcengine/cli`，skill 由该 CLI 包一并
   安装，不再重复执行 skill 安装命令。
2. 启动时存在 `ve`，且版本 `>= 1.1.3`：运行 `ve skills update`；该命令同时
   支持首次安装和后续更新（`ve skills` 从 v1.1.3 开始提供）。
3. 启动时存在 `ve`，且版本 `<= 1.1.2` 或无法识别：自动使用原有的 bundle +
   `npx skills` 兼容链路。该链路完全由 `skills-setup` 管理。

## 2.2 常用命令

| 需求 | 命令 |
|---|---|
| 默认安装 ve 与核心 skills | `npx -y @volcengine/skills-setup` |
| 跳过 CLI 安装，只更新已有 `ve` 的 skill | `... --skip-install` |
| 跳过单独的 skill 更新步骤 | `... --skip-skills` |
| 强制升级 ve 到最新 | `... --update` |
| ve 装到本地项目而非全局 | `... --local` |
| 先预览不执行 | `... --dry-run` |

## 环境要求

- Node.js **>= 20**（自带 npm）。
- `ve <= 1.1.2` 的兼容链路还要求 `PATH` 上有 `tar` 或 `unzip`，并能访问
  bundle 下载地址；该链路使用 `npx --yes` 和全局 `fetch`。
- 能访问 npm；`ve skills update` 还需要能访问其 skill 发布源。

## 用法

```bash
# 在本目录直接运行（无需安装）：
node setup.js [选项]

# 或作为包安装后，通过 bin 调用：
skills-setup [选项]
```

常用示例：

```bash
# 默认：确保 ve（全局），安装或更新核心 skills
node setup.js

# 只预览将执行的命令，不真正执行
node setup.js --dry-run

# ve 装到当前项目而非全局
node setup.js --local

# 不安装 CLI，只更新已有 ve 的 skill
node setup.js --skip-install
```

## 参数（Options）

| Flag | 默认值 | 作用 |
|---|---|---|
| `--local` | 关（全局） | 安装 ve 时 `npm install` 不带 `-g`。 |
| `--skip-install` | 关 | 跳过 ve 安装；若已有 `ve`，仍按版本更新 skill。 |
| `--skip-skills` | 关 | 跳过单独的 skill 更新/兼容安装步骤。 |
| `--force` | 关 | 即使已存在也重装 ve。 |
| `--update` | 关 | 即使已存在也强制把 ve 升级到 `@latest`。 |
| `--dry-run` | 关 | 打印版本判断及计划执行的命令，不真正执行。 |
| `-h`, `--help` | — | 显示帮助。 |

## 退出码

| 码 | 含义 |
|---|---|
| `0` | 成功，或无事可做（已存在 / dry-run / help）。 |
| `1` | 用法 / 校验错误。 |
| `2` | 部分失败（已执行的步骤中有失败）。 |
| `3` | 全部失败（每个已执行步骤都失败）。 |

步骤不会 fail-fast：某个安装/skill 失败不会中断其余步骤，结果汇总为上表退出码。
