## Volcengine Command Line Tools

中文 | [English](README.EN.MD)

### 特别说明
为了便于用户使用，从v1.0.20版本开始，火山引擎CLI工具命令前缀由“volcengine-cli”  更新为“ve”。低版本不受影响，请升级到v1.0.20及以后版本的用户及时更新指令前缀（可参考alias命令统一设置）。

###  概述
1. 火山引擎-命令行工具
2. Go版本最低1.5+ 推荐是用1.12+

### 安装 火山引擎CLI 

#### 通过release获取客户端
1. https://github.com/volcengine/volcengine-cli/releases 获取最新版本
2. 下载对应操作系统的版本 解压使用

#### 自行编译获取客户端

1. 使用build.sh编译客户端
    ```shell
    # 如果是mac 
    sh build.sh darwin
    mv ve-darwin ve
    # 如果是windows
    sh build.sh windows
    mv ve-windows ve
    # 如果是linux
    sh build.sh linux
    mv ve-linux ve
    ```


​    

### 配置 火山引擎CLI

将 火山引擎CLI 配置进环境变量

1. 检查一下 $PATH 系统变量是否存在 /usr/local/bin，若没有则请您根据实际情况为 火山引擎CLI 设置可用的环境变量

2. 执行下面的命令，将 ve 拷贝至 /usr/local/bin 目录下即可使用

    ```shell
    sudo cp ve /usr/local/bin
    ```



#### 配置凭证

火山引擎 CLI 支持多种凭证模式，按以下优先级解析凭证：

1. **配置文件 profile**：若存在活跃的 profile，使用 profile 中的凭证信息
2. **默认凭证链**：若配置文件为空或无 profile，自动按以下顺序尝试获取凭证：
   - 环境变量（`VOLCENGINE_ACCESS_KEY` / `VOLCENGINE_SECRET_KEY`）
   - OIDC Token（通过环境变量 `VOLCENGINE_OIDC_TOKEN_FILE` + `VOLCENGINE_OIDC_ROLE_TRN`）
   - ECS 实例角色（通过 IMDS 自动获取，仅在 ECS 环境中有效）
3. 以上全部失败时，报错提示

##### 凭证模式说明

| 模式 | 说明 | 必填参数 |
|------|------|----------|
| `ak` | AK/SK 静态凭证（默认） | `access-key`, `secret-key` |
| `ststoken` | AK/SK + 临时 Session Token | `access-key`, `secret-key`, `session-token` |
| `sso` | SSO 单点登录 | 通过 `ve configure sso` 配置 |
| `ramrolearn` | 角色扮演（通过 STS AssumeRole） | `access-key`, `secret-key`, `role-name`, `account-id` |
| `oidc` | OIDC Token 换取临时凭证 | `oidc-token-file`, `role-trn` |
| `ecsrole` | ECS 实例角色（通过 IMDS 获取） | `role-name` |

##### 方式一：通过 ve configure set 配置 profile

**AK 模式（默认）**

```shell
ve configure set --profile test --region cn-beijing --access-key ak --secret-key sk
```

**STS Token 模式**

```shell
ve configure set --profile test-sts --mode ststoken --region cn-beijing \
  --access-key ak --secret-key sk --session-token your-session-token
```

**角色扮演模式（RamRoleArn）**

```shell
ve configure set --profile test-ram --mode ramrolearn --region cn-beijing \
  --access-key ak --secret-key sk --role-name YourRoleName --account-id 2100000000
```

**OIDC 模式**

```shell
ve configure set --profile test-oidc --mode oidc --region cn-beijing \
  --oidc-token-file /path/to/oidc/token --role-trn trn:iam::2100000000:role/YourRoleName
```

**ECS 实例角色模式**

```shell
ve configure set --profile test-ecs --mode ecsrole --region cn-beijing \
  --role-name YourEcsRoleName
```

支持参数及相关说明

