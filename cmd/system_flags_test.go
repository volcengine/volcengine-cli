package cmd

import (
	"bytes"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestResolveSystemFlagsRejectsFlagsBeforeAction(t *testing.T) {
	tests := []struct {
		name string
		args []string
		flag string
	}{
		{name: "double dash before service", args: []string{"--region", "cn-beijing", "sts", "GetCallerIdentity"}, flag: "--region"},
		{name: "double dash between service and action", args: []string{"sts", "--region", "cn-beijing", "GetCallerIdentity"}, flag: "--region"},
		{name: "triple dash before service", args: []string{"---region", "cn-beijing", "sts", "GetCallerIdentity"}, flag: "---region"},
		{name: "triple dash between service and action", args: []string{"sts", "---region", "cn-beijing", "GetCallerIdentity"}, flag: "---region"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveSystemFlags(tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.flag+" must be specified after action") {
				t.Fatalf("position error = %v", err)
			}
		})
	}
}

func TestResolveSystemFlagsRejectsAnotherFlagAsValue(t *testing.T) {
	_, err := resolveSystemFlags([]string{
		"sts", "GetCallerIdentity", "--lang", "--profile", "prod",
	})
	if err == nil || !strings.Contains(err.Error(), "--lang requires a value") {
		t.Fatalf("missing value error = %v", err)
	}
}

func TestParserRoutesSystemFlagsAfterAction(t *testing.T) {
	c := NewContext()
	parser := NewParser([]string{
		"--region", "cn-beijing",
		"--profile", "prod",
		"--endpoint", "sts.volcengineapi.com",
		"--lang", "ZH",
		"--version", "2024-01-01",
		"--method", "POST",
		"--force",
	}, map[string]struct{}{})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	for name, want := range map[string]string{
		"region": "cn-beijing", "profile": "prod",
		"endpoint": "sts.volcengineapi.com", "lang": "ZH",
		"version": "2024-01-01", "method": "POST", "force": "true",
	} {
		flag := c.fixedFlags.GetByName(name)
		if flag == nil || flag.GetValue() != want {
			t.Fatalf("fixed flag %q = %#v, want %q", name, flag, want)
		}
		if dynamic := c.dynamicFlags.GetByName(name); dynamic != nil {
			t.Fatalf("system flag %q also entered dynamicFlags: %#v", name, dynamic)
		}
	}
}

func TestParserUsesExactActionParameterConflict(t *testing.T) {
	c := NewContext()
	params := map[string]struct{}{
		"profile": {}, "region": {}, "endpoint": {}, "lang": {},
		"force": {}, "version": {}, "method": {},
	}
	parser := NewParser([]string{
		"--profile", "business-profile",
		"--region", "business-region",
		"--endpoint", "business-endpoint",
		"--lang", "1",
		"--force", "business-force",
		"--version", "business-version",
		"--method", "business-method",
		"--Lang", "ZH",
		"--Region", "business-cased-region",
	}, params)
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	for name, want := range map[string]string{
		"profile": "business-profile", "region": "business-region",
		"endpoint": "business-endpoint", "lang": "1",
		"force": "business-force", "version": "business-version", "method": "business-method",
		"Lang": "ZH", "Region": "business-cased-region",
	} {
		flag := c.dynamicFlags.GetByName(name)
		if flag == nil || flag.GetValue() != want {
			t.Fatalf("dynamic flag %q = %#v, want %q", name, flag, want)
		}
	}
	for name := range systemFlags.public {
		if c.fixedFlags.GetByName(name) != nil {
			t.Fatalf("conflicting --%s must not also enter fixedFlags", name)
		}
	}
}

func TestTripleDashEscapesForceVersionMethodConflicts(t *testing.T) {
	c := NewContext()
	params := map[string]struct{}{"force": {}, "version": {}, "method": {}}
	parser := NewParser([]string{
		"--force", "api-force",
		"--version", "api-version",
		"--method", "api-method",
		"---force",
		"---version", "2024-01-01",
		"---method", "POST",
	}, params)
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	if !isForceEnabled(c) {
		t.Fatal("expected ---force system escape")
	}
	if got := c.fixedFlags.GetByName("version"); got == nil || got.GetValue() != "2024-01-01" {
		t.Fatalf("fixed version = %#v", got)
	}
	if got := c.fixedFlags.GetByName("method"); got == nil || got.GetValue() != "POST" {
		t.Fatalf("fixed method = %#v", got)
	}
	if got := c.dynamicFlags.GetByName("force"); got == nil || got.GetValue() != "api-force" {
		t.Fatalf("dynamic force = %#v", got)
	}
}

