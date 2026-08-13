package cmd

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

func stubParamDescriptionsAsset(jsonText string) func() {
	oldLoad := loadParamDescriptionsAsset

	paramDescriptionsOnce = sync.Once{}
	paramDescriptions = paramDescriptionsData{}
	loadParamDescriptionsAsset = func() ([]byte, error) {
		return []byte(jsonText), nil
	}

	return func() {
		paramDescriptionsOnce = sync.Once{}
		paramDescriptions = paramDescriptionsData{}
		loadParamDescriptionsAsset = oldLoad
	}
}

func TestNormalizeParamDescKey(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"SubnetId", "SubnetId"},
		{"NetworkInterfaces.SubnetId", "NetworkInterfaces.SubnetId"},
		{"NetworkInterfaces.1.SubnetId", "NetworkInterfaces.SubnetId"},
		{"Tags.1.Key", "Tags.Key"},
		{"Tags.N.Key", "Tags.Key"},
		{"A.10.B.2.C", "A.B.C"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeParamDescKey(c.in); got != c.want {
			t.Fatalf("normalizeParamDescKey(%q)=%q, want %q", c.in, got, c.want)
		}
	}
	if got := normalizeParamDescKeyKeepN("Tags.1.Key"); got != "Tags.N.Key" {
		t.Fatalf("normalizeParamDescKeyKeepN: got %q", got)
	}
}

func TestLookupParamDescriptionLanguageAndFallback(t *testing.T) {
	restore := stubParamDescriptionsAsset(`{
  "apis": {
    "ecs": {
      "2020-04-01": {
        "RunInstances": {
          "ZoneId": {
            "description_cn": "可用区ID。\n第二行忽略",
            "description_en": "Availability zone ID."
          },
          "InstanceName": {
            "description_cn": "实例名称"
          }
        }
      }
    }
  }
}`)
	defer restore()

	restoreLang := setLanguageForTest(LanguageEnglish)
	defer restoreLang()

	if got := lookupParamDescription("ecs", "2020-04-01", "RunInstances", "ZoneId"); got != "Availability zone ID." {
		t.Fatalf("EN ZoneId: got %q", got)
	}
	// EN missing → empty (no cross-language fallback).
	if got := lookupParamDescription("ecs", "2020-04-01", "RunInstances", "InstanceName"); got != "" {
		t.Fatalf("EN without description_en should be empty, got %q", got)
	}

	setCurrentLanguage(LanguageSimplifiedChinese)
	// Full multi-line text is returned (no first-line truncation).
	if got := lookupParamDescription("ecs", "2020-04-01", "RunInstances", "ZoneId"); got != "可用区ID。\n第二行忽略" {
		t.Fatalf("ZH ZoneId full text: got %q", got)
	}
	// ZH still works when only CN is present.
	if got := lookupParamDescription("ecs", "2020-04-01", "RunInstances", "InstanceName"); got != "实例名称" {
		t.Fatalf("ZH InstanceName: got %q", got)
	}
}

func TestLookupParamDescriptionNoCrossLanguageFallback(t *testing.T) {
	restore := stubParamDescriptionsAsset(`{
  "apis": {
    "ecs": {
      "2020-04-01": {
        "RunInstances": {
          "OnlyEn": { "description_en": "english only", "example_en": "en-ex" },
          "OnlyCn": { "description_cn": "仅中文", "example_cn": "cn-ex" }
        }
      }
    }
  }
}`)
	defer restore()

	restoreLang := setLanguageForTest(LanguageEnglish)
	defer restoreLang()
	if got := lookupParamDescription("ecs", "2020-04-01", "RunInstances", "OnlyCn"); got != "" {
		t.Fatalf("EN must not fall back to CN, got %q", got)
	}
	if got := lookupParamDescription("ecs", "2020-04-01", "RunInstances", "OnlyEn"); got != "english only" {
		t.Fatalf("EN OnlyEn: got %q", got)
	}
	info, ok := lookupParamDescriptionInfo("ecs", "2020-04-01", "RunInstances", "OnlyCn")
	if !ok || info.example != "" {
		t.Fatalf("EN must not fall back to CN example: ok=%v example=%q", ok, info.example)
	}

	setCurrentLanguage(LanguageSimplifiedChinese)
	if got := lookupParamDescription("ecs", "2020-04-01", "RunInstances", "OnlyEn"); got != "" {
		t.Fatalf("ZH must not fall back to EN, got %q", got)
	}
	if got := lookupParamDescription("ecs", "2020-04-01", "RunInstances", "OnlyCn"); got != "仅中文" {
		t.Fatalf("ZH OnlyCn: got %q", got)
	}
	info, ok = lookupParamDescriptionInfo("ecs", "2020-04-01", "RunInstances", "OnlyCn")
	if !ok || info.example != "cn-ex" {
		t.Fatalf("ZH OnlyCn example: ok=%v example=%q", ok, info.example)
	}
}