```shell
profile: 配置名称，如果 profile 已存在，则该命令会修改已有配置；否则就会新建配置，并将当前使用的profile指定为新配置
mode: 凭证模式，可选值：ak, ststoken, sso, ramrolearn, oidc, ecsrole；默认为 ak
access-key: 您的 AK
secret-key: 您的 SK
region: 地域，如 cn-beijing
session-token: 临时凭证的 Session Token（ststoken 模式必填）
role-name: 角色名称（ramrolearn、ecsrole 模式必填）
account-id: 账户 ID（ramrolearn 模式必填）
oidc-token-file: OIDC Token 文件路径（oidc 模式必填，也可通过环境变量 VOLCENGINE_OIDC_TOKEN_FILE 设置）
role-trn: 角色 TRN（oidc 模式必填，也可通过环境变量 VOLCENGINE_OIDC_ROLE_TRN 设置）
disable-ssl: 是否禁用 ssl，默认为 false
endpoint: 可以不填，默认值为 open.volcengineapi.com；当 endpoint-resolver 为 standard 时该值会被忽略
endpoint-resolver: 可选；指定 endpoint 解析器。设置为 standard（不区分大小写）时使用标准解析器；否则当 endpoint 非空时使用 endpoint
use-dual-stack: 可选；是否启用双栈，默认为 false
```

##### 方式二：通过环境变量配置（默认凭证链）

若配置文件中无配置，CLI 会自动通过默认凭证链获取凭证。最常用的方式是设置环境变量：

```shell
export VOLCENGINE_ACCESS_KEY=AK
export VOLCENGINE_SECRET_KEY=SK
export VOLCENGINE_REGION=cn-beijing

# 可选：自定义 endpoint；当 VOLCENGINE_ENDPOINT_RESOLVER=standard 时会忽略
export VOLCENGINE_ENDPOINT=open.volcengineapi.com

# 可选：endpoint 解析器；设置为 standard（不区分大小写）使用标准解析器
export VOLCENGINE_ENDPOINT_RESOLVER=standard

# 可选：是否启用双栈，true/false
export VOLCENGINE_USE_DUALSTACK=false

# 是否禁用SSL, 不设置的话默认为false
export VOLCENGINE_DISABLE_SSL=false

# 如果使用临时凭证，需要提供
export VOLCENGINE_SESSION_TOKEN=sessionToken
```

OIDC 凭证通过环境变量配置：

```shell
export VOLCENGINE_OIDC_TOKEN_FILE=/path/to/oidc/token
export VOLCENGINE_OIDC_ROLE_TRN=trn:iam::2100000000:role/YourRoleName
export VOLCENGINE_REGION=cn-beijing
```

ECS 实例角色无需额外配置，CLI 会自动通过 IMDS 获取凭证（仅在 ECS 环境中有效）。

如需禁用默认凭证链（例如在调试或安全敏感场景下，确保仅使用显式配置的 profile），可设置以下环境变量：

```shell
export VOLCENGINE_DISABLE_DEFAULT_CREDENTIALS=true
```

设置后，若无活跃的 profile，CLI 将直接报错而不会尝试从环境变量或 IMDS 自动获取凭证。



#### configure命令的其他操作

1. 火山引擎CLI使用configure配置profile之后，会优先使用profile中的鉴权信息进行接口签名访问
2. 使用 `ve configure set` 新增或者修改普通 AK profile 后，当前 configure 中的默认使用 profile 会被切换到新增或者修改的 profile 上；`ve configure sso` 不会自动切换当前 profile，配置完成后如需默认使用该 SSO profile，请执行 `ve configure profile --profile [配置名]`
3. 如果存在多个profile，请在调用接口前，使用ve configure profile --profile [配置名] 进行切换，以保证正确的使用

###### 获取指定配置(profile)信息

```shell
ve configure get --profile [配置名]
```

其中 profile 为可选字段，若不指定 profile 字段则会展示当前配置；若指定了 profile 则 火山引擎CLI 会尝试获取指定配置并展示

