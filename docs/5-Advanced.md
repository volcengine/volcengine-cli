[← Usage](4-Usage.md) | Advanced Usage[(中文)](5-Advanced-zh.md)

---

## Advanced Usage

This document covers shell completion, colored output, debug logs, and common questions. These features are not required for API calls, but they improve daily ergonomics and troubleshooting.

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

## FAQ

### Why is `---debug` unsupported?

Debug is not a CLI fixed flag. Use `VOLCENGINE_CLI_DEBUG`:

```shell
VOLCENGINE_CLI_DEBUG=true ve sts GetCallerIdentity
```

The supported fixed flags are:

```text
---profile, ---region, ---endpoint, ---lang
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
