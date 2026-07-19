[← 使用指南](4-Usage-zh.md) | 高级用法[(English)](5-Advanced.md)

---

## 高级用法

本文包含自动补全、彩色输出、debug 日志和常见问题。这些能力不是调用 API 的必需步骤，但能提升日常使用效率和排障效率。

## 自动补全

CLI 支持生成 Bash、Zsh、fish 和 PowerShell 的补全脚本：

```shell
ve completion --help
```

### Bash

临时启用：

```shell
source <(ve completion bash)
```

每次打开 shell 自动启用：

```shell
echo 'source <(ve completion bash)' >> ~/.bashrc
source ~/.bashrc
```

系统级安装：

```shell
ve completion bash > /etc/bash_completion.d/ve
```

Bash 补全依赖 `bash-completion`。安装和检查：

```shell
# CentOS/RHEL
yum install bash-completion

# Debian/Ubuntu
apt-get install bash-completion

# 启用
source /usr/share/bash-completion/bash_completion

# 检查
type _init_completion
```

如果出现 `_get_comp_words_by_ref: command not found`，通常是 `bash-completion` 未安装或未 source。

macOS 使用 Homebrew 时：

```shell
ve completion bash > "$(brew --prefix)/etc/bash_completion.d/ve"
```

### Zsh

如果还没有启用 `compinit`：

```shell
echo "autoload -U compinit; compinit" >> ~/.zshrc
```

安装补全脚本：

```shell
ve completion zsh > "${fpath[1]}/_ve"
```

重新打开 shell，或执行：

```shell
source ~/.zshrc
```

### fish

临时启用：

```shell
ve completion fish | source
```

每次打开 shell 自动启用：

```shell
mkdir -p ~/.config/fish/completions
ve completion fish > ~/.config/fish/completions/ve.fish
```

### PowerShell

临时启用：

```powershell
ve completion powershell | Out-String | Invoke-Expression
```

保存脚本后，可在 PowerShell profile 中 source：

```powershell
ve completion powershell > ve.ps1
```

## 彩色输出

CLI 默认输出 JSON。可以启用彩色显示，便于终端阅读：

```shell
ve enable-color
```

关闭彩色显示：

```shell
ve disable-color
```

这两个命令会修改配置文件中的 `enableColor`。彩色输出影响 `ve configure get`、`ve configure list` 和 API 响应 JSON 的展示，不改变响应内容。

## Debug 日志

CLI debug 日志用于定位配置解析、参数构造和 SDK 调用问题。开启方式是设置环境变量：

```shell
VOLCENGINE_CLI_DEBUG=true ve sts GetCallerIdentity
```

关闭值：

```shell
VOLCENGINE_CLI_DEBUG=false
VOLCENGINE_CLI_DEBUG=0
VOLCENGINE_CLI_DEBUG=off
VOLCENGINE_CLI_DEBUG=no
VOLCENGINE_CLI_DEBUG=
```

其它非空值均视为开启。

开启后日志默认追加写入配置目录下的小时日志文件：

```text
~/.volcengine/logs/YYYYMMDDHH.log
```

例如：

```text
~/.volcengine/logs/2026061814.log
```

同一个小时内多次调用会追加到同一个文件。目录权限为 `0700`，日志文件权限为 `0600`。CLI 会拒绝写入符号链接或多硬链接日志文件，避免 debug 内容被追加到非预期文件。

debug 日志会记录：

- action 开始信息：service、action、version、method、content type。
- client 配置：profile 来源、凭证模式、region、endpoint、endpoint resolver、代理是否配置等。
- 参数构造结果：动态参数名、是否来自 `--body`、脱敏后的输入。
- SDK 请求尝试和请求结果。
- 错误阶段和耗时。

敏感字段会脱敏，例如 AK/SK、token、password、signature、private key 等常见字段。

排查示例：

```shell
VOLCENGINE_CLI_DEBUG=true ve sts GetCallerIdentity ---region cn-beijing
tail -n 100 ~/.volcengine/logs/$(date +%Y%m%d%H).log
```

## 常见问题

### `---debug` 为什么不支持？

debug 不是 CLI fixed flag。当前只通过 `VOLCENGINE_CLI_DEBUG` 环境变量开启：

```shell
VOLCENGINE_CLI_DEBUG=true ve sts GetCallerIdentity
```

当前支持的 fixed flags 只有：

```text
---profile, ---region, ---endpoint, ---lang
```

### 为什么提示缺少 region？

API 调用时必须能解析到 region。设置方式按优先级为：

1. `---region`
2. profile 中的 `region`
3. `VOLCENGINE_REGION`

示例：

```shell
ve sts GetCallerIdentity ---region cn-beijing
```

或：

```shell
ve configure set --profile prod --region cn-beijing
```

### 为什么配置了环境变量但没有生效？

如果当前存在 current profile，CLI 会优先使用 profile。环境变量默认凭证链主要在没有活跃 profile 时使用。

可以临时指定 profile：

```shell
ve sts GetCallerIdentity ---profile prod
```

也可以切换 current：

```shell
ve configure profile --profile prod
```

### 为什么 SSO 配置完成后仍用旧账号？

`ve configure sso` 只写入 SSO profile，不会切换 current。执行：

```shell
ve configure profile --profile my-dev
```

### 无图形界面怎么登录？

SSO：

```shell
ve configure sso --profile my-dev --sso-session my-sso --no-browser
ve sso login --sso-session my-sso --no-browser
```

Console Login：

```shell
ve login --profile dev --region cn-beijing --remote
```

### JSON body 报 `json format error` 怎么办？

`--body` 只接受 JSON object 或 JSON array。检查引号和 shell 转义：

```shell
ve rds_mysql ModifyDBInstanceIPList \
  --body '{"InstanceId":"mysql-xxxxxx","GroupName":"default","IPList":["10.20.30.40"]}'
```

不要把 `--body` 和其它 API 参数混用。

---

[← 使用指南](4-Usage-zh.md) | 高级用法[(English)](5-Advanced.md)
