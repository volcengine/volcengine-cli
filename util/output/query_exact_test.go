package output

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestQueryStructuralProjectionPreservesExactJSONNumbers(t *testing.T) {
	large := json.Number("9007199254740993")
	decimal := json.Number("0.10000000000000001")
	data := map[string]interface{}{
		"Result": map[string]interface{}{
			"Large":   large,
			"Decimal": decimal,
		},
	}

	got, err := ApplyQuery(data, "Result.{Large:Large,Decimal:Decimal}")
	if err != nil {
		t.Fatal(err)
	}
	result := got.(map[string]interface{})
	if result["Large"] != large || result["Decimal"] != decimal {
		t.Fatalf("projection changed exact numbers: %#v", result)
	}
}

func TestQueryEqualityPreservesExactJSONNumbers(t *testing.T) {
	data := map[string]interface{}{
		"Large":     json.Number("9007199254740993"),
		"SameLarge": json.Number("9007199254740993"),
		"Different": json.Number("9007199254740992"),
	}
	for _, tc := range []struct {
		expr string
		want bool
	}{
		{"Large == SameLarge", true},
		{"Large != SameLarge", false},
		{"Large == Different", false},
		{"Large != Different", true},
	} {
		got, err := ApplyQuery(data, tc.expr)
		if err != nil || got != tc.want {
			t.Errorf("ApplyQuery(%q) = %#v, %v; want %v", tc.expr, got, err, tc.want)
		}
	}
}

func TestQueryEqualitySupportsStructuresLengthsAndScalarFields(t *testing.T) {
	shared := []interface{}{
		map[string]interface{}{"Id": json.Number("9007199254740993")},
		"web",
	}
	data := map[string]interface{}{
		"ObjectA":  map[string]interface{}{"Items": shared},
		"ObjectB":  map[string]interface{}{"Items": append([]interface{}(nil), shared...)},
		"ListA":    []interface{}{"a", "b"},
		"ListB":    []interface{}{"a", "b"},
		"Short":    []interface{}{"a"},
		"NameA":    "web",
		"NameB":    "web",
		"EnabledA": true,
		"EnabledB": true,
		"MissingA": nil,
		"MissingB": nil,
	}
	for _, tc := range []struct {
		expr string
		want bool
	}{
		{"ObjectA == ObjectB", true},
		{"ListA == ListB", true},
		{"length(ListA) == length(ListB)", true},
		{"length(ListA) == length(Short)", false},
		{"NameA == NameB", true},
		{"EnabledA == EnabledB", true},
		{"MissingA == MissingB", true},
	} {
		got, err := ApplyQuery(data, tc.expr)
		if err != nil || got != tc.want {
			t.Errorf("ApplyQuery(%q) = %#v, %v; want %v", tc.expr, got, err, tc.want)
		}
	}
}

func TestQueryAverageCannotBeRewrittenFromUnrelatedSourceNumber(t *testing.T) {
	data := map[string]interface{}{
		"Decimal": json.Number("0.10000000000000001"),
		"Items":   []interface{}{json.Number("0.05"), json.Number("0.15")},
	}
	if _, err := ApplyQuery(data, "avg(Items)"); err == nil {
		t.Fatal("avg was evaluated through an inexact float path")
	}
}

func TestQuerySafeStringFilterStillWorksWithJSONNumbersPresent(t *testing.T) {
	data := map[string]interface{}{
		"Items": []interface{}{
			map[string]interface{}{"Name": "web", "Id": json.Number("9007199254740993")},
			map[string]interface{}{"Name": "db", "Id": json.Number("9007199254740992")},
		},
	}
	got, err := ApplyQuery(data, "Items[?Name=='web'].Id")
	if err != nil {
		t.Fatal(err)
	}
	ids := got.([]interface{})
	if len(ids) != 1 || ids[0] != json.Number("9007199254740993") {
		t.Fatalf("safe string filter lost exact ID: %#v", got)
	}
}

