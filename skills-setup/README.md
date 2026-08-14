# skills-setup

> **Language**: English · [中文](./README.zh.md)

One command to install the Volcengine CLI (`ve`), the ARK CLI (`arkcli`), and
their companion skills:

```bash
npx -y @volcengine/skills-setup
```

> **Prerequisites**: Node.js **>= 20**. The compatibility path used with
> `ve <= 1.1.2` also requires `unzip` (or `tar`) on your `PATH`. See
> [Requirements](#requirements) for details.

## 1. What it does

`skills-setup` does three things, and every step is **idempotent** (safe to
re-run):

1. **Detect** whether `ve` and `arkcli` are already on your `PATH`.
2. **Install** the missing CLI tools via npm, always pinned to `@latest`
   (nothing happens if already present — use `--update` to force an upgrade).
3. **Install or update agent skills**. If `ve` was initially missing, the
   `@volcengine/cli` package installs the skills and no separate skill command
   runs. If `ve` already exists, its version selects the update path.

## 2.1 What gets installed

| Category | Item | Source | Where it lands |
|---|---|---|---|
| CLI tool | `ve` | `@volcengine/cli` (npm) | npm global bin (`--local` → project `node_modules/.bin`) |
| CLI tool | `arkcli` | `@volcengine/ark-cli` (npm) | same as above (`ark-cli` spelling also accepted when detecting) |
| Agent skills | core `volcengine/volcengine-skills` skills | `ve skills update`; pre-packaged **bundle zip** for old versions | the target agent's skills directory |

> The compatibility bundle contains **only** `volcengine-cli`, `volcengine-troubleshooting`,
> `volcengine-knowledge-search`, and `volcengine-find-skills` from `skills/core`.
> The `arkcli-*` skills are **not** re-bundled: installing
> `@volcengine/ark-cli` (step 2) already ships them, so bundling them again would
> just install the same skills twice.

**Skill installation rules**:

1. `ve` is missing at startup: npm installs `@volcengine/cli`, whose package
   installs the skills as part of that installation. No second skill install is
   run.
2. `ve` exists and its version is `> 1.1.2`: run `ve skills update`, which
   handles both first-time installation and later updates.
3. `ve` exists and its version is `<= 1.1.2` or cannot be determined: use the
   existing bundle + `npx skills` compatibility path automatically. This path
   is fully managed by `skills-setup`.

## 2.2 Common commands

| Goal | Command |
|---|---|
| Install everything (default) | `npx -y @volcengine/skills-setup` |
| Skip CLI installation and update skills for an existing `ve` | `... --skip-install` |
| Skip the separate skill update step | `... --skip-skills` |
| Force-upgrade ve/arkcli to latest | `... --update` |
| Install ve/arkcli into the local project | `... --local` |
| Preview without executing | `... --dry-run` |

## Requirements

- Node.js **>= 20** (bundles npm).
- The `ve <= 1.1.2` compatibility path also requires `tar` or `unzip` on
  `PATH` and access to the bundle URL; it uses `npx --yes` and global `fetch`.
- Network access to npm and, for `ve skills update`, its skill release source.

## Usage

```bash
# From this directory (no install needed):
node setup.js [options]

# Or, once installed as a package, via the bin:
skills-setup [options]
```

Common examples:

```bash
# Defaults: ensure ve+arkcli (global), then install or update companion skills
node setup.js

# See exactly what would run, without executing anything
node setup.js --dry-run

# Install ve/arkcli into the current project instead of globally
node setup.js --local

# Do not install CLIs; only update skills for an existing ve
node setup.js --skip-install
```

## Options

| Flag | Default | Effect |
|---|---|---|
| `--local` | off (global) | `npm install` without `-g` for missing binaries. |
| `--skip-install` | off | Skip ve/arkcli installation; update skills if `ve` exists. |
| `--skip-skills` | off | Skip the separate skill update/compatibility step. |
| `--force` | off | Reinstall binaries even if already present. |
| `--update` | off | Force-upgrade ve/arkcli to `@latest` even if present. |
| `--dry-run` | off | Print the version check and planned commands; run nothing. |
| `-h`, `--help` | — | Show help. |

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success, or nothing to do (already present / dry-run / help). |
| `1` | Usage / validation error. |
| `2` | Partial failure (some executed steps failed). |
| `3` | Total failure (every executed step failed). |

Steps never fail fast: a failing install/skill does not abort the rest; results
are aggregated into the exit code above.
