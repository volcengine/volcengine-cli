[← Usage](4-Usage.md) | Advanced Usage[(中文)](5-Advanced-zh.md)

---

## Advanced Usage

This document covers shell completion, colored output, debug logs, `---force` invocation, and common questions. These features are not required for API calls, but they improve daily ergonomics and troubleshooting.

## Shell Completion

The CLI can generate completion scripts for Bash, Zsh, fish, and PowerShell:

```shell
ve completion --help
```

### Bash

Enable for the current shell:

```shell
source <(ve completion bash)
```

Enable for every new shell:

```shell
echo 'source <(ve completion bash)' >> ~/.bashrc
source ~/.bashrc
```

System-level installation:

```shell
ve completion bash > /etc/bash_completion.d/ve
```

Bash completion depends on `bash-completion`. Install and verify it:

```shell
# CentOS/RHEL
yum install bash-completion

# Debian/Ubuntu
apt-get install bash-completion

# Enable
source /usr/share/bash-completion/bash_completion

# Check
type _init_completion
```

If `_get_comp_words_by_ref: command not found` appears, `bash-completion` is usually missing or not sourced.

On macOS with Homebrew:

```shell
ve completion bash > "$(brew --prefix)/etc/bash_completion.d/ve"
```

### Zsh

If `compinit` is not enabled:

```shell
echo "autoload -U compinit; compinit" >> ~/.zshrc
```

Install the completion script:

```shell
ve completion zsh > "${fpath[1]}/_ve"
```

Start a new shell, or run:

```shell
source ~/.zshrc
```

### fish

Enable for the current shell:

```shell
ve completion fish | source
```

Enable for every new shell:

```shell
mkdir -p ~/.config/fish/completions
ve completion fish > ~/.config/fish/completions/ve.fish
```

### PowerShell

Enable for the current shell:

```powershell
ve completion powershell | Out-String | Invoke-Expression
```

Save the script and source it from your PowerShell profile:

```powershell
ve completion powershell > ve.ps1
```

## Colored Output

The CLI prints JSON by default. Enable colored display for easier reading in terminals:

```shell
ve enable-color
```

Disable colored display:

```shell
ve disable-color
```

These commands update `enableColor` in the config file. Colored output affects `ve configure get`, `ve configure list`, and API response JSON display. It does not change response content.

## Debug Logs

CLI debug logs help diagnose config resolution, parameter building, and SDK call issues. Enable them with an environment variable:

```shell
VOLCENGINE_CLI_DEBUG=true ve sts GetCallerIdentity
```

Values that disable debug:

```shell
VOLCENGINE_CLI_DEBUG=false
VOLCENGINE_CLI_DEBUG=0
VOLCENGINE_CLI_DEBUG=off
VOLCENGINE_CLI_DEBUG=no
VOLCENGINE_CLI_DEBUG=
```

Any other non-empty value enables debug.

When enabled, logs are appended to the hourly log file under the config directory:

```text
~/.volcengine/logs/YYYYMMDDHH.log
```

Example:

```text
~/.volcengine/logs/2026061814.log
```

Multiple calls in the same hour append to the same file. The directory permission is `0700`, and the log file permission is `0600`. The CLI rejects symbolic links and multi-hard-linked log files to avoid appending debug content to unexpected files.

Debug logs include:

- Action start information: service, action, version, method, content type.
- Client config: profile source, credential mode, region, endpoint, endpoint resolver, whether proxies are configured, and related settings.
- Input building result: dynamic parameter names, whether input came from `--body`, and sanitized input.
- SDK request attempts and call result.
- Error stage and duration.

Sensitive fields are masked, including common AK/SK, token, password, signature, and private key fields.

Debug inspection example:

```shell
VOLCENGINE_CLI_DEBUG=true ve sts GetCallerIdentity ---region cn-beijing
tail -n 100 ~/.volcengine/logs/$(date +%Y%m%d%H).log
```

## Self-upgrade (`ve upgrade`)