func TestQueryBooleanAndNullEqualityStillWorksWithJSONNumbersPresent(t *testing.T) {
	data := map[string]interface{}{
		"Items": []interface{}{
			map[string]interface{}{
				"Name": "enabled", "Enabled": true, "DeletedAt": nil,
				"Id": json.Number("9007199254740993"),
			},
			map[string]interface{}{
				"Name": "disabled", "Enabled": false, "DeletedAt": "2026-08-20",
				"Id": json.Number("9007199254740992"),
			},
		},
	}

	cases := []struct {
		expr string
		want string
	}{
		{"Items[?Enabled == `true`].Name | [0]", "enabled"},
		{"Items[?`false` == Enabled].Name | [0]", "disabled"},
		{"Items[?DeletedAt == `null`].Name | [0]", "enabled"},
		{"Items[?`null` != DeletedAt].Name | [0]", "disabled"},
	}
	for _, tc := range cases {
		got, err := ApplyQuery(data, tc.expr)
		if err != nil {
			t.Errorf("safe comparison %q failed: %v", tc.expr, err)
			continue
		}
		if got != tc.want {
			t.Errorf("safe comparison %q = %#v, want %q", tc.expr, got, tc.want)
		}
	}
}

func TestQueryRejectsEscapingNumericJSONLiterals(t *testing.T) {
	for _, expr := range []string{
		"`9007199254740993`",
		"`{\"N\":9007199254740993}`",
		"`[9007199254740993,9007199254740992]`",
		"{N:`9007199254740993`}",
		"[`9007199254740993`]",
		"Items[].`9007199254740993`",
		"values(`{\"N\":9007199254740993}`)",
		"to_array(`9007199254740993`)",
		"reverse(`[9007199254740993]`)",
		"map(&N, `[{\"N\":9007199254740993}]`)",
		"merge(`{\"N\":9007199254740993}`, Other)",
		"not_null(Missing, `9007199254740993`)",
		"to_string(`9007199254740993`)",
	} {
		if _, err := CompileQuery(expr); err == nil {
			t.Errorf("numeric JSON literal escaped validation in %q", expr)
		}
	}
}

func TestQueryAllowsShapeOnlyNumericJSONLiterals(t *testing.T) {
	cases := []struct {
		expr string
		want interface{}
	}{
		{"length(`[9007199254740993]`)", float64(1)},
		{"length(( `[9007199254740993,9007199254740992]` ))", float64(2)},
	}
	for _, tc := range cases {
		got, err := ApplyQuery(nil, tc.expr)
		if err != nil {
			t.Errorf("shape-only query %q failed: %v", tc.expr, err)
			continue
		}
		if got != tc.want {
			t.Errorf("shape-only query %q = %#v, want %#v", tc.expr, got, tc.want)
		}
	}

	got, err := ApplyQuery(nil, "keys(`{\"N\":9007199254740993}`)")
	if err != nil {
		t.Fatal(err)
	}
	keys := got.([]interface{})
	if len(keys) != 1 || keys[0] != "N" {
		t.Fatalf("keys(shape-only literal) = %#v, want [N]", got)
	}
}

