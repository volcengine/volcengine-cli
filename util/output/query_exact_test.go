package output

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jmespath/go-jmespath"
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

func TestQueryEqualityUsesJSONNumericValue(t *testing.T) {
	data := map[string]interface{}{
		"Integer":      json.Number("1"),
		"Decimal":      json.Number("1.0"),
		"Exponent":     json.Number("1e3"),
		"Thousand":     json.Number("1000"),
		"NegativeZero": json.Number("-0"),
		"Zero":         json.Number("0"),
		"Other":        json.Number("1.0000000000000000001"),
	}
	for _, tc := range []struct {
		expr string
		want bool
	}{
		{"Integer == Decimal", true},
		{"Exponent == Thousand", true},
		{"NegativeZero == Zero", true},
		{"Integer != Other", true},
	} {
		got, err := ApplyQuery(data, tc.expr)
		if err != nil || got != tc.want {
			t.Errorf("ApplyQuery(%q) = %#v, %v; want %v", tc.expr, got, err, tc.want)
		}
	}
}

func TestQueryRecursiveEqualityUsesJSONNumericValue(t *testing.T) {
	data := map[string]interface{}{
		"Left": map[string]interface{}{
			"Items": []interface{}{json.Number("1"), json.Number("1e3"), json.Number("-0")},
		},
		"Right": map[string]interface{}{
			"Items": []interface{}{json.Number("1.0"), json.Number("1000"), json.Number("0")},
		},
	}
	got, err := ApplyQuery(data, "Left == Right")
	if err != nil || got != true {
		t.Fatalf("recursive numeric equality = %#v, %v; want true", got, err)
	}
}

func TestQueryProjectionAndToStringKeepOriginalNumberTokens(t *testing.T) {
	data := map[string]interface{}{
		"A":      json.Number("1"),
		"B":      json.Number("1.0"),
		"Nested": map[string]interface{}{"N": json.Number("1e3")},
	}
	got, err := ApplyQuery(data, "{Eq:A==B,A:A,B:B,BText:to_string(B),NestedText:to_string(Nested)}")
	if err != nil {
		t.Fatal(err)
	}
	result := got.(map[string]interface{})
	if result["Eq"] != true || result["A"] != json.Number("1") || result["B"] != json.Number("1.0") {
		t.Fatalf("projection changed equality or source tokens: %#v", result)
	}
	if result["BText"] != "1.0" || result["NestedText"] != `{"N":1e3}` {
		t.Fatalf("to_string changed source tokens: %#v", result)
	}
}

func TestQueryCanonicalJSONNumberHandlesHugeTokensWithoutValueExpansion(t *testing.T) {
	hugeExponent := "1e" + strings.Repeat("9", 70000)
	canonical, ok := canonicalJSONNumber(hugeExponent)
	if !ok || canonical != hugeExponent {
		t.Fatalf("huge exponent canonicalization failed: ok=%v len=%d", ok, len(canonical))
	}

	hugeMantissa := "1" + strings.Repeat("0", 70000)
	canonical, ok = canonicalJSONNumber(hugeMantissa)
	if !ok || canonical != "1e70000" {
		t.Fatalf("huge mantissa canonicalization = %q, %v", canonical, ok)
	}

	got, err := ApplyQuery(map[string]interface{}{
		"A": json.Number(hugeExponent),
		"B": json.Number(hugeExponent),
	}, "A == B")
	if err != nil || got != true {
		t.Fatalf("huge number equality = %#v, %v; want true", got, err)
	}
}

