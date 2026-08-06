package cmd

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

func TestLazyActionUsageBuildsOnDemand(t *testing.T) {
	command := &cobra.Command{
		Use:  "DemoAction",
		Long: "demo",
	}
	var conciseCalls, detailCalls int
	setLazyActionUsage(command, func(detail bool) []string {
		if detail {
			detailCalls++
			return []string{"Name string with-desc"}
		}
		conciseCalls++
		return []string{"Name string"}
	})
	if conciseCalls != 0 || detailCalls != 0 {
		t.Fatalf("usage built eagerly: concise=%d detail=%d", conciseCalls, detailCalls)
	}

	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	if err := command.Usage(); err != nil {
		t.Fatalf("first Usage: %v", err)
	}
	if conciseCalls != 1 {
		t.Fatalf("concise build calls=%d, want 1", conciseCalls)
	}
	if detailCalls != 0 {
		t.Fatalf("detail build calls=%d, want 0 for default Usage", detailCalls)
	}
	if !strings.Contains(output.String(), "Name string") {
		t.Fatalf("rendered usage missing params:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "--detail") {
		t.Fatalf("concise usage missing --detail tip:\n%s", output.String())
	}

	// Detail mode rebuilds with detail=true (no dual template cache).
	output.Reset()
	setActionHelpDetail(command, true)
	if err := command.Usage(); err != nil {
		t.Fatalf("detail Usage: %v", err)
	}
	if detailCalls != 1 {
		t.Fatalf("detail build calls=%d, want 1", detailCalls)
	}
	if !strings.Contains(output.String(), "with-desc") {
		t.Fatalf("detail usage missing full params:\n%s", output.String())
	}

	// Back to concise clears detail annotation path.
	output.Reset()
	setActionHelpDetail(command, false)
	if err := command.Usage(); err != nil {
		t.Fatalf("concise again: %v", err)
	}
	if conciseCalls != 2 {
		t.Fatalf("concise rebuild calls=%d, want 2", conciseCalls)
	}
}

func TestGenerateActionCmdDoesNotLoadParamDescriptionsEagerly(t *testing.T) {
	oldLoad := loadParamDescriptionsAsset
	paramDescriptionsOnce = sync.Once{}
	paramDescriptions = paramDescriptionsData{}
	loadCalls := 0
	loadParamDescriptionsAsset = func() ([]byte, error) {
		loadCalls++
		return []byte(`{"apis":{}}`), nil
	}
	defer func() {
		paramDescriptionsOnce = sync.Once{}
		paramDescriptions = paramDescriptionsData{}
		loadParamDescriptionsAsset = oldLoad
	}()

	basic := []string{"Name"}
	commands := generateActionCmd("demo", map[string]*VolcengineMeta{
		"DoThing": {
			Request: &MetaInfo{Basic: &basic},
		},
	}, nil)
	if loadCalls != 0 {
		t.Fatalf("param descriptions loaded during command construction: %d", loadCalls)
	}
	if len(commands) != 1 {
		t.Fatalf("commands=%d, want 1", len(commands))
	}

	var output bytes.Buffer
	commands[0].SetOut(&output)
	commands[0].SetErr(&output)
	// Default -h is concise: must not load the param description corpus.
	if err := commands[0].Usage(); err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if loadCalls != 0 {
		t.Fatalf("param descriptions load calls=%d, want 0 for concise help", loadCalls)
	}

	setActionHelpDetail(commands[0], true)
	if err := commands[0].Usage(); err != nil {
		t.Fatalf("detail Usage: %v", err)
	}
	if loadCalls != 1 {
		t.Fatalf("param descriptions load calls=%d, want 1 for --detail help", loadCalls)
	}
}

func TestParseActionHelpArgs(t *testing.T) {
	cases := []struct {
		args           []string
		wantHelp       bool
		wantDetail     bool
	}{
		{nil, false, false},
		{[]string{"-h"}, true, false},
		{[]string{"--help"}, true, false},
		{[]string{"-h", "--detail"}, true, true},
		{[]string{"--detail", "--help"}, true, true},
		{[]string{"--detail"}, false, false},
		{[]string{"--ZoneId", "cn-beijing", "-h"}, true, false},
		{[]string{"--ZoneId", "cn-beijing", "-h", "--detail"}, true, true},
		{[]string{"--Detail"}, false, false}, // case-sensitive; not a help control
		{[]string{"--help=true"}, false, false},
		{[]string{"--detail=true", "-h"}, true, false}, // equals form is not the bare --detail token
	}
	for _, c := range cases {
		gotHelp, gotDetail := parseActionHelpArgs(c.args)
		if gotHelp != c.wantHelp || gotDetail != c.wantDetail {
			t.Fatalf("parseActionHelpArgs(%v)=(%v,%v), want (%v,%v)",
				c.args, gotHelp, gotDetail, c.wantHelp, c.wantDetail)
		}
	}
}

func TestBuildActionHelpParamLinesPreservesPrefixAndSortsKeys(t *testing.T) {
	restoreLang := setLanguageForTest(LanguageEnglish)
	defer restoreLang()
	params := []param{
		{key: "ZoneId", typeName: "string", required: true, description: "zone help"},
		{key: "Count", typeName: "integer", required: false, description: "count help"},
	}
	lines := buildActionHelpParamLines("ecs", "RunInstances", params, []string{"body '{}'"}, false)
	if len(lines) < 3 {
		t.Fatalf("want prefix + 2 params, got %#v", lines)
	}
	if lines[0] != "body '{}'" {
		t.Fatalf("prefix must stay first, got %q", lines[0])
	}
	// Keys sorted: Count before ZoneId
	if !strings.Contains(lines[1], "Count") || !strings.Contains(lines[2], "ZoneId") {
		t.Fatalf("expected Count then ZoneId skeleton, got %#v", lines[1:])
	}
	// Concise must not embed descriptions
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "zone help") || strings.Contains(joined, "count help") {
		t.Fatalf("concise lines leaked descriptions:\n%s", joined)
	}
	// Input slice must not be mutated
	if params[0].key != "ZoneId" {
		t.Fatalf("input params reordered or mutated: %+v", params)
	}
}

