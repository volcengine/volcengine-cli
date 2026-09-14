## Volcengine Command Line Tools

---
[![Volcengine SKILL & CLI Survey](img_new.png)](https://www.volcengine.com/survey/q/v2/7662656599489085475/q36U8r81/5ffc/channel=doc)

To help us improve your experience with Volcengine SKILL & CLI, we invite you to participate in our [survey](https://www.volcengine.com/survey/q/v2/7662656599489085475/q36U8r81/5ffc/channel=doc) . Rewards are available for participants.

---

[中文](README.MD) | English

### Special Note

Starting from v1.0.20, the Volcengine CLI command prefix changed from `volcengine-cli` to `ve`. Earlier versions are not affected. After upgrading to v1.0.20 or later, update scripts that still call `volcengine-cli`.

Starting from v1.1.3, `ve` can install and manage Skills (the `ve skills` commands, and the automatic core Skill installation performed by the npm package). Versions earlier than v1.1.3 do not support Skills, so upgrade to v1.1.3 or later first.

### Overview

Volcengine CLI (`ve`) is a unified command-line entry point for developers, automation scripts, and AI agents using Volcengine. It provides general OpenAPI calls, credential and profile management, SSO, and Console Login. It also exposes product CLIs through `ve arkcli` and `ve tosutil`, with on-demand installation and managed upgrades, and provides Skill management and an MCP server for agents.

Use `ve <service> <action>` for general cloud resource APIs, or `ve arkcli <command>` and `ve tosutil <command>` for the Ark and object storage product tools. Each product CLI retains its own commands, authentication settings, arguments, and output rules; see [Proxy CLIs](#proxy-clis) below.

Go 1.18 or later is required to build from source. Prebuilt Release and npm installations do not require Go.

### Quick Install

Install with npm:

```shell
npm install -g @volcengine/cli
ve version
```

The npm package requires Node.js >= 14 and provides the `ve` command.

Starting from v1.1.3, installing or upgrading the CLI with npm also installs the core Volcengine Skills;
versions earlier than v1.1.3 install no Skills. A Skill installation failure does not fail the CLI
installation. Set `VOLCENGINE_CLI_SKIP_SKILLS=1` to skip Skill installation temporarily.

You can also download from Release:

1. Open <https://github.com/volcengine/volcengine-cli/releases>.
2. Download the archive for your OS and architecture.
3. Extract it to get `ve`, or `ve.exe` on Windows.

Or build from source:

```shell
sh build.sh
```

Put `ve` on PATH:

```shell
sudo cp ve /usr/local/bin
ve version
```

### Upgrade to the latest version

```shell
ve upgrade
```

- **standalone / Release**: download and replace the current binary in place
- **npm**: runs `npm install -g @volcengine/cli@latest` (on failure, prints the same manual command)
- **Homebrew**: runs `brew update`, then `brew upgrade volcengine-cli`

### Skill Management

> Skill installation and management require **v1.1.3 or later**. Run `ve version` to check, and upgrade with `ve upgrade` or `npm install -g @volcengine/cli@latest` if you are on an older version.

Core Skills are installed under `~/.agents/skills` and `~/.claude/skills`. When OpenClaw, Hermes Agent, or Trae
is detected, Skills are also installed in its dedicated directory. Codex, Cursor, and OpenCode can read
`~/.agents/skills` directly. See
[`volcengine-skills`](https://github.com/volcengine/volcengine-skills/blob/main/README_en.md#skills) for the
full Skill catalog and usage details.

```shell
ve skills install
ve skills update
ve skills uninstall
```

- `install`: Install core Skills that are not already installed.
- `update`: Update to the latest version and restore missing or modified core Skills to the latest official content. If no Skills are installed, it performs an installation.
- `uninstall`: Remove core Skills installed by `ve` while preserving locally modified content.

### Proxy CLIs

`ve` can run `arkcli` and `tosutil` as transparent second-level commands:

```shell
ve arkcli --help
ve tosutil version
```

The standard TOS entry point is `ve tosutil`; the previous `ve toscli` spelling remains a compatibility alias.

See each tool's official documentation for its commands, authentication, configuration, and parameters:

| Tool | Invocation through `ve` | Official documentation |
| --- | --- | --- |
| Ark CLI | `ve arkcli <command> [arguments...]` | [Ark CLI usage guide](https://github.com/volcengine/ark-cli#readme) |
| tosutil | `ve tosutil <command> [arguments...]` | [tosutil usage guide](https://www.volcengine.com/docs/6349/148775?lang=en) |

To run examples from those guides, replace the leading `arkcli` with `ve arkcli`, or `tosutil` with `ve tosutil`, and keep the remaining arguments unchanged. Use `ve arkcli --help` and `ve tosutil help` for help matching the installed version; argument syntax and output formats follow the respective tool's rules.

The native `ve` API flags `--profile`, `--region`, and `--endpoint` cannot precede a proxy command. Place flags supported by the child CLI after `arkcli` or `tosutil` so that it can interpret them. Session refresh only applies to the current `ve` Console Login cache; it does not automatically translate all `ve` authentication or profile settings into child CLI configuration.

Arguments after `arkcli` or `tosutil` are forwarded unchanged to the delegated CLI. Before forwarding, `ve` checks the current profile and attempts to refresh temporary STS credentials when the profile uses Console Login, has an active session, and is close to expiry. A missing login or refresh failure does not block delegation, so the child CLI can handle login itself; help and version invocations skip session refresh. On Unix, `ve` then replaces itself with the target CLI via `exec`, leaving no proxy parent process; Windows uses a minimal waiting wrapper.

Lookup checks the system `PATH` first and then the ve-managed directory. The managed directory defaults to `~/.volcengine/proxy-cli/bin` and can be overridden with `VOLCENGINE_CLI_PROXY_PATH`. If neither location contains the command, the first invocation installs it automatically from the independent manifest at `https://cloudcache.volccdn.com/ve/proxycli/manifest.json`; when the manifest is unavailable, an embedded fixed-version manifest uses the same CDN and never GitHub. The default directory stores managed installation state in `~/.volcengine/proxy-cli/install-state.json`; custom managed directories receive independent path-keyed state files. Missing or corrupt state is treated as not installed and rebuilt.

Managed versions follow the same background-notice model as `ve`: normal invocations launch an independent short-lived helper at most once every 24 hours, so the check neither blocks the target CLI nor dies during the Unix `exec` handoff. A later invocation prints an stderr notice when an update exists, leaving the upgrade decision to the user. External binaries found on `PATH` do not participate in this check; `VOLCENGINE_CLI_DISABLE_UPDATE_CHECK=1` disables background checks for both ve and managed proxy CLIs.

```shell
ve proxycli upgrade                 # install or upgrade all managed proxy CLIs
ve proxycli upgrade arkcli          # manage arkcli only
ve proxycli upgrade tosutil --yes    # manage tosutil only, without prompting
```

External installations found on `PATH` remain owned by their package manager and are never overwritten by `ve proxycli upgrade`. npm installations of `ve` do not install proxy CLIs by default; set `VOLCENGINE_CLI_INSTALL_PROXY_CLIS=1` to preinstall them.

### Quick Start

```shell
ve configure set --profile default --region cn-beijing --access-key AK --secret-key SK
ve sts GetCallerIdentity
```

You can also use environment variables:

```shell
export VOLCENGINE_ACCESS_KEY=AK
export VOLCENGINE_SECRET_KEY=SK
export VOLCENGINE_REGION=cn-beijing
ve sts GetCallerIdentity
```

### Documentation

1. [Getting Started](docs/1-GettingStarted.md): install, build from source, configure PATH, and make the first API call.
2. [Authentication](docs/2-Authentication.md): AK/SK, default credential chain, OIDC, ECS instance role, SSO, and Console Login.
3. [Configuration](docs/3-Configuration.md): inspect, list, switch, update, and delete profiles.
4. [Usage](docs/4-Usage.md): native API and product CLI calls, official product documentation links, system flags, JSON body, arrays and nested parameters, plus `--query` filtering and `--output` formats.
5. [Advanced Usage](docs/5-Advanced.md): shell completion, colored output, debug logs, `--force` invocation, and FAQ.
6. [MCP Server](docs/6-MCP.md): stdio, SSE, Streamable HTTP, OAuth, header credentials, and region resolution.

### Security and privacy
This project takes security seriously.
For vulnerability reporting and supported versions, see [SECURITY.md](SECURITY.md).
