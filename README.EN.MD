## Volcengine Command Line Tools

---
[![Volcengine SKILL & CLI Survey](img_new.png)](https://www.volcengine.com/survey/q/v2/7662656599489085475/q36U8r81/5ffc/channel=doc)

To help us improve your experience with Volcengine SKILL & CLI, we invite you to participate in our [survey](https://www.volcengine.com/survey/q/v2/7662656599489085475/q36U8r81/5ffc/channel=doc) . Rewards are available for participants.

---

[中文](README.MD) | English

### Special Note

Starting from v1.0.20, the Volcengine CLI command prefix changed from `volcengine-cli` to `ve`. Earlier versions are not affected. After upgrading to v1.0.20 or later, update scripts that still call `volcengine-cli`.

### Overview

Volcengine CLI is a command-line tool for calling and managing Volcengine OpenAPI. You can use it to configure credentials, switch profiles, discover services and actions, call APIs, and obtain temporary credentials through SSO or Console Login.

Go 1.17 or later is required to build from source. Prebuilt Release and npm installations do not require Go.

### Quick Install

Install with npm:

```shell
npm install -g @volcengine/cli
ve version
```

The npm package requires Node.js >= 14 and provides the `ve` command.

Installing or upgrading the CLI with npm also installs the core Volcengine Skills. A Skill installation
failure does not fail the CLI installation. Set `VOLCENGINE_CLI_SKIP_SKILLS=1` to skip Skill installation
temporarily.

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
4. [Usage](docs/4-Usage.md): service discovery, API calls, system flags, JSON body, arrays and nested parameters, plus `--query` filtering and `--output` formats.
5. [Advanced Usage](docs/5-Advanced.md): shell completion, colored output, debug logs, `--force` invocation, and FAQ.

### Security and privacy
This project takes security seriously.
For vulnerability reporting and supported versions, see [SECURITY.md](SECURITY.md).