若配置不存在，则返回的配置字段全为默认值



###### 显示当前所有配置(profile)信息

```shell
ve configure list
```

###### 切换当前使用的配置(profile)信息 请确保版本大于或者等于>=1.0.16

```shell
ve configure profile --profile [配置名]
```

其中 profile 为必选字段，指定了 profile 则 火山引擎CLI 会尝试获切换当前的配值

若配置不存在，当前的配置不会发生切换，并且会给出错误的提示


###### 新建/修改配置(profile)

```shell
ve configure set --profile [配置名] --region [地区] --access-key [用户的AK] --secret-key [用户的SK] --endpoint [地区对应的endpoint]
```

新建或者修改配置后，当前的使用配置会切换到修改的配置上

新建配置时必须指定 profile 字段，修改已有配置必须指定 profile 字段

> 注意：region 在 `configure set` 时非强制，但运行 API 调用时必须存在（通过 profile 配置、`---region` 覆盖或环境变量 `VOLCENGINE_REGION`）。

其余可指定的字段：

- mode
- access-key
- secret-key
- region
- session-token
- role-name
- account-id
- oidc-token-file
- role-trn
- disable-ssl
- endpoint
- endpoint-resolver
- use-dual-stack

各个字段的作用您可以参考上节中 "支持参数及相关说明" 部分



###### 删除配置(profile)

```shell
ve configure delete --profile [配置名]
```

删除配置时必须指定 profile 字段以指示需要删除的配置名

若待删除的是当前正在使用的配置，则删除成功后 火山引擎CLI 会尝试从剩余的配置中随机挑选一个作为当前配置

#### SSO 快速上手

SSO 配置分两层：`sso-session` 保存企业 SSO 入口（Start URL、Region、Scopes），SSO profile 保存某个账号和角色的绑定信息。首次使用建议按下面顺序执行：

```shell
# 1. 创建 SSO 会话。registration-scopes 可省略，按提示回车即可使用默认值
ve configure sso-session --name my-sso --start-url https://{custom}.volccloudidentity.com/userportal --region cn-beijing

# 2. 创建 SSO 类型 profile，完成设备码授权，并在交互列表中选择 account 和 role
ve configure sso --profile my-dev --sso-session my-sso

# 3. 将当前默认 profile 切换到刚创建的 SSO profile
ve configure profile --profile my-dev

# 4. 后续直接执行业务命令；STS 过期时 CLI 会按需静默刷新
ve [service] [action] [params]
```

注意：`ve configure sso` 会写入 SSO profile，但不会自动修改当前默认 profile。若跳过第 3 步，业务命令仍会使用原来的 current profile。

#### SSO 命令关系

| 命令 | 什么时候用 | 主要作用 | 是否切换当前 profile |
| --- | --- | --- | --- |
| `ve configure sso-session` | 每个 SSO 入口通常配置一次 | 保存 Start URL、Region、Scopes，可被多个 SSO profile 复用 | 否 |
| `ve configure sso` | 每个 account + role 组合配置一次 | 关联 SSO 会话，执行首次授权，选择账号和角色，写入 SSO profile | 否 |
| `ve configure profile --profile [配置名]` | 需要让业务命令默认使用某个 profile 时 | 切换当前默认 profile | 是 |
| `ve sso login` | 被提示重新登录，或希望主动刷新 SSO 登录态时 | 重新执行设备码授权并缓存 access token | 否 |
| `ve sso logout` | 需要退出某个或全部 SSO 会话时 | 撤销缓存令牌，删除 token cache，清理 STS 临时凭证 | 否 |

#### 配置 SSO 会话 (configure sso-session)

```shell
ve configure sso-session --name [会话名] --start-url [SSO Start URL] --region [地区] --registration-scopes [scope1,scope2]
```

该命令用于新增或修改 SSO 会话，后续可被 `ve configure sso` 复用。参数说明：

