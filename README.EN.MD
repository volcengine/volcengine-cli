## Volcengine Command Line Tools

[中文](README.MD) | English

### Special Note

Starting from v1.0.20, the Volcengine CLI command prefix changed from `volcengine-cli` to `ve`. Earlier versions are not affected. After upgrading to v1.0.20 or later, update scripts that still call `volcengine-cli`.

### Overview

Volcengine CLI is a command-line tool for calling and managing Volcengine OpenAPI. You can use it to configure credentials, switch profiles, discover services and actions, call APIs, and obtain temporary credentials through SSO or Console Login.

Go 1.12+ is recommended.

### Quick Install

Install with npm:

```shell
npm install -g @volcengine/cli
ve version
```

The npm package requires Node.js >= 14 and provides the `ve` command.

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
4. [Usage](docs/4-Usage.md): service discovery, API calls, fixed flags, JSON body, arrays, and nested parameters.
5. [Advanced Usage](docs/5-Advanced.md): shell completion, colored output, debug logs, and FAQ.

### Security and privacy
This project takes security seriously.
For vulnerability reporting and supported versions, see [SECURITY.md](SECURITY.md).
