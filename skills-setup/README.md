# skills-setup

> **Language**: English · [中文](./README.zh.md)

One command to install the Volcengine CLI (`ve`), the ARK CLI (`arkcli`), and
their companion skills:

```bash
npx -y @volcengine/skills-setup
```

> **Prerequisites**: Node.js **>= 20** (provides `npx` and global `fetch`) and
> `unzip` (or `tar`) on your `PATH` to extract the skills bundle. See
> [Requirements](#requirements) for details.

## 1. What it does

`skills-setup` does three things, and every step is **idempotent** (safe to
re-run):

1. **Detect** whether `ve` and `arkcli` are already on your `PATH`.
2. **Install** the missing CLI tools via npm (nothing happens if already present).
3. **Install agent skills** from a pre-packaged bundle into your AI agents.

## 2.1 What gets installed

| Category | Item | Source | Where it lands |
|---|---|---|---|
| CLI tool | `ve` | `@volcengine/cli` (npm) | npm global bin (`--local` → project `node_modules/.bin`) |
| CLI tool | `arkcli` | `@volcengine/ark-cli` (npm) | same as above (`ark-cli` spelling also accepted when detecting) |
| Agent skills | all `volcengine/volcengine-skills` skills | pre-packaged **bundle zip** | the target agent's skills directory |

> The bundle contains **only** the `volcengine-*` skills. The `arkcli-*` skills
> are **not** re-bundled: installing `@volcengine/ark-cli` (step 2) already ships
> them, so bundling them again would just install the same skills twice.

**How skills are installed** — instead of cloning GitHub at runtime, the tool:

1. Downloads a **bundle `.zip`** (contains the skill directories from
   `volcengine/volcengine-skills`).
2. Extracts it locally with `tar` (falling back to `unzip`).
3. Hands the extracted directory to `npx skills add <dir>`.

By default skills install:

- **All skills** (`-s '*'`).
- To a **built-in list of default agents** (the `*` agent wildcard is
  intentionally avoided because it pulls in `promptscript`, which fails on a
  global install):

  ```
  claude-code, codex, deepagents, cursor, antigravity, antigravity-cli,
  openclaw, hermes-agent, opencode, trae, pi
  ```

  Agents that read skills from the shared **`~/.agent/skills`** directory are
  covered transitively by this install, so they do **not** need to be listed
  separately. To target any other agent, pass `--agent <name>` (see
  [2.2](#22-how-to-customize-the-command)).
- Into the **user-global** scope (`skills add -g`); the exact directory is
  chosen by the `skills` CLI per agent (e.g. Claude Code → `~/.claude/skills/`).
  Add `--skills-project` to install into the current project instead.

## 2.2 How to customize the command

All customization is done with flags. **Tool flags** (`--agent`, `--skill`,
`--dry-run`, …) go **before** `--`; anything **after** `--` is forwarded
verbatim to the underlying `skills add`.

| Goal | Command |
|---|---|
| Install everything (default) | `npx -y @volcengine/skills-setup` |
| Only one agent | `npx -y @volcengine/skills-setup --agent claude-code` |
| Multiple agents | `... --agent claude-code --agent codex` |
| Only certain skills | `... --skill volcengine-cli --skill volcengine-api` |
| Use a specific bundle URL | `... --bundle-url https://<tos>/skills-bundle.zip` |
| Use a local bundle zip | `... --bundle-file ./skills-bundle.zip` |
| Skills only (skip ve/arkcli) | `... --skip-install` |
| Tools only (skip skills) | `... --skip-skills` |
| Install ve/arkcli into the local project | `... --local` |
| Install skills into the current project | `... --skills-project` |
| Preview without executing | `... --dry-run` |
| Forward any extra `skills add` flag | `... -- --full-depth --subagent reviewer` |

## Requirements

- Node.js **>= 20** (bundles npm for `npx --yes`; global `fetch` downloads the
  bundle).
- `tar` or `unzip` on `PATH` to extract the bundle zip.
- Network access to npm and the bundle URL.

## Usage

```bash
# From this directory (no install needed — zero dependencies):
node setup.js [options] [-- <extra args forwarded to `skills add`>]

# Or, once installed as a package, via the bin:
skills-setup [options]
```

Common examples:

```bash
# Defaults: ensure ve+arkcli (global), install all skills to all supported agents
node setup.js

# See exactly what would run, without executing anything
node setup.js --dry-run

# Only Claude Code, install ve/arkcli into the local project instead of globally
node setup.js --agent claude-code --local

# Only install skills (assume ve/arkcli already installed), forward extra flags
node setup.js --skip-install -- --full-depth --subagent reviewer

# Use a specific bundle URL / a local bundle zip, preview only
node setup.js --bundle-url https://<tos>/skills-bundle.zip --dry-run
node setup.js --bundle-file ./skills-bundle.zip --skill volcengine-cli --dry-run
```

## Options

| Flag | Default | Effect |
|---|---|---|
| `--agent <name>` | built-in default agent list | Target agent(s). Repeatable or comma-separated. |
| `--skill <name>` | all skills (`*`) | Skill name(s). Repeatable or comma-separated. |
| `--bundle-url <url>` | `$SKILLS_BUNDLE_URL`, else built-in | Download the skills bundle zip from this URL. |
| `--bundle-file <path>` | — | Use a local bundle zip instead of downloading. |
| `--local` | off (global) | `npm install` without `-g` for missing binaries. |
| `--skills-project` | off (global) | Install skills into the current project instead of the user-global scope. |
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
  values are validated against a safe character set, `--bundle-url` must be an
  http(s) URL, and package names are fixed constants — so user input cannot
  inject options or shell commands. On Windows, npm/npx are `.cmd` shims that
  require the shell, so it runs them with `shell:true`; the same validation
  keeps that path safe.
- **Testable**: all side effects funnel through injectable `exec` / `download`
  (and an injectable `isExecutable` for detection), so `setup_test.js` runs with
  **zero network and zero real installs**:

  ```bash
  npm test   # or: node setup_test.js
  ```