func TestQueryExactNumberSearchIsConcurrentSafe(t *testing.T) {
	query, err := CompileQuery("{Eq:A==B,Text:to_string(B),B:B}")
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]interface{}{"A": json.Number("1"), "B": json.Number("1.0")}
	var wg sync.WaitGroup
	errors := make(chan string, 32)
	for i := 0; i < cap(errors); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, searchErr := query.Search(data)
			if searchErr != nil {
				errors <- searchErr.Error()
				return
			}
			result := got.(map[string]interface{})
			if result["Eq"] != true || result["Text"] != "1.0" || result["B"] != json.Number("1.0") {
				errors <- "unexpected concurrent result"
			}
		}()
	}
	wg.Wait()
	close(errors)
	for message := range errors {
		t.Error(message)
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

func TestQueryAllowsNumbersDerivedWithoutReadingResponseNumbers(t *testing.T) {
	data := map[string]interface{}{
		"A":     []interface{}{"a", "b"},
		"B":     []interface{}{"x"},
		"Items": []interface{}{json.Number("9007199254740993"), json.Number("1.0")},
	}
	for _, tc := range []struct {
		expr string
		want interface{}
	}{
		{"length(A) > length(B)", true},
		{"(length(A)) >= (length(B))", true},
		{"abs(length(Items))", float64(2)},
		{"ceil(length(Items))", float64(2)},
		{"floor(abs(length(Items)))", float64(2)},
		{"to_number('42')", float64(42)},
		{"to_number('4.2e1') == length(Items)", false},
		{"abs(to_number('-2')) == length(Items)", true},
		{"length(`[9007199254740993]`) < length(Items)", true},
	} {
		got, err := ApplyQuery(data, tc.expr)
		if err != nil {
			t.Errorf("safe derived-number query %q failed: %v", tc.expr, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("safe derived-number query %q = %#v, want %#v", tc.expr, got, tc.want)
		}
	}
}

func TestQueryRejectsNumericOperationsThatMayReadResponseNumbers(t *testing.T) {
	for _, expr := range []string{
		"abs(N)",
		"ceil(N)",
		"floor(N)",
		"to_number(N)",
		"N > length(B)",
		"length(A) > N",
		"N || length(A) > length(B)",
		"length(A) > length(B) || N",
		"(N || length(A)) > length(B)",
		"length(A) > (length(B) || N)",
		"Foo | length(A) > length(B)",
		"Items[?length(A) > length(B)]",
		"N == length(A)",
		"length(A) == N",
		"N == to_number('1')",
		"to_number('1') != N",
	} {
		if _, err := CompileQuery(expr); err == nil {
			t.Errorf("potential response-number query %q was accepted", expr)
		}
	}
}

func TestQueryRejectsNestedMixedNumericEquality(t *testing.T) {
	for _, expr := range []string{
		"{Eq:length(A)==N}",
		"{Eq:N==length(A)}",
		"[length(A)==N]",
		"[N==length(A)]",
		"not_null(length(A)==N, false)",
		"not_null(N==length(A), false)",
		"Flag && length(A)==N",
		"Flag && N==length(A)",
		"Items[?length(Tags)==Count]",
		"Items[?Count==length(Tags)]",
		"map(&length(Tags)==Count, Items)",
		"map(&Count==length(Tags), Items)",
		"map(&(length(Tags)==Count), Items)",
		"map(&(Count==length(Tags)), Items)",
	} {
		if _, err := CompileQuery(expr); err == nil {
			t.Errorf("nested mixed numeric equality %q was accepted", expr)
		}
	}
}

func TestUpstreamJMESPathSilentlyMiscomparesNestedMixedNumbers(t *testing.T) {
	data := map[string]interface{}{
		"A":    []interface{}{"x"},
		"N":    json.Number("1"),
		"Flag": true,
		"Items": []interface{}{map[string]interface{}{
			"Tags":  []interface{}{"x"},
			"Count": json.Number("1"),
		}},
	}
	for _, tc := range []struct {
		expr string
		want interface{}
	}{
		{"{Eq:length(A)==N}", map[string]interface{}{"Eq": false}},
		{"{Eq:N==length(A)}", map[string]interface{}{"Eq": false}},
		{"[length(A)==N]", []interface{}{false}},
		{"[N==length(A)]", []interface{}{false}},
		{"not_null(length(A)==N, false)", false},
		{"not_null(N==length(A), false)", false},
		{"Flag && length(A)==N", false},
		{"Flag && N==length(A)", false},
	} {
		got, err := jmespath.Search(tc.expr, data)
		if err != nil {
			t.Fatalf("upstream probe %q failed unexpectedly: %v", tc.expr, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("upstream probe %q = %#v, want silent wrong result %#v", tc.expr, got, tc.want)
		}
	}
	for _, expr := range []string{
		"Items[?length(Tags)==Count]",
		"Items[?Count==length(Tags)]",
		"map(&length(Tags)==Count, Items)",
		"map(&Count==length(Tags), Items)",
		"map(&(length(Tags)==Count), Items)",
		"map(&(Count==length(Tags)), Items)",
	} {
		got, err := jmespath.Search(expr, data)
		if err != nil {
			t.Fatal(err)
		}
		items, ok := got.([]interface{})
		if !ok || (strings.HasPrefix(expr, "Items") && len(items) != 0) ||
			(strings.HasPrefix(expr, "map") && !reflect.DeepEqual(items, []interface{}{false})) {
			t.Fatalf("upstream mixed-number collection %q = %#v, want silent wrong result", expr, got)
		}
	}
}

func TestQueryAllowsNestedSafeNumericEquality(t *testing.T) {
	data := map[string]interface{}{
		"A": []interface{}{"x"},
		"B": []interface{}{"y"},
		"Items": []interface{}{map[string]interface{}{
			"Tags":  []interface{}{"x"},
			"Names": []interface{}{"y"},
		}},
	}
	for _, expr := range []string{
		"{Eq:length(A)==length(B)}",
		"[length(A)==to_number('1')]",
		"not_null(length(A)==length(B), `false`)",
		"Flag && length(A)==length(B)",
		"Items[?length(Tags)==length(Names)]",
		"map(&length(Tags)==length(Names), Items)",
		"map(&(length(Tags)==length(Names)), Items)",
	} {
		if _, err := ApplyQuery(data, expr); err != nil {
			t.Errorf("nested safe numeric equality %q failed: %v", expr, err)
		}
	}
}

func TestQuerySafeNumericFunctionsStillRejectNumericJSONLiterals(t *testing.T) {
	for _, expr := range []string{
		"abs(`1`)",
		"ceil(`1.2`)",
		"floor(`1.2`)",
		"to_number(`42`)",
		"length(A) > `1`",
		"to_number('42') > `1`",
		"length(`1`)",
		"keys(`[1]`)",
	} {
		if _, err := CompileQuery(expr); err == nil {
			t.Errorf("numeric JSON literal query %q was accepted", expr)
		}
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

func TestQueryContainsKeepsSameTokenNumberMembership(t *testing.T) {
	data := map[string]interface{}{
		"Numbers": []interface{}{json.Number("1.0")},
		"Needle":  json.Number("1.0"),
	}
	got, err := ApplyQuery(data, "contains(Numbers, Needle)")
	if err != nil || got != true {
		t.Fatalf("same-token numeric contains = %#v, %v; want true", got, err)
	}
}

func TestQueryContainsUsesJSONNumericValue(t *testing.T) {
	for _, tc := range []struct {
		name   string
		member json.Number
		needle json.Number
	}{
		{name: "integer member", member: json.Number("1"), needle: json.Number("1.0")},
		{name: "decimal member", member: json.Number("1.0"), needle: json.Number("1")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := map[string]interface{}{
				"Numbers": []interface{}{tc.member},
				"Needle":  tc.needle,
			}
			got, err := ApplyQuery(data, "contains(Numbers, Needle)")
			if err != nil || got != true {
				t.Fatalf("equivalent-token numeric contains = %#v, %v; want true", got, err)
			}
		})
	}
}

func TestQueryObjectProjectionDoesNotEvaluateSyntheticValues(t *testing.T) {
	data := map[string]interface{}{
		"Values": map[string]interface{}{
			"integer": json.Number("1"),
			"decimal": json.Number("1.0"),
		},
	}
	for _, expr := range []string{
		"Values.*",
		"Values.*.not_null(@, 'fallback')",
	} {
		got, err := ApplyQuery(data, expr)
		if err != nil {
			t.Fatal(err)
		}
		values, ok := got.([]interface{})
		if !ok {
			t.Fatalf("object projection %q = %#v, want a list", expr, got)
		}
		if len(values) != 2 {
			t.Fatalf("object projection %q = %#v, want exactly the two source values", expr, values)
		}
		for _, value := range values {
			if value != json.Number("1") && value != json.Number("1.0") {
				t.Fatalf("object projection %q produced synthetic value %#v", expr, value)
			}
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

func TestQuerySortByDoesNotMutateSharedInput(t *testing.T) {
	query, err := CompileQuery("sort_by(Items, &Name)[].Name")
	if err != nil {
		t.Fatal(err)
	}
	items := []interface{}{
		map[string]interface{}{"Name": "web"},
		map[string]interface{}{"Name": "db"},
	}
	data := map[string]interface{}{"Items": items}

	got, err := query.Search(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []interface{}{"db", "web"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sort_by result = %#v, want %#v", got, want)
	}
	if items[0].(map[string]interface{})["Name"] != "web" || items[1].(map[string]interface{})["Name"] != "db" {
		t.Fatalf("sort_by mutated input: %#v", items)
	}
}

func TestQuerySortByKeepsEmptyArrayShape(t *testing.T) {
	got, err := ApplyQuery(map[string]interface{}{"Items": []interface{}{}}, "sort_by(Items, &Name)")
	if err != nil {
		t.Fatal(err)
	}
	items, ok := got.([]interface{})
	if !ok || items == nil || len(items) != 0 {
		t.Fatalf("empty sort_by result = %#v, want non-nil empty array", got)
	}
}

func TestQuerySortBySharedInputIsConcurrentSafe(t *testing.T) {
	query, err := CompileQuery("sort_by(Items, &Name)[].Name")
	if err != nil {
		t.Fatal(err)
	}
	items := make([]interface{}, 64)
	for i := range items {
		items[i] = map[string]interface{}{"Name": fmt.Sprintf("item-%03d", len(items)-i)}
	}
	data := map[string]interface{}{"Items": items}

	start := make(chan struct{})
	errors := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < cap(errors); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, searchErr := query.Search(data)
			if searchErr != nil {
				errors <- searchErr
				return
			}
			values := result.([]interface{})
			if len(values) != len(items) || values[0] != "item-001" || values[len(values)-1] != "item-064" {
				errors <- fmt.Errorf("unexpected sorted result: %#v", values)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errors)
	for searchErr := range errors {
		t.Error(searchErr)
	}
	if items[0].(map[string]interface{})["Name"] != "item-064" {
		t.Fatalf("concurrent sort_by mutated input: %#v", items[0])
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

func TestQueryByFunctionsRejectSingleExactResponseNumber(t *testing.T) {
	data := map[string]interface{}{
		"Items": []interface{}{
			map[string]interface{}{"Id": "large", "Size": json.Number("9007199254740993")},
		},
	}
	for _, expr := range []string{
		"sort_by(Items, &Size)",
		"max_by(Items, &Size)",
		"min_by(Items, &Size)",
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