func TestActionUsageDetailTipChinese(t *testing.T) {
	restore := setLanguageForTest(LanguageSimplifiedChinese)
	defer restore()
	out := actionUsageTemplate("", []string{"Id string"}, false)
	want := "默认帮助为简洁模式。查看完整参数描述与示例：-h --detail（或 --help --detail）。"
	if !strings.Contains(out, want) {
		t.Fatalf("missing Chinese detail tip %q in:\n%s", want, out)
	}
	detailOut := actionUsageTemplate("", []string{"Id string"}, true)
	if strings.Contains(detailOut, want) {
		t.Fatalf("detail mode should omit tip:\n%s", detailOut)
	}
	// Bare-detail error ZH catalog entry stays in sync with English tr() key.
	err := errBareDetailWithoutHelp()
	if !strings.Contains(err.Error(), "--Detail") {
		t.Fatalf("ZH bare-detail error should mention --Detail casing: %v", err)
	}
	if strings.Contains(err.Error(), "--detail <value>") {
		t.Fatalf("should not suggest lowercase --detail as API value form: %v", err)
	}
}

func TestBareDetailWithoutValue(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"--detail"}, true},
		{[]string{"--detail", "--ZoneId"}, true}, // next is a flag → bare
		{[]string{"--detail", "-h"}, false},      // single-dash is a value to the parser (help path still wins if -h present)
		{[]string{"--detail", "-1"}, false},      // dash-prefixed value is valid
		{[]string{"--ZoneId", "cn", "--detail"}, true},
		{[]string{"--detail", "foo"}, false},
		{[]string{"--Detail"}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := bareDetailWithoutValue(c.args); got != c.want {
			t.Fatalf("bareDetailWithoutValue(%v)=%v, want %v", c.args, got, c.want)
		}
	}
}

