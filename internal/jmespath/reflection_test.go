package jmespath

import (
	"reflect"
	"strings"
	"testing"
)

// The jpArray type check admits any slice, so the array functions must cope
// with named slice types instead of asserting []interface{} and panicking.
func TestArrayFunctionsAcceptNamedSliceTypes(t *testing.T) {
	data := map[string]interface{}{
		"Strings": []string{"c", "a", "b"},
	}
	cases := []struct {
		expr string
		want interface{}
	}{
		{"contains(Strings, 'a')", true},
		{"contains(Strings, 'zz')", false},
		{"reverse(Strings)", []interface{}{"b", "a", "c"}},
		{"map(&@, Strings)", []interface{}{"c", "a", "b"}},
		{"sort_by(Strings, &@)", []interface{}{"a", "b", "c"}},
		{"max_by(Strings, &@)", "c"},
		{"min_by(Strings, &@)", "a"},
	}
	for _, tc := range cases {
		got, err := Search(tc.expr, data)
		if err != nil {
			t.Errorf("Search(%q): %v", tc.expr, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Search(%q) = %#v, want %#v", tc.expr, got, tc.want)
		}
	}
}

// Widening a named slice must not turn a genuine type error into a panic.
func TestArrayFunctionsStillRejectNonArrays(t *testing.T) {
	data := map[string]interface{}{"Object": map[string]interface{}{"a": 1}}
	for _, expr := range []string{"reverse(Object)", "sort_by(Object, &@)", "map(&@, Object)"} {
		if _, err := Search(expr, data); err == nil {
			t.Errorf("Search(%q) = nil error, want a type error", expr)
		}
	}
}

// Variadic functions type-check their arguments too, so a handler never has to
// assert the type itself.
func TestVariadicFunctionsTypeCheckArguments(t *testing.T) {
	data := map[string]interface{}{
		"Object": map[string]interface{}{"a": "1"},
		"Other":  map[string]interface{}{"b": "2"},
		"Text":   "not-an-object",
		"List":   []interface{}{"x"},
	}
	for _, expr := range []string{"merge(Text)", "merge(Object, Text)", "merge(Object, List)", "merge(Missing)"} {
		got, err := Search(expr, data)
		if err == nil {
			t.Errorf("Search(%q) = %#v, want a type error", expr, got)
			continue
		}
		// The message reaches the user verbatim, so it must not leak Go syntax.
		if strings.Contains(err.Error(), "jpType") || strings.Contains(err.Error(), "[]interface {}") {
			t.Errorf("Search(%q) leaked Go syntax: %v", expr, err)
		}
		if !strings.Contains(err.Error(), "expected: object") {
			t.Errorf("Search(%q) = %v, want the expected type named", expr, err)
		}
	}

	got, err := Search("merge(Object, Other)", data)
	if err != nil {
		t.Fatalf("merge on objects: %v", err)
	}
	want := map[string]interface{}{"a": "1", "b": "2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merge = %#v, want %#v", got, want)
	}

	// not_null accepts any type, including absent keys.
	if got, err := Search("not_null(Missing, Text)", data); err != nil || got != "not-an-object" {
		t.Fatalf("not_null = %#v, err=%v", got, err)
	}
}

// A type error names the offending value, but a large response must not be
// dumped into the message wholesale.
func TestTypeErrorBoundsTheReportedValue(t *testing.T) {
	big := make([]interface{}, 200)
	for i := range big {
		big[i] = map[string]interface{}{"InstanceID": "i-0123456789abcdef"}
	}
	_, err := Search("merge(Items)", map[string]interface{}{"Items": big})
	if err == nil {
		t.Fatal("expected a type error")
	}
	if len(err.Error()) > 200 {
		t.Fatalf("error message not bounded (%d bytes): %v", len(err.Error()), err)
	}
	if !strings.HasSuffix(err.Error(), "expected: object") {
		t.Fatalf("unexpected message: %v", err)
	}

	// A short value is still reported in full.
	_, err = Search("merge(Text)", map[string]interface{}{"Text": "req-1"})
	if err == nil || !strings.Contains(err.Error(), "req-1") {
		t.Fatalf("short value should survive: %v", err)
	}
}

// A field lookup on a pointer to a non-struct has nothing to resolve; it must
// yield null rather than panic inside reflect.
func TestFieldFromStructHandlesPointerToNonStruct(t *testing.T) {
	number := 3
	numbers := []int{1, 2}
	for _, value := range []interface{}{&number, &numbers, new(string), new(map[string]int)} {
		got, err := Search("foo", value)
		if err != nil {
			t.Errorf("Search on %T: %v", value, err)
			continue
		}
		if got != nil {
			t.Errorf("Search on %T = %#v, want nil", value, got)
		}
	}
}

func TestFieldFromStructResolvesPointerToStruct(t *testing.T) {
	type instance struct {
		Name string
	}
	got, err := Search("name", &instance{Name: "web"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got != "web" {
		t.Fatalf("Search = %#v, want \"web\"", got)
	}

	nested := map[string]interface{}{"Item": &instance{Name: "db"}}
	got, err = Search("Item.name", nested)
	if err != nil {
		t.Fatalf("Search nested: %v", err)
	}
	if got != "db" {
		t.Fatalf("Search nested = %#v, want \"db\"", got)
	}
}
