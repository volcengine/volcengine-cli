package cmd

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestRootHelpIncludesFixedFlags(t *testing.T) {
	cmd := *rootCmd
	cmd.SetUsageTemplate(rootUsageTemplate())
	var b bytes.Buffer
	cmd.SetOut(&b)
	cmd.SetErr(&b)

	if err := cmd.Usage(); err != nil {
		t.Fatalf("Usage returned error: %v", err)
	}
	out := b.String()
	for _, want := range expectedFixedFlagsForTest() {
		if !strings.Contains(out, want) {
			t.Fatalf("root help missing %q:\n%s", want, out)
		}
	}
}

func TestRootUsageIncludesServiceTableHeader(t *testing.T) {
	out := rootUsageTemplate()
	for _, want := range []string{
		"Available Commands:",
		"  Service                 Description",
		"  -------                 -----------",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("root usage missing %q:\n%s", want, out)
		}
	}
}

func TestServiceAndActionDescriptions(t *testing.T) {
	restoreLanguage := setLanguageForTest(LanguageEnglish)
	defer restoreLanguage()

	if got := formatServiceShort("sts"); got != "Security Token Service" {
		t.Fatalf("unexpected sts service description: %q", got)
	}
	if got := formatActionShort("sts", "GetCallerIdentity"); got != "Get identity information for the request credential" {
		t.Fatalf("unexpected sts action description: %q", got)
	}
	if got := formatActionLong("sts", "GetCallerIdentity"); got != "Get identity information for the request credential" {
		t.Fatalf("unexpected sts action long description: %q", got)
	}

	setCurrentLanguage(LanguageSimplifiedChinese)
	if got := formatServiceShort("sts"); got != "安全凭证服务" {
		t.Fatalf("unexpected Chinese sts service description: %q", got)
	}
	if got := formatActionShort("sts", "GetCallerIdentity"); got != "获取请求者身份信息" {
		t.Fatalf("unexpected Chinese sts action description: %q", got)
	}
}

func TestExplorerDescriptionsLoadFromAssetJSON(t *testing.T) {
	restore := stubExplorerDescriptionsAsset(`{
  "services": {
    "demo": {
      "service_cn": "演示服务",
      "service_en": "Demo Service"
    }
  },
  "apis": {
    "demo": {
      "DoThing": {
        "name_cn": "执行操作",
        "name_en": "Do thing",
        "description_cn": "执行演示操作。",
        "description_en": "Run the demo operation."
      },
      "DoOtherThing": {
        "name_cn": "执行其他操作",
        "name_en": "Do other thing"
      }
    }
  }
}`)
	defer restore()

	restoreLanguage := setLanguageForTest(LanguageEnglish)
	defer restoreLanguage()

	if got := formatServiceShort("demo"); got != "Demo Service" {
		t.Fatalf("unexpected service description: %q", got)
	}
	if got := formatActionShort("demo", "DoThing"); got != "Do thing" {
		t.Fatalf("unexpected action short: %q", got)
	}
	if got := formatActionLong("demo", "DoThing"); got != "Run the demo operation." {
		t.Fatalf("unexpected action long: %q", got)
	}
	if got := formatActionLong("demo", "DoOtherThing"); got != "Do other thing" {
		t.Fatalf("unexpected action long with name_en fallback: %q", got)
	}

	setCurrentLanguage(LanguageSimplifiedChinese)
	if got := formatServiceShort("demo"); got != "演示服务" {
		t.Fatalf("unexpected Chinese service description: %q", got)
	}
	if got := formatActionLong("demo", "DoThing"); got != "执行演示操作。" {
		t.Fatalf("unexpected Chinese action long: %q", got)
	}
}

func TestActionUsageIncludesLongDescription(t *testing.T) {
	restoreLanguage := setLanguageForTest(LanguageSimplifiedChinese)
	defer restoreLanguage()
	out := actionUsageTemplate("获取请求者身份信息 - Get identity information for the request credential", []string{"InstanceId string"})
	if !strings.Contains(out, "获取请求者身份信息 - Get identity information for the request credential") {
		t.Fatalf("action usage missing long description:\n%s", out)
	}
}

