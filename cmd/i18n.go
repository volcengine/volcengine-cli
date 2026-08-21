package cmd

import (
	"fmt"
	"os"
	"strings"
)

type Language string

const (
	LanguageEnglish           Language = "EN"
	LanguageSimplifiedChinese Language = "ZH"
)

type languageResolution struct {
	args       []string
	language   Language
	fixedFlags map[string]string
	err        error
}

// Pass systemFlags explicitly so Go's package initializer records the
// dependency and builds the registry before process argument preprocessing.
var processLanguageResolution = resolveProcessLanguage(systemFlags)
var currentLanguage = processLanguageResolution.language

func resolveProcessLanguage(registry *systemFlagRegistry) languageResolution {
	resolution, err := resolveSystemFlagsWithRegistry(os.Args[1:], registry)
	language := languageFromEnvironment(os.LookupEnv)
	if err != nil {
		return languageResolution{language: language, err: err}
	}
	if value, ok := resolution.fixedFlags["lang"]; ok {
		language, _ = normalizeLanguage(value)
	}
	return languageResolution{
		args:       resolution.args,
		language:   language,
		fixedFlags: resolution.fixedFlags,
	}
}

func resolveLanguage(args []string, lookupEnv func(string) (string, bool)) ([]string, Language, error) {
	language := languageFromEnvironment(lookupEnv)
	resolution, err := resolveSystemFlagsWithRegistry(args, systemFlags)
	if err != nil {
		return nil, language, err
	}
	if value, ok := resolution.fixedFlags["lang"]; ok {
		language, _ = normalizeLanguage(value)
	}
	return resolution.args, language, nil
}

func languageFromEnvironment(lookupEnv func(string) (string, bool)) Language {
	if lookupEnv == nil {
		return LanguageEnglish
	}
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if value, ok := lookupEnv(key); ok && strings.TrimSpace(value) != "" {
			language, supported := normalizeLanguage(value)
			if supported {
				return language
			}
			return LanguageEnglish
		}
	}
	return LanguageEnglish
}

func normalizeLanguage(value string) (Language, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if index := strings.IndexAny(normalized, ".@"); index >= 0 {
		normalized = normalized[:index]
	}
	normalized = strings.Replace(normalized, "_", "-", -1)

	if normalized == "en" || strings.HasPrefix(normalized, "en-") {
		return LanguageEnglish, true
	}
	switch normalized {
	case "zh", "zh-cn", "zh-sg", "zh-hans", "zh-hans-cn", "zh-hans-sg":
		return LanguageSimplifiedChinese, true
	default:
		return LanguageEnglish, false
	}
}

func setCurrentLanguage(language Language) {
	if language == LanguageSimplifiedChinese {
		currentLanguage = language
		return
	}
	currentLanguage = LanguageEnglish
}

func setLanguageForTest(language Language) func() {
	previous := currentLanguage
	setCurrentLanguage(language)
	return func() {
		currentLanguage = previous
	}
}

func tr(english string) string {
	if currentLanguage != LanguageSimplifiedChinese {
		return english
	}
	if chinese, ok := simplifiedChineseMessages[english]; ok {
		return chinese
	}
	if chinese, ok := simplifiedChineseCommandMessages[english]; ok {
		return chinese
	}
	return english
}

func trf(english string, args ...interface{}) string {
	return fmt.Sprintf(tr(english), args...)
}

func trErrorf(english string, args ...interface{}) error {
	return fmt.Errorf(tr(english), args...)
}