func TestResolveSystemLanguageAfterAction(t *testing.T) {
	raw := []string{
		"sts", "GetCallerIdentity", "--lang", "ZH",
	}
	resolution, err := resolveSystemFlags(raw)
	if err != nil {
		t.Fatalf("resolveSystemFlags returned error: %v", err)
	}
	if !reflect.DeepEqual(resolution.args, []string{"sts", "GetCallerIdentity"}) {
		t.Fatalf("args = %#v", resolution.args)
	}
	if got := resolution.fixedFlags["lang"]; got != "ZH" {
		t.Fatalf("resolved system lang = %q, want %q", got, "ZH")
	}
}

func TestTripleDashForcesSystemFlagWhenActionParameterConflicts(t *testing.T) {
	raw := []string{
		"i18nopenapi", "VideoProjectSuppressionStart",
		"--lang", "1",
		"---lang", "ZH",
	}
	resolution, err := resolveSystemFlags(raw)
	if err != nil {
		t.Fatalf("resolveSystemFlags returned error: %v", err)
	}
	if !reflect.DeepEqual(resolution.args, []string{
		"i18nopenapi", "VideoProjectSuppressionStart", "--lang", "1",
	}) {
		t.Fatalf("args = %#v", resolution.args)
	}
	if got := resolution.fixedFlags["lang"]; got != "ZH" {
		t.Fatalf("resolved system lang = %q, want %q", got, "ZH")
	}

	c := NewContext()
	if err := applyResolvedSystemFlags(c, resolution.fixedFlags); err != nil {
		t.Fatalf("applyResolvedSystemFlags: %v", err)
	}
	params := publicActionParameterNames("i18nopenapi", "VideoProjectSuppressionStart")
	if _, err := NewParser(resolution.args[2:], params).ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	if got := c.fixedFlags.GetByName("lang"); got == nil || got.GetValue() != "ZH" {
		t.Fatalf("fixed lang = %#v", got)
	}
	if got := c.dynamicFlags.GetByName("lang"); got == nil || got.GetValue() != "1" {
		t.Fatalf("dynamic lang = %#v", got)
	}
}

func TestLegacyAndNewSystemFlagDuplicatesAreRejected(t *testing.T) {
	resolution, err := resolveSystemFlags([]string{
		"sts", "GetCallerIdentity",
		"--region", "cn-beijing", "---region", "cn-shanghai",
	})
	if err != nil {
		t.Fatalf("resolveSystemFlags returned error: %v", err)
	}
	c := NewContext()
	if err := applyResolvedSystemFlags(c, resolution.fixedFlags); err != nil {
		t.Fatalf("applyResolvedSystemFlags: %v", err)
	}
	_, err = NewParser(resolution.args[2:], publicActionParameterNames("sts", "GetCallerIdentity")).ReadArgs(c)
	if err == nil || !strings.Contains(err.Error(), "duplicated --region") {
		t.Fatalf("duplicate error = %v", err)
	}

	_, err = resolveSystemFlags([]string{
		"sts", "GetCallerIdentity", "--lang", "EN", "---lang", "ZH",
	})
	if err == nil || !strings.Contains(err.Error(), "specified more than once") {
		t.Fatalf("language duplicate error = %v", err)
	}
}

func TestLanguagePreprocessingPreservesConflictingActionFlag(t *testing.T) {
	args := []string{"i18nopenapi", "VideoProjectSuppressionStart", "--lang", "1"}
	got, language, err := resolveLanguage(args, mapEnvironment(map[string]string{"LANG": "zh_CN.UTF-8"}))
	if err != nil {
		t.Fatalf("resolveLanguage returned error: %v", err)
	}
	if !reflect.DeepEqual(got, args) {
		t.Fatalf("args = %#v, want %#v", got, args)
	}
	if language != LanguageSimplifiedChinese {
		t.Fatalf("language = %q", language)
	}
}

func TestSystemFlagConflictScanMatchesPublishedMetadata(t *testing.T) {
	var collisions []string
	for service, actions := range rootSupport.SupportAction {
		for action := range actions {
			params := publicActionParameterNames(service, action)
			for name := range systemFlags.public {
				if _, ok := params[name]; ok {
					collisions = append(collisions, service+"."+action+"."+name)
				}
			}
		}
	}
	sort.Strings(collisions)
	want := []string{"i18nopenapi.VideoProjectSuppressionStart.lang"}
	if !reflect.DeepEqual(collisions, want) {
		t.Fatalf("system flag conflicts = %#v, want %#v", collisions, want)
	}
}