func TestLookupParamDescriptionIndexedKeyAndExactVersionOnly(t *testing.T) {
	restore := stubParamDescriptionsAsset(`{
  "apis": {
    "ecs": {
      "2020-04-01": {
        "RunInstances": {
          "NetworkInterfaces.SubnetId": {
            "description_en": "Subnet of the ENI."
          },
          "Tags.N.Key": {
            "description_en": "Tag key via N placeholder."
          }
        }
      },
      "2019-01-01": {
        "RunInstances": {
          "ImageId": {
            "description_en": "old image"
          },
          "NetworkInterfaces.SubnetId": {
            "description_en": "old subnet text must not leak"
          }
        }
      }
    }
  }
}`)
	defer restore()
	restoreLang := setLanguageForTest(LanguageEnglish)
	defer restoreLang()

	if got := lookupParamDescription("ecs", "2020-04-01", "RunInstances", "NetworkInterfaces.1.SubnetId"); got != "Subnet of the ENI." {
		t.Fatalf("indexed key: got %q", got)
	}
	// MetaTypes-style .N key in help should match asset Tags.N.Key first, or strip to Tags.Key.
	if got := lookupParamDescription("ecs", "2020-04-01", "RunInstances", "Tags.1.Key"); got != "Tag key via N placeholder." {
		t.Fatalf("N placeholder match: got %q", got)
	}
	// preferred version has action but not ImageId; must not pick another version's param text.
	if got := lookupParamDescription("ecs", "2020-04-01", "RunInstances", "ImageId"); got != "" {
		t.Fatalf("expected empty on preferred version miss, got %q", got)
	}
	// preferred version missing entirely → no cross-version fallback (S17).
	if got := lookupParamDescription("ecs", "2099-01-01", "RunInstances", "NetworkInterfaces.SubnetId"); got != "" {
		t.Fatalf("must not fall back to another API version, got %q", got)
	}
	// Exact older version still resolves only that version's text.
	if got := lookupParamDescription("ecs", "2019-01-01", "RunInstances", "ImageId"); got != "old image" {
		t.Fatalf("exact older version: got %q", got)
	}
	if got := lookupParamDescription("ecs", "2019-01-01", "RunInstances", "NetworkInterfaces.SubnetId"); got != "old subnet text must not leak" {
		t.Fatalf("exact older version subnet: got %q", got)
	}
}

func TestParamVersionCandidatesExactOnly(t *testing.T) {
	verMap := map[string]map[string]map[string]paramDescription{
		"2020-04-01": {
			"RunInstances": {"ZoneId": {DescriptionEn: "z"}},
		},
		"2019-01-01": {
			"RunInstances": {"ZoneId": {DescriptionEn: "old"}},
		},
	}
	if got := paramVersionCandidates(verMap, "2020-04-01", "RunInstances"); len(got) != 1 || got[0] != "2020-04-01" {
		t.Fatalf("exact hit: got %v", got)
	}
	if got := paramVersionCandidates(verMap, "2099-01-01", "RunInstances"); len(got) != 0 {
		t.Fatalf("missing preferred version must not fall back: got %v", got)
	}
	if got := paramVersionCandidates(verMap, "", "RunInstances"); len(got) != 0 {
		t.Fatalf("empty preferred must not pick a version: got %v", got)
	}
	if got := paramVersionCandidates(verMap, "2020-04-01", "NoSuchAction"); len(got) != 0 {
		t.Fatalf("missing action on preferred: got %v", got)
	}
}