```shell
name: SSO 会话名称；未提供时进入交互式选择/创建模式
start-url: SSO Start URL；必填。格式为“用户登录链接 + /userportal”后缀，例如 https://{custom}.volccloudidentity.com/userportal；编辑已有会话时回车可沿用默认值
region: SSO 区域；默认 cn-beijing；编辑已有会话时回车可沿用默认值
registration-scopes: SSO scope 列表（逗号分隔）；可省略，默认 cloudidentity:account:access,offline_access；允许值仅 cloudidentity:account:access 和 offline_access
```

交互式流程说明：
- 若当前无任何 SSO 会话，会提示输入会话名，不能为空
- 若已有会话，会进入可搜索的列表选择，可选 “<Create new session>” 创建新会话
- 编辑已有会话时，Start URL、Region、Scopes 会回填默认值，回车可沿用
- Scopes 会进行 trim、去重与校验；未输入时使用默认值

#### 配置 SSO 类型 profile (configure sso)

```shell
ve configure sso --profile [配置名] --sso-session [会话名] [--no-browser]
```

该命令用于创建或更新 SSO 类型 profile。执行时会关联一个 SSO 会话，走设备码授权流程，拉取账号列表和角色列表供用户选择，最后把 `mode=sso`、`sso-session-name`、`account-id`、`role-name`、`region` 写入 `~/.volcengine/config.json`。

参数说明：

```shell
profile: profile 名称；未输入时可回车，默认使用 {sso-role-name}-{sso-account-id}
sso-session: SSO 会话名称；未输入时进入交互式选择/创建模式
no-browser: 添加 --no-browser 表示禁止自动打开浏览器，适合无图形界面的服务器环境
```

注意事项：
- 若指定的 profile 已存在且不是 SSO 类型，将拒绝覆盖并返回错误
- 若指定的 SSO 会话不存在，将引导创建，并要求输入 Start URL、Region 与 Scopes
- `configure sso` 会完成首次授权和账号/角色选择，但不会切换当前默认 profile；请按需执行 `ve configure profile --profile [配置名]`
- 若要切换账号或角色，请重新执行 `ve configure sso --profile [配置名] --sso-session [会话名]`

#### 日常登录、自动刷新与登出

##### 业务命令的自动刷新

当前默认 profile 为 SSO 类型时，业务命令会自动检查并刷新 STS 临时凭证：
- 如果 profile 中的 `session-token` 未过期，直接复用
- 如果 STS 缺失或已过期，CLI 会使用缓存的 SSO access token 和 profile 中的 `account-id` / `role-name` 换取新的 STS，并写回 profile
- 如果 SSO access token 已过期或接近过期，CLI 只会尝试使用 refresh_token 静默刷新，不会在业务命令中自动打开浏览器
- 如果没有缓存 access token、refresh_token 缺失、客户端注册过期或刷新失败，业务命令会提示重新执行 `ve sso login`

##### SSO 登录 (sso login)

```shell
ve sso login --profile [配置名]
# 或
ve sso login --sso-session [会话名]
# 或不传参数，按已配置的 sso-session 自动选择/交互选择
ve sso login
```

`ve sso login` 用于显式重新登录 SSO，会重新走设备码授权并缓存新的 access token。与 AWS SSO login 语义一致，每次执行都会重新认证当前 SSO 会话，不会使用已有 refresh_token 静默换取 access token。

参数说明：

```shell
profile: 指定要使用的 SSO profile；必须存在且类型为 sso，且已配置 sso-session
sso-session: 指定要使用的 SSO 会话；会话必须存在且有效
no-browser: 添加 --no-browser 表示禁止自动打开浏览器
```

