package cmd

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestRootHelpIncludesFixedFlags(t *testing.T) {
	registerRootSystemFlags()
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
	out := actionUsageTemplate("获取请求者身份信息 - Get identity information for the request credential", []string{"InstanceId string"}, false)
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

			out := jsonActionUsageTemplate("", []string{"PageSize integer", "Filter.Name string"}, "body '{\n    \"Filter\": {}\n}'", false)
			if !strings.Contains(out, tt.want) {
				t.Fatalf("JSON action usage missing grouped parameters:\n%s", out)
			}
		})
	}
}

func TestNonJSONActionUsageKeepsSingleParameterList(t *testing.T) {
	restoreLanguage := setLanguageForTest(LanguageEnglish)
	defer restoreLanguage()

	out := actionUsageTemplate("", []string{"InstanceId string"}, false)
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

	out := jsonActionUsageTemplate("", nil, "body '{}'", false)
	if strings.Contains(out, "Parameter Form:") {
		t.Fatalf("JSON action usage contains an empty parameter form:\n%s", out)
	}
	if !strings.Contains(out, "JSON Form:\n    --body '{}'") {
		t.Fatalf("JSON action usage missing body form:\n%s", out)
	}
}

func TestJSONActionUsageDetailTip(t *testing.T) {
	restoreLanguage := setLanguageForTest(LanguageEnglish)
	defer restoreLanguage()

	concise := jsonActionUsageTemplate("", []string{"Name string"}, "body '{}'", false)
	if !strings.Contains(concise, "Default help is concise") || !strings.Contains(concise, "-h --detail") {
		t.Fatalf("JSON concise usage missing detail tip:\n%s", concise)
	}
	if !strings.Contains(concise, "Parameter Form:") || !strings.Contains(concise, "JSON Form:") {
		t.Fatalf("JSON usage missing dual forms:\n%s", concise)
	}
	detail := jsonActionUsageTemplate("", []string{"Name string"}, "body '{}'", true)
	if strings.Contains(detail, "Default help is concise") {
		t.Fatalf("JSON detail usage should omit concise tip:\n%s", detail)
	}
}

func TestFormatParamUsageEntriesPreservesDetailContinuationColumns(t *testing.T) {
	// formatParamsHelpUsage embeds absolute continuation indents for a "  --" first-line prefix.
	// formatParamUsageEntries must not re-indent those continuations when indent is "  ".
	params := []param{{
		key: "Name", typeName: "string", required: false,
		description: "first line of description\nsecond line of description",
	}}
	raw := formatParamsHelpUsage(params, true)
	if len(raw) != 1 || !strings.Contains(raw[0], "\n") {
		t.Fatalf("expected multi-line detail entry, got %#v", raw)
	}
	origLines := strings.Split(raw[0], "\n")

	out := formatParamUsageEntries(raw, nonJSONUsageParamIndent)
	outLines := strings.Split(out, "\n")
	if len(outLines) != len(origLines) {
		t.Fatalf("line count changed: got %d want %d\n%s", len(outLines), len(origLines), out)
	}
	if !strings.HasPrefix(outLines[0], nonJSONUsageParamIndent+"--") {
		t.Fatalf("first line should start with %q--, got %q", nonJSONUsageParamIndent, outLines[0])
	}
	for i := 1; i < len(outLines); i++ {
		if outLines[i] != origLines[i] {
			t.Fatalf("continuation %d re-indented:\n got %q\nwant %q", i, outLines[i], origLines[i])
		}
	}

	// Under Parameter Form, section indent is deeper; continuations get a matching pad.
	jsonOut := formatParamUsageEntries(raw, jsonSectionUsageParamIndent)
	jsonLines := strings.Split(jsonOut, "\n")
	if !strings.HasPrefix(jsonLines[0], jsonSectionUsageParamIndent+"--") {
		t.Fatalf("JSON section first line prefix: %q", jsonLines[0])
	}
	extra := len(jsonSectionUsageParamIndent) - len(nonJSONUsageParamIndent)
	pad := strings.Repeat(" ", extra)
	for i := 1; i < len(jsonLines); i++ {
		want := pad + origLines[i]
		if jsonLines[i] != want {
			t.Fatalf("JSON section continuation %d:\n got %q\nwant %q", i, jsonLines[i], want)
		}
	}
}

func TestFormatBodyUsageEntryReindentsPrettyJSON(t *testing.T) {
	body := "body '{\n    \"Filter\": {}\n}'"
	out := formatBodyUsageEntry(body, jsonSectionUsageParamIndent)
	// After re-indent, each line after the first is prefixed with section indent.
	want := "    --body '{\n" + "    " + "    \"Filter\": {}\n" + "    " + "}'"
	if out != want {
		t.Fatalf("body format:\n got %q\nwant %q", out, want)
	}
	// Master dual-form test string still matches via Contains.
	full := jsonActionUsageTemplate("", nil, body, false)
	if !strings.Contains(full, "JSON Form:\n    --body '{") || !strings.Contains(full, "\"Filter\"") {
		t.Fatalf("JSON Form body missing in template:\n%s", full)
	}
}

