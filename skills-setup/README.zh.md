# skills-setup

> **语言**: [English](./README.md) · 中文

一条命令，快捷安装火山引擎命令行工具（`ve`）、ARK 命令行工具（`arkcli`）及配套 skill：

```bash
npx -y @volcengine/skills-setup
```

> **前置依赖**：Node.js **>= 20**（提供 `npx` 与全局 `fetch`），以及 `PATH` 上的
> `unzip`（或 `tar`）用于解压 skill bundle。详见 [环境要求](#环境要求)。

## 1. 它做什么

`skills-setup` 做三件事，每一步都是**幂等的**（可安全重复运行）：

1. **检测**：扫描 `PATH`，判断 `ve`、`arkcli` 是否已安装。
2. **按需安装**：缺失的 CLI 工具用 npm 装上（已存在则跳过）。
3. **安装 agent skills**：从预打包 bundle 装到你的 AI agent 里。

## 2.1 安装了哪些内容

| 类别 | 内容 | 来源 | 落地位置 |
|---|---|---|---|
| CLI 工具 | `ve` | `@volcengine/cli`（npm） | npm 全局 bin（`--local` → 项目 `node_modules/.bin`） |
| CLI 工具 | `arkcli` | `@volcengine/ark-cli`（npm） | 同上（检测时 `arkcli` / `ark-cli` 两种拼写都认） |
| Agent Skills | `volcengine/volcengine-skills` 全部 skill | 预打包 **bundle zip** | 目标 agent 的 skills 目录 |

> bundle 里**只含** `volcengine-*` 这批 skill；**不再打包** `arkcli-*`——因为第 2
> 步安装 `@volcengine/ark-cli` 时它自带这批 skill，再打包会重复安装同一批 skill。

**skill 是怎么装的** —— 运行时**不从 GitHub clone**，而是：

1. 下载一个 **bundle `.zip`**（内含 `volcengine/volcengine-skills` 仓库的
   skill 目录）。
2. 用 `tar`（失败回退 `unzip`）在本地解压。
3. 把解压目录交给 `npx skills add <目录>`。

默认的安装行为：

- **全部 skill**（`-s '*'`）。
- 装到**内置的默认 agent 列表**（不用 `*` 通配 agent，因为 `*` 会带上全局
  安装会失败的 `promptscript`）：

  ```
  claude-code, codex, deepagents, cursor, antigravity, antigravity-cli,
  openclaw, hermes-agent, opencode, trae, pi
  ```

  从共享目录 **`~/.agent/skills`** 读取 skill 的 agent 会被这次安装**顺带覆盖**，
  无需单独安装。若要给列表之外的其他 agent 装 skill，用 `--agent <name>` 指定
  （见 [2.2](#22-如何自定义命令)）。
- 装到**用户级全局目录**（`skills add -g`）；具体目录由 `skills` CLI 按目标
  agent 决定（如 Claude Code → `~/.claude/skills/`）。加 `--skills-project`
  则改装到当前项目目录。

## 2.2 如何自定义命令

所有自定义都通过 flag 完成。**本工具自己的 flag**（`--agent`、`--skill`、
`--dry-run` 等）必须放在 `--` **之前**；`--` **之后**的所有 token 会原样透传
给底层的 `skills add`。

| 需求 | 命令 |
|---|---|
| 默认全装 | `npx -y @volcengine/skills-setup` |
| 只装到某个 agent | `npx -y @volcengine/skills-setup --agent claude-code` |
| 装到多个 agent | `... --agent claude-code --agent codex` |
| 只装某个/某些 skill | `... --skill volcengine-cli --skill volcengine-api` |
| 指定 bundle 下载地址 | `... --bundle-url https://<tos>/skills-bundle.zip` |
| 用本地 bundle zip | `... --bundle-file ./skills-bundle.zip` |
| 只装 skill，不装工具 | `... --skip-install` |
| 只装工具，不下 skill | `... --skip-skills` |
| ve/arkcli 装到本地项目而非全局 | `... --local` |
| skill 装到当前项目而非全局 | `... --skills-project` |
| 先预览不执行 | `... --dry-run` |
| 透传任意 `skills add` 额外参数 | `... -- --full-depth --subagent reviewer` |

## 环境要求

- Node.js **>= 20**（自带 npm，供 `npx --yes` 使用；用全局 `fetch` 下载
  bundle）。
- `PATH` 上有 `tar` 或 `unzip`，用于解压 bundle zip。
- 能访问 npm 与 bundle 下载地址。

## 用法

```bash
# 在本目录直接运行（零依赖，无需安装）：
node setup.js [选项] [-- <透传给 `skills add` 的额外参数>]

# 或作为包安装后，通过 bin 调用：
skills-setup [选项]
```

常用示例：

```bash
# 默认：确保 ve+arkcli（全局），把全部 skill 装到所有默认 agent
node setup.js

# 只预览将执行的命令，不真正执行
node setup.js --dry-run

# 只给 Claude Code，ve/arkcli 装到当前项目而非全局
node setup.js --agent claude-code --local

# 只装 skill（假设 ve/arkcli 已装），并透传额外参数
node setup.js --skip-install -- --full-depth --subagent reviewer

# 指定 bundle 地址 / 本地 bundle zip，只预览
node setup.js --bundle-url https://<tos>/skills-bundle.zip --dry-run
node setup.js --bundle-file ./skills-bundle.zip --skill volcengine-cli --dry-run
```

## 参数（Options）

| Flag | 默认值 | 作用 |
|---|---|---|
| `--agent <name>` | 内置默认 agent 列表 | 目标 agent，可重复或逗号分隔。 |
| `--skill <name>` | 全部 skill（`*`） | skill 名，可重复或逗号分隔。 |
| `--bundle-url <url>` | `$SKILLS_BUNDLE_URL`，否则内置默认 | 从该地址下载 skill bundle zip。 |
| `--bundle-file <path>` | — | 用本地 bundle zip 代替下载。 |
| `--local` | 关（全局） | 安装缺失二进制时 `npm install` 不带 `-g`。 |
| `--skills-project` | 关（全局） | 把 skill 装到当前项目而非用户级全局目录。 |
| `--copy` | 关 | 给 `skills add` 传 `--copy`。 |
| `--full-depth` | 关 | 给 `skills add` 传 `--full-depth`。 |
| `--no-yes` | 关（保留 `-y`） | 不自动确认 `skills add`。 |
| `--skip-install` | 关 | 跳过 ve/arkcli 检测与安装，只装 skill。 |
| `--skip-skills` | 关 | 只装二进制，不装 skill。 |
| `--force` | 关 | 即使已存在也重装二进制。 |
| `--dry-run` | 关 | 打印将执行的 npm/npx 命令，不真正执行。 |
| `-h`, `--help` | — | 显示帮助。 |
| `-- <tokens>` | — | `--` 之后的 token 原样追加到每个 `skills add`。 |

## 退出码

| 码 | 含义 |
|---|---|
| `0` | 成功，或无事可做（已存在 / dry-run / help）。 |
| `1` | 用法 / 校验错误。 |
| `2` | 部分失败（已执行的步骤中有失败）。 |
| `3` | 全部失败（每个已执行步骤都失败）。 |

步骤不会 fail-fast：某个安装/skill 失败不会中断其余步骤，结果汇总为上表退出码。

## 设计说明

- **零依赖**，CommonJS，风格对齐 `../npm/install.js`。
- **安全性**：unix 上每个子进程都用 argv 数组启动（绝不用 shell 命令字符串，
  `shell:false`）。`--agent`/`--skill`/透传值都按安全字符集校验，`--bundle-url`
  必须是 http(s) URL，包名是固定常量——所以用户输入无法注入选项或 shell 命令。
  Windows 上 npm/npx 是 `.cmd` 脚本，需要 shell，因此用 `shell:true` 运行；
  同样的校验保证这条路径依然安全。
- **可测试**：所有副作用都收敛到可注入的 `exec` / `download`（检测用可注入的
  `isExecutable`），因此 `setup_test.js` 在**零网络、零真实安装**下即可运行：

  ```bash
  npm test   # 或：node setup_test.js
  ```
