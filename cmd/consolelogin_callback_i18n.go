package cmd

import "strings"

const callbackDefaultLang = "zh"

// callbackPageMessages 保存 OAuth callback 页面上的固定文案。
// OAuth 服务端返回的错误内容保持原文展示，避免排障时和服务端日志无法对齐。
type callbackPageMessages struct {
	DocumentTitleSuccess string `json:"documentTitleSuccess"`
	DocumentTitleFailure string `json:"documentTitleFailure"`
	SuccessTitle         string `json:"successTitle"`
	FailureTitle         string `json:"failureTitle"`
	SuccessCopy          string `json:"successCopy"`
	FailureCopy          string `json:"failureCopy"`
	OAuthErrorLabel      string `json:"oauthErrorLabel"`
}

// callbackPageData 是注入 callback.html 的唯一动态数据入口。
// 页面端只通过 textContent 渲染这些值，避免把 OAuth 错误内容当作 HTML 执行。
type callbackPageData struct {
	Lang         string               `json:"lang"`
	ErrorMessage string               `json:"errorMessage"`
	Messages     callbackPageMessages `json:"messages"`
}

var callbackMessagesByLang = map[string]callbackPageMessages{
	"en": {
		DocumentTitleSuccess: "Volcengine Authentication Successful",
		DocumentTitleFailure: "Volcengine Authentication Failed",
		SuccessTitle:         "Authentication successful",
		FailureTitle:         "Authentication failed",
		SuccessCopy:          "You can close this page and return to\nthe terminal.",
		FailureCopy:          "Please return to the terminal.",
		OAuthErrorLabel:      "OAuth error",
	},
	"zh": {
		DocumentTitleSuccess: "火山引擎认证成功",
		DocumentTitleFailure: "火山引擎认证失败",
		SuccessTitle:         "认证成功",
		FailureTitle:         "认证失败",
		SuccessCopy:          "你可以关闭此页面并返回\n终端继续操作。",
		FailureCopy:          "请返回终端继续操作。",
		OAuthErrorLabel:      "OAuth 错误",
	},
}

var callbackLangAliases = map[string]string{
	"en":         "en",
	"en-us":      "en",
	"en-gb":      "en",
	"zh":         "zh",
	"zh-cn":      "zh",
	"zh-hans":    "zh",
	"zh-hans-cn": "zh",
	"zh-tw":      "zh",
	"zh-hk":      "zh",
	"zh-mo":      "zh",
	"zh-hant":    "zh",
	"zh-hant-tw": "zh",
}

// normalizeCallbackLang 将 URL 参数中的语言码归一成页面支持的规范语言码。
// 当前回调页只支持中文和英文；未知语言统一回退中文，避免页面渲染空文案。
func normalizeCallbackLang(lang string) string {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(lang), "_", "-"))
	if normalized == "" {
		return callbackDefaultLang
	}

	if canonical, ok := callbackLangAliases[normalized]; ok {
		return canonical
	}

	// 仅对中文、英文的地区变体做保守回退，例如 zh-SG -> zh、en-AU -> en。
	if base := strings.Split(normalized, "-")[0]; base != "" {
		if canonical, ok := callbackLangAliases[base]; ok {
			return canonical
		}
	}

	return callbackDefaultLang
}

func callbackMessagesForLang(lang string) callbackPageMessages {
	messages, ok := callbackMessagesByLang[normalizeCallbackLang(lang)]
	if ok {
		return messages
	}
	return callbackMessagesByLang[callbackDefaultLang]
}

func newCallbackPageData(errorMessage, lang string) callbackPageData {
	normalizedLang := normalizeCallbackLang(lang)
	return callbackPageData{
		Lang:         normalizedLang,
		ErrorMessage: errorMessage,
		Messages:     callbackMessagesForLang(normalizedLang),
	}
}
