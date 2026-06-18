Getting Started[(中文)](1-GettingStarted-zh.md) | [Authentication →](2-Authentication.md)

---

## Getting Started

This guide explains how to install `ve`, put it on your PATH, and make a minimal API call.

## Requirements

- Go 1.5+ is required. Go 1.12+ is recommended.
- Starting from v1.0.20, the command prefix changed from `volcengine-cli` to `ve`. Earlier versions are not affected. After upgrading to v1.0.20 or later, update scripts that still call `volcengine-cli`.

## Install

### Install with npm

The CLI is published as the npm package `@volcengine/cli`. If Node.js >= 14 is available, install it globally:

```shell
npm install -g @volcengine/cli
```

The package provides the `ve` command:

```shell
ve version
ve --help
```

To upgrade to the latest version:

```shell
npm update -g @volcengine/cli
```

### Download from Release

1. Open <https://github.com/volcengine/volcengine-cli/releases>.
2. Download the archive for your OS and architecture.
3. Extract it to get `ve`, or `ve.exe` on Windows.

### Build from Source

The repository provides `build.sh`. It can auto-detect the current OS and architecture, or build for an explicit target.

```shell
# Build for the current machine
sh build.sh

# Specify OS; architecture is still auto-detected
sh build.sh darwin
sh build.sh linux
sh build.sh windows

# Cross-compile with an explicit architecture: amd64 / arm64 / 386 / arm
sh build.sh linux amd64

# Show help
sh build.sh -h
```

The output binary is `ve`, or `ve.exe` on Windows.

## Configure PATH

When installed globally with npm, npm places `ve` in the global bin directory. If the command is unavailable, check whether npm's global bin directory is in PATH:

```shell
npm bin -g
```

When using Release or a source build, make sure the directory containing `ve` is in your PATH. A common setup is:

```shell
sudo cp ve /usr/local/bin
```

Verify the command:

```shell
ve version
ve --help
```

If `/usr/local/bin` is not in `$PATH`, configure PATH for your shell.

## Minimal Configuration

The most direct setup is an AK/SK profile:

```shell
ve configure set --profile default --region cn-beijing --access-key AK --secret-key SK
```

You can also skip the config file and use environment variables:

```shell
export VOLCENGINE_ACCESS_KEY=AK
export VOLCENGINE_SECRET_KEY=SK
export VOLCENGINE_REGION=cn-beijing
```

See [Authentication](2-Authentication.md) for more credential modes.

## First API Call

List supported services:

```shell
ve --help
```

List actions under a service:

```shell
ve ecs --help
```

Show action parameters:

```shell
ve ecs DescribeRegions --help
```

Call an API:

```shell
ve sts GetCallerIdentity
```

Override region for one invocation:

```shell
ve sts GetCallerIdentity ---region cn-beijing
```

`---region` is a CLI fixed flag and does not conflict with API parameters written as `--Param value`. See [Usage](4-Usage.md) for more examples.

---

Getting Started[(中文)](1-GettingStarted-zh.md) | [Authentication →](2-Authentication.md)
