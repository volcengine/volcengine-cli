package cmd

import (
	"bytes"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestResolveSystemFlagsBeforeService(t *testing.T) {
	resolution, err := resolveSystemFlags([]string{
		"--region", "cn-beijing",
		"--profile", "prod",
		"--endpoint", "sts.volcengineapi.com",
		"--lang", "ZH",
		"sts", "GetCallerIdentity",
	})
	if err != nil {
		t.Fatalf("resolveSystemFlags returned error: %v", err)
	}
	if !reflect.DeepEqual(resolution.args, []string{"sts", "GetCallerIdentity"}) {
		t.Fatalf("args = %#v", resolution.args)
	}
	want := map[string]string{
		"region": "cn-beijing", "profile": "prod",
		"endpoint": "sts.volcengineapi.com", "lang": "ZH",
	}
	if !reflect.DeepEqual(resolution.fixedFlags, want) {
		t.Fatalf("fixed flags = %#v, want %#v", resolution.fixedFlags, want)
	}
}

func TestResolveSystemFlagsRejectsAnotherFlagAsValue(t *testing.T) {
	_, err := resolveSystemFlags([]string{
		"--region", "--profile", "prod", "sts", "GetCallerIdentity",
	})
	if err == nil || !strings.Contains(err.Error(), "--region requires a value") {
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
	}, map[string]struct{}{})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	for name, want := range map[string]string{
		"region": "cn-beijing", "profile": "prod",
		"endpoint": "sts.volcengineapi.com", "lang": "ZH",
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
	params := map[string]struct{}{"lang": {}}
	parser := NewParser([]string{"--lang", "1", "--Lang", "ZH", "--Region", "business"}, params)
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	for name, want := range map[string]string{"lang": "1", "Lang": "ZH", "Region": "business"} {
		flag := c.dynamicFlags.GetByName(name)
		if flag == nil || flag.GetValue() != want {
			t.Fatalf("dynamic flag %q = %#v, want %q", name, flag, want)
		}
	}
	if c.fixedFlags.GetByName("lang") != nil {
		t.Fatal("conflicting --lang must not also enter fixedFlags")
	}
}

func TestSystemAndActionLangCanHaveDifferentValues(t *testing.T) {
	raw := []string{
		"--lang", "ZH",
		"i18nopenapi", "VideoProjectSuppressionStart",
		"--lang", "1",
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
		"--region", "cn-beijing", "sts", "GetCallerIdentity",
		"---region", "cn-shanghai",
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
		"--lang", "EN", "sts", "GetCallerIdentity", "---lang", "ZH",
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
			for name := range publicSystemFlags {
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
	conflictAction, _, err := rootCmd.Find([]string{"i18nopenapi", "VideoProjectSuppressionStart"})
	if err != nil {
		t.Fatalf("find conflict action: %v", err)
	}
	if conflictAction.Flags().Lookup("lang") == nil {
		t.Fatal("conflicting business --lang must remain registered for action completion")
	}
	var output bytes.Buffer
	if err := rootCmd.GenBashCompletion(&output); err != nil {
		t.Fatalf("GenBashCompletion returned error: %v", err)
	}
	completion := output.String()
	for _, name := range []string{"--profile", "--region", "--endpoint", "--lang"} {
		if !strings.Contains(completion, name) {
			t.Fatalf("completion missing %q", name)
		}
	}
	for _, name := range []string{"---profile", "---region", "---endpoint", "---lang"} {
		if strings.Contains(completion, name) {
			t.Fatalf("completion exposes historical alias %q", name)
		}
	}
}
