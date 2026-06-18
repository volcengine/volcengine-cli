## Volcengine Command Line Tools

中文 | [English](README.EN.MD)

### 特别说明

从 v1.0.20 开始，火山引擎 CLI 命令前缀由 `volcengine-cli` 更新为 `ve`。低版本不受影响；升级到 v1.0.20 及以后版本后，请同步更新脚本中的命令前缀。

### 概述

火山引擎 CLI 是用于调用和管理火山引擎 OpenAPI 的命令行工具。你可以通过它配置凭证、切换 profile、查询服务接口、调用 API，并使用 SSO 或 Console Login 获取临时凭证。

Go 版本最低 1.5+，推荐使用 1.12+。

### 快速安装

使用 npm 安装：

```shell
npm install -g @volcengine/cli
ve version
```

npm 包要求 Node.js >= 14，安装后提供 `ve` 命令入口。

也可以从 Release 下载：

1. 打开 <https://github.com/volcengine/volcengine-cli/releases> 获取最新版本。
2. 下载对应操作系统和架构的压缩包。
3. 解压得到 `ve`，Windows 下为 `ve.exe`。

或从源码编译：

```shell
sh build.sh
```

将 `ve` 放到 PATH 中：

```shell
sudo cp ve /usr/local/bin
ve version
```

### 快速开始

```shell
ve configure set --profile default --region cn-beijing --access-key AK --secret-key SK
ve sts GetCallerIdentity
```

也可以使用环境变量：

```shell
export VOLCENGINE_ACCESS_KEY=AK
export VOLCENGINE_SECRET_KEY=SK
export VOLCENGINE_REGION=cn-beijing
ve sts GetCallerIdentity
```

### 文档

1. [入门](docs/1-GettingStarted-zh.md)：安装、源码编译、PATH 配置、第一次调用 API。
2. [认证与登录](docs/2-Authentication-zh.md)：AK/SK、默认凭证链、OIDC、ECS 实例角色、SSO、Console Login。
3. [配置管理](docs/3-Configuration-zh.md)：profile 查看、列表、切换、修改、删除。
4. [使用指南](docs/4-Usage-zh.md)：服务查询、API 调用、固定参数、JSON body、数组和嵌套参数。
5. [高级用法](docs/5-Advanced-zh.md)：自动补全、彩色输出、debug 日志、常见问题。

### Security and privacy
This project takes security seriously.
For vulnerability reporting and supported versions, see [SECURITY.md](SECURITY.md).