func TestJSONActionUsageSeparatesParameterForms(t *testing.T) {
	tests := []struct {
		name     string
		language Language
		want     string
	}{
		{
			name:     "English",
			language: LanguageEnglish,
			want: "Available Parameters:\n\n" +
				"  Parameter Form:\n" +
				"    --Filter.Name string\n" +
				"    --PageSize integer\n\n" +
				"  JSON Form:\n" +
				"    --body '{\n" +
				"        \"Filter\": {}\n" +
				"    }'",
		},
		{
			name:     "Simplified Chinese",
			language: LanguageSimplifiedChinese,
			want: "可用参数：\n\n" +
				"  参数方式：\n" +
				"    --Filter.Name string\n" +
				"    --PageSize integer\n\n" +
				"  JSON 方式：\n" +
				"    --body '{\n" +
				"        \"Filter\": {}\n" +
				"    }'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreLanguage := setLanguageForTest(tt.language)
			defer restoreLanguage()

			out := jsonActionUsageTemplate("", []string{"PageSize integer", "Filter.Name string"}, "body '{\n    \"Filter\": {}\n}'")
			if !strings.Contains(out, tt.want) {
				t.Fatalf("JSON action usage missing grouped parameters:\n%s", out)
			}
		})
	}
}

func TestNonJSONActionUsageKeepsSingleParameterList(t *testing.T) {
	restoreLanguage := setLanguageForTest(LanguageEnglish)
	defer restoreLanguage()

	out := actionUsageTemplate("", []string{"InstanceId string"})
	if !strings.Contains(out, "Available Parameters:\n  --InstanceId string") {
		t.Fatalf("non-JSON action usage changed unexpectedly:\n%s", out)
	}
	for _, unwanted := range []string{"Parameter Form:", "JSON Form:"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("non-JSON action usage unexpectedly contains %q:\n%s", unwanted, out)
		}
	}
}

func TestJSONActionUsageOmitsEmptyParameterForm(t *testing.T) {
	restoreLanguage := setLanguageForTest(LanguageEnglish)
	defer restoreLanguage()

	out := jsonActionUsageTemplate("", nil, "body '{}'")
	if strings.Contains(out, "Parameter Form:") {
		t.Fatalf("JSON action usage contains an empty parameter form:\n%s", out)
	}
	if !strings.Contains(out, "JSON Form:\n    --body '{}'") {
		t.Fatalf("JSON action usage missing body form:\n%s", out)
	}
}

func TestServiceUsageIncludesActionTableHeader(t *testing.T) {
	out := serviceUsageTemplate()
	for _, want := range []string{
		"Available Actions:",
		"  Action                  Description",
		"  ------                  -----------",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("service usage missing %q:\n%s", want, out)
		}
	}
}

func stubExplorerDescriptionsAsset(data string) func() {
	oldDescriptions := explorerDescriptions
	oldLoad := loadExplorerDescriptionsAsset

	explorerDescriptionsOnce = sync.Once{}
	explorerDescriptions = explorerDescriptionsData{}
	loadExplorerDescriptionsAsset = func() ([]byte, error) {
		if data == "" {
			return nil, fmt.Errorf("not found")
		}
		return []byte(data), nil
	}

	return func() {
		loadExplorerDescriptionsAsset = oldLoad
		explorerDescriptions = oldDescriptions
		explorerDescriptionsOnce = sync.Once{}
	}
}

func TestServiceUsageIncludesFixedFlags(t *testing.T) {
	out := serviceUsageTemplate()
	for _, want := range expectedFixedFlagsForTest() {
		if !strings.Contains(out, want) {
			t.Fatalf("service usage missing %q:\n%s", want, out)
		}
	}
}

func TestActionUsageIncludesFixedFlags(t *testing.T) {
	out := actionUsageTemplate("", []string{"InstanceId string"})
	for _, want := range expectedFixedFlagsForTest() {
		if !strings.Contains(out, want) {
			t.Fatalf("action usage missing %q:\n%s", want, out)
		}
	}
}

func expectedFixedFlagsForTest() []string {
	return []string{"---profile", "---region", "---endpoint", "---lang"}
}
