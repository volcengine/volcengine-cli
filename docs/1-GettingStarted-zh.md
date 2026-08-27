入门[(English)](1-GettingStarted.md) | [认证与登录 →](2-Authentication-zh.md)

---

## 入门

本文介绍如何安装 `ve`、把它放入系统 PATH，并完成一次最小可用的 API 调用。

## 环境要求

- 仅从源码编译时需要 Go 1.17 或更高版本；使用 Release 预编译包或 npm 安装时不需要 Go。
- 从 v1.0.20 开始，命令前缀由 `volcengine-cli` 更新为 `ve`。低版本不受影响；升级到 v1.0.20 及以后版本后，请同步更新脚本中的命令前缀。
- 从 v1.1.3 开始，`ve` 才支持安装和管理 Skill（`ve skills` 系列命令，以及 npm 安装时自动安装核心 Skill）。v1.1.3 之前的版本没有该能力，需要使用 Skill 时请先升级。

## 安装

### 通过 npm 安装

CLI 已发布为 npm 包 `@volcengine/cli`。如果本机已有 Node.js >= 14，可以直接全局安装：

```shell
npm install -g @volcengine/cli
```

安装后会提供 `ve` 命令入口：

```shell
ve version
ve --help
```

从 v1.1.3 开始，npm 安装或升级 CLI 时会自动安装火山引擎的核心 Skill；v1.1.3 之前的版本不会安装 Skill。
Skill 安装失败不影响 CLI 本身安装，若需要暂时跳过可设置 `VOLCENGINE_CLI_SKIP_SKILLS=1`。
安装完成后可用 `ve skills install`、`ve skills update`、`ve skills uninstall` 管理核心 Skill（同样要求 v1.1.3 及以上）。

**npm 安装**请通过 npm 升级，以保持包元数据一致：

```shell
npm install -g @volcengine/cli@latest
```

`ve upgrade` 会识别 npm 安装并委托执行上述 `npm install -g` 命令（不会原地替换二进制）。若委托失败，会打印上述手动命令，便于你处理权限或网络问题后自行重试。

**Release 解压**或**源码编译**（standalone）可使用：

```shell
ve upgrade
ve upgrade --yes
ve upgrade --version 1.0.49
```

### 通过 Release 获取客户端

1. 打开 <https://github.com/volcengine/volcengine-cli/releases> 获取最新版本。
2. 下载对应操作系统和架构的压缩包。
3. 解压后得到 `ve`，Windows 下为 `ve.exe`。

### 自行编译客户端

仓库提供 `build.sh`，可自动识别当前系统和架构，也支持显式指定目标平台。

```shell
# 按当前机器 OS 和架构编译
sh build.sh

# 指定 OS，架构仍自动探测
sh build.sh darwin
sh build.sh linux
sh build.sh windows

# 交叉编译，第二个参数指定架构：amd64 / arm64 / 386 / arm
sh build.sh linux amd64

# 查看帮助
sh build.sh -h
```

构建产物为 `ve`，Windows 产物为 `ve.exe`。

## 配置 PATH

通过 npm 全局安装时，npm 会把 `ve` 安装到全局 bin 目录；如果命令不可用，请检查 npm global bin 是否在 PATH 中：

```shell
npm bin -g
```

通过 Release 或源码编译时，需要确保 `ve` 所在目录在系统 PATH 中。常见做法是复制到 `/usr/local/bin`：

```shell
sudo cp ve /usr/local/bin
```

检查是否生效：

```shell
ve version
ve --help
```

如果 `/usr/local/bin` 不在 `$PATH` 中，请按你的 shell 环境配置 PATH。

## 最小配置

最直接的方式是创建一个 AK/SK profile：

```shell
ve configure set --profile default --region cn-beijing --access-key AK --secret-key SK
```

也可以不写配置文件，临时使用环境变量：

```shell
export VOLCENGINE_ACCESS_KEY=AK
export VOLCENGINE_SECRET_KEY=SK
export VOLCENGINE_REGION=cn-beijing
```

更多认证方式见 [认证与登录](2-Authentication-zh.md)。

## 第一次调用 API

查看支持的服务：

```shell
ve --help
```

查看某个服务的接口：

```shell
ve ecs --help
```

查看接口参数：

```shell
ve ecs DescribeRegions --help
```

调用接口：

```shell
ve sts GetCallerIdentity
```

选择一个响应字段，并以纯文本输出：

```shell
ve sts GetCallerIdentity --query 'Result.AccountId' --output text
```

支持的输出格式和查询安全规则见[过滤与输出格式](4-Usage-zh.md#过滤与输出格式)。

临时指定 region：

```shell
ve sts GetCallerIdentity --region cn-beijing
```

`--region` 是对外公开的 CLI 系统参数，统一放在 Action 后。若当前 Action 存在大小写完全相同的业务参数，双横线形式优先作为业务参数。更多调用示例见 [使用指南](4-Usage-zh.md)。

---

入门[(English)](1-GettingStarted.md) | [认证与登录 →](2-Authentication-zh.md)
