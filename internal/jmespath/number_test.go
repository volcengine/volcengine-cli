package jmespath

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompareNumbersUsesExactJSONDecimalValues(t *testing.T) {
	cases := []struct {
		left, right interface{}
		want        int
	}{
		{json.Number("8"), json.Number("4"), 1},
		{json.Number("8"), json.Number("8.0"), 0},
		{json.Number("8"), json.Number("8.1"), -1},
		{json.Number("9007199254740993"), json.Number("9007199254740992"), 1},
		{json.Number("1e3"), json.Number("1000"), 0},
		{json.Number("-0"), json.Number("0"), 0},
		{json.Number("1"), float64(1), 0},
		{json.Number("2"), float64(1), 1},
	}
	for _, tc := range cases {
		got, ok := compareNumbers(tc.left, tc.right)
		if !ok {
			t.Errorf("compareNumbers(%v, %v) not numeric", tc.left, tc.right)
			continue
		}
		if got != tc.want {
			t.Errorf("compareNumbers(%v, %v) = %d, want %d", tc.left, tc.right, got, tc.want)
		}
	}
}

func TestIntegerFromNumberCeilFloor(t *testing.T) {
	cases := []struct {
		in    interface{}
		ceil  json.Number
		floor json.Number
	}{
		{json.Number("1.2"), "2", "1"},
		{json.Number("-1.2"), "-1", "-2"},
		{json.Number("2"), "2", "2"},
		{float64(1.2), "2", "1"},
	}
	for _, tc := range cases {
		got, err := integerFromNumber(tc.in, true)
		if err != nil || got != tc.ceil {
			t.Errorf("ceil(%v) = %q, %v; want %q", tc.in, got, err, tc.ceil)
		}
		got, err = integerFromNumber(tc.in, false)
		if err != nil || got != tc.floor {
			t.Errorf("floor(%v) = %q, %v; want %q", tc.in, got, err, tc.floor)
		}
	}
}

func TestCanonicalJSONNumberHandlesHugeTokensWithoutValueExpansion(t *testing.T) {
	hugeExponent := "1e" + strings.Repeat("9", 70000)
	canonical, ok := CanonicalJSONNumber(hugeExponent)
	if !ok || canonical != hugeExponent {
		t.Fatalf("huge exponent canonicalization failed: ok=%v len=%d", ok, len(canonical))
	}

	hugeMantissa := "1" + strings.Repeat("0", 70000)
	canonical, ok = CanonicalJSONNumber(hugeMantissa)
	if !ok || canonical != "1e70000" {
		t.Fatalf("huge mantissa canonicalization = %q, %v", canonical, ok)
	}
}

func TestArithmeticKeepsEveryFiniteDecimalDigit(t *testing.T) {
	longDecimal := "0." + strings.Repeat("1234567890", 6)
	cases := []struct {
		expr string
		data interface{}
		want json.Number
	}{
		{"abs(N)", json.Number("-" + longDecimal), json.Number(longDecimal)},
		{"abs(N)", json.Number("-1e-50"), json.Number("1e-50")},
		{"abs(N)", json.Number("-0"), json.Number("0")},
		{"sum(N)", []interface{}{json.Number("1e-50"), json.Number("0")}, json.Number("0." + strings.Repeat("0", 49) + "1")},
		{"sum(N)", []interface{}{json.Number("9007199254740993"), json.Number("1")}, json.Number("9007199254740994")},
		{"avg(N)", []interface{}{json.Number("0.05"), json.Number("0.15")}, json.Number("0.1")},
		{"ceil(N)", json.Number("9007199254740993.5"), json.Number("9007199254740994")},
	}
	for _, tc := range cases {
		got, err := Search(tc.expr, map[string]interface{}{"N": tc.data})
		if err != nil {
			t.Errorf("Search(%q, %v) failed: %v", tc.expr, tc.data, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Search(%q, %v) = %#v, want %v", tc.expr, tc.data, got, tc.want)
		}
	}
}

// Only avg() can produce a repeating decimal. It must round rather than run
// forever, but a tiny magnitude must not round all the way down to zero.
func TestAverageRoundsRepeatingDecimalsWithSignificantDigits(t *testing.T) {
	got, err := Search("avg(N)", map[string]interface{}{
		"N": []interface{}{json.Number("1"), json.Number("0"), json.Number("0")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != json.Number("0."+strings.Repeat("3", roundedSignificantDigits)) {
		t.Fatalf("avg of repeating decimal = %#v", got)
	}

	got, err = Search("avg(N)", map[string]interface{}{
		"N": []interface{}{json.Number("1e-50"), json.Number("0"), json.Number("0")},
	})
	if err != nil {
		t.Fatal(err)
	}
	small, ok := got.(json.Number)
	if !ok || small.String() != "0."+strings.Repeat("0", 50)+strings.Repeat("3", roundedSignificantDigits) {
		t.Fatalf("avg of tiny repeating decimal = %#v, want %d significant digits", got, roundedSignificantDigits)
	}
}

// Comparison and abs() stay exact for any token, while arithmetic that would
// have to expand a huge exponent into digits fails with an explicit message.
func TestArithmeticRefusesTokensTooLargeToExpand(t *testing.T) {
	huge := json.Number("-1e20000")
	data := map[string]interface{}{"N": huge, "Items": []interface{}{huge}}
	for _, expr := range []string{"sum(Items)", "avg(Items)", "ceil(N)", "floor(N)"} {
		_, err := Search(expr, data)
		if err == nil || !strings.Contains(err.Error(), "too large for exact arithmetic") {
			t.Errorf("Search(%q) error = %v, want a too-large-for-exact-arithmetic error", expr, err)
		}
	}
	for _, tc := range []struct {
		expr string
		want interface{}
	}{
		{"abs(N)", json.Number("1e20000")},
		{"N < `1`", true},
		{"max(Items)", huge},
	} {
		got, err := Search(tc.expr, data)
		if err != nil || got != tc.want {
			t.Errorf("Search(%q) = %#v, %v; want %#v", tc.expr, got, err, tc.want)
		}
	}
}

func TestJSONLiteralNumbersStayJSONNumber(t *testing.T) {
	got, err := Search("`9007199254740993`", nil)
	if err != nil {
		t.Fatal(err)
	}
	n, ok := got.(json.Number)
	if !ok || n.String() != "9007199254740993" {
		t.Fatalf("literal = %#v, want json.Number 9007199254740993", got)
	}
}
