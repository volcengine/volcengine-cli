package cmd

import (
	"github.com/spf13/cobra"
	"github.com/volcengine/volcengine-cli/internal/proxycli"
	"reflect"
	"testing"
)

func TestNativeVefaasArgumentsAndOpenAPICoexist(t *testing.T) {
	native := []string{"vefaas", "login", "--profile", "child", "--region", "r", "--endpoint", "e", "--lang", "child-language", "--version", "--", "", "中文"}
	got, err := resolveSystemFlags(native)
	if err != nil || !reflect.DeepEqual(got.args, native) || len(got.fixedFlags) != 0 {
		t.Fatal(got, err)
	}
	actions := rootSupport.GetAllAction("vefaas")
	if len(actions) == 0 {
		t.Fatal("missing OpenAPI actions")
	}
	api := []string{"vefaas", actions[0], "--lang", "ZH"}
	got, err = resolveSystemFlags(api)
	if err != nil || got.fixedFlags["lang"] != "ZH" || !reflect.DeepEqual(got.args, api[:2]) {
		t.Fatal(got, err)
	}
	if isProxyCLIInvocation("vefaas", api[1:]) || !isProxyCLIInvocation("vefaas", []string{"--help"}) {
		t.Fatal("shared dispatch misclassified")
	}
	if isProxyCLIInvocation("vefaas", []string{"NewUnlistedAction", "--force"}) {
		t.Fatal("explicit force action lost")
	}
}

func TestOfficialNativeNamesAreExposed(t *testing.T) {
	for id, name := range map[string]string{"tls-cli": "volclog", "vefaas-cli": "vefaas", "rtc-cli": "vertc", "nest-cli": "nest", "starling-cli": "starling", "openviking-cli": "ov"} {
		tool, err := processProxyCatalog.Ready(proxycli.Tool(id))
		if err != nil || tool.Command != name || tool.Executable != name || !isProxyCLIForwardCommand(name) {
			t.Fatal(id, name, tool, err)
		}
	}
}

func TestSharedProxyRoutingPreservesNativeArgumentsAndServiceTree(t *testing.T) {
	for _, test := range []struct {
		name   string
		args   []string
		native bool
	}{
		{"native-help-action-token", []string{"vefaas", "--help", "ListFunctions"}, true},
		{"native-profile-action-token", []string{"vefaas", "--profile", "prod", "ListFunctions", "--help"}, true},
		{"native-command", []string{"vefaas", "login", "--check"}, true},
		{"known-api", []string{"vefaas", "ListFunctions", "--help"}, false},
		{"forced-api", []string{"vefaas", "NewUnlistedAction", "--force"}, false},
		{"service-help", []string{"help", "vefaas"}, false},
		{"root-help", []string{"--help"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := &cobra.Command{Use: "ve"}
			service := &cobra.Command{Use: "vefaas", DisableFlagParsing: true}
			action := &cobra.Command{Use: "ListFunctions"}
			service.AddCommand(action)
			root.AddCommand(service)
			restore := prepareSharedProxyRouting(root, test.args)
			if test.native {
				target, remaining, err := root.Find(test.args)
				if err != nil || target != service || !reflect.DeepEqual(remaining, test.args[1:]) {
					t.Fatalf("native routing: target=%v args=%q err=%v", target, remaining, err)
				}
			} else if len(service.Commands()) != 1 || service.Commands()[0] != action {
				t.Fatal("OpenAPI/help service tree was modified")
			}
			restore()
			if len(service.Commands()) != 1 || service.Commands()[0] != action {
				t.Fatal("service tree not restored")
			}
		})
	}
}