func TestAttachAndFormatParamsHelpUsage(t *testing.T) {
	restore := stubParamDescriptionsAsset(`{
  "apis": {
    "ecs": {
      "2020-04-01": {
        "RunInstances": {
          "ZoneId": { "description_en": "Zone id help" },
          "Count": { "description_en": "Count help" }
        }
      }
    }
  }
}`)
	defer restore()
	restoreLang := setLanguageForTest(LanguageEnglish)
	defer restoreLang()

	// Isolate version map without rebuilding full rootSupport assets.
	oldRoot := rootSupport
	rootSupport = &RootSupport{Versions: map[string]string{"ecs": "2020-04-01"}}
	defer func() { rootSupport = oldRoot }()

	params := []param{
		{key: "Count", typeName: "integer"},
		{key: "ZoneId", typeName: "string"},
		{key: "Unknown", typeName: "string"},
	}
	attachParamDescriptions("ecs", "RunInstances", params)
	lines := formatParamsHelpUsage(params, true)
	// Described params get a blank separator between entries.
	var content []string
	for _, line := range lines {
		if line != "" {
			content = append(content, line)
		}
	}
	if len(content) != 3 {
		t.Fatalf("want 3 content lines, got %d: %v", len(content), lines)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Zone id help") || !strings.Contains(joined, "Count help") {
		t.Fatalf("missing descriptions:\n%s", joined)
	}
	if !strings.Contains(joined, "Unknown") {
		t.Fatalf("missing Unknown param:\n%s", joined)
	}
	// Unknown should not invent a description beyond key/type
	for _, line := range lines {
		if strings.Contains(line, "Unknown") && strings.Contains(line, "help") {
			t.Fatalf("unexpected help text on Unknown: %q", line)
		}
	}
}

func TestFormatParamsHelpUsageWithoutDescriptionUnchangedStyle(t *testing.T) {
	restoreLang := setLanguageForTest(LanguageEnglish)
	defer restoreLang()
	params := []param{
		{key: "Id", typeName: "string"},
		{key: "Name", typeName: "string"},
	}
	lines := formatParamsHelpUsage(params, true)
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %v", lines)
	}
	for _, line := range lines {
		// key + type + Required/Optional (no free-form description)
		trimmed := strings.TrimRight(line, " ")
		parts := strings.Fields(trimmed)
		if len(parts) != 3 {
			t.Fatalf("expected key+type+required, got %q", line)
		}
		if parts[2] != "Optional" && parts[2] != "Required" {
			t.Fatalf("expected Required/Optional column, got %q", line)
		}
	}
}

