package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestForceErrorMessagesLocalized(t *testing.T) {
	restoreLanguage := setLanguageForTest(LanguageSimplifiedChinese)
	defer restoreLanguage()

	c := NewContext()
	parser := NewParser([]string{"---force"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	err := validateForceCall(c, "newservice")
	if err == nil {
		t.Fatal("expected missing version error")
	}
	// Chinese locale should not fall back to the English catalog key.
	if strings.Contains(err.Error(), "---version is required when using ---force") {
		t.Fatalf("expected localized ---version error, still English: %v", err)
	}
	if !strings.Contains(err.Error(), "---version") || !strings.Contains(err.Error(), "newservice") {
		t.Fatalf("expected localized ---version error mentioning service, got: %v", err)
	}

	err = tryExecuteGenericInvoke([]string{"newservice", "DescribeNewResource"})
	if err == nil {
		t.Fatal("expected unknown service force guidance error")
	}
	if strings.Contains(err.Error(), "unknown service") {
		t.Fatalf("expected localized unknown service error, still English: %v", err)
	}
	if !strings.Contains(err.Error(), "---force") || !strings.Contains(err.Error(), "newservice") {
		t.Fatalf("expected localized unknown service error, got: %v", err)
	}
}

func TestIsForceEnabled(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		want           bool
		wantPositional []string
	}{
		{name: "bare force flag", args: []string{"---force"}, want: true},
		{name: "force does not accept true value", args: []string{"---force", "true"}, want: true, wantPositional: []string{"true"}},
		{name: "force does not accept false value", args: []string{"---force", "false"}, want: true, wantPositional: []string{"false"}},
		{name: "no force", args: []string{"---version", "2024-01-01"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewContext()
			parser := NewParser(tt.args)
			positional, err := parser.ReadArgs(c)
			if err != nil {
				t.Fatalf("ReadArgs returned error: %v", err)
			}
			if got := isForceEnabled(c); got != tt.want {
				t.Fatalf("isForceEnabled() = %v, want %v", got, tt.want)
			}
			if tt.wantPositional != nil {
				if len(positional) != len(tt.wantPositional) {
					t.Fatalf("positional = %v, want %v", positional, tt.wantPositional)
				}
				for i := range tt.wantPositional {
					if positional[i] != tt.wantPositional[i] {
						t.Fatalf("positional[%d] = %q, want %q", i, positional[i], tt.wantPositional[i])
					}
				}
			}
		})
	}
}

func TestValidateForceCallRequiresVersion(t *testing.T) {
	c := NewContext()
	parser := NewParser([]string{"---force"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	err := validateForceCall(c, "newservice")
	if err == nil {
		t.Fatal("expected missing version error")
	}
	if !strings.Contains(err.Error(), "---version is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateForceCallRequiresEndpointForUnlistedService(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT", "")()
	c := NewContext()
	parser := NewParser([]string{"---force", "---version", "2024-01-01"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}

	err := validateForceCall(c, "newservice")
	if err == nil {
		t.Fatal("expected missing endpoint error")
	}
	if !strings.Contains(err.Error(), "endpoint is required for unlisted service") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateForceCallAcceptsProfileEndpointForUnlistedService(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT", "")()
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT_RESOLVER", "")()
	c := NewContext()
	c.config = &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {
				Name:     "default",
				Endpoint: "open.volcengineapi.com",
			},
		},
	}
	parser := NewParser([]string{"---force", "---version", "2024-01-01"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	if err := validateForceCall(c, "newservice"); err != nil {
		t.Fatalf("profile endpoint should satisfy unlisted force endpoint check, got: %v", err)
	}
}

func TestValidateForceCallAcceptsEnvEndpointForUnlistedService(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT", "open.volcengineapi.com")()
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT_RESOLVER", "")()
	c := NewContext()
	parser := NewParser([]string{"---force", "---version", "2024-01-01"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	if err := validateForceCall(c, "newservice"); err != nil {
		t.Fatalf("VOLCENGINE_ENDPOINT should satisfy unlisted force endpoint check, got: %v", err)
	}
}

func TestValidateForceCallRejectsStandardResolverWithoutFixedHost(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT", "")()
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT_RESOLVER", "standard")()
	c := NewContext()
	parser := NewParser([]string{"---force", "---version", "2024-01-01"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	err := validateForceCall(c, "newservice")
	if err == nil {
		t.Fatal("standard resolver alone should not satisfy unlisted endpoint check")
	}
	if !strings.Contains(err.Error(), "endpoint is required for unlisted service") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateForceCallRejectsProfileEndpointWhenResolverStandard(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT", "")()
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT_RESOLVER", "")()
	c := NewContext()
	c.config = &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {
				Name:             "default",
				Endpoint:         "open.volcengineapi.com",
				EndpointResolver: "standard",
			},
		},
	}
	parser := NewParser([]string{"---force", "---version", "2024-01-01"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	err := validateForceCall(c, "newservice")
	if err == nil {
		t.Fatal("profile endpoint ignored under standard resolver should not pass unlisted check")
	}
	if !strings.Contains(err.Error(), "endpoint is required for unlisted service") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateForceCallAcceptsExplicitEndpointWithStandardResolver(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT", "")()
	c := NewContext()
	c.config = &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {
				Name:             "default",
				EndpointResolver: "standard",
			},
		},
	}
	parser := NewParser([]string{"---force", "---version", "2024-01-01", "---endpoint", "open.volcengineapi.com"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	if err := validateForceCall(c, "newservice"); err != nil {
		t.Fatalf("---endpoint should clear resolver and pass, got: %v", err)
	}
}

func TestValidateForceCallRejectsAutoAddressingAsFixedHost(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT", "auto-addressing")()
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT_RESOLVER", "")()
	c := NewContext()
	parser := NewParser([]string{"---force", "---version", "2024-01-01"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	err := validateForceCall(c, "newservice")
	if err == nil {
		t.Fatal("auto-addressing should not count as fixed host for unlisted service")
	}
	if !strings.Contains(err.Error(), "endpoint-resolver=standard alone is not enough") {
		t.Fatalf("expected fixed-host guidance, got: %v", err)
	}
}

func TestValidateForceCallRejectsExplicitAutoAddressingEndpoint(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT", "")()
	c := NewContext()
	parser := NewParser([]string{"---force", "---version", "2024-01-01", "---endpoint", "auto-addressing"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	if err := validateForceCall(c, "newservice"); err == nil {
		t.Fatal("---endpoint auto-addressing should not pass unlisted fixed-host check")
	}
}

func TestValidateForceCallRejectsEnvHostWhenEnvResolverStandard(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT", "open.volcengineapi.com")()
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT_RESOLVER", "standard")()
	c := NewContext()
	parser := NewParser([]string{"---force", "---version", "2024-01-01"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	if err := validateForceCall(c, "newservice"); err == nil {
		t.Fatal("env host under standard resolver should not pass unlisted check")
	}
}

func TestValidateForceCallUsesNonCurrentProfileEndpoint(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT", "")()
	c := NewContext()
	c.config = &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {Name: "default", Endpoint: ""},
			"prod":    {Name: "prod", Endpoint: "prod.volcengineapi.com"},
		},
	}
	parser := NewParser([]string{"---force", "---version", "2024-01-01", "---profile", "prod"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	if err := validateForceCall(c, "newservice"); err != nil {
		t.Fatalf("---profile endpoint should satisfy check, got: %v", err)
	}
}

func TestValidateForceCallRejectsMissingProfile(t *testing.T) {
	c := NewContext()
	c.config = &Configure{
		Current:  "default",
		Profiles: map[string]*Profile{"default": {Name: "default", Endpoint: "open.volcengineapi.com"}},
	}
	parser := NewParser([]string{"---force", "---version", "2024-01-01", "---profile", "missing"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	err := validateForceCall(c, "newservice")
	if err == nil || !strings.Contains(err.Error(), `profile "missing" not found`) {
		t.Fatalf("expected missing profile error, got: %v", err)
	}
}

func TestValidateForceCallFallsBackToMetadataVersionForKnownService(t *testing.T) {
	c := NewContext()
	parser := NewParser([]string{"---force"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}
	if err := validateForceCall(c, "sts"); err != nil {
		t.Fatalf("known service should fall back to metadata version, got: %v", err)
	}
	if got := apiVersionForCall(c, "sts"); got == "" {
		t.Fatal("expected non-empty metadata version for sts")
	}
}

func TestCallSdkReturnsErrorWhenEndpointResolutionFails(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ACCESS_KEY", "ak-test")()
	defer setenvForTest(t, "VOLCENGINE_SECRET_KEY", "sk-test")()
	defer setenvForTest(t, "VOLCENGINE_REGION", "cn-beijing")()
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT", "")()
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT_RESOLVER", "standard")()

	c := NewContext()
	sdk, err := NewSimpleClient(c)
	if err != nil {
		t.Fatalf("NewSimpleClient returned error: %v", err)
	}

	_, err = sdk.CallSdk(SdkClientInfo{
		ServiceName: "definitely_unlisted_service",
		Action:      "DescribeResource",
		Version:     "2024-01-01",
		Method:      "GET",
	}, &map[string]interface{}{})
	if err == nil {
		t.Fatal("expected endpoint resolution error")
	}
	if !strings.Contains(err.Error(), "failed to initialize SDK client") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildForceInvocationInputSetsJSONBodyWithoutMetadata(t *testing.T) {
	c := NewContext()
	parser := NewParser([]string{"--SomeParam", "value"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}

	built, err := buildForceInvocationInput(c, "newservice", "NewAction", "application/json")
	if err != nil {
		t.Fatalf("buildForceInvocationInput returned error: %v", err)
	}
	if !built.jsonBody {
		t.Fatal("expected jsonBody for application/json content type")
	}
	if built.fromBody {
		t.Fatal("expected flat params without --body")
	}
	input, ok := built.value.(map[string]interface{})
	if !ok || input["SomeParam"] != "value" {
		t.Fatalf("unexpected fallback input: %#v", built.value)
	}
}

func TestBuildForceInvocationInputBodyWithoutMetadata(t *testing.T) {
	c := NewContext()
	parser := NewParser([]string{"--body", `{"probe":"ok"}`})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs: %v", err)
	}
	built, err := buildForceInvocationInput(c, "newservice", "NewAction", "application/json")
	if err != nil {
		t.Fatalf("buildForceInvocationInput: %v", err)
	}
	if !built.jsonBody || !built.fromBody {
		t.Fatalf("expected --body without metadata: jsonBody=%v fromBody=%v", built.jsonBody, built.fromBody)
	}
	// parseJSONBody returns *map[string]interface{}
	m, ok := built.value.(*map[string]interface{})
	if !ok {
		// also accept map form
		if mm, ok2 := built.value.(map[string]interface{}); ok2 {
			if mm["probe"] != "ok" {
				t.Fatalf("unexpected body: %#v", built.value)
			}
			return
		}
		t.Fatalf("unexpected body type: %#v", built.value)
	}
	if (*m)["probe"] != "ok" {
		t.Fatalf("unexpected body: %#v", *m)
	}
}

func TestResolveCallStyleContentTypeOverrideAndBodyDefault(t *testing.T) {
	c := NewContext()
	parser := NewParser([]string{
		"--header", "Content-Type=application/json; charset=utf-8",
		"--body", `{"a":1}`,
	})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs: %v", err)
	}
	_, ct, headers, err := resolveCallStyle(c, "definitely_unlisted_svc", "Act")
	if err != nil {
		t.Fatal(err)
	}
	if !isJSONContentType(ct) {
		t.Fatalf("content-type override = %q", ct)
	}
	if len(headers) != 1 {
		t.Fatalf("headers should be returned once from resolveCallStyle, got %#v", headers)
	}

	c2 := NewContext()
	parser2 := NewParser([]string{"--body", `{"a":1}`})
	if _, err := parser2.ReadArgs(c2); err != nil {
		t.Fatalf("ReadArgs: %v", err)
	}
	_, ct2, _, err := resolveCallStyle(c2, "definitely_unlisted_svc", "Act")
	if err != nil {
		t.Fatal(err)
	}
	if ct2 != "application/json" {
		t.Fatalf("expected --body to default content-type to application/json, got %q", ct2)
	}
}

func TestCollectRequestHeadersRepeatableAndParse(t *testing.T) {
	c := NewContext()
	parser := NewParser([]string{
		"--header", "X-Foo=bar",
		"--header", "X-Bar=baz=qux",
		"--header", "Content-Type=application/json",
	})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs: %v", err)
	}
	headers, err := collectRequestHeaders(c)
	if err != nil {
		t.Fatalf("collectRequestHeaders: %v", err)
	}
	if len(headers) != 3 {
		t.Fatalf("headers len = %d, want 3: %#v", len(headers), headers)
	}
	if headers[0].Name != "X-Foo" || headers[0].Value != "bar" {
		t.Fatalf("header[0] = %#v", headers[0])
	}
	if headers[1].Name != "X-Bar" || headers[1].Value != "baz=qux" {
		t.Fatalf("header[1] = %#v", headers[1])
	}
	ct, err := contentTypeFromHeaders(headers)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "application/json" {
		t.Fatalf("contentTypeFromHeaders = %q", ct)
	}

	// invalid format
	cBad := NewContext()
	parserBad := NewParser([]string{"--header", "NoEquals"})
	if _, err := parserBad.ReadArgs(cBad); err != nil {
		t.Fatalf("ReadArgs: %v", err)
	}
	if _, err := collectRequestHeaders(cBad); err == nil {
		t.Fatal("expected invalid --header error")
	}

	// blocked sensitive header
	cBlock := NewContext()
	parserBlock := NewParser([]string{"--header", "Authorization=secret"})
	if _, err := parserBlock.ReadArgs(cBlock); err != nil {
		t.Fatalf("ReadArgs: %v", err)
	}
	if _, err := collectRequestHeaders(cBlock); err == nil {
		t.Fatal("expected blocked Authorization header error")
	}

	// empty Content-Type value
	cEmpty := NewContext()
	parserEmpty := NewParser([]string{"--header", "Content-Type="})
	if _, err := parserEmpty.ReadArgs(cEmpty); err != nil {
		t.Fatalf("ReadArgs: %v", err)
	}
	headersEmpty, err := collectRequestHeaders(cEmpty)
	if err != nil {
		t.Fatalf("collectRequestHeaders: %v", err)
	}
	if _, err := contentTypeFromHeaders(headersEmpty); err == nil {
		t.Fatal("expected empty Content-Type error")
	}

	// --header must not become request body params
	input, fromBody, err := buildActionInput(c.dynamicFlags.flags, nil, true)
	if err != nil {
		t.Fatalf("buildActionInput: %v", err)
	}
	if fromBody {
		t.Fatal("did not pass --body")
	}
	m, ok := input.(map[string]interface{})
	if !ok {
		t.Fatalf("input type %T", input)
	}
	if _, exists := m["header"]; exists {
		t.Fatalf("header leaked into body: %#v", m)
	}
}

func TestIsJSONContentTypeMediaType(t *testing.T) {
	cases := map[string]bool{
		"application/json":                  true,
		"application/json; charset=utf-8":   true,
		"APPLICATION/JSON;charset=UTF-8":    true,
		" application/json ; charset=utf-8": true,
		"application/xml":                   false,
		"":                                  false,
	}
	for in, want := range cases {
		if got := isJSONContentType(in); got != want {
			t.Fatalf("isJSONContentType(%q)=%v want %v", in, got, want)
		}
	}
}

func TestContentTypeFromHeadersLastWins(t *testing.T) {
	headers := []requestHeader{
		{Name: "Content-Type", Value: "application/xml"},
		{Name: "X-Other", Value: "1"},
		{Name: "content-type", Value: "application/json; charset=utf-8"},
	}
	ct, err := contentTypeFromHeaders(headers)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "application/json; charset=utf-8" {
		t.Fatalf("last Content-Type wins: got %q", ct)
	}
}

func TestResolveCallStyleHeaderOverridesMetadataContentType(t *testing.T) {
	// Find a bundled action whose metadata Content-Type is non-empty and not
	// the override value, so we can prove --header wins over metadata.
	var svcName, actionName, metaCT string
	for _, svc := range rootSupport.GetAllSvc() {
		for _, action := range rootSupport.GetAllAction(svc) {
			apiInfo := rootSupport.GetApiInfo(svc, action)
			if apiInfo == nil || strings.TrimSpace(apiInfo.ContentType) == "" {
				continue
			}
			if isJSONContentType(apiInfo.ContentType) {
				// Prefer a JSON action so override to text/plain is clearly different.
				svcName, actionName, metaCT = svc, action, apiInfo.ContentType
				break
			}
			if svcName == "" {
				svcName, actionName, metaCT = svc, action, apiInfo.ContentType
			}
		}
		if svcName != "" && isJSONContentType(metaCT) {
			break
		}
	}
	if svcName == "" {
		t.Fatal("expected at least one action with ContentType metadata")
	}

	c := NewContext()
	parser := NewParser([]string{"--header", "Content-Type=text/plain"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs: %v", err)
	}
	_, ct, _, err := resolveCallStyle(c, svcName, actionName)
	if err != nil {
		t.Fatalf("resolveCallStyle: %v", err)
	}
	if ct != "text/plain" {
		t.Fatalf("expected --header Content-Type to override metadata %q, got %q", metaCT, ct)
	}
	if ct == metaCT {
		t.Fatalf("override should differ from metadata Content-Type %q", metaCT)
	}
}

func TestBuildForceInvocationInputUsesActionInputWithMetadata(t *testing.T) {
	for _, svc := range rootSupport.GetAllSvc() {
		for _, action := range rootSupport.GetAllAction(svc) {
			apiInfo := rootSupport.GetApiInfo(svc, action)
			if apiInfo == nil || !isJSONContentType(apiInfo.ContentType) {
				continue
			}
			if rootSupport.GetApiMeta(svc, action) == nil {
				continue
			}
			c := NewContext()
			parser := NewParser([]string{"--body", `{"probe":"ok"}`})
			if _, err := parser.ReadArgs(c); err != nil {
				t.Fatalf("ReadArgs returned error: %v", err)
			}
			built, err := buildForceInvocationInput(c, svc, action, apiInfo.ContentType)
			if err != nil {
				t.Fatalf("buildForceInvocationInput(%s,%s): %v", svc, action, err)
			}
			if !built.jsonBody || !built.fromBody {
				t.Fatalf("expected metadata JSON action to use buildActionInput, got jsonBody=%v fromBody=%v", built.jsonBody, built.fromBody)
			}
			return
		}
	}
	t.Fatal("expected at least one application/json action in metadata")
}

func TestBuildForceInvocationInputKeepsStringMetaLiteralForNonJSON(t *testing.T) {
	// With ApiMeta, force must match doAction: string-typed params stay literals even if JSON-looking.
	for _, svc := range rootSupport.GetAllSvc() {
		for _, action := range rootSupport.GetAllAction(svc) {
			apiInfo := rootSupport.GetApiInfo(svc, action)
			if apiInfo != nil && isJSONContentType(apiInfo.ContentType) {
				continue
			}
			meta := rootSupport.GetApiMeta(svc, action)
			if meta == nil || meta.Request == nil {
				continue
			}
			var stringParam string
			for name, mt := range meta.Request.MetaTypes {
				if mt != nil && mt.TypeName == "string" && !strings.Contains(name, ".") {
					stringParam = name
					break
				}
			}
			if stringParam == "" {
				continue
			}
			jsonLooking := `{"k":"v"}`
			c := NewContext()
			parser := NewParser([]string{"--" + stringParam, jsonLooking})
			if _, err := parser.ReadArgs(c); err != nil {
				t.Fatalf("ReadArgs: %v", err)
			}
			ct := ""
			if apiInfo != nil {
				ct = apiInfo.ContentType
			}
			built, err := buildForceInvocationInput(c, svc, action, ct)
			if err != nil {
				t.Fatalf("buildForceInvocationInput: %v", err)
			}
			input, ok := built.value.(map[string]interface{})
			if !ok {
				t.Fatalf("expected map input, got %#v", built.value)
			}
			if got, ok := input[stringParam].(string); !ok || got != jsonLooking {
				t.Fatalf("force+meta string param %s.%s %q should stay literal string, got %#v",
					svc, action, stringParam, input[stringParam])
			}
			return
		}
	}
	t.Skip("no non-JSON action with string meta param in bundled metadata")
}

func TestBuildForceInputOmitsFixedFlags(t *testing.T) {
	c := NewContext()
	parser := NewParser([]string{
		"---version", "2024-01-01",
		"---endpoint", "open.volcengineapi.com",
		"---force",
		"--SomeParam", "value",
		"--IPList", `["10.20.30.40"]`,
	})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs returned error: %v", err)
	}

	input := buildForceInput(c)
	if len(input) != 2 {
		t.Fatalf("expected 2 API params, got %#v", input)
	}
	if input["SomeParam"] != "value" {
		t.Fatalf("unexpected SomeParam: %#v", input["SomeParam"])
	}
	list, ok := input["IPList"].([]interface{})
	if !ok || len(list) != 1 {
		t.Fatalf("expected parsed IPList array, got %#v", input["IPList"])
	}
}

func TestTryExecuteGenericInvokeSkipsKnownService(t *testing.T) {
	err := tryExecuteGenericInvoke([]string{"sts", "GetCallerIdentity"})
	if !errors.Is(err, errNotGenericInvoke) {
		t.Fatalf("expected errNotGenericInvoke, got %v", err)
	}
}

func TestTryExecuteGenericInvokeSkipsBuiltinCommand(t *testing.T) {
	err := tryExecuteGenericInvoke([]string{"configure", "list"})
	if !errors.Is(err, errNotGenericInvoke) {
		t.Fatalf("expected errNotGenericInvoke, got %v", err)
	}
}

// ensureInitRootCmd 保证 help/version 等已注册。initRootCmd 非幂等（重复注册 flag 会 panic）。
func ensureInitRootCmd() {
	if rootCmd.Flags().Lookup("version") != nil {
		return
	}
	initRootCmd()
}

func TestTryExecuteGenericInvokeSkipsHelpCommand(t *testing.T) {
	// help/version 在 initRootCmd 中注册（与 runMain 一致）。
	ensureInitRootCmd()
	err := tryExecuteGenericInvoke([]string{"help"})
	if !errors.Is(err, errNotGenericInvoke) {
		t.Fatalf("expected errNotGenericInvoke, got %v", err)
	}
}

func TestIsRegisteredRootSubcommandCoversRootCommands(t *testing.T) {
	ensureInitRootCmd()

	// 动态覆盖：凡已挂到 rootCmd 的名称都必须被识别（新增根命令无需再改名单）。
	for _, c := range rootCmd.Commands() {
		name := c.Name()
		if !isRegisteredRootSubcommand(name) {
			t.Fatalf("isRegisteredRootSubcommand(%q) = false, want true", name)
		}
		if err := tryExecuteGenericInvoke([]string{name}); !errors.Is(err, errNotGenericInvoke) {
			t.Fatalf("tryExecuteGenericInvoke(%q) = %v, want errNotGenericInvoke", name, err)
		}
	}

	// 抽检若干 builtin / service
	for _, name := range []string{"configure", "login", "upgrade", "version", "sts", "help"} {
		if !isRegisteredRootSubcommand(name) {
			t.Fatalf("expected %q to be a registered root subcommand", name)
		}
	}
	if isRegisteredRootSubcommand("definitely-not-a-registered-service") {
		t.Fatal("unknown name should not be registered")
	}
}

func TestIsRegisteredRootSubcommandTracksNewRootCommand(t *testing.T) {
	const name = "force-reg-probe-cmd"
	if isRegisteredRootSubcommand(name) {
		t.Fatalf("%q unexpectedly already registered", name)
	}
	probe := &cobra.Command{Use: name, Hidden: true}
	rootCmd.AddCommand(probe)
	t.Cleanup(func() {
		rootCmd.RemoveCommand(probe)
	})
	if !isRegisteredRootSubcommand(name) {
		t.Fatalf("after AddCommand, isRegisteredRootSubcommand(%q) = false", name)
	}
	if err := tryExecuteGenericInvoke([]string{name, "AnyAction", "---force", "---version", "2024-01-01"}); !errors.Is(err, errNotGenericInvoke) {
		t.Fatalf("new root command should defer to cobra, got: %v", err)
	}
}

func TestTryExecuteGenericInvokeSkipsRootFlags(t *testing.T) {
	for _, arg := range []string{"-v", "--version", "-h", "--help", "-x", "--foo", "---force"} {
		t.Run(arg, func(t *testing.T) {
			err := tryExecuteGenericInvoke([]string{arg})
			if !errors.Is(err, errNotGenericInvoke) {
				t.Fatalf("expected errNotGenericInvoke, got %v", err)
			}
		})
	}
}

func TestTryExecuteGenericInvokeForceBeforeActionName(t *testing.T) {
	stubExecuteInvocation(t, errStubInvocation)
	err := tryExecuteGenericInvoke([]string{
		"newservice", "---version", "2024-01-01", "---endpoint", "newservice.cn-beijing.volcengineapi.com", "---force", "DescribeNewResource",
	})
	if !errors.Is(err, errStubInvocation) {
		t.Fatalf("expected stub invocation after force path, got: %v", err)
	}
	if strings.Contains(err.Error(), "use ---force with ---version") {
		t.Fatalf("---force before action should be recognized, got: %v", err)
	}
}

func TestTryExecuteGenericInvokeRequiresForce(t *testing.T) {
	err := tryExecuteGenericInvoke([]string{"newservice", "DescribeNewResource", "---version", "2024-01-01"})
	if err == nil {
		t.Fatal("expected error without ---force")
	}
	if !strings.Contains(err.Error(), "---force") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTryExecuteGenericInvokeDescriptionDashHIsNotHelp(t *testing.T) {
	// -h as an API flag value must not short-circuit to unknown-service help (nil error).
	err := tryExecuteGenericInvoke([]string{"newservice", "DescribeNewResource", "--Description", "-h"})
	if err == nil {
		t.Fatal("expected force validation error, got nil (help short-circuit?)")
	}
	if !strings.Contains(err.Error(), "---force") {
		t.Fatalf("expected ---force requirement after skipping help scan, got: %v", err)
	}
}

// TestForcePathAcceptsArgsAfterLanguageStrip 锁住 rebase 后的交界约定：
// ---lang 必须在 tryExecuteGenericInvoke 之前由 resolveLanguage 剥离；
// runMain 应传 processLanguageResolution.args，而不是 os.Args[1:]。
func TestForcePathAcceptsArgsAfterLanguageStrip(t *testing.T) {
	raw := []string{
		"newservice", "DescribeNewResource",
		"---version", "2024-01-01",
		"---endpoint", "newservice.cn-beijing.volcengineapi.com",
		"---force",
		"---lang", "ZH",
	}

	// 未剥离时 parser 会拒绝 ---lang。
	if _, err := parseInvocationArgs(raw[1:]); err == nil || !strings.Contains(err.Error(), "---lang") {
		t.Fatalf("expected parser to reject unstripped ---lang, got: %v", err)
	}

	stripped, lang, err := resolveLanguage(raw, func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("resolveLanguage: %v", err)
	}
	if lang != LanguageSimplifiedChinese {
		t.Fatalf("language = %v, want ZH", lang)
	}

	stubExecuteInvocation(t, errStubInvocation)
	if err := tryExecuteGenericInvoke(stripped); !errors.Is(err, errStubInvocation) {
		t.Fatalf("force path after language strip should reach invocation, got: %v", err)
	}
}

func TestPrintUnknownServiceHelp(t *testing.T) {
	oldStdout := os.Stdout
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("os.Pipe: %v", pipeErr)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = oldStdout })

	err := tryExecuteGenericInvoke([]string{"newservice", "-h"})
	w.Close()
	var buf bytes.Buffer
	if _, copyErr := io.Copy(&buf, r); copyErr != nil {
		t.Fatalf("read captured stdout: %v", copyErr)
	}
	if err != nil {
		t.Fatalf("expected help output without error, got: %v", err)
	}
	help := buf.String()
	if !strings.Contains(help, "Use ---force with ---version") {
		t.Fatalf("help should mention ---force and ---version, got:\n%s", help)
	}
	if !strings.Contains(help, "endpoint") {
		t.Fatalf("expected unknown service help text, got: %q", help)
	}
	// 与 root/service/action usage 共用 localizedFixedFlagsHelp，应包含 ---lang。
	for _, flag := range []string{"---force", "---version", "---method", "--header", "--body", "---lang"} {
		if !strings.Contains(help, flag) {
			t.Fatalf("unknown service help missing %q:\n%s", flag, help)
		}
	}
	if !strings.Contains(help, "Reserved double-dash") && !strings.Contains(help, "双横线保留") {
		t.Fatalf("help should separate reserved double-dash controls, got:\n%s", help)
	}
}

func TestTryExecuteGenericInvokeShowsHelpWithoutAction(t *testing.T) {
	err := tryExecuteGenericInvoke([]string{"newservice"})
	if err != nil {
		t.Fatalf("expected usage help without error, got: %v", err)
	}
}

func TestResolveCallStyleUsesMetadataWhenAvailable(t *testing.T) {
	c := NewContext()
	method, contentType, headers, err := resolveCallStyle(c, "sts", "GetCallerIdentity")
	if err != nil {
		t.Fatalf("resolveCallStyle: %v", err)
	}
	if method == "" {
		t.Fatal("expected method from metadata or default")
	}
	if headers != nil && len(headers) != 0 {
		t.Fatalf("expected no headers, got %#v", headers)
	}
	_ = contentType
}

func TestResolveCallStyleDefaultsToGETLikeNormalPath(t *testing.T) {
	c := NewContext()
	method, _, _, err := resolveCallStyle(c, "sts", "TotallyUnknownAction")
	if err != nil {
		t.Fatalf("resolveCallStyle: %v", err)
	}
	if method != "GET" {
		t.Fatalf("expected default GET, got %q", method)
	}
}

func TestExplicitHTTPMethodRejectsInvalidValue(t *testing.T) {
	c := NewContext()
	parser := NewParser([]string{"---method", "DELETE"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs: %v", err)
	}
	if _, err := explicitHTTPMethod(c); err == nil {
		t.Fatal("expected unsupported method error")
	}
}

func TestDispatchServiceActionRejectsUnknownWithoutForce(t *testing.T) {
	c := NewContext()
	err := dispatchServiceAction(c, "sts", "UnknownAction", false)
	if err == nil {
		t.Fatal("expected unsupported action error")
	}
	if !strings.Contains(err.Error(), "is not a supported action") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAPIVersionForCallPrefersFlag(t *testing.T) {
	c := NewContext()
	parser := NewParser([]string{"---version", "2099-01-01"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs: %v", err)
	}
	if got := apiVersionForCall(c, "sts"); got != "2099-01-01" {
		t.Fatalf("expected flag version, got %q", got)
	}
}

func TestTryExecuteGenericInvokeRequiresActionWhenOnlyFlags(t *testing.T) {
	err := tryExecuteGenericInvoke([]string{"newservice", "---version", "2024-01-01"})
	if err == nil {
		t.Fatal("expected error when only flags and no action")
	}
	if !strings.Contains(err.Error(), "specify an action name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunServiceCmdForceUnknownActionParsesFlags(t *testing.T) {
	captured := stubExecuteInvocation(t, errStubInvocation)
	err := runServiceCmd(&cobra.Command{}, "sts", []string{"GetCallerIdentity"},
		[]string{"UnknownAction", "---version", "2024-01-01", "---force"})
	if !errors.Is(err, errStubInvocation) {
		t.Fatalf("expected stub invocation error, got: %v", err)
	}
	if captured.action != "UnknownAction" || captured.version != "2024-01-01" {
		t.Fatalf("unexpected invocation params: action=%q version=%q", captured.action, captured.version)
	}
}
