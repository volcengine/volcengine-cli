package cmd

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestResolveLanguageExplicitFlag(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantArgs []string
		wantLang Language
	}{
		{name: "english", args: []string{"---lang", "EN", "--help"}, wantArgs: []string{"--help"}, wantLang: LanguageEnglish},
		{name: "english locale", args: []string{"sts", "--help", "---lang", "en_US"}, wantArgs: []string{"sts", "--help"}, wantLang: LanguageEnglish},
		{name: "simplified chinese", args: []string{"sts", "---lang", "ZH", "--help"}, wantArgs: []string{"sts", "--help"}, wantLang: LanguageSimplifiedChinese},
		{name: "simplified chinese locale", args: []string{"sts", "GetCallerIdentity", "---lang", "zh-CN", "--help"}, wantArgs: []string{"sts", "GetCallerIdentity", "--help"}, wantLang: LanguageSimplifiedChinese},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotArgs, gotLang, err := resolveLanguage(tt.args, emptyEnvironment)
			if err != nil {
				t.Fatalf("resolveLanguage returned error: %v", err)
			}
			if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Fatalf("args = %#v, want %#v", gotArgs, tt.wantArgs)
			}
			if gotLang != tt.wantLang {
				t.Fatalf("language = %q, want %q", gotLang, tt.wantLang)
			}
		})
	}
}

func TestLocalizedInteractiveCommand(t *testing.T) {
	restoreLanguage := setLanguageForTest(LanguageSimplifiedChinese)
	defer restoreLanguage()

	command := newLoginCmd()
	if command.Short != "通过浏览器登录火山引擎控制台" {
		t.Fatalf("login short = %q", command.Short)
	}
	if usage := command.Flags().Lookup("region").Usage; !strings.Contains(usage, "地域") {
		t.Fatalf("region flag usage was not localized: %q", usage)
	}

	var output bytes.Buffer
	region, err := promptForConsoleLoginRegion(strings.NewReader("\n"), &output, defaultConsoleLoginRegion)
	if err != nil {
		t.Fatalf("promptForConsoleLoginRegion returned error: %v", err)
	}
	if region != defaultConsoleLoginRegion || !strings.Contains(output.String(), "请输入地域") {
		t.Fatalf("localized region prompt = %q, region = %q", output.String(), region)
	}
}

func TestLocalizedWrappedErrorPreservesCause(t *testing.T) {
	restoreLanguage := setLanguageForTest(LanguageSimplifiedChinese)
	defer restoreLanguage()

	cause := errors.New("sentinel")
	err := trErrorf("writing config: %w", cause)
	if !errors.Is(err, cause) {
		t.Fatalf("translated error does not wrap the original cause: %v", err)
	}
	if !strings.Contains(err.Error(), "写入配置失败") {
		t.Fatalf("translated error = %q", err)
	}
}

func TestLocalizedUsageTemplates(t *testing.T) {
	restoreLanguage := setLanguageForTest(LanguageSimplifiedChinese)
	defer restoreLanguage()

	for name, output := range map[string]string{
		"root":      rootUsageTemplate(),
		"service":   serviceUsageTemplate(),
		"action":    actionUsageTemplate("", nil),
		"login":     loginUsageTemplate(),
		"configure": configureUsageTemplate(),
		"sso":       ssoUsageTemplate(),
	} {
		if !strings.Contains(output, "用法：") || !strings.Contains(output, "---lang") {
			t.Fatalf("%s usage template was not localized:\n%s", name, output)
		}
	}
}

