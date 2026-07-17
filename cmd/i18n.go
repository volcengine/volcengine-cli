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
	args     []string
	language Language
	err      error
}

var processLanguageResolution = resolveProcessLanguage()
var currentLanguage = processLanguageResolution.language

func resolveProcessLanguage() languageResolution {
	args, language, err := resolveLanguage(os.Args[1:], os.LookupEnv)
	if err != nil {
		language = languageFromEnvironment(os.LookupEnv)
	}
	return languageResolution{args: args, language: language, err: err}
}

func resolveLanguage(args []string, lookupEnv func(string) (string, bool)) ([]string, Language, error) {
	language := languageFromEnvironment(lookupEnv)
	filtered := make([]string, 0, len(args))
	found := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "---lang=") {
			return nil, language, fmt.Errorf("---lang does not support '=' syntax; use '---lang <value>'")
		}
		if arg != "---lang" {
			filtered = append(filtered, arg)
			continue
		}
		if found {
			return nil, language, fmt.Errorf("---lang cannot be specified more than once")
		}
		found = true

		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
			return nil, language, fmt.Errorf("---lang requires a value")
		}
		i++
		value := args[i]

		language, _ = normalizeLanguage(value)
	}

	return filtered, language, nil
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
	"Usage:":                                "用法：",
	"Examples:":                             "示例：",
	"Available Commands:":                   "可用命令：",
	"Available Actions:":                    "可用操作：",
	"Available Parameters:":                 "可用参数：",
	"Additional Commands:":                  "其他命令：",
	"Additional help topics:":               "其他帮助主题：",
	"Flags:":                                "参数：",
	"Global Flags:":                         "全局参数：",
	"Aliases:":                              "别名：",
	"Fixed Flags:":                          "固定参数：",
	"Service":                               "服务",
	"Action":                                "操作",
	"Description":                           "说明",
	"Use a configured profile only for this invocation.":                            "仅为本次调用使用指定配置档案。",
	"Override the region only for this invocation.":                                 "仅为本次调用覆盖地域。",
	"Override the endpoint only for this invocation.":                               "仅为本次调用覆盖接入地址。",
	"Set the display language for this invocation (EN or ZH).":                      "设置本次调用的显示语言（EN 或 ZH）。",
	`Use "{{.CommandPath}} [service] --help" for more information about a service.`: "使用 \"{{.CommandPath}} [service] --help\" 查看服务的更多信息。",
	`Use "{{.CommandPath}} [action] --help" for more information about an action.`:  "使用 \"{{.CommandPath}} [action] --help\" 查看操作的更多信息。",
	`Use "{{.CommandPath}} [command] --help" for more information about a command.`: "使用 \"{{.CommandPath}} [command] --help\" 查看命令的更多信息。",
}
