# skills-setup

A small, dependency-free Node.js CLI that bootstraps the Volcengine agent
toolchain:

1. **Detects** whether the `ve` and `arkcli` commands are on your `PATH`, and
   installs the missing ones via npm:
   - `ve`  → `@volcengine/cli`
   - `arkcli` → `@volcengine/ark-cli` (the `ark-cli` spelling is also accepted
     during detection)
2. **Downloads agent skills** with [`npx skills add`](https://skills.sh) from
   both official repos:
   - `volcengine/volcengine-skills`
   - `volcengine/ark-cli`
3. **Forwards custom `skills` parameters** so you can target specific agents /
   skills or pass any extra flag through.

By default it installs **all skills to all agents** (`-s '*' -a '*' -y`); use
`--agent` to narrow the target agent(s).

---

## 一条命令：`npx -y @volcengine/skills-setup`

> `-y` 让 npx 免确认自动拉取本包，适合 CI / 首次运行。**本包尚未发布到 npm** 时，用本地路径代替包名：`npx -y ./skills-setup`（详见下方「Usage」）。

### 核心做哪些事情（三步）

1. **检测工具**：扫描 `PATH`，判断 `ve`、`arkcli` 是否已安装。
2. **按需安装**（缺失才装，已存在则跳过，幂等）：
   - `ve` ← `npm i -g @volcengine/cli`
   - `arkcli` ← `npm i -g @volcengine/ark-cli`（检测时 `arkcli` / `ark-cli` 两种拼写都认）
   - 加 `--local` 可改装到当前项目而非全局。
3. **下载 skills**：用 `npx skills add` 从两个官方仓库拉取 agent skills，默认 **全部 skill × 全部 agent**（等价 `-s '*' -a '*' -y`）。

### 下载 / 安装了什么内容

| 类别 | 内容 | 来源 | 落地位置 |
|---|---|---|---|
| CLI 工具 | `ve` | `@volcengine/cli`（npm） | npm 全局 bin（`--local` → 本地 `node_modules/.bin`） |
| CLI 工具 | `arkcli` | `@volcengine/ark-cli`（npm） | 同上 |
| Agent Skills | volcengine 全部 skill | `volcengine/volcengine-skills`（GitHub） | 目标 agent 的 skills 目录 |
| Agent Skills | ark-cli 全部 skill | `volcengine/ark-cli`（GitHub） | 目标 agent 的 skills 目录 |

> skill 具体写到哪个目录由 `skills` CLI 按目标 agent 决定（如 claude-code → `.claude/skills/`；加 `--skills-global` 则装到用户级全局目录）。

### 自定义指令（可选参数）

| 需求 | 命令 |
|---|---|
| 默认全装 | `npx -y @volcengine/skills-setup` |
| 只装到某个 agent | `npx -y @volcengine/skills-setup --agent claude-code` |
| 装到多个 agent | `... --agent claude-code --agent codex` |
| 只装某个/某些 skill | `... --skill sign --skill sts` |
| 只处理某一个仓库 | `... --repo volcengine/ark-cli` |
| 只下 skill，不装工具 | `... --skip-install` |
| 只装工具，不下 skill | `... --skip-skills` |
| 装到本地项目而非全局 | `... --local` |
| 先预览不执行 | `... --dry-run` |
| 透传任意 `skills add` 额外参数 | `... -- --full-depth --subagent reviewer` |

> **参数位置规则**：本工具自己的 flag（如 `--agent` / `--dry-run` / `--skip-install`）必须放在 `--` **之前**；`--` **之后**的所有 token 会原样透传给底层的 `skills add`。完整参数表见下方 [Options](#options)。

---

## Requirements

- Node.js **>= 16** (bundles npm >= 8, required for `npx --yes`).
- Network access to npm and GitHub (the `skills` CLI pulls skills from GitHub).

## Usage

```bash
# From this directory (no install needed — zero dependencies):
node setup.js [options] [-- <extra args forwarded to `skills add`>]

# Or, once installed as a package, via the bin:
skills-setup [options]
```

### Common examples

```bash
# Defaults: ensure ve+arkcli (global), install all skills to all agents
node setup.js

# See exactly what would run, without executing anything
node setup.js --dry-run

# Only Claude Code, install ve/arkcli into the local project instead of globally
node setup.js --agent claude-code --local

# Only download skills (assume ve/arkcli already installed), forward extra flags
node setup.js --skip-install -- --full-depth --subagent reviewer

# Only one repo, a single skill, preview only
node setup.js --repo volcengine/ark-cli --skill sign --dry-run
```

## Options

| Flag | Default | Effect |
|---|---|---|
| `--agent <name>` | all agents (`*`) | Target agent(s). Repeatable or comma-separated. |
| `--skill <name>` | all skills (`*`) | Skill name(s). Repeatable or comma-separated. |
| `--repo <slug>` | both official repos | Restrict to a whitelisted repo. Repeatable. |
| `--local` | off (global) | `npm install` without `-g` for missing binaries. |
| `--skills-global` | off | Pass `-g` to `skills add` (user-level skills). |
| `--copy` | off | Pass `--copy` to `skills add`. |
| `--full-depth` | off | Pass `--full-depth` to `skills add`. |
| `--no-yes` | off (keeps `-y`) | Do not auto-confirm `skills add`. |
| `--skip-install` | off | Skip binary detect/install; skills only. |
| `--skip-skills` | off | Install binaries only; no skills. |
| `--force` | off | Reinstall binaries even if already present. |
| `--dry-run` | off | Print planned npm/npx commands; run nothing. |
| `-h`, `--help` | — | Show help. |
| `-- <tokens>` | — | Extra tokens appended verbatim to each `skills add`. |

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success, or nothing to do (already present / dry-run / help). |
| `1` | Usage / validation error. |
| `2` | Partial failure (some executed steps failed). |
| `3` | Total failure (every executed step failed). |

Steps never fail fast: a failing install/skill does not abort the rest; results
are aggregated into the exit code above.

## Design notes

- **Zero dependencies**, CommonJS, mirrors the style of `../npm/install.js`.
- **Security**: every child process is spawned with an argv array (never a shell
  command string) on unix (`shell:false`). `--agent`/`--skill`/passthrough
  values are validated against a safe character set, repos are whitelisted, and
  package names are fixed constants — so user input cannot inject options or
  shell commands. On Windows, npm/npx are `.cmd` shims that require the shell, so
  it runs them with `shell:true`; the same validation keeps that path safe.
- **Testable**: all side effects funnel through one injectable `exec` (and an
  injectable `isExecutable` for detection), so `setup_test.js` runs with **zero
  network and zero real installs**. Run the tests with:

  ```bash
  npm test   # or: node setup_test.js
  ```