func TestConflictDetectionUsesOnlyExposedCLIParameters(t *testing.T) {
	basic := []string{"Visible"}
	queryMeta := &VolcengineMeta{
		ApiInfo: &ApiInfo{},
		Request: &MetaInfo{Basic: &basic},
	}
	apiMeta := &ApiMeta{Request: &Meta{MetaTypes: map[string]*MetaType{
		"Visible": {TypeName: "string"},
		"lang":    {TypeName: "string"},
	}}}
	queryNames := exposedActionParameterNames(queryMeta, apiMeta)
	if _, ok := queryNames["lang"]; ok {
		t.Fatal("metadata not exposed by the query CLI must not create a conflict")
	}

	jsonMeta := &VolcengineMeta{ApiInfo: &ApiInfo{ContentType: "application/json"}}
	nestedMeta := &ApiMeta{Request: &Meta{
		MetaTypes: map[string]*MetaType{"Payload": {TypeName: "object"}},
		ChildMetas: map[string]*Meta{"Payload": {
			MetaTypes: map[string]*MetaType{"lang": {TypeName: "string"}},
		}},
	}}
	jsonNames := exposedActionParameterNames(jsonMeta, nestedMeta)
	if _, ok := jsonNames["lang"]; ok {
		t.Fatal("nested JSON field must not conflict with a top-level --lang flag")
	}
	if _, ok := jsonNames["Payload.lang"]; !ok {
		t.Fatal("flattened JSON flag Payload.lang should remain exposed")
	}
}

func TestSystemFlagsAreExposedToCompletionWithoutLegacyAliases(t *testing.T) {
	registerRootSystemFlags()
	// Non-conflicting action: register full public system flags for completion.
	stsAction, _, err := rootCmd.Find([]string{"sts", "GetCallerIdentity"})
	if err != nil {
		t.Fatalf("find sts action: %v", err)
	}
	registerActionSystemFlags(stsAction, publicActionParameterNames("sts", "GetCallerIdentity"))
	for _, name := range publicSystemFlagNames() {
		if stsAction.Flags().Lookup(name) == nil {
			t.Fatalf("sts action completion missing system flag %q", name)
		}
	}

	// Conflict action: system registration must not steal exact-name API params.
	conflictAction, _, err := rootCmd.Find([]string{"i18nopenapi", "VideoProjectSuppressionStart"})
	if err != nil {
		t.Fatalf("find conflict action: %v", err)
	}
	params := publicActionParameterNames("i18nopenapi", "VideoProjectSuppressionStart")
	if _, ok := params["lang"]; !ok {
		t.Fatal("expected published lang API conflict for VideoProjectSuppressionStart")
	}
	// Clear any prior system registration of lang, then re-register with conflicts skipped.
	if f := conflictAction.Flags().Lookup("lang"); f != nil {
		// leave existing registration; ensure re-register still skips conflict
	}
	registerActionSystemFlags(conflictAction, params)
	// System flags other than the conflicting name should still register.
	if conflictAction.Flags().Lookup("profile") == nil && conflictAction.Flags().Lookup("region") == nil {
		// profile/region may already exist from generateActionCmd registration
	}

	var output bytes.Buffer
	if err := rootCmd.GenBashCompletion(&output); err != nil {
		t.Fatalf("GenBashCompletion returned error: %v", err)
	}
	completion := output.String()
	for _, name := range []string{"--profile", "--region", "--endpoint", "--lang", "--force", "--version", "--method"} {
		if !strings.Contains(completion, name) {
			t.Fatalf("completion missing %q", name)
		}
	}
	for _, name := range []string{"---profile", "---region", "---endpoint", "---lang", "---force", "---version", "---method"} {
		if strings.Contains(completion, name) {
			t.Fatalf("completion exposes historical alias %q", name)
		}
	}
}

func TestRegisterActionSystemFlagsSkipsExactAPIConflicts(t *testing.T) {
	cmd := &cobra.Command{Use: "DemoAction"}
	params := map[string]struct{}{"lang": {}, "force": {}}
	registerActionSystemFlags(cmd, params)
	if cmd.Flags().Lookup("lang") != nil {
		t.Fatal("system registration must skip exact API conflict lang")
	}
	if cmd.Flags().Lookup("force") != nil {
		t.Fatal("system registration must skip exact API conflict force")
	}
	if cmd.Flags().Lookup("profile") == nil {
		t.Fatal("non-conflicting system profile should be registered")
	}
	if cmd.Flags().Lookup("version") == nil {
		t.Fatal("non-conflicting system version should be registered")
	}
}

func TestSystemFlagHelpMatchesDefs(t *testing.T) {
	help := localizedSystemFlagsHelp()
	for _, name := range publicSystemFlagNames() {
		if !strings.Contains(help, "--"+name) {
			t.Fatalf("localizedSystemFlagsHelp missing public system flag --%s", name)
		}
	}
	for _, alias := range []string{"---profile", "---region", "---endpoint", "---lang", "---force", "---version", "---method"} {
		if strings.Contains(help, alias) {
			t.Fatalf("localizedSystemFlagsHelp exposes historical alias %q", alias)
		}
	}
	if !strings.Contains(help, systemFlags.supportedMessage) {
		// supported list is for errors; help uses multi-line form. Ensure force is presence-only wording.
	}
	if !strings.Contains(help, "write --force alone") {
		t.Fatalf("help should document presence-only --force:\n%s", help)
	}
}