func TestAttachParamDescriptionsKeepsSDKRequired(t *testing.T) {
	restore := stubParamDescriptionsAsset(`{
  "apis": {
    "vpc": {
      "2020-04-01": {
        "CreateVpc": {
          "CidrBlock": {
            "description_cn": "VPC网段",
            "description_en": "CIDR",
            "example_cn": "172.16.0.0/12",
            "example_en": "172.16.0.0/12",
            "required": true
          },
          "DryRun": {
            "description_cn": "预检",
            "example_cn": "false",
            "required": false
          },
          "ZoneId": {
            "description_cn": "可用区",
            "example_cn": "cn-beijing-a"
          }
        }
      }
    }
  }
}`)
	defer restore()
	oldRoot := rootSupport
	rootSupport = &RootSupport{Versions: map[string]string{"vpc": "2020-04-01"}}
	defer func() { rootSupport = oldRoot }()
	restoreLang := setLanguageForTest(LanguageSimplifiedChinese)
	defer restoreLang()

	params := []param{
		{key: "CidrBlock", typeName: "string", required: false},
		{key: "DryRun", typeName: "boolean", required: true},
		{key: "Unknown", typeName: "string", required: true},
		{key: "ZoneId", typeName: "string", required: true},
	}
	attachParamDescriptions("vpc", "CreateVpc", params)
	if params[0].required || params[0].description != "VPC网段" || params[0].example != "172.16.0.0/12" {
		t.Fatalf("CidrBlock: required=%v desc=%q example=%q", params[0].required, params[0].description, params[0].example)
	}
	if !params[1].required || params[1].description != "预检" || params[1].example != "false" {
		t.Fatalf("DryRun: required=%v desc=%q example=%q", params[1].required, params[1].description, params[1].example)
	}
	if !params[2].required || params[2].description != "" || params[2].example != "" {
		t.Fatalf("Unknown should keep metadata required and empty desc/example: %+v", params[2])
	}
	if !params[3].required || params[3].description != "可用区" || params[3].example != "cn-beijing-a" {
		t.Fatalf("ZoneId must keep SDK required: %+v", params[3])
	}

	lines := formatParamsHelpUsage(params, true)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "必选") || !strings.Contains(joined, "可选") {
		t.Fatalf("want localized required labels in:\n%s", joined)
	}
	if !strings.Contains(joined, "CidrBlock") || !strings.Contains(joined, "VPC网段") {
		t.Fatalf("missing CidrBlock help:\n%s", joined)
	}
	if !strings.Contains(joined, "示例：") || !strings.Contains(joined, "172.16.0.0/12") {
		t.Fatalf("missing example line:\n%s", joined)
	}
	// Layout: description lines then "示例：value" on its own line (no blank gap).
	cidrs := ""
	for _, line := range lines {
		if strings.Contains(line, "CidrBlock") {
			cidrs = line
			break
		}
	}
	parts := strings.Split(cidrs, "\n")
	foundEx := -1
	for i, part := range parts {
		if strings.Contains(part, "示例：") && strings.Contains(part, "172.16.0.0/12") {
			foundEx = i
			break
		}
	}
	if foundEx < 1 {
		t.Fatalf("example should be after description on one line:\n%s", cidrs)
	}
	if strings.TrimSpace(parts[foundEx-1]) == "" {
		t.Fatalf("did not want blank line immediately before example:\n%s", cidrs)
	}
}

func TestFormatParamsHelpUsageEscapesTemplateBraces(t *testing.T) {
	params := []param{
		{key: "X", typeName: "string", description: "use {{value}} carefully"},
	}
	lines := formatParamsHelpUsage(params, true)
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %v", lines)
	}
	if strings.Contains(lines[0], "{{value}}") {
		t.Fatalf("raw template braces leaked into usage line: %q", lines[0])
	}
	if !strings.Contains(lines[0], `{{"{{"}}`) {
		t.Fatalf("expected escaped braces in %q", lines[0])
	}

	cmd := &cobra.Command{Use: "test"}
	cmd.SetUsageTemplate(actionUsageTemplate("", lines, true))
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	if err := cmd.Usage(); err != nil {
		t.Fatalf("escaped usage template must parse: %v", err)
	}
	if !strings.Contains(output.String(), "use {{value}} carefully") {
		t.Fatalf("literal braces not preserved in rendered help:\n%s", output.String())
	}
}

func TestFormatParamsHelpUsageConciseOmitsDescriptionAndExample(t *testing.T) {
	restoreLang := setLanguageForTest(LanguageEnglish)
	defer restoreLang()
	params := []param{
		{
			key:         "ZoneId",
			typeName:    "string",
			required:    true,
			description: "Availability zone ID.\nSecond line should not appear.",
			example:     "cn-beijing-a",
		},
		{key: "Count", typeName: "integer", required: false, description: "Count help"},
	}
	lines := formatParamsHelpUsage(params, false)
	if len(lines) != 2 {
		t.Fatalf("want 2 skeleton lines, got %d: %#v", len(lines), lines)
	}
	joined := strings.Join(lines, "\n")
	for _, forbidden := range []string{"Availability", "Second line", "Count help", "cn-beijing-a", "Example:"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("concise help must omit descriptions/examples, found %q in:\n%s", forbidden, joined)
		}
	}
	if !strings.Contains(joined, "ZoneId") || !strings.Contains(joined, "Required") {
		t.Fatalf("concise help missing skeleton columns:\n%s", joined)
	}
	if !strings.Contains(joined, "Count") || !strings.Contains(joined, "Optional") {
		t.Fatalf("concise help missing Count Optional:\n%s", joined)
	}
}

