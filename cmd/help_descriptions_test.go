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
	if got := formatServiceShort("sts"); !strings.Contains(got, "安全") || !strings.Contains(got, "Security Token Service") {
		t.Fatalf("unexpected sts service description: %q", got)
	}
	if got := formatActionShort("sts", "GetCallerIdentity"); got != "获取请求者身份信息" {
		t.Fatalf("unexpected sts action description: %q", got)
	}
	if got := formatActionLong("sts", "GetCallerIdentity"); got != "获取请求者身份信息 - Get identity information for the request credential" {
		t.Fatalf("unexpected sts action long description: %q", got)
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

	if got := formatServiceShort("demo"); got != "演示服务 - Demo Service" {
		t.Fatalf("unexpected service description: %q", got)
	}
	if got := formatActionShort("demo", "DoThing"); got != "执行操作" {
		t.Fatalf("unexpected action short: %q", got)
	}
	if got := formatActionLong("demo", "DoThing"); got != "执行操作 - Run the demo operation." {
		t.Fatalf("unexpected action long: %q", got)
	}
	if got := formatActionLong("demo", "DoOtherThing"); got != "执行其他操作 - Do other thing" {
		t.Fatalf("unexpected action long with name_en fallback: %q", got)
	}
}

func TestActionUsageIncludesLongDescription(t *testing.T) {
	out := actionUsageTemplate("获取请求者身份信息 - Get identity information for the request credential", []string{"InstanceId string"})
	if !strings.Contains(out, "获取请求者身份信息 - Get identity information for the request credential") {
		t.Fatalf("action usage missing long description:\n%s", out)
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
	return []string{"---profile", "---region", "---endpoint", "---debug", "---debug-log-file"}
}