func TestResolveLanguageFromEnvironment(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want Language
	}{
		{name: "default english", env: map[string]string{}, want: LanguageEnglish},
		{name: "lang chinese", env: map[string]string{"LANG": "zh_CN.UTF-8"}, want: LanguageSimplifiedChinese},
		{name: "lc messages wins", env: map[string]string{"LANG": "en_US.UTF-8", "LC_MESSAGES": "zh_CN.UTF-8"}, want: LanguageSimplifiedChinese},
		{name: "lc all wins", env: map[string]string{"LANG": "zh_CN.UTF-8", "LC_MESSAGES": "zh_CN.UTF-8", "LC_ALL": "en_US.UTF-8"}, want: LanguageEnglish},
		{name: "unsupported locale falls back to english", env: map[string]string{"LANG": "fr_FR.UTF-8"}, want: LanguageEnglish},
		{name: "traditional chinese is not enabled", env: map[string]string{"LANG": "zh_TW.UTF-8"}, want: LanguageEnglish},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got, err := resolveLanguage(nil, mapEnvironment(tt.env))
			if err != nil {
				t.Fatalf("resolveLanguage returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("language = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveLanguageFallsBackForUnsupportedExplicitLanguage(t *testing.T) {
	gotArgs, gotLang, err := resolveLanguage([]string{"sts", "---lang", "FR", "--help"}, mapEnvironment(map[string]string{"LANG": "zh_CN.UTF-8"}))
	if err != nil {
		t.Fatalf("resolveLanguage returned error: %v", err)
	}
	if !reflect.DeepEqual(gotArgs, []string{"sts", "--help"}) {
		t.Fatalf("args = %#v, want language flag removed", gotArgs)
	}
	if gotLang != LanguageEnglish {
		t.Fatalf("language = %q, want English fallback", gotLang)
	}

	_, gotLang, err = resolveLanguage([]string{"---lang", "zh-TW"}, emptyEnvironment)
	if err != nil {
		t.Fatalf("resolveLanguage returned error: %v", err)
	}
	if gotLang != LanguageEnglish {
		t.Fatalf("language = %q for traditional Chinese, want English fallback", gotLang)
	}
}

func TestResolveLanguagePreservesActionArguments(t *testing.T) {
	args := []string{
		"ecs", "DescribeInstances",
		"--InstanceIds.1", "i-123",
		"---lang", "ZH",
		"---region", "cn-beijing",
	}
	want := []string{
		"ecs", "DescribeInstances",
		"--InstanceIds.1", "i-123",
		"---region", "cn-beijing",
	}

	got, language, err := resolveLanguage(args, emptyEnvironment)
	if err != nil {
		t.Fatalf("resolveLanguage returned error: %v", err)
	}
	if language != LanguageSimplifiedChinese {
		t.Fatalf("language = %q", language)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("action args = %#v, want %#v", got, want)
	}
}

func TestResolveLanguageRejectsMalformedFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing value", args: []string{"---lang"}, want: "requires a value"},
		{name: "duplicate flag", args: []string{"---lang", "EN", "---lang", "ZH"}, want: "specified more than once"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := resolveLanguage(tt.args, emptyEnvironment)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want text %q", err, tt.want)
			}
		})
	}
}

func TestLocalizedExplorerDescriptions(t *testing.T) {
	restoreDescriptions := stubExplorerDescriptionsAsset(`{
  "services": {
    "demo": {"service_cn": "演示服务", "service_en": "Demo Service"}
  },
  "apis": {
    "demo": {
      "DoThing": {
        "name_cn": "执行操作",
        "name_en": "Do thing",
        "description_cn": "执行演示操作。",
        "description_en": "Run the demo operation."
      }
    }
  }
}`)
	defer restoreDescriptions()

	restoreLanguage := setLanguageForTest(LanguageEnglish)
	defer restoreLanguage()
	if got := formatServiceShort("demo"); got != "Demo Service" {
		t.Fatalf("English service description = %q", got)
	}
	if got := formatActionShort("demo", "DoThing"); got != "Do thing" {
		t.Fatalf("English action short = %q", got)
	}
	if got := formatActionLong("demo", "DoThing"); got != "Run the demo operation." {
		t.Fatalf("English action long = %q", got)
	}

	setCurrentLanguage(LanguageSimplifiedChinese)
	if got := formatServiceShort("demo"); got != "演示服务" {
		t.Fatalf("Chinese service description = %q", got)
	}
	if got := formatActionShort("demo", "DoThing"); got != "执行操作" {
		t.Fatalf("Chinese action short = %q", got)
	}
	if got := formatActionLong("demo", "DoThing"); got != "执行演示操作。" {
		t.Fatalf("Chinese action long = %q", got)
	}
}

func emptyEnvironment(string) (string, bool) {
	return "", false
}

func mapEnvironment(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