func TestQueryContainsSupportsResponseStringsAndArrays(t *testing.T) {
	data := map[string]interface{}{
		"Result": map[string]interface{}{
			"Name": "web-server",
			"Tags": []interface{}{"prod", "web"},
			"Numbers": []interface{}{
				json.Number("9007199254740993"),
			},
			"ExactNumber":     json.Number("9007199254740993"),
			"DifferentNumber": json.Number("9007199254740992"),
		},
	}
	cases := []struct {
		expr string
		want bool
	}{
		{"contains(Result.Name, 'web')", true},
		{"contains(Result.Tags, 'web')", true},
		{"contains(Result.Tags, 'missing')", false},
		{"contains(Result.Numbers, Result.ExactNumber)", true},
		{"contains(Result.Numbers, Result.DifferentNumber)", false},
	}
	for _, tc := range cases {
		query, err := CompileQuery(tc.expr)
		if err != nil {
			t.Errorf("CompileQuery(%q): %v", tc.expr, err)
			continue
		}
		got, err := query.Search(data)
		if err != nil {
			t.Errorf("Search(%q): %v", tc.expr, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Search(%q) = %#v, want %v", tc.expr, got, tc.want)
		}
	}
}

func TestQueryContainsRejectsNumericJSONLiterals(t *testing.T) {
	for _, expr := range []string{
		"contains(Result.Numbers, `9007199254740993`)",
		"contains(`[9007199254740993]`, Result.Number)",
		"contains(not_null(`[9007199254740993]`, 'unused'), '9007199254740992')",
	} {
		if _, err := CompileQuery(expr); err == nil {
			t.Errorf("numeric JSON literal in contains escaped validation: %q", expr)
		}
	}
}

func TestQueryStringOrderingFunctionsRemainAvailable(t *testing.T) {
	data := map[string]interface{}{
		"Result": map[string]interface{}{
			"Instances": []interface{}{
				map[string]interface{}{"InstanceId": "i-web"},
				map[string]interface{}{"InstanceId": "i-db"},
			},
		},
	}
	cases := []struct {
		expr string
		want interface{}
	}{
		{"sort_by(Result.Instances, &InstanceId)[].InstanceId", []interface{}{"i-db", "i-web"}},
		{"max_by(Result.Instances, &InstanceId).InstanceId", "i-web"},
		{"min_by(Result.Instances, &InstanceId).InstanceId", "i-db"},
		{"type('web')", "string"},
		{"sort(`[\"web\",\"db\"]`)", []interface{}{"db", "web"}},
		{"max(`[\"web\",\"db\"]`)", "web"},
		{"min(`[\"web\",\"db\"]`)", "db"},
	}
	for _, tc := range cases {
		got, err := ApplyQuery(data, tc.expr)
		if err != nil {
			t.Errorf("ApplyQuery(%q): %v", tc.expr, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ApplyQuery(%q) = %#v, want %#v", tc.expr, got, tc.want)
		}
	}
}

func TestQueryOrderingFunctionsRejectExactResponseNumbers(t *testing.T) {
	data := map[string]interface{}{
		"Items": []interface{}{
			map[string]interface{}{"Id": "large", "Size": json.Number("9007199254740993")},
			map[string]interface{}{"Id": "small", "Size": json.Number("9007199254740992")},
		},
		"Exact": json.Number("9007199254740993"),
	}
	for _, expr := range []string{
		"sort_by(Items, &Size)",
		"max_by(Items, &Size)",
		"min_by(Items, &Size)",
		"type(Exact)",
	} {
		query, err := CompileQuery(expr)
		if err != nil {
			t.Errorf("CompileQuery(%q): %v", expr, err)
			continue
		}
		if got, err := query.Search(data); err == nil {
			t.Errorf("Search(%q) = %#v, want explicit json.Number type error", expr, got)
		}
	}
}

func TestQueryScalarOrderingAcceptsResponseStringsAndRejectsNumbersExplicitly(t *testing.T) {
	stringsData := map[string]interface{}{"Items": []interface{}{"web", "db"}}
	for _, tc := range []struct {
		expr string
		want interface{}
	}{
		{"sort(Items)", []interface{}{"db", "web"}},
		{"max(Items)", "web"},
		{"min(Items)", "db"},
	} {
		got, err := ApplyQuery(stringsData, tc.expr)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ApplyQuery(%q) = %#v, %v; want %#v", tc.expr, got, err, tc.want)
		}
	}

	numbers := map[string]interface{}{
		"Items": []interface{}{json.Number("9007199254740993"), json.Number("9007199254740992")},
	}
	for _, expr := range []string{
		"sort(Items)",
		"max(Items)",
		"min(Items)",
	} {
		query, err := CompileQuery(expr)
		if err != nil {
			t.Errorf("CompileQuery(%q): %v", expr, err)
			continue
		}
		if got, err := query.Search(numbers); err == nil {
			t.Errorf("Search(%q) = %#v, want explicit json.Number type error", expr, got)
		}
	}
}

func TestQueryNumericJSONLiteralsRemainRejectedInEqualityAndOrdering(t *testing.T) {
	for _, expr := range []string{
		"N == `1`",
		"`[1]` == Values",
		"Object == `{\"N\":1}`",
		"sort(`[9007199254740993]`)",
		"max(`[\"web\",9007199254740993]`)",
	} {
		if _, err := CompileQuery(expr); err == nil {
			t.Errorf("numeric JSON literal query %q was accepted", expr)
		}
	}
}