func TestActionRunE_HelpShortCircuitAndBareDetail(t *testing.T) {
	restoreLang := setLanguageForTest(LanguageEnglish)
	defer restoreLang()

	basic := []string{"Name"}
	commands := generateActionCmd("demo", map[string]*VolcengineMeta{
		"DoThing": {Request: &MetaInfo{Basic: &basic}},
	}, nil)
	if len(commands) != 1 {
		t.Fatalf("commands=%d", len(commands))
	}
	action := commands[0]
	// Parent only for CommandPath in usage template; call RunE directly (DisableFlagParsing).
	parent := &cobra.Command{Use: "demo"}
	parent.AddCommand(action)

	// -h: shows concise help, does not require credentials / SDK.
	var out bytes.Buffer
	action.SetOut(&out)
	action.SetErr(&out)
	if err := action.RunE(action, []string{"-h"}); err != nil {
		t.Fatalf("-h RunE: %v", err)
	}
	if !strings.Contains(out.String(), "Name") {
		t.Fatalf("-h missing skeleton:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "-h --detail") {
		t.Fatalf("-h missing improved tip:\n%s", out.String())
	}
	// Annotation must be cleared after help.
	if getActionHelpDetail(action) {
		t.Fatal("detail annotation should be cleared after -h")
	}

	// bare --detail: friendly error, not generic must-set-value.
	out.Reset()
	err := action.RunE(action, []string{"--detail"})
	if err == nil {
		t.Fatal("expected error for bare --detail")
	}
	if !strings.Contains(err.Error(), "only expands help when used with") {
		t.Fatalf("unexpected bare --detail error: %v", err)
	}
	if strings.Contains(err.Error(), "must set value") {
		t.Fatalf("should not use generic parser error: %v", err)
	}

	// --detail with value is not the bare-detail help error (may be API param).
	// We only assert it does not return errBareDetailWithoutHelp; it may fail later on credentials.
	out.Reset()
	err = action.RunE(action, []string{"--detail", "x"})
	if err != nil && strings.Contains(err.Error(), "only expands help when used with") {
		t.Fatalf("--detail with value should not use bare-detail help error: %v", err)
	}

	// -h --detail: full help path sets annotation then clears; corpus may load.
	out.Reset()
	if err := action.RunE(action, []string{"-h", "--detail"}); err != nil {
		t.Fatalf("-h --detail RunE: %v", err)
	}
	if !strings.Contains(out.String(), "Name") {
		t.Fatalf("-h --detail missing params:\n%s", out.String())
	}
	if strings.Contains(out.String(), "Default help is concise") {
		t.Fatalf("detail help should omit concise tip:\n%s", out.String())
	}
	if getActionHelpDetail(action) {
		t.Fatal("detail annotation should be cleared after -h --detail")
	}
}

func TestCreateCommandUsageRendersLiteralTemplateBraces(t *testing.T) {
	restoreLanguage := setLanguageForTest(LanguageEnglish)
	defer restoreLanguage()

	command, _, err := rootCmd.Find([]string{"ecs", "CreateCommand"})
	if err != nil {
		t.Fatalf("find ecs CreateCommand: %v", err)
	}
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	// {{Param}} appears in full parameter descriptions/examples (detail help only).
	setActionHelpDetail(command, true)
	if err := command.Usage(); err != nil {
		t.Fatalf("CreateCommand Usage: %v", err)
	}
	if !strings.Contains(output.String(), "{{Param}}") {
		t.Fatalf("literal template example missing from help:\n%s", output.String())
	}
}

func TestResolveActionHTTPMethodUsesMetadataByDefault(t *testing.T) {
	apiInfo := &ApiInfo{Method: "POST"}
	method, err := resolveActionHTTPMethod(NewContext(), apiInfo)
	if err != nil {
		t.Fatalf("resolveActionHTTPMethod: %v", err)
	}
	if method != "POST" {
		t.Fatalf("expected metadata method POST, got %q", method)
	}
}

func TestResolveActionHTTPMethodOverrideFromFlag(t *testing.T) {
	c := NewContext()
	parser := NewParser([]string{"---method", "GET"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs: %v", err)
	}
	apiInfo := &ApiInfo{Method: "POST"}
	method, err := resolveActionHTTPMethod(c, apiInfo)
	if err != nil {
		t.Fatalf("resolveActionHTTPMethod: %v", err)
	}
	if method != "GET" {
		t.Fatalf("expected ---method to override metadata, got %q", method)
	}
}

func TestResolveActionHTTPMethodDefaultsToGET(t *testing.T) {
	method, err := resolveActionHTTPMethod(NewContext(), nil)
	if err != nil {
		t.Fatalf("resolveActionHTTPMethod: %v", err)
	}
	if method != "GET" {
		t.Fatalf("expected default GET, got %q", method)
	}
}

func TestBuildActionInputRejectsBodyWithFlattenedParams(t *testing.T) {
	body := &Flag{Name: "body"}
	body.SetValue(`{"InstanceId":"mysql-1"}`)
	instanceID := &Flag{Name: "InstanceId"}
	instanceID.SetValue("mysql-1")

	_, _, err := buildActionInput([]*Flag{body, instanceID}, nil, true)
	if err == nil {
		t.Fatal("expected --body and flattened params conflict")
	}
}

func TestBuildActionInputParsesJsonBody(t *testing.T) {
	body := &Flag{Name: "body"}
	body.SetValue(`{"InstanceId":"mysql-1","IPList":["10.20.30.40"]}`)

	input, fromBody, err := buildActionInput([]*Flag{body}, nil, true)
	if err != nil {
		t.Fatalf("buildActionInput returned error: %v", err)
	}
	if !fromBody {
		t.Fatal("expected input to be marked from body")
	}
	m, ok := input.(*map[string]interface{})
	if !ok {
		t.Fatalf("expected *map input, got %T", input)
	}
	if (*m)["InstanceId"] != "mysql-1" {
		t.Fatalf("expected InstanceId to be parsed, got %#v", (*m)["InstanceId"])
	}
}

func TestBuildActionInputSupportsFlattenedJsonBodyParams(t *testing.T) {
	apiMeta := &ApiMeta{Request: &Meta{MetaTypes: map[string]*MetaType{
		"InstanceId": {TypeName: "string"},
		"GroupName":  {TypeName: "string"},
		"IPList":     {TypeName: "array[string]"},
	}}}

	instanceID := &Flag{Name: "InstanceId"}
	instanceID.SetValue("mysql-1")
	groupName := &Flag{Name: "GroupName"}
	groupName.SetValue("group-a")
	ipList := &Flag{Name: "IPList"}
	ipList.SetValue(`["10.20.30.40","50.60.70.80"]`)

	input, fromBody, err := buildActionInput([]*Flag{instanceID, groupName, ipList}, apiMeta, true)
	if err != nil {
		t.Fatalf("buildActionInput returned error: %v", err)
	}
	if fromBody {
		t.Fatal("flattened params should not be marked from body")
	}
	m, ok := input.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map input, got %T", input)
	}
	if m["InstanceId"] != "mysql-1" || m["GroupName"] != "group-a" {
		t.Fatalf("unexpected scalar params: %#v", m)
	}
	gotList, ok := m["IPList"].([]interface{})
	if !ok || len(gotList) != 2 {
		t.Fatalf("expected IPList JSON array, got %#v", m["IPList"])
	}
}

func TestFormatActionErrorNoCredentialProviders(t *testing.T) {
	err := formatActionError(assertErr("NoCredentialProviders: no valid providers in chain. Deprecated."))
	if err == nil {
		t.Fatal("expected formatted error")
	}
	if got := err.Error(); got != "credentials not configured, please run 've login' or 've configure set', or set VOLCENGINE_ACCESS_KEY and VOLCENGINE_SECRET_KEY environment variables" {
		t.Fatalf("unexpected error: %q", got)
	}
}

type assertErr string

func (e assertErr) Error() string {
	return string(e)
}

func TestIsStringParam(t *testing.T) {
	tests := []struct {
		name     string
		apiMeta  *ApiMeta
		param    string
		expected bool
	}{
		{
			name:     "nil apiMeta",
			apiMeta:  nil,
			param:    "PolicyDocument",
			expected: false,
		},
		{
			name: "string type param",
			apiMeta: &ApiMeta{
				Request: &Meta{
					MetaTypes: map[string]*MetaType{
						"PolicyDocument": {TypeName: "string"},
						"PolicyName":     {TypeName: "string"},
					},
				},
			},
			param:    "PolicyDocument",
			expected: true,
		},
		{
			name: "non-string type param",
			apiMeta: &ApiMeta{
				Request: &Meta{
					MetaTypes: map[string]*MetaType{
						"Filters": {TypeName: "object"},
					},
				},
			},
			param:    "Filters",
			expected: false,
		},
		{
			name: "unknown param",
			apiMeta: &ApiMeta{
				Request: &Meta{
					MetaTypes: map[string]*MetaType{
						"PolicyName": {TypeName: "string"},
					},
				},
			},
			param:    "Unknown",
			expected: false,
		},
		{
			name: "indexed repeated string param",
			apiMeta: &ApiMeta{
				Request: &Meta{
					MetaTypes: map[string]*MetaType{
						"TagFilters.N.Key": {TypeName: "string"},
					},
				},
			},
			param:    "TagFilters.1.Key",
			expected: true,
		},
		{
			name: "indexed repeated string array element",
			apiMeta: &ApiMeta{
				Request: &Meta{
					MetaTypes: map[string]*MetaType{
						"TagFilters.N.Values.N": {TypeName: "array[string]"},
					},
				},
			},
			param:    "TagFilters.1.Values.1",
			expected: true,
		},
		{
			name: "indexed root string array element",
			apiMeta: &ApiMeta{
				Request: &Meta{
					MetaTypes: map[string]*MetaType{
						"ResourceNames.N": {TypeName: "array[string]"},
					},
				},
			},
			param:    "ResourceNames.0",
			expected: true,
		},
		{
			name: "root string array is not treated as string literal",
			apiMeta: &ApiMeta{
				Request: &Meta{
					MetaTypes: map[string]*MetaType{
						"ResourceNames": {TypeName: "array[string]"},
					},
				},
			},
			param:    "ResourceNames",
			expected: false,
		},
		{
			name: "indexed repeated non-string param",
			apiMeta: &ApiMeta{
				Request: &Meta{
					MetaTypes: map[string]*MetaType{
						"TagFilters.N": {TypeName: "object"},
					},
				},
			},
			param:    "TagFilters.1",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isStringParam(tt.apiMeta, tt.param)
			if got != tt.expected {
				t.Errorf("isStringParam(%v, %q) = %v, want %v", tt.apiMeta, tt.param, got, tt.expected)
			}
		})
	}
}
