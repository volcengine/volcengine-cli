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