func TestActionUsageTemplateDetailMultiLineThroughRealFormatter(t *testing.T) {
	params := []param{{
		key: "ZoneId", typeName: "string", required: true,
		description: "availability zone id\nsecond help line",
	}}
	lines := formatParamsHelpUsage(params, true)
	out := actionUsageTemplate("", lines, true)
	if !strings.Contains(out, "  --ZoneId") {
		t.Fatalf("missing param first line:\n%s", out)
	}
	if !strings.Contains(out, "second help line") {
		t.Fatalf("missing continuation text:\n%s", out)
	}
	// Continuations must not gain an extra "  --" prefix.
	if strings.Contains(out, "  --second help line") || strings.Contains(out, "\n  --second") {
		t.Fatalf("continuation incorrectly treated as a new flag:\n%s", out)
	}
}

func TestActionUsageSeparatesAPIParametersFromSystemFlags(t *testing.T) {
	restoreLanguage := setLanguageForTest(LanguageEnglish)
	defer restoreLanguage()

	tests := map[string]string{
		"non-JSON": actionUsageTemplate("", []string{"lang integer"}, false),
		"JSON":     jsonActionUsageTemplate("", []string{"lang integer"}, "body '{}'", false),
	}
	for name, out := range tests {
		t.Run(name, func(t *testing.T) {
			apiIndex := strings.Index(out, "Available Parameters:")
			if apiIndex < 0 {
				apiIndex = strings.Index(out, "API Parameters:")
			}
			systemIndex := strings.Index(out, "System Flags:")
			if apiIndex < 0 || systemIndex < 0 || apiIndex >= systemIndex {
				t.Fatalf("API and system parameters are not in separate ordered sections:\n%s", out)
			}
			if strings.Count(out, "--lang") < 2 {
				t.Fatalf("conflicting --lang should appear in API params and system flags:\n%s", out)
			}
		})
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
	restoreLanguage := setLanguageForTest(LanguageEnglish)
	defer restoreLanguage()

	out := actionUsageTemplate("", []string{"InstanceId string"}, false)
	for _, want := range expectedFixedFlagsForTest() {
		if !strings.Contains(out, want) {
			t.Fatalf("action usage missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "--detail") {
		t.Fatalf("concise action usage missing --detail tip:\n%s", out)
	}
	detailOut := actionUsageTemplate("", []string{"InstanceId string"}, true)
	if strings.Contains(detailOut, "Use -h --detail") {
		t.Fatalf("detail action usage should not include concise tip:\n%s", detailOut)
	}
	enTip := actionUsageTemplate("", []string{"InstanceId string"}, false)
	if !strings.Contains(enTip, "Default help is concise") || !strings.Contains(enTip, "-h --detail") {
		t.Fatalf("concise tip missing default/example form:\n%s", enTip)
	}
}

func TestPublicHelpOmitsLegacySystemFlagAliases(t *testing.T) {
	for name, output := range map[string]string{
		"root":    rootUsageTemplate(),
		"service": serviceUsageTemplate(),
		"action":  actionUsageTemplate("", nil, false),
	} {
		// Derive aliases from systemFlagDefs so new system flags are covered.
		for _, flag := range publicSystemFlagNames() {
			if strings.Contains(output, "---"+flag) {
				t.Fatalf("%s help exposes conflict-escape alias ---%s:\n%s", name, flag, output)
			}
		}
	}
}

func TestPublicDocsOnlyAdvertiseDoubleDashSystemFlags(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}
	repoRoot := filepath.Dir(filepath.Dir(currentFile))
	publicDocs := []string{
		"README.MD",
		"README.EN.MD",
		"docs/1-GettingStarted-zh.md",
		"docs/1-GettingStarted.md",
		"docs/2-Authentication-zh.md",
		"docs/2-Authentication.md",
		"docs/3-Configuration-zh.md",
		"docs/3-Configuration.md",
		"docs/4-Usage-zh.md",
		"docs/4-Usage.md",
		"docs/5-Advanced-zh.md",
		"docs/5-Advanced.md",
	}
	// Public docs advertise the double-dash form only. Derive the forbidden alias
	// list from systemFlagDefs so a newly added system flag is covered
	// automatically; the previous hand-written list silently missed
	// ---output / ---query.
	forbidden := []string{
		"三横线",
		"triple-dash",
	}
	for _, name := range publicSystemFlagNames() {
		forbidden = append(forbidden, "---"+name)
	}

	for _, name := range publicDocs {
		content, err := ioutil.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("read public doc %s: %v", name, err)
		}
		for _, value := range forbidden {
			if strings.Contains(string(content), value) {
				t.Errorf("public doc %s exposes internal compatibility syntax %q", name, value)
			}
		}
	}
}

func expectedFixedFlagsForTest() []string {
	// 与 localizedSystemFlagsHelp / root·service·action usage 保持一致：
	// 对外 system flags 全部双横线；三横线别名不展示；保留控制参数也在帮助中。
	return []string{"--profile", "--region", "--endpoint", "--lang", "--version", "--method", "--force", "--output", "--query", "--header", "--body"}
}
