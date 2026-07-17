package cmd

var simplifiedChineseCommandMessages = map[string]string{
	"Log in to Volcengine Console via browser": "通过浏览器登录火山引擎控制台",
	`Authenticate with Volcengine Console using OAuth 2.0 + PKCE.
Opens a browser for authentication and caches temporary STS credentials locally.

Supports two modes:
  - Local (default): Opens browser on the same device
  - Remote (--remote): For headless environments, displays URL and accepts code input`: `使用 OAuth 2.0 + PKCE 登录火山引擎控制台。
打开浏览器完成认证，并在本地缓存临时 STS 凭证。

支持两种模式：
  - 本地（默认）：在当前设备上打开浏览器
  - 远程（--remote）：适用于无界面环境，显示登录地址并接收授权码`,
	"Configuration profile name":                                        "配置档案名称",
	"Region (prompts when omitted; empty input defaults to cn-beijing)": "地域（省略时交互输入，直接回车默认使用 cn-beijing）",
	"Enable cross-device (remote) login mode":                           "启用跨设备（远程）登录模式",
	"Override signin service endpoint URL":                              "覆盖登录服务接入地址",
	"Log out of Volcengine Console and clear cached credentials":        "退出火山引擎控制台登录并清除缓存凭证",
	`Remove locally cached login credentials for the specified profile or all profiles.

This is a purely local operation - no network requests are made to any server.
It deletes the cached STS token files from disk and clears the login_session
from the CLI configuration.

Modes:
  - Default: Logs out the specified profile (or "default" if not specified)
  - --all:   Logs out all configured console-login profiles with active sessions`: `删除指定配置档案或所有配置档案在本地缓存的登录凭证。

此操作仅修改本地数据，不会向任何服务器发送网络请求。
它会删除磁盘中的 STS 令牌缓存文件，并清除 CLI 配置中的 login_session。

模式：
  - 默认：退出指定配置档案（未指定时使用 "default"）
  - --all：退出所有具有有效会话的 console-login 配置档案`,
	`  # Log out the default profile
  ve logout

  # Log out a specific profile
  ve logout --profile my-profile

  # Log out all profiles and clear all cached login credentials
  ve logout --all`: `  # 退出默认配置档案
  ve logout

  # 退出指定配置档案
  ve logout --profile my-profile

  # 退出所有配置档案并清除全部缓存登录凭证
  ve logout --all`,
	"Log out all profiles and remove all cached login credentials":               "退出所有配置档案并删除全部缓存登录凭证",
	"Successfully logged in!":                                                    "登录成功！",
	"Credentials cached for profile: %s\n":                                       "凭证已缓存到配置档案：%s\n",
	"STS credentials expire at: %s\n":                                            "STS 凭证到期时间：%s\n",
	"Profile %q is currently using login_session %q.\n":                          "配置档案 %q 当前正在使用 login_session %q。\n",
	"The new login would replace it with %q.\n":                                  "新登录会将其替换为 %q。\n",
	"Replace the existing login_session? [y/N]: ":                                "是否替换现有 login_session？[y/N]：",
	"Please enter region [%s]: ":                                                 "请输入地域 [%s]：",
	"Attempting to automatically open the login page in your default browser.":   "正在尝试使用默认浏览器打开登录页面。",
	"If the browser does not open, open the following URL:":                      "如果浏览器未打开，请访问以下地址：",
	"Open the following URL in a browser on any device:":                         "请在任意设备的浏览器中打开以下地址：",
	"After completing login, enter the authorization code shown in the browser:": "完成登录后，请输入浏览器中显示的授权码：",
	"Authorization code: ":                                                       "授权码：",
	"Profile %q does not have an active login session. Nothing to do.\n":         "配置档案 %q 没有有效登录会话，无需操作。\n",
	"Successfully logged out of profile %q.\n":                                   "已成功退出配置档案 %q。\n",
	"No configuration found; nothing to log out.":                                "未找到配置，无需退出。",
	"Warning: failed to remove cache for profile %q: %v\n":                       "警告：删除配置档案 %q 的缓存失败：%v\n",
	"  Logged out profile %q\n":                                                  "  已退出配置档案 %q\n",
	"Warning: failed to update config after logout: %v\n":                        "警告：退出后更新配置失败：%v\n",
	"Successfully logged out %d console-login profile(s).\n":                     "已成功退出 %d 个 console-login 配置档案。\n",
	"No console-login profiles with active sessions found. Nothing to do.":       "未找到具有有效会话的 console-login 配置档案，无需操作。",
	"Note: Local cache has been removed for future CLI sessions.":                "注意：已删除本地缓存，后续 CLI 会话将不再使用它。",
	"Already-running tools that loaded temporary STS credentials before logout":  "退出前已经加载临时 STS 凭证的运行中工具",
	"may continue to use them until those credentials expire.":                   "仍可能继续使用这些凭证，直到凭证过期。",
	"nil input reader":                                                   "输入读取器为空",
	"resolving login region: %w":                                         "解析登录地域失败：%w",
	"generating code verifier: %w":                                       "生成代码校验器失败：%w",
	"generating state: %w":                                               "生成 state 失败：%w",
	"exchanging authorization code for token: %w":                        "使用授权码交换令牌失败：%w",
	"parsing STS credentials: %w":                                        "解析 STS 凭证失败：%w",
	"extracting login session from id_token: %w":                         "从 id_token 提取登录会话失败：%w",
	"confirming login session replacement: %w":                           "确认替换登录会话失败：%w",
	"login canceled: existing login_session was not replaced":            "登录已取消：未替换现有 login_session",
	"writing login cache: %w":                                            "写入登录缓存失败：%w",
	"writing config: %w":                                                 "写入配置失败：%w",
	"starting callback server: %w":                                       "启动回调服务器失败：%w",
	"waiting for authorization callback: %w":                             "等待授权回调失败：%w",
	"authorization failed: %s":                                           "授权失败：%s",
	"state mismatch: expected %s, got %s (possible CSRF attack)":         "state 不匹配：期望 %s，实际 %s（可能存在 CSRF 攻击）",
	"authorization callback did not include an authorization code":       "授权回调中没有授权码",
	"reading authorization code from stdin: %w":                          "从标准输入读取授权码失败：%w",
	"authorization code cannot be empty":                                 "授权码不能为空",
	"base64 decoding authorization response: %w":                         "Base64 解码授权响应失败：%w",
	"parsing decoded authorization response: %w":                         "解析解码后的授权响应失败：%w",
	`decoded authorization response does not contain a "code" parameter`: "解码后的授权响应不包含 \"code\" 参数",
	"no configuration found; nothing to log out":                         "未找到配置，无需退出",
	"profile %q not found in configuration":                              "配置中未找到配置档案 %q",
	"removing cached token for profile %q: %w":                           "删除配置档案 %q 的缓存令牌失败：%w",
	"updating config after logout: %w":                                   "退出后更新配置失败：%w",
	"resolving cache file path: %w":                                      "解析缓存文件路径失败：%w",
	"removing %s: %w":                                                    "删除 %s 失败：%w",
	"Manage CLI profiles and credentials":                                "管理 CLI 配置档案和凭证",
	"show target profile's information":                                  "显示指定配置档案的信息",
	`Description:
  show target profile's information
  if no profile name specified, show default profile`: `说明：
  显示指定配置档案的信息
  未指定配置档案名称时，显示默认配置档案`,
	"target profile name":                       "目标配置档案名称",
	"add new profile, or modify target profile": "新增配置档案或修改指定配置档案",
	`Description:
  add new profile, or modify target profile:
      1. if profile not exist, add new;
      2. if profile exist, modify target field

  supported modes: ak, sso, console-login, ramrolearn, oidc, ecsrole

Examples:
  ve configure set --profile test --region cn-beijing --access-key ak --secret-key sk
  ve configure set --profile test-ram --mode ramrolearn --region cn-beijing --access-key ak --secret-key sk --role-name YourRoleName --account-id 2100000000
  ve configure set --profile test-oidc --mode oidc --region cn-beijing --oidc-token-file /path/to/oidc/token --role-trn trn:iam::2100000000:role/YourRoleName
  ve configure set --profile test-ecs --mode ecsrole --region cn-beijing --role-name YourEcsRoleName`: `说明：
  新增配置档案或修改指定配置档案：
      1. 配置档案不存在时新增；
      2. 配置档案存在时修改指定字段

  支持的模式：ak、sso、console-login、ramrolearn、oidc、ecsrole

示例：
  ve configure set --profile test --region cn-beijing --access-key ak --secret-key sk
  ve configure set --profile test-ram --mode ramrolearn --region cn-beijing --access-key ak --secret-key sk --role-name YourRoleName --account-id 2100000000
  ve configure set --profile test-oidc --mode oidc --region cn-beijing --oidc-token-file /path/to/oidc/token --role-trn trn:iam::2100000000:role/YourRoleName
  ve configure set --profile test-ecs --mode ecsrole --region cn-beijing --role-name YourEcsRoleName`,
	"credential mode (ak, sso, console-login, ramrolearn, oidc, ecsrole)": "凭证模式（ak、sso、console-login、ramrolearn、oidc、ecsrole）",
	"your access key(AK)":                                   "您的访问密钥（AK）",
	"your secret key(SK)":                                   "您的私密密钥（SK）",
	"your region":                                           "您的地域",
	"endpoint bind with region":                             "与地域绑定的接入地址",
	"endpoint resolver (auto-addressing)":                   "接入地址解析器（自动寻址）",
	"HTTP proxy URL used by the SDK when SSL is disabled":   "SDK 禁用 SSL 时使用的 HTTP 代理地址",
	"HTTPS proxy URL used by the SDK":                       "SDK 使用的 HTTPS 代理地址",
	"your session token":                                    "您的会话令牌",
	"your sso session name":                                 "您的 SSO 会话名称",
	"your account id (required for ramrolearn mode)":        "您的账号 ID（ramrolearn 模式必填）",
	"your role name (required for ramrolearn/ecsrole mode)": "您的角色名称（ramrolearn/ecsrole 模式必填）",
	"path to OIDC token file (required for oidc mode)":      "OIDC 令牌文件路径（oidc 模式必填）",
	"role TRN (required for oidc mode)":                     "角色 TRN（oidc 模式必填）",
	"disable ssl":                                           "禁用 SSL",
	"use dual-stack endpoints":                              "使用双栈接入地址",
	"list all profiles":                                     "列出所有配置档案",
	"delete target profile":                                 "删除指定配置档案",
	"change target profile":                                 "切换到指定配置档案",
	`Description:
  list all profiles`: `说明：
  列出所有配置档案`,
	`Description:
  delete target profile`: `说明：
  删除指定配置档案`,
	`Description:
  change target profile`: `说明：
  切换到指定配置档案`,
	"SSO session [%s] configured successfully.\n": "SSO 会话 [%s] 配置成功。\n",
	"add or modify SSO session":                   "新增或修改 SSO 会话",
	`Description:
  add new SSO session, or modify target SSO session:
      1. if SSO session not exist, add new;
      2. if SSO session exist, modify target field

Examples:
  ve configure sso-session --name my-sso --start-url https://{custom}.volccloudidentity.com/userportal --region cn-beijing`: `说明：
  新增 SSO 会话或修改指定 SSO 会话：
      1. SSO 会话不存在时新增；
      2. SSO 会话存在时修改指定字段

示例：
  ve configure sso-session --name my-sso --start-url https://{custom}.volccloudidentity.com/userportal --region cn-beijing`,
	"SSO session name": "SSO 会话名称",
	"SSO start URL":    "SSO 起始地址",
	"SSO region":       "SSO 地域",
	"comma-separated SSO registration scopes (cloudidentity:account:access,offline_access)": "以逗号分隔的 SSO 注册权限范围（cloudidentity:account:access,offline_access）",
	"%s cannot be empty\n":        "%s 不能为空\n",
	"Please enter SSO Start URL:": "请输入 SSO 起始地址：",
	"SSO Start URL":               "SSO 起始地址",
	"Please enter SSO region:":    "请输入 SSO 地域：",
	"Please enter SSO registration scopes (comma-separated, allowed: %s) [%s]:":           "请输入 SSO 注册权限范围（逗号分隔，允许值：%s）[%s]：",
	"Please enter SSO registration scopes (comma-separated, allowed: %s) %s:":             "请输入 SSO 注册权限范围（逗号分隔，允许值：%s）%s：",
	"invalid SSO registration scope %q, allowed values: %s":                               "无效的 SSO 注册权限范围 %q，允许值：%s",
	"Enter profile name (press Enter to use default: {sso-role-name}-{sso-account-id}): ": "请输入配置档案名称（直接回车使用默认值：{sso-role-name}-{sso-account-id}）：",
	"SSO profile [%s] configured successfully.\n":                                         "SSO 配置档案 [%s] 配置成功。\n",
	"configure SSO type profile":                                                          "配置 SSO 类型的配置档案",
	`Description:
  configure SSO type profile with profile.mode=sso
  this command will guide you through the SSO authorization process
  and save the profile configuration to ~/.volcengine/config.json
  if the specified SSO session doesn't exist, it will be created automatically`: `说明：
  配置 profile.mode=sso 的 SSO 类型配置档案
  此命令将引导您完成 SSO 授权流程
  并将配置档案保存到 ~/.volcengine/config.json
  如果指定的 SSO 会话不存在，将自动创建`,
	"profile name": "配置档案名称",
	"Do not automatically open the browser during device authorization": "设备授权期间不自动打开浏览器",
	"SSO session already exists":                                        "SSO 会话已存在",
	"Please enter SSO session name:":                                    "请输入 SSO 会话名称：",
	"SSO session name cannot be empty":                                  "SSO 会话名称不能为空",
	"<Create new session>":                                              "<创建新会话>",
	"Select or create SSO session (type to filter, Enter to choose)":    "选择或创建 SSO 会话（输入内容筛选，按回车选择）",
	"Enter new SSO session name":                                        "请输入新的 SSO 会话名称",
	`
--------- SSO Session ----------
Name:   {{ .Name }}
Region: {{ sessionRegion .Session }}
URL:    {{ sessionStart .Session }}
Scopes: {{ sessionScopes .Session }}`: `
--------- SSO 会话 ----------
名称：  {{ .Name }}
地域：  {{ sessionRegion .Session }}
地址：  {{ sessionStart .Session }}
范围：  {{ sessionScopes .Session }}`,
	"Please enter SSO start URL:":                                                            "请输入 SSO 起始地址：",
	"SSO start URL cannot be empty":                                                          "SSO 起始地址不能为空",
	"Please enter SSO region [cn-beijing]:":                                                  "请输入 SSO 地域 [cn-beijing]：",
	"failed to save SSO session configuration: %v":                                           "保存 SSO 会话配置失败：%v",
	"Single sign-on (SSO) related operations":                                                "单点登录（SSO）相关操作",
	"Manage operations related to single sign-on (SSO), including login, configuration, etc": "管理单点登录（SSO）的登录、配置等操作",
	"Perform SSO login operations":                                                           "执行 SSO 登录操作",
	`Login via SSO, obtain the access token and store it in the cache.
This command requires specifying a configured profile, and this profile must be associated with a valid SS-session.
After a successful login, the system will automatically store the access token for subsequent operations.`: `通过 SSO 登录，获取访问令牌并将其存入缓存。
此命令需要指定已配置的配置档案，并且该配置档案必须关联有效的 SSO 会话。
登录成功后，系统会自动保存访问令牌供后续操作使用。`,
	`  # Login to SSO using the specified profile
  volcengine-cli sso login --profile my-sso-profile
  # Login to SSO using the specified sso-session
  volcengine-cli sso login --sso-session my-sso-session`: `  # 使用指定配置档案登录 SSO
  volcengine-cli sso login --profile my-sso-profile
  # 使用指定 SSO 会话登录
  volcengine-cli sso login --sso-session my-sso-session`,
	"the configuration file cannot be loaded":                                      "无法加载配置文件",
	"the specified profile was not found: %s":                                      "未找到指定配置档案：%s",
	"the specified profile is not of sso type":                                     "指定配置档案不是 SSO 类型",
	"the specified profile does not have sso-session configured":                   "指定配置档案未配置 SSO 会话",
	"the specified sso-session was not found: %s":                                  "未找到指定 SSO 会话：%s",
	"the specified sso-session is invalid: %s":                                     "指定 SSO 会话无效：%s",
	"no sso-session configured":                                                    "未配置 SSO 会话",
	"login failed for sso-session [%s]: %v\n":                                      "SSO 会话 [%s] 登录失败：%v\n",
	"login successfully for sso-session [%s]\n":                                    "SSO 会话 [%s] 登录成功\n",
	"login successfully":                                                           "登录成功",
	"Specify the name of the configuration file to be used":                        "指定要使用的配置档案名称",
	"Specify the SSO session to use when no profile is provided":                   "未提供配置档案时指定要使用的 SSO 会话",
	"Select SSO session (type to filter, Enter to choose)":                         "选择 SSO 会话（输入内容筛选，按回车选择）",
	"All SSO sessions":                                                             "所有 SSO 会话",
	"Select SSO session to logout (type to filter, Enter to choose)":               "选择要退出的 SSO 会话（输入内容筛选，按回车选择）",
	"failed to logout some sso sessions: %s":                                       "部分 SSO 会话退出失败：%s",
	"Perform SSO logout operations":                                                "执行 SSO 退出操作",
	"Logout from SSO by revoking the cached token and clearing local credentials.": "通过撤销缓存令牌并清除本地凭证退出 SSO。",
	`  # Logout SSO by profile
  volcengine-cli sso logout --profile my-sso-profile
  # Logout SSO by sso-session
  volcengine-cli sso logout --sso-session my-sso-session`: `  # 按配置档案退出 SSO
  volcengine-cli sso logout --profile my-sso-profile
  # 按 SSO 会话退出
  volcengine-cli sso logout --sso-session my-sso-session`,
	"logout successfully":                                           "退出成功",
	"Specify the SSO session to log out":                            "指定要退出的 SSO 会话",
	"To authorize, open the following URL in your browser:\n\n%s\n": "请在浏览器中打开以下地址完成授权：\n\n%s\n",
	"Attempting to open your default browser.":                      "正在尝试打开默认浏览器。",
	"If the browser does not open or you want to authorize from another device, open the following URL:\n\n%s\n": "如果浏览器未打开，或需要在其他设备上授权，请打开以下地址：\n\n%s\n",
	"Failed to open the browser automatically: %v\n":                                                             "自动打开浏览器失败：%v\n",
	"Please complete authorization promptly to avoid timeout. This device code expires in %d seconds.\n":         "请及时完成授权以免超时。此设备码将在 %d 秒后过期。\n",
	"SSO profile [%s] has been configured successfully\n":                                                        "SSO 配置档案 [%s] 配置成功\n",
	"Select account (type to filter, Enter to choose)":                                                           "选择账号（输入内容筛选，按回车选择）",
	"Select role (type to filter, Enter to choose)":                                                              "选择角色（输入内容筛选，按回车选择）",
	"Show help for %s":                                               "显示 %s 的帮助",
	"the SSO session must be specified":                              "必须指定 SSO 会话",
	"there is no SSO session named %s in the configuration file":     "配置文件中不存在名为 %s 的 SSO 会话",
	"failed to refresh stsToken: failed to obtain the config in ctx": "刷新 stsToken 失败：无法从上下文获取配置",
	"failed to refresh stsToken: profile is nil":                     "刷新 stsToken 失败：配置档案为空",
	"the start URL of SSO session %s is not configured":              "SSO 会话 %s 未配置起始地址",
	"failed to get role credentials: %w":                             "获取角色凭证失败：%w",
	"SSO access token cannot be refreshed because client credentials are missing; please log in using the `sso login` command":  "缺少客户端凭证，无法刷新 SSO 访问令牌；请使用 `sso login` 命令登录",
	"SSO access token cannot be refreshed because client registration has expired; please log in using the `sso login` command": "客户端注册已过期，无法刷新 SSO 访问令牌；请使用 `sso login` 命令登录",
	"failed to start device authorization: %w":                                                                           "启动设备授权失败：%w",
	"failed to start device authorization: verificationURI is empty":                                                     "启动设备授权失败：verificationURI 为空",
	"failed to poll access token: %w":                                                                                    "轮询访问令牌失败：%w",
	"authorization has timed out. Please try again":                                                                      "授权已超时，请重试",
	"no cached access token found; please log in using the `sso login` command":                                          "未找到缓存的访问令牌；请使用 `sso login` 命令登录",
	"SSO access token cannot be refreshed because refresh token is missing; please log in using the `sso login` command": "缺少刷新令牌，无法刷新 SSO 访问令牌；请使用 `sso login` 命令登录",
	"failed to refresh SSO access token; please log in using the `sso login` command: %w":                                "刷新 SSO 访问令牌失败；请使用 `sso login` 命令登录：%w",
	"currently, only device code authentication is supported":                                                            "当前仅支持设备码认证",
	"failed to obtain the access token: %v":                                                                              "获取访问令牌失败：%v",
	"failed to select the account and role: %v":                                                                          "选择账号和角色失败：%v",
	"access token is empty, please login again":                                                                          "访问令牌为空，请重新登录",
	"no available accounts found for the current user":                                                                   "当前用户没有可用账号",
	"no roles available under account %s":                                                                                "账号 %s 下没有可用角色",
	"failed to get access token: %w":                                                                                     "获取访问令牌失败：%w",
	"failed to list accounts: %w":                                                                                        "获取账号列表失败：%w",
	"failed to list roles for account %s: %w":                                                                            "获取账号 %s 的角色列表失败：%w",
	"failed to read access token cache: %w":                                                                              "读取访问令牌缓存失败：%w",
	"failed to parse access token expiry: %w":                                                                            "解析访问令牌到期时间失败：%w",
	"your access token has expired. Please log in again using the `sso login` command":                                   "访问令牌已过期，请使用 `sso login` 命令重新登录",
	"the SSO information is incomplete. Please configure the profile first":                                              "SSO 信息不完整，请先配置配置档案",
	"the sign-in URL of SSO session %s is not configured":                                                                "SSO 会话 %s 未配置登录地址",
	"token cache is empty":                                                                                               "令牌缓存为空",
	"client credentials are missing in the cache, please login first":                                                    "缓存中缺少客户端凭证，请先登录",
	"failed to remove token cache file: %v":                                                                              "删除令牌缓存文件失败：%v",
	`To load completions:

Bash:

  $ source <(%[1]s completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ %[1]s completion bash > /etc/bash_completion.d/%[1]s
  # macOS:
  $ %[1]s completion bash > $(brew --prefix)/etc/bash_completion.d/%[1]s

Zsh:

  # If shell completion is not already enabled in your environment,
  # you will need to enable it.  You can execute the following once:

  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ %[1]s completion zsh > "${fpath[1]}/_%[1]s"

  # You will need to start a new shell for this setup to take effect.

fish:

  $ %[1]s completion fish | source

  # To load completions for each session, execute once:
  $ %[1]s completion fish > ~/.config/fish/completions/%[1]s.fish

PowerShell:

  PS> %[1]s completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> %[1]s completion powershell > %[1]s.ps1
  # and source this file from your PowerShell profile.
`: `加载自动补全：

Bash：

  $ source <(%[1]s completion bash)

  # 为每个会话加载自动补全，只需执行一次：
  # Linux：
  $ %[1]s completion bash > /etc/bash_completion.d/%[1]s
  # macOS：
  $ %[1]s completion bash > $(brew --prefix)/etc/bash_completion.d/%[1]s

Zsh：

  # 如果当前环境尚未启用 Shell 自动补全，需要先启用。以下命令只需执行一次：

  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # 为每个会话加载自动补全，只需执行一次：
  $ %[1]s completion zsh > "${fpath[1]}/_%[1]s"

  # 需要启动新的 Shell 才能使此设置生效。

fish：

  $ %[1]s completion fish | source

  # 为每个会话加载自动补全，只需执行一次：
  $ %[1]s completion fish > ~/.config/fish/completions/%[1]s.fish

PowerShell：

  PS> %[1]s completion powershell | Out-String | Invoke-Expression

  # 为每个新会话加载自动补全，请运行：
  PS> %[1]s completion powershell > %[1]s.ps1
  # 并在 PowerShell 配置文件中引入此文件。
`,
	"no profile created": "尚未创建配置档案",
	"no profile name specified, show current profile: [%v]\n":                                 "未指定配置档案名称，显示当前配置档案：[%v]\n",
	"*** current profile: %v ***\n":                                                           "*** 当前配置档案：%v ***\n",
	"configuration profile %v not found":                                                      "未找到配置档案 %v",
	"delete current profile, set new current profile to [%v]\n":                               "已删除当前配置档案，新的当前配置档案为 [%v]\n",
	"mode %q requires --access-key":                                                           "模式 %q 需要 --access-key",
	"mode %q requires --secret-key":                                                           "模式 %q 需要 --secret-key",
	"mode %q requires login-session; run 've login' first":                                    "模式 %q 需要 login-session；请先运行 've login'",
	"mode %q requires --role-name":                                                            "模式 %q 需要 --role-name",
	"mode %q requires --account-id":                                                           "模式 %q 需要 --account-id",
	"mode %q requires --oidc-token-file":                                                      "模式 %q 需要 --oidc-token-file",
	"mode %q requires --role-trn":                                                             "模式 %q 需要 --role-trn",
	"unsupported mode %q, supported modes: ak, sso, console-login, ramrolearn, oidc, ecsrole": "不支持模式 %q，支持的模式：ak、sso、console-login、ramrolearn、oidc、ecsrole",
	`
--------- Account ----------
Name:   {{ .AccountName }}
ID:     {{ .AccountID }}`: `
--------- 账号 ----------
名称：{{ .AccountName }}
ID：  {{ .AccountID }}`,
	`
--------- Role ----------
Name:    {{ .RoleName }}
Account: {{ .AccountID }}`: `
--------- 角色 ----------
名称：{{ .RoleName }}
账号：{{ .AccountID }}`,
	"creating custom cache directory %s: %w":                         "创建自定义缓存目录 %s 失败：%w",
	"getting config directory: %w":                                   "获取配置目录失败：%w",
	"creating cache directory %s: %w":                                "创建缓存目录 %s 失败：%w",
	"marshalling login cache: %w":                                    "序列化登录缓存失败：%w",
	"creating temp file: %w":                                         "创建临时文件失败：%w",
	"writing temp cache file: %w":                                    "写入临时缓存文件失败：%w",
	"closing temp cache file: %w":                                    "关闭临时缓存文件失败：%w",
	"setting cache file permissions: %w":                             "设置缓存文件权限失败：%w",
	"renaming temp cache file: %w":                                   "重命名临时缓存文件失败：%w",
	"reading cache file %s: %w":                                      "读取缓存文件 %s 失败：%w",
	"parsing cache file %s: %w":                                      "解析缓存文件 %s 失败：%w",
	"id_token is empty":                                              "id_token 为空",
	"id_token does not have a valid JWT structure":                   "id_token 不具有有效的 JWT 结构",
	"base64-decoding JWT payload: %w":                                "Base64 解码 JWT 载荷失败：%w",
	"parsing JWT payload JSON: %w":                                   "解析 JWT 载荷 JSON 失败：%w",
	`id_token JWT payload does not contain a "trn" claim`:            "id_token 的 JWT 载荷不包含 \"trn\" 声明",
	"no configuration loaded":                                        "未加载配置",
	"profile %q not found in config":                                 "配置中未找到配置档案 %q",
	"profile %q does not have a login_session; run 've login' first": "配置档案 %q 没有 login_session；请先运行 've login'",
	"no active session. Please run 've login' first":                 "没有有效会话，请先运行 've login'",
	"parsing cached STS credentials: %w":                             "解析缓存的 STS 凭证失败：%w",
	"parsing issued_at %q: %w":                                       "解析 issued_at %q 失败：%w",
	"no refresh token available. Session expired. Please run 've login' to re-authenticate": "没有可用的刷新令牌，会话已过期；请运行 've login' 重新认证",
	"failed to refresh session token. Please run 've login' to re-authenticate. %w":         "刷新会话令牌失败；请运行 've login' 重新认证。%w",
	"parsing refreshed STS credentials: %w":                                                 "解析刷新后的 STS 凭证失败：%w",
	"Warning: failed to update login cache: %v\n":                                           "警告：更新登录缓存失败：%v\n",
	"console oauth request failed: %s %s":                                                   "控制台 OAuth 请求失败：%s %s",
	"request cannot be nil":                                                                 "请求不能为空",
	"grant_type is required":                                                                "必须提供 grant_type",
	"client_id is required":                                                                 "必须提供 client_id",
	"code is required for authorization_code grant":                                         "authorization_code 授权必须提供 code",
	"code_verifier is required for authorization_code grant":                                "authorization_code 授权必须提供 code_verifier",
	"refresh_token is required for refresh_token grant":                                     "refresh_token 授权必须提供 refresh_token",
	"unsupported grant_type: %s":                                                            "不支持 grant_type：%s",
	"failed to build request: %w":                                                           "构建请求失败：%w",
	"request failed: %w":                                                                    "请求失败：%w",
	"failed to read response: %w":                                                           "读取响应失败：%w",
	"ExchangeToken succeeded but response was empty":                                        "ExchangeToken 成功但响应为空",
	"access_token is empty":                                                                 "access_token 为空",
	"failed to parse STS credentials from access_token: %w":                                 "从 access_token 解析 STS 凭证失败：%w",
	"parsed STS credentials missing access_key_id":                                          "解析后的 STS 凭证缺少 access_key_id",
	"parsed STS credentials missing secret_access_key":                                      "解析后的 STS 凭证缺少 secret_access_key",
	"parsed STS credentials missing session_token":                                          "解析后的 STS 凭证缺少 session_token",
	"Warning: ":                     "警告：",
	"failed to create listener: %w": "创建监听器失败：%w",
	"timed out waiting for OAuth callback after %v":                           "等待 OAuth 回调超时（%v）",
	"failed to load callback html template asset: %w":                         "加载回调页面模板资源失败：%w",
	"failed to marshal callback page data: %w":                                "序列化回调页面数据失败：%w",
	"Method not allowed":                                                      "不允许使用此方法",
	"generate code_verifier failed: %w":                                       "生成 code_verifier 失败：%w",
	"generate state failed: %w":                                               "生成 state 失败：%w",
	"OAuth callback server stopped unexpectedly: %v":                          "OAuth 回调服务器意外停止：%v",
	"received non-GET OAuth callback request: method=%s path=%s":              "收到非 GET 的 OAuth 回调请求：method=%s path=%s",
	"OAuth callback returned error=%q":                                        "OAuth 回调返回错误=%q",
	"OAuth callback did not include both code and error; login flow may fail": "OAuth 回调既未包含 code 也未包含 error，登录流程可能失败",
	"failed to render OAuth callback page; fallback page is used: %v":         "渲染 OAuth 回调页面失败，将使用备用页面：%v",
	"failed to write OAuth callback page response: %v":                        "写入 OAuth 回调页面响应失败：%v",
	"failed to create temp file: %w":                                          "创建临时文件失败：%w",
	"failed to set cache file permissions: %w":                                "设置缓存文件权限失败：%w",
	"failed to write cache file: %w":                                          "写入缓存文件失败：%w",
	"failed to close cache file: %w":                                          "关闭缓存文件失败：%w",
	"failed to replace cache file: %w":                                        "替换缓存文件失败：%w",
	"failed to open the cache file: %v":                                       "打开缓存文件失败：%v",
	"failed to build registration cache key: %w":                              "生成注册缓存键失败：%w",
	"failed to open client cache file: %v":                                    "打开客户端缓存文件失败：%v",
	"failed to close the client cache file: %v":                               "关闭客户端缓存文件失败：%v",
	"failed to read the client cache: %v":                                     "读取客户端缓存失败：%v",
	"client registration is empty":                                            "客户端注册信息为空",
	"failed to create the cache directory: %v":                                "创建缓存目录失败：%v",
	"failed to register client: %w":                                           "注册客户端失败：%w",
	"failed to persist client registration: %w":                               "持久化客户端注册信息失败：%w",
	"failed to cache client credentials: %w":                                  "缓存客户端凭证失败：%w",
	"client registration is required to store token":                          "保存令牌需要客户端注册信息",
	"client registration is required to create token":                         "创建令牌需要客户端注册信息",
	"client registration is required to refresh token":                        "刷新令牌需要客户端注册信息",
	"client registration is required to start device authorization":           "启动设备授权需要客户端注册信息",
	"failed to decode token response (status %d, requestId: %s): %w":          "解析令牌响应失败（状态码 %d，requestId：%s）：%w",
	"callback server error: %v":                                               "回调服务器错误：%v",
}