登录行为说明：
- 未指定 profile 和 sso-session 时：未配置任何会话会返回错误，仅有一个会话时直接使用该会话登录，有多个会话时进入交互式选择（可搜索筛选）
- `sso login` 不会重新选择 account/role；账号和角色来自 `ve configure sso` 写入的 `account-id` / `role-name`
- 当前仅支持设备码授权流程，`--no-browser` 用于关闭自动打开浏览器

##### SSO 登出 (sso logout)

```shell
ve sso logout --sso-session [会话名]
# 或不传参数，按已配置的 sso-session 自动选择/交互选择
ve sso logout
```

`ve sso logout` 用于退出 SSO 登录态。指定 `--sso-session` 时退出该会话；不指定时，未配置任何会话会返回错误，仅有一个会话时直接登出该会话，有多个会话时进入交互式选择，并支持选择 “All SSO sessions” 批量登出。

登出会执行：
- 撤销该 SSO 会话缓存的 refresh token（如果存在）
- 删除该 SSO 会话的 token cache
- 清理关联 SSO profile 中的 STS 临时凭证字段：`access-key`、`secret-key`、`session-token`、`sts-expiration`

登出不会执行：
- 不删除 SSO profile
- 不删除 sso-session 配置
- 不清除 profile 中的 `account-id` / `role-name`

因此，登出后如需继续使用原账号和角色，重新执行 `ve sso login` 即可；如需切换账号或角色，请重新执行 `ve configure sso`。

#### SSO 常见问题

- 执行完 `ve configure sso` 后业务命令还在使用旧账号怎么办？
  请执行 `ve configure profile --profile [配置名]` 切换当前默认 profile。`configure sso` 只写入 profile，不会自动切换 current profile。

- 什么时候需要执行 `ve sso login`？
  首次 `ve configure sso` 已完成授权。日常业务命令会自动复用或静默刷新凭证；只有当 CLI 提示重新登录，或者你想主动刷新 SSO 登录态时，再执行 `ve sso login`。

- 为什么 `ve sso login` 每次都会重新打开授权流程？
  这是显式登录语义：每次都会重新走设备码授权，不会用已有 refresh_token 静默完成登录。

- 想换账号或角色怎么办？
  重新执行 `ve configure sso --profile [配置名] --sso-session [会话名]`，在交互列表中重新选择 account 和 role。

- 没有图形界面的机器怎么登录？
  在 `ve configure sso` 或 `ve sso login` 后加 `--no-browser`，按命令行输出的授权地址在浏览器中完成授权。

- Scopes 应该怎么填？
  通常可以省略，默认使用 `cloudidentity:account:access,offline_access`。如果手动填写，只允许 `cloudidentity:account:access` 和 `offline_access`，CLI 会自动去重并校验。

#### Console 登录 (login)

```shell
ve login --profile [配置名]
# 或跨设备登录
ve login --profile [配置名] --remote
```

该命令用于通过火山引擎控制台完成 OAuth 2.0 + PKCE 登录，并将临时 STS 凭证缓存到本地，后续调用接口时可直接复用。支持参数及说明：

```shell
profile: 配置名称，可选，默认为 default
region: 地域，可选；未指定时优先复用当前 profile 中已配置的 region
remote: 是否启用跨设备登录模式；开启后会输出登录链接，并要求手动输入浏览器中展示的授权码
endpoint-url: 可选；登录服务地址覆盖项，默认值为 https://signin.volcengine.com
```

登录行为说明：
- 默认模式为本机登录：CLI 会启动本地回调服务，尝试自动打开浏览器，并等待浏览器完成登录后回跳
- 指定 `--remote` 后为跨设备登录：适用于无图形界面或无法本地打开浏览器的环境，CLI 会打印登录 URL，用户可在任意设备完成登录，再将浏览器展示的授权码粘贴回终端
- 登录成功后，会将 `mode` 写为 `console-login`，并在 profile 中记录 `login-session`；对应的令牌缓存保存在 `~/.volcengine/login/cache/`
- 若 profile 已存在，命令会直接复用并覆盖该 profile，并将其更新为 `console-login` 模式
- 若 profile 已绑定其他 `login-session`，CLI 会提示是否替换，只有确认后才会覆盖
- 若未显式指定 `--region`，且 profile 中已存在 region，CLI 会自动复用该值

