package cmd

import (
	"strings"
	"sync"
	"testing"
)

func stubParamDescriptionsAsset(jsonText string) func() {
	oldOnce := paramDescriptionsOnce
	oldData := paramDescriptions
	oldLoad := loadParamDescriptionsAsset

	paramDescriptionsOnce = sync.Once{}
	paramDescriptions = paramDescriptionsData{}
	loadParamDescriptionsAsset = func() ([]byte, error) {
		return []byte(jsonText), nil
	}

	return func() {
		paramDescriptionsOnce = oldOnce
		paramDescriptions = oldData
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
	// EN missing → fall back to CN
	if got := lookupParamDescription("ecs", "2020-04-01", "RunInstances", "InstanceName"); got != "实例名称" {
		t.Fatalf("EN fallback InstanceName: got %q", got)
	}

	setCurrentLanguage(LanguageSimplifiedChinese)
	if got := lookupParamDescription("ecs", "2020-04-01", "RunInstances", "ZoneId"); got != "可用区ID。" {
		t.Fatalf("ZH ZoneId first line: got %q", got)
	}
}

func TestLookupParamDescriptionIndexedKeyAndVersionFallback(t *testing.T) {
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
	// preferred version has action but not ImageId; should not wrongly pick old version for missing param on preferred
	if got := lookupParamDescription("ecs", "2020-04-01", "RunInstances", "ImageId"); got != "" {
		t.Fatalf("expected empty on preferred version miss, got %q", got)
	}
	// preferred version missing entirely → fall back to version that has action
	if got := lookupParamDescription("ecs", "2099-01-01", "RunInstances", "NetworkInterfaces.SubnetId"); got != "Subnet of the ENI." {
		t.Fatalf("version fallback: got %q", got)
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
	lines := formatParamsHelpUsage(params)
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d: %v", len(lines), lines)
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
	params := []param{
		{key: "Id", typeName: "string"},
		{key: "Name", typeName: "string"},
	}
	lines := formatParamsHelpUsage(params)
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %v", lines)
	}
	for _, line := range lines {
		// key + type only (padding may add trailing spaces before end)
		trimmed := strings.TrimRight(line, " ")
		parts := strings.Fields(trimmed)
		if len(parts) != 2 {
			t.Fatalf("expected key+type only, got %q", line)
		}
	}
}

func TestFormatParamsHelpUsageEscapesTemplateBraces(t *testing.T) {
	params := []param{
		{key: "X", typeName: "string", description: "use {{value}} carefully"},
	}
	lines := formatParamsHelpUsage(params)
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %v", lines)
	}
	if strings.Contains(lines[0], "{{value}}") {
		t.Fatalf("raw template braces leaked into usage line: %q", lines[0])
	}
	if !strings.Contains(lines[0], `{{"{{"}}`) {
		t.Fatalf("expected escaped braces in %q", lines[0])
	}
}

func TestLookupParamDescriptionMissingAsset(t *testing.T) {
	oldOnce := paramDescriptionsOnce
	oldData := paramDescriptions
	oldLoad := loadParamDescriptionsAsset
	paramDescriptionsOnce = sync.Once{}
	paramDescriptions = paramDescriptionsData{}
	loadParamDescriptionsAsset = func() ([]byte, error) {
		return nil, errParamAssetMissing
	}
	defer func() {
		paramDescriptionsOnce = oldOnce
		paramDescriptions = oldData
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