Behavior depends on how `ve` was installed:

| Install source | Default action |
|----------------|----------------|
| **Homebrew** (macOS/Linux; Homebrew/Linuxbrew/Cellar paths) | `brew update` then `brew upgrade volcengine-cli` (network required; `--version` needs `--force`) |
| **npm** (`node_modules/@volcengine/cli`) | Prints `npm install -g @volcengine/cli@...`; no in-place replace (exit 0) |
| **standalone** (Release zip, source build, etc.) | Download and replace the current binary in place |

```shell
ve upgrade              # source-aware: brew / npm guidance / standalone self-upgrade
ve upgrade --yes        # skip confirmation for standalone in-place (never implies package-manager upgrade)
ve upgrade --version 1.0.49
#   standalone: pin/downgrade in place
#   npm: prints "npm install -g @volcengine/cli@1.0.49" (exit 0; --force not required)
#   Homebrew: errors unless --force (in-place replace; not a brew pin)
ve upgrade --force      # npm/Homebrew in-place replace (not recommended; still prompts unless --yes)
ve upgrade --force --version 1.0.49
```

For standalone installs, without `--version` the CLI installs only a version newer than the running binary; a stale manifest cannot trigger an implicit downgrade. Downgrades require an explicit `--version`.

Standalone flow: download the platform zip and checksum from the official CDN (`https://cloudcache.volccdn.com/ve`), verify SHA256, then atomically replace the running binary. On failure the previous binary is kept/restored. If either CDN artifact is unavailable, the CLI falls back to GitHub Releases. On Windows, a temporary helper completes replacement after the running process exits and reports the final result through the same stdout/stderr streams.

### Version check and upgrade notice

On any `ve` invocation the CLI may start a lightweight background version check (at most once every 24 hours by default; about 1.5s network timeout). Command exit never waits for an in-flight check. If a cached or already-completed check finds a newer version, the CLI prints a notice to **stderr** (the suggested command is install-source aware); it never writes to stdout, so pipelines stay intact.

Environment variables:

| Variable | Description |
|----------|-------------|
| `VOLCENGINE_CLI_DISABLE_UPDATE_CHECK=1` | Disable background version checks and notices |
| `VOLCENGINE_CLI_UPDATE_CHECK_TTL_HOURS` | Cache TTL in hours (default 24) |
| `VOLCENGINE_CLI_DOWNLOAD_BASE_URL` | Override download base URL (default CDN) |
| `VOLCENGINE_CLI_INSTALL_METHOD` | Override install detection: `standalone`, `npm`, or `homebrew` |

Cache file: `~/.volcengine/cli/version_check.json`.

## Force Invocation

The CLI ships with metadata for a subset of cloud products. In normal mode it validates that the service and action exist. If a product or API is not yet bundled, or local metadata lags behind the service, you may see `unsupported action` or `unknown command`. Use `---force` to skip service/action validation and issue an RPC call directly.

### When to Use It

- Call a **service not yet listed** in metadata
- Call a **new action** under a known service
- Call an API with a **version not in bundled metadata** (via `---version`)

Unknown API parameters already pass through in normal mode. `---force` mainly removes limits at the **service / action / API version** level.

### Fixed Flag Requirements

| Flag | Required | Description |
| --- | --- | --- |
| `---force` | Yes | Presence-only switch; enables force mode when present; does not accept `true`/`false` values |
| `---version` | Depends on service | **Required for unlisted services**; **optional for bundled services**, falling back to metadata. Can also override the bundled API version |
| `---endpoint` | Depends on service | **Required for unlisted services**; optional for bundled services, where it can be inferred from service metadata and `---region` (profile endpoint is ignored) |
| `---method` | No | HTTP method: `GET` or `POST`; same on normal and force paths: explicit value → action metadata → default `GET` |
| `---region` | Depends on config | Same as normal calls; a region must be resolvable |

Notes:

