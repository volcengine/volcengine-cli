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
	"Use a configured profile only for this invocation.":                                                                                                                                        "仅为本次调用使用指定配置档案。",
	"Override the region only for this invocation.":                                                                                                                                             "仅为本次调用覆盖地域。",
	"Override the endpoint only for this invocation.":                                                                                                                                           "仅为本次调用覆盖接入地址。",
	"Set the display language for this invocation (EN or ZH).":                                                                                                                                  "设置本次调用的显示语言（EN 或 ZH）。",
	"API version; uses metadata when omitted (required with --force for unlisted services).":                                                                                                    "API 版本；省略时使用元数据（未收录服务配合 --force 时必填）。",
	"HTTP method GET or POST; explicit value overrides metadata, else metadata, else GET.":                                                                                                      "HTTP 方法，GET 或 POST；显式值优先，否则用元数据，再否则默认 GET。",
	"Reserved double-dash controls (not API parameters):":                                                                                                                                       "双横线保留控制参数（不是 API 业务参数）：",
	"Add a custom HTTP header as Name=Value; repeatable. Content-Type overrides metadata when set. Host/Authorization/Content-Length are blocked.":                                              "添加自定义 HTTP 请求头，格式为 Name=Value，可重复。设置 Content-Type 时优先于元数据。Host/Authorization/Content-Length 不允许覆盖。",
	"JSON request body for application/json style calls; mutually exclusive with other API parameters.":                                                                                         "application/json 风格调用的 JSON 请求体；不能与其他 API 参数同时使用。",
	"Skip service/action metadata validation and force the call (presence-only; write --force alone, not --force true).":                                                                        "跳过 service/action 元数据校验并强制发起调用（纯开关；只写 --force，不要写 --force true）。",
	"Set response output format (json|table|text|yaml|off). Default: json. off still calls the API but skips --query evaluation.":                                                               "设置响应输出格式（json|table|text|yaml|off）。默认：json。off 仍会发请求，但跳过 --query 求值。",
	"JMESPath expression to filter/project the full response (paths usually start at Result.*) before formatting.":                                                                              "在格式化前用于过滤/投影完整响应的 JMESPath 表达式（路径通常从 Result.* 开始）。",
	"--force is required for force invocation":                                                                                                                                                  "强制调用需要 --force",
	"--version is required when using --force for service %q":                                                                                                                                   "对服务 %q 使用 --force 时必须提供 --version",
	"endpoint is required for unlisted service %q: set --endpoint, or configure endpoint in the profile / VOLCENGINE_ENDPOINT (endpoint-resolver=standard alone is not enough)":                 "未收录服务 %q 需要固定 endpoint：请设置 --endpoint，或在 profile / VOLCENGINE_ENDPOINT 中配置（仅 endpoint-resolver=standard 不够）",
	"unknown service %q: specify an action name":                                                                                                                                                "未知服务 %q：请指定 action 名称",
	"unknown service %q: use --force with --version, and a fixed endpoint via --endpoint or profile/VOLCENGINE_ENDPOINT (endpoint-resolver=standard alone is not enough)":                       "未知服务 %q：请配合 --force 与 --version，并通过 --endpoint 或 profile/VOLCENGINE_ENDPOINT 提供固定 endpoint（仅 endpoint-resolver=standard 不够）",
	`"%s" is not bundled in local metadata. Use --force with --version, and a fixed endpoint via --endpoint or profile/VOLCENGINE_ENDPOINT (endpoint-resolver=standard alone is not enough).`:   `"%s" 未收录在本地元数据中。请配合 --force 与 --version，并通过 --endpoint 或 profile/VOLCENGINE_ENDPOINT 提供固定 endpoint（仅 endpoint-resolver=standard 不够）。`,
	`Use "{{.CommandPath}} [service] --help" for more information about a service.`:                                                                                                             "使用 \"{{.CommandPath}} [service] --help\" 查看服务的更多信息。",
	`Use "{{.CommandPath}} [action] --help" for more information about an action.`:                                                                                                              "使用 \"{{.CommandPath}} [action] --help\" 查看操作的更多信息。",
	`Use "{{.CommandPath}} [command] --help" for more information about a command.`:                                                                                                             "使用 \"{{.CommandPath}} [command] --help\" 查看命令的更多信息。",
	"Default help is concise. For full parameter descriptions and examples: -h --detail (or --help --detail).":                                                                                  "默认帮助为简洁模式。查看完整参数描述与示例：-h --detail（或 --help --detail）。",
	"--detail only expands help when used with -h/--help. Use -h --detail (or --help --detail). If you meant an API parameter named Detail, pass a value with matching case: --Detail <value>.": "--detail 仅在与 -h/--help 联用时展开帮助。请使用 -h --detail（或 --help --detail）。若指 API 参数 Detail，请用匹配大小写传值：--Detail <value>。",
}