func TestFormatParamsHelpUsageMultiLineDescription(t *testing.T) {
	restoreLang := setLanguageForTest(LanguageSimplifiedChinese)
	defer restoreLang()
	params := []param{
		{
			key:         "DryRun",
			typeName:    "boolean",
			required:    false,
			description: "是否只预检此次请求。取值：\n\n* true：仅检查\n* false：正常请求",
		},
		{key: "Name", typeName: "string", required: true, description: "单行"},
	}
	lines := formatParamsHelpUsage(params, true)
	// Described params: trailing "\n" on previous entry so Join("\n") yields a blank
	// separator without inserting an empty "" entry (which would become bare "--").
	if len(lines) != 2 {
		t.Fatalf("want 2 entries (separator via trailing newline), got %d: %#v", len(lines), lines)
	}
	if !strings.HasSuffix(lines[0], "\n") {
		t.Fatalf("want trailing blank separator on first entry: %#v", lines[0])
	}
	// One entry may contain embedded newlines for multi-line desc.
	if !strings.Contains(lines[0], "是否只预检此次请求。取值：") {
		t.Fatalf("missing first desc line:\n%s", lines[0])
	}
	if !strings.Contains(lines[0], "* true：仅检查") || !strings.Contains(lines[0], "* false：正常请求") {
		t.Fatalf("multi-line body truncated:\n%s", lines[0])
	}
	// After actionUsageTemplate prefixes "  --", continuation lines stay aligned.
	// formatParamsHelpUsage itself embeds full indent for continuations.
	parts := strings.Split(strings.TrimRight(lines[0], "\n"), "\n")
	if len(parts) < 3 {
		t.Fatalf("expected multi-line entry, got %d parts: %#v", len(parts), parts)
	}
	// Continuation must start with spaces (indent under description column).
	// After compacting blank lines, second line is first bullet.
	if strings.TrimLeft(parts[1], " ") == parts[1] {
		t.Fatalf("continuation line not indented: %q", parts[1])
	}
	if !strings.Contains(lines[1], "单行") {
		t.Fatalf("single-line entry broken: %q", lines[1])
	}
	if !strings.Contains(lines[0], "可选") || !strings.Contains(lines[1], "必选") {
		t.Fatalf("required column missing:\n%s\n%s", lines[0], lines[1])
	}
}

func TestLookupParamDescriptionMissingAsset(t *testing.T) {
	oldLoad := loadParamDescriptionsAsset
	paramDescriptionsOnce = sync.Once{}
	paramDescriptions = paramDescriptionsData{}
	loadParamDescriptionsAsset = func() ([]byte, error) {
		return nil, errParamAssetMissing
	}
	defer func() {
		paramDescriptionsOnce = sync.Once{}
		paramDescriptions = paramDescriptionsData{}
		loadParamDescriptionsAsset = oldLoad
	}()

	if got := lookupParamDescription("ecs", "2020-04-01", "RunInstances", "ZoneId"); got != "" {
		t.Fatalf("expected empty on missing asset, got %q", got)
	}
}

// errParamAssetMissing is a sentinel for tests only.
type paramAssetMissingError struct{}

func (paramAssetMissingError) Error() string { return "param asset missing" }

var errParamAssetMissing = paramAssetMissingError{}