- `---version` is the **OpenAPI version**, not the CLI tool version. Use `ve version` or `ve -v` for the CLI version.
- Unlisted services require an explicit `---endpoint` because the CLI has no service metadata from which to resolve one.
- Bundled services can omit `---version` in force mode, same as normal calls (e.g. `ve sts GetCallerIdentity ---force`).
- `---method` uses the same resolution order on normal and force paths: explicit `---method` overrides metadata; otherwise bundled action `Method`; otherwise defaults to `GET` (`---force` does not change this).
- Force control flags use **three hyphens** `---`, separate from API parameters with `--`.

### Examples

Normal metadata-validated call:

```shell
ve rds_mysql ModifyDBInstanceIPList \
  --InstanceId mysql-xxxxxx \
  --GroupName default \
  --IPList '["10.20.30.40"]'
```

Force-call an unlisted service:

```shell
ve newservice DescribeNewResource \
  ---version 2024-01-01 \
  ---endpoint open.volcengineapi.com \
  --SomeParam value \
  ---force
```

Known service, unknown action (`---version` optional; falls back to service metadata):

```shell
ve sts SomeNewAction \
  ---region cn-beijing \
  --Param1 value \
  ---force
```

Known service and action, skip validation only:

```shell
ve sts GetCallerIdentity ---region cn-beijing ---force
```

Override API version and endpoint:

```shell
ve ecs DescribeInstances \
  ---version 2024-01-01 \
  ---endpoint ecs.cn-beijing.volcengineapi.com \
  ---region cn-beijing \
  ---force
```

### Help for Unlisted Services

`ve <unknown-service> -h` or a bare service name prints force-invocation usage instead of a generic error:

```shell
ve newservice -h
ve newservice
```

### Common Errors

Missing `---version` for an unlisted service:

```text
---version is required when using ---force for service "newservice"
```

Missing `---endpoint` for an unlisted service:

```text
---endpoint is required when using ---force for unlisted service "newservice"
```

Unlisted service without `---force`:

```text
unknown service "newservice": use ---force with ---version and ---endpoint to call unlisted APIs
```

## FAQ

### Why is `---debug` unsupported?

Debug is not a CLI fixed flag. Use `VOLCENGINE_CLI_DEBUG`:

```shell
VOLCENGINE_CLI_DEBUG=true ve sts GetCallerIdentity
```

The supported fixed flags are:

```text
---profile, ---region, ---endpoint, ---lang, ---version, ---method, ---force
```

### Why does the CLI say region is missing?

API calls must resolve a region. Priority:

1. `---region`
2. `region` in profile
3. `VOLCENGINE_REGION`

Example:

```shell
ve sts GetCallerIdentity ---region cn-beijing
```

Or:

```shell
ve configure set --profile prod --region cn-beijing
```

### Why did my environment variables not take effect?

If a current profile exists, the CLI uses the profile first. The environment-based default credential chain is mainly used when no active profile is available.

Override profile for one call:

```shell
ve sts GetCallerIdentity ---profile prod
```

Switch current:

```shell
ve configure profile --profile prod
```

### Why do service commands still use the old account after SSO setup?

`ve configure sso` writes an SSO profile but does not switch current. Run:

```shell
ve configure profile --profile my-dev
```

### How do I log in without a graphical browser?

SSO:

```shell
ve configure sso --profile my-dev --sso-session my-sso --no-browser
ve sso login --sso-session my-sso --no-browser
```

Console Login:

```shell
ve login --profile dev --region cn-beijing --remote
```

### Why does `--body` return `json format error`?

`--body` only accepts a JSON object or JSON array. Check quoting and shell escaping:

```shell
ve rds_mysql ModifyDBInstanceIPList \
  --body '{"InstanceId":"mysql-xxxxxx","GroupName":"default","IPList":["10.20.30.40"]}'
```

Do not mix `--body` with other API parameters.

---

[← Usage](4-Usage.md) | Advanced Usage[(中文)](5-Advanced-zh.md)