使用建议：
- 适合需要通过控制台身份快速换取临时凭证的场景，无需手动准备 AK/SK
- 若本地会话过期且缓存中存在 refresh token，CLI 会自动尝试刷新；若刷新失败，则需要重新执行 `ve login`
- 如需隔离不同身份，建议为不同控制台账号使用不同的 `--profile`


#### Console 登出 (logout)

```shell
ve logout --profile [配置名]
# 或清除所有登录凭证
ve logout --all
```

该命令用于退出通过 `ve login` 建立的控制台登录会话，清除本地缓存的临时 STS 凭证。这是一个纯本地操作，不会发起任何网络请求。支持参数及说明：

```shell
profile: 配置名称，可选，默认为 default
all: 是否清除所有 console-login 类型 profile 的登录凭证缓存；开启后忽略 --profile 参数
```

登出行为说明：
- 默认模式为单 profile 登出：CLI 会根据 profile 中记录的 `login-session` 定位并删除 `~/.volcengine/login/cache/` 下对应的缓存文件，同时清除配置中的 `login-session` 字段
- 指定 `--all` 后为批量登出：CLI 遍历配置文件中的所有 Profile，筛选出 `console-login` 模式且 `login-session` 非空的条目，逐个通过 SHA-1 定位并删除对应的缓存文件，同时清除配置中的 `login-session` 字段。单个 Profile 清理失败不影响其余 Profile 的处理
- 仅 `console-login` 模式的 profile 允许执行登出操作；若 profile 使用的是 AK 或 SSO 模式，命令会返回错误
- 若 profile 不存在活跃的登录会话（`login-session` 为空），命令会提示无操作并正常退出
- 登出后，已被其他工具加载的临时凭证可能在过期前（通常 15 分钟内）仍然有效

使用建议：
- 在共享设备上使用完毕后，建议执行 `ve logout` 清除本地凭证
- 若需彻底清理所有登录缓存（如排查问题或设备交接），使用 `ve logout --all`
- 登出后若需重新使用，执行 `ve login` 即可重新建立登录会话

#### 配置自动补全

使用 ve completion --help 可以查看各种终端下配置自动补全的方式，用户可以根据提示信息自己选择是否配置自动补全功能

<br/>

##### Bash

火山引擎CLI 的 Bash 补全脚本可以通过 ve completion bash 进行查看，在 shell 中导入该自动补全脚本，即可开启自动补全功能

###### 1. 安装 bash-completion

火山引擎CLI 补全脚本依赖于工具 bash-completiom，所以你必须先安装并启用它（可以用命令 type _init_completion 检查 bash-completion 是否已安装）

1. 安装：yum install bash-completion 或 apt-get install bash-completion

2. 启用 bash-completion：source /usr/share/bash-completion/bash_completion

    建议将 source /usr/share/bash-completion/bash_completion 添加至 ~/.bashrc 中

3. 检查 bash-completion 是否安装成功：执行 type _init_completion 验证 bash-completion 的安装状态



###### 2. 配置自动补全

按以下步骤配置即可在 Bash 下开启自动补全功能：

1. echo 'source <(ve completion bash)' >> ~/.bashrc
2. ve completion bash > /etc/bash_completion.d/ve

之后重新加载 Shell （或者 source ~/.bashrc） 后即可生效



若出现 _get_comp_words_by_ref: command not found 的错误，请再次检查 bash-completion 是否安装配置成功

<br/>

##### Zsh

火山引擎CLI 的 Zsh 补全脚本可以通过 ve completion zsh 进行查看，在 shell 中导入该自动补全脚本，即可开启自动补全功能

按以下步骤配置即可在 Zsh 下开启自动补全功能：