func TestFormatParamsHelpUsageCJKColumnAlignment(t *testing.T) {
	restoreLang := setLanguageForTest(LanguageSimplifiedChinese)
	defer restoreLang()
	params := []param{
		{key: "A", typeName: "string", required: true, description: "短"},
		{key: "LongerName", typeName: "boolean", required: false, description: "较长参数名"},
	}
	lines := formatParamsHelpUsage(params, true)
	var content []string
	for _, line := range lines {
		if line == "" {
			continue
		}
		content = append(content, strings.Split(line, "\n")[0])
	}
	if len(content) != 2 {
		t.Fatalf("want 2 content headers, got %#v (all=%#v)", content, lines)
	}
	idx0 := strings.Index(content[0], "短")
	idx1 := strings.Index(content[1], "较长参数名")
	if idx0 < 0 || idx1 < 0 {
		t.Fatalf("missing descriptions:\n%s\n%s", content[0], content[1])
	}
	if displayWidth(content[0][:idx0]) != displayWidth(content[1][:idx1]) {
		t.Fatalf("description columns misaligned:\n%q (w=%d)\n%q (w=%d)",
			content[0], displayWidth(content[0][:idx0]), content[1], displayWidth(content[1][:idx1]))
	}
	if !strings.Contains(content[0], "必选") || !strings.Contains(content[1], "可选") {
		t.Fatalf("missing required labels:\n%s\n%s", content[0], content[1])
	}
}

func TestFormatParamsHelpUsageBlankSeparatorBetweenDescribedParams(t *testing.T) {
	restoreLang := setLanguageForTest(LanguageEnglish)
	defer restoreLang()
	params := []param{
		{key: "A", typeName: "string", description: "line1\nline2"},
		{key: "B", typeName: "string", description: "only"},
	}
	lines := formatParamsHelpUsage(params, true)
	if len(lines) != 2 {
		t.Fatalf("want 2 entries with trailing-newline separator, got %#v", lines)
	}
	if !strings.HasSuffix(lines[0], "\n") {
		t.Fatalf("want trailing blank separator on A: %#v", lines[0])
	}
	if !strings.Contains(lines[0], "line1") || !strings.Contains(lines[0], "line2") {
		t.Fatalf("A body broken: %q", lines[0])
	}
	if !strings.Contains(lines[1], "only") {
		t.Fatalf("B body broken: %q", lines[1])
	}
	// Simulate actionUsageTemplate join: trailing \n + join \n => blank line, no bare "--".
	joined := strings.Join([]string{"  --" + lines[0], "  --" + lines[1]}, "\n")
	if strings.Contains(joined, "  --\n") && !strings.Contains(joined, "  --A") {
		// only flag empty prefix as failure if we somehow still produce bare --
	}
	if strings.Contains(joined, "\n  --\n") || strings.HasSuffix(joined, "\n  --") {
		t.Fatalf("bare -- separator leaked into joined help:\n%s", joined)
	}
}

func TestFormatParamsHelpUsageCompactsInternalBlankLines(t *testing.T) {
	params := []param{
		{
			key:         "X",
			typeName:    "string",
			description: "head\n\n* a\n\n* b",
			example:     "val",
		},
	}
	restoreLang := setLanguageForTest(LanguageEnglish)
	defer restoreLang()
	lines := formatParamsHelpUsage(params, true)
	if len(lines) != 1 {
		t.Fatalf("want 1 entry, got %#v", lines)
	}
	parts := strings.Split(lines[0], "\n")
	for i, part := range parts {
		if strings.TrimSpace(part) == "" {
			t.Fatalf("internal blank line not compacted at %d: %#v", i, parts)
		}
	}
	joined := strings.Join(parts, "\n")
	if !strings.Contains(joined, "Example: val") {
		t.Fatalf("missing example: %q", joined)
	}
	exIdx := -1
	for i, part := range parts {
		if strings.Contains(part, "Example: val") {
			exIdx = i
			break
		}
	}
	if exIdx < 1 {
		t.Fatalf("example missing: %#v", parts)
	}
	if !strings.Contains(parts[exIdx-1], "* b") {
		t.Fatalf("example should immediately follow last desc line: %#v", parts)
	}
}