var simplifiedChineseMessages = map[string]string{
	"Show CLI version":                      "显示 CLI 版本",
	"Generate shell autocompletion scripts": "生成 Shell 自动补全脚本",
	"Manage Volcengine Agent Skills":        "管理火山引擎 Agent Skills",
	"Install Volcengine Agent Skills":       "安装火山引擎 Agent Skills",
	"Update Volcengine Agent Skills":        "更新火山引擎 Agent Skills",
	"Uninstall Volcengine Agent Skills":     "卸载火山引擎 Agent Skills",
	"Usage:":                                "用法：",
	"Examples:":                             "示例：",
	"Available Commands:":                   "可用命令：",
	"Available Actions:":                    "可用操作：",
	"Available Parameters:":                 "可用参数：",
	"API Parameters:":                       "API 参数：",
	"Parameter Form:":                       "参数方式：",
	"JSON Form:":                            "JSON 方式：",
	"Additional Commands:":                  "其他命令：",
	"Additional help topics:":               "其他帮助主题：",
	"Flags:":                                "参数：",
	"Global Flags:":                         "全局参数：",
	"Aliases:":                              "别名：",
	"Fixed Flags:":                          "固定参数：",
	"CLI Control Flags:":                    "CLI 控制参数：",
	"System Flags:":                         "系统参数：",
	"Service":                               "服务",
	"Action":                                "操作",
	"Description":                           "说明",
	"Required":                              "必选",
	"Optional":                              "可选",
	"Example:":                              "示例：",
	"Use a configured profile only for this invocation.":                                                                                           "仅为本次调用使用指定配置档案。",
	"Override the region only for this invocation.":                                                                                                "仅为本次调用覆盖地域。",
	"Override the endpoint only for this invocation.":                                                                                              "仅为本次调用覆盖接入地址。",
	"Set the display language for this invocation (EN or ZH).":                                                                                     "设置本次调用的显示语言（EN 或 ZH）。",
	"API version; uses metadata when omitted (required with --force for unlisted services).":                                                       "API 版本；省略时使用元数据（未收录服务配合 --force 时必填）。",
	"HTTP method GET or POST; explicit value overrides metadata, else metadata, else GET.":                                                         "HTTP 方法，GET 或 POST；显式值优先，否则用元数据，再否则默认 GET。",
	"Reserved double-dash controls (not API parameters):":                                                                                          "双横线保留控制参数（不是 API 业务参数）：",
	"Add a custom HTTP header as Name=Value; repeatable. Content-Type overrides metadata when set. Host/Authorization/Content-Length are blocked.": "添加自定义 HTTP 请求头，格式为 Name=Value，可重复。设置 Content-Type 时优先于元数据。Host/Authorization/Content-Length 不允许覆盖。",
	"JSON request body for application/json style calls; mutually exclusive with other API parameters.":                                            "application/json 风格调用的 JSON 请求体；不能与其他 API 参数同时使用。",
	"locking login cache: %w":                                                       "锁定登录缓存失败：%w",
	"releasing login cache lock: %w":                                                "释放登录缓存锁失败：%w",
	"reading previous login cache: %w":                                              "读取原登录缓存失败：%w",
	"resolving cache file path for profile %q: %w":                                  "解析配置档案 %q 的缓存路径失败：%w",
	"locking cached token for profile %q: %w":                                       "锁定配置档案 %q 的令牌缓存失败：%w",
	"releasing cache lock for profile %q: %w":                                       "释放配置档案 %q 的缓存锁失败：%w",
	"updating config before logout: %w":                                             "退出登录前更新配置失败：%w",
	"removing cached token for profile %q after config was cleared: %w":             "清除配置后删除配置档案 %q 的令牌缓存失败：%w",
	"releasing credential cache lock: %w":                                           "释放凭证缓存锁失败：%w",
	"Warning: failed to remove cache for profile %q after config was cleared: %v\n": "警告：清除配置后删除配置档案 %q 的缓存失败：%v\n",
	"locking cache file: %w":                                                        "锁定缓存文件失败：%w",
	"releasing cache lock: %w":                                                      "释放缓存锁失败：%w",
	"failed to release token cache lock: %w":                                        "释放令牌缓存锁失败：%w",
	"SSO token cache changed while configuring the profile; retry the command":      "配置档案期间 SSO 令牌缓存已发生变化；请重试该命令",
	"cached token is required to refresh access token":                              "刷新访问令牌需要已有的缓存令牌",
	"SSO token cache changed while refreshing; retry the command":                   "刷新期间 SSO 令牌缓存已发生变化；请重试该命令",
	"login cache changed while refreshing; retry or run 've login' again":           "刷新期间登录缓存已发生变化；请重试或再次运行 've login'",
	"refreshed credentials were discarded because the login session changed: %w":    "登录会话发生变化，已丢弃刷新的凭证：%w",
	"failed to remove token cache file after clearing config: %w":                   "清除配置后删除令牌缓存文件失败：%w",
	"Add an explicit API parameter as Name=Value; repeatable and available only with --force, mainly for system-name conflicts on unlisted APIs.": "以 Name=Value 显式添加 API 业务参数，可重复；仅可配合 --force 使用，主要用于未收录接口与系统参数同名的场景。",
	"--api-param is only available with --force":                                                                         "--api-param 仅可配合 --force 使用",
	"invalid --api-param %q, expected Name=Value":                                                                        "无效的 --api-param %q，应为 Name=Value 格式",
	"invalid --api-param %q, parameter name must not be empty":                                                           "无效的 --api-param %q，参数名不能为空",
	"--api-param name %q is reserved by the CLI":                                                                         "--api-param 名称 %q 已由 CLI 保留",
	"--api-param parameter %q cannot be specified more than once":                                                        "--api-param 业务参数 %q 不能重复指定",
	"--api-param parameter %q conflicts with direct --%s":                                                                "--api-param 业务参数 %q 与直接传入的 --%s 冲突",
	"Skip service/action metadata validation and force the call (presence-only; write --force alone, not --force true).": "跳过 service/action 元数据校验并强制发起调用（纯开关；只写 --force，不要写 --force true）。",
	"Set response output format (json|table|table-num|text|yaml|off). Default: json. table-num is table plus a row-number column. off still calls the API but skips response-dependent --query evaluation.": "设置响应输出格式（json|table|table-num|text|yaml|off）。默认：json。table-num 为表格加行号列。off 仍会发请求，但跳过依赖响应数据的 --query 求值。",
	"JMESPath expression to filter/project the full response (paths usually start at Result.*) before formatting.":                                                                                          "在格式化前用于过滤/投影完整响应的 JMESPath 表达式（路径通常从 Result.* 开始）。",
	"--force is required for force invocation":                "强制调用需要 --force",
	"--version is required when using --force for service %q": "对服务 %q 使用 --force 时必须提供 --version",
	"endpoint is required for unlisted service %q: set --endpoint, or configure endpoint in the profile / VOLCENGINE_ENDPOINT (endpoint-resolver=standard alone is not enough)": "未收录服务 %q 需要固定 endpoint：请设置 --endpoint，或在 profile / VOLCENGINE_ENDPOINT 中配置（仅 endpoint-resolver=standard 不够）",
	"unknown service %q: specify an action name": "未知服务 %q：请指定 action 名称",
	"unknown service %q: use --force with --version, and a fixed endpoint via --endpoint or profile/VOLCENGINE_ENDPOINT (endpoint-resolver=standard alone is not enough)":                       "未知服务 %q：请配合 --force 与 --version，并通过 --endpoint 或 profile/VOLCENGINE_ENDPOINT 提供固定 endpoint（仅 endpoint-resolver=standard 不够）",
	`"%s" is not bundled in local metadata. Use --force with --version, and a fixed endpoint via --endpoint or profile/VOLCENGINE_ENDPOINT (endpoint-resolver=standard alone is not enough).`:   `"%s" 未收录在本地元数据中。请配合 --force 与 --version，并通过 --endpoint 或 profile/VOLCENGINE_ENDPOINT 提供固定 endpoint（仅 endpoint-resolver=standard 不够）。`,
	`Use "{{.CommandPath}} [service] --help" for more information about a service.`:                                                                                                             "使用 \"{{.CommandPath}} [service] --help\" 查看服务的更多信息。",
	`Use "{{.CommandPath}} [action] --help" for more information about an action.`:                                                                                                              "使用 \"{{.CommandPath}} [action] --help\" 查看操作的更多信息。",
	`Use "{{.CommandPath}} [command] --help" for more information about a command.`:                                                                                                             "使用 \"{{.CommandPath}} [command] --help\" 查看命令的更多信息。",
	"Default help is concise. For full parameter descriptions and examples: -h --detail (or --help --detail).":                                                                                  "默认帮助为简洁模式。查看完整参数描述与示例：-h --detail（或 --help --detail）。",
	"--detail only expands help when used with -h/--help. Use -h --detail (or --help --detail). If you meant an API parameter named Detail, pass a value with matching case: --Detail <value>.": "--detail 仅在与 -h/--help 联用时展开帮助。请使用 -h --detail（或 --help --detail）。若指 API 参数 Detail，请用匹配大小写传值：--Detail <value>。",
}
