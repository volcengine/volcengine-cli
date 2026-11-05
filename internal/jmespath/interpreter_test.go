package jmespath

import "testing"

// A projection must surface an error from its left-hand side instead of
// returning a silent null.
func TestProjectionsReturnLeftHandSideErrors(t *testing.T) {
	data := map[string]interface{}{"a": 1}
	for _, expr := range []string{"sort(@)[]", "sort(@)[?@]", "sort(@).*"} {
		if got, err := Search(expr, data); err == nil {
			t.Errorf("Search(%q) = %#v, nil error; want a type error", expr, got)
		}
	}
}