func TestDisplayWidthCJK(t *testing.T) {
	if displayWidth("ab") != 2 {
		t.Fatalf("ascii width")
	}
	if displayWidth("可选") != 4 {
		t.Fatalf("cjk width want 4 got %d", displayWidth("可选"))
	}
	if displayWidth("Optional") != 8 {
		t.Fatalf("english Optional width")
	}
	if got := padRightDisplay("可选", 8); displayWidth(got) != 8 {
		t.Fatalf("padRightDisplay width=%d value=%q", displayWidth(got), got)
	}
}

func TestWrapHelpDisplayLineLongEnglish(t *testing.T) {
	long := "Client token, used to ensure request idempotency. The client automatically generates a parameter value to ensure it is unique across different requests, preventing duplicate operations when the API call times out or a server error occurs and the client retries multiple times. Only ASCII characters are supported, and the value cannot exceed 64 characters. If ClientToken is not provided, idempotency validation will not be performed for this API call."
	lines := wrapHelpDisplayLine(long, 72)
	if len(lines) < 3 {
		t.Fatalf("expected multi-line wrap, got %d: %#v", len(lines), lines)
	}
	for i, line := range lines {
		if displayWidth(line) > 72 {
			t.Fatalf("line %d wider than 72 cols (w=%d): %q", i, displayWidth(line), line)
		}
		if strings.TrimSpace(line) == "" {
			t.Fatalf("unexpected blank wrap line at %d: %#v", i, lines)
		}
	}
	// Words should not be glued; first line should end near a word boundary.
	if !strings.Contains(lines[0], "idempotency") && !strings.Contains(lines[0], "Client token") {
		t.Fatalf("unexpected first wrap line: %q", lines[0])
	}
	joined := strings.Join(lines, " ")
	// All original words still present after wrap (spaces may change).
	for _, word := range []string{"idempotency", "ASCII", "ClientToken", "64"} {
		if !strings.Contains(joined, word) {
			t.Fatalf("missing word %q after wrap: %q", word, joined)
		}
	}
}

func TestFormatParamsHelpUsageWrapsLongEnglishDescription(t *testing.T) {
	restoreLang := setLanguageForTest(LanguageEnglish)
	defer restoreLang()
	long := "Client token, used to ensure request idempotency. The client automatically generates a parameter value to ensure it is unique across different requests, preventing duplicate operations when the API call times out or a server error occurs and the client retries multiple times. Only ASCII characters are supported, and the value cannot exceed 64 characters."
	params := []param{
		{key: "ClientToken", typeName: "string", required: false, description: long, example: "123e4567-e89b-12d3-a456-42665544****"},
		{key: "Name", typeName: "string", required: false, description: "short"},
	}
	lines := formatParamsHelpUsage(params, true)
	if len(lines) != 2 {
		t.Fatalf("want 2 entries, got %#v", lines)
	}
	parts := strings.Split(strings.TrimRight(lines[0], "\n"), "\n")
	if len(parts) < 3 {
		t.Fatalf("long EN description should soft-wrap, got %#v", parts)
	}
	// Continuations are indented under description column (spaces), not column 0 text.
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		if strings.TrimLeft(parts[i], " ") == parts[i] {
			t.Fatalf("continuation not indented at %d: %q", i, parts[i])
		}
	}
	if !strings.Contains(lines[0], "Example: 123e4567-e89b-12d3-a456-42665544****") {
		t.Fatalf("missing example after wrapped body:\n%s", lines[0])
	}
}

func TestWrapHelpDisplayLinePreservesBulletPrefix(t *testing.T) {
	line := "* " + strings.Repeat("word ", 40)
	wrapped := wrapHelpDisplayLine(strings.TrimSpace(line), 40)
	if len(wrapped) < 2 {
		t.Fatalf("expected wrap, got %#v", wrapped)
	}
	for i, w := range wrapped {
		if !strings.HasPrefix(w, "* ") {
			t.Fatalf("line %d lost bullet prefix: %q", i, w)
		}
	}
}
