package jmespath

import (
	"encoding/json"
	"testing"
)

// sort of an empty array is still an array; it must encode as [] like sort_by.
func TestSortEmptyArrayStaysArray(t *testing.T) {
	for _, expr := range []string{"sort(`[]`)", "sort_by(`[]`, &name)"} {
		got, err := Search(expr, nil)
		if err != nil {
			t.Fatalf("Search(%q): %v", expr, err)
		}
		body, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		if string(body) != "[]" {
			t.Fatalf("Search(%q) = %s, want []", expr, body)
		}
	}
}