1. Zsh 中启用 compinit：
    echo "autoload -U compinit; compinit" >> ~/.zshrc
2. 配置自动补全
    ve completion zsh > "${fpath[1]}/_ve"

之后重新加载 Shell （或者 source ~/.zshrc） 后即可生效





#### 配置颜色 

使用 ve enable-color 可以开启彩色显示功能

使用 ve disable-color 可以关闭彩色显示功能




### 使用 火山引擎CLI
#### 查询支持的服务列表及服务下支持的接口列表

1. 查询支持的服务列表
   ```shell
   ve [-h|--help]
   ```
2. 查询服务下支持的接口列表
   ```shell
   ve ecs [-h|--help]
   ```

#### 查看版本

```shell
ve version
```

```shell
ve -v
```



#### 调用API

使用 火山引擎CLI 调用 API 时，基本命令结构如下：

```shell
ve <service name> <action> [--parameter1 value1 --parameter2 value2 ...]
```

您可以使用 ve &lt;service name&gt; &lt;action&gt; --help 查看想要调用的API的参数列表、返回结果等信息，例如：ve ecs DescribeInstances --help

若需要查看更详细的信息，您也可以在 https://www.volcengine.com/docs 进行查阅

<br/>

##### 运行时覆盖参数

调用 API 时，可以使用 `---`（三横线）前缀指定 CLI 内部参数，覆盖当前 profile 的设置。`---` 参数不会与 API 参数冲突：

- `---profile`：运行时指定使用的 profile（不修改配置文件）
- `---region`：运行时覆盖 region

```shell
# 使用指定 profile 调用 API
ve ecs DescribeInstances ---profile prod

# 使用指定 profile 并覆盖 region
ve ecs DescribeInstances ---profile prod ---region ap-southeast-1

# ---region 也可以单独使用，覆盖当前 profile 的 region
ve ecs DescribeInstances ---region cn-shanghai --InstanceId "i-xxx"
```

> 注意：`--`（双横线）是 API 参数，`---`（三横线）是 CLI 内部参数，两者互不干扰。

<br/>

以下提供一些代码示例：

- 基本使用

    ```shell
    ve ecs DescribeInstances
    ```

    ```shell
    ve rds_mysql ListDBInstanceIPLists --InstanceId "xxxxxx"
    ```

- 支持传入 JSON

    ```shell
    ve rds_mysql ModifyDBInstanceIPList --InstanceId "xxxxxx" --GroupName "xxxxxx" --IPList '["10.20.30.40", "50.60.70.80"]'
    ```

- 对于 ContentType 为 application/json 的请求，火山引擎CLI 还支持直接将 body 作为 JSON 数据传入

    ```shell
    ve rds_mysql ModifyDBInstanceIPList --body '{"InstanceId":"xxxxxx", "GroupName": "xxxxxx", "IPList": ["10.20.30.40", "50.60.70.80"]}'
    ```

- 使用不同凭证模式调用 API

    ```shell
    # 使用 ECS 实例角色（需先配置 ecsrole 模式的 profile）
    ve configure set --profile ecs-role --mode ecsrole --role-name MyRole --region cn-beijing
    ve ecs DescribeInstances ---profile ecs-role

    # 使用 OIDC 凭证
    ve configure set --profile ci-oidc --mode oidc --region cn-beijing \
      --oidc-token-file /var/run/secrets/oidc-token --role-trn trn:iam::2100000000:role/CIRole
    ve ecs DescribeInstances ---profile ci-oidc

    # 无配置文件，通过环境变量 + 默认凭证链
    export VOLCENGINE_ACCESS_KEY=AK
    export VOLCENGINE_SECRET_KEY=SK
    export VOLCENGINE_REGION=cn-beijing
    ve ecs DescribeInstances
    ```
    

## Security and privacy
This project takes security seriously. 
For vulnerability reporting and supported versions, see [SECURITY.md](SECURITY.md)    





