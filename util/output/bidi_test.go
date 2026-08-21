package output

import (
	"bytes"
	"strings"
	"testing"
)

const (
	dangerousBidiControls = "\u061c\u200e\u200f\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069"
	escapedBidiControls   = `\u061C\u200E\u200F\u202A\u202B\u202C\u202D\u202E\u2066\u2067\u2068\u2069`
	safeShapingText       = "woman: \U0001F469\u200D\U0001F4BB; composed: e\u0301"
)

func TestEscapeCellStringEscapesDangerousBidiControls(t *testing.T) {
	input := "before" + dangerousBidiControls + safeShapingText + "after"
	want := "before" + escapedBidiControls + safeShapingText + "after"

	if got := escapeCellString(input); got != want {
		t.Fatalf("escapeCellString() = %q, want %q", got, want)
	}
}

func TestTableAndTextEscapeDangerousBidiControlsInCells(t *testing.T) {
	data := []interface{}{map[string]interface{}{
		"Value": "before" + dangerousBidiControls + safeShapingText + "after",
	}}

	for _, format := range []Format{FormatTable, FormatText} {
		t.Run(string(format), func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteWithOptions(&buf, format, data, Options{TerminalWidth: -1}); err != nil {
				t.Fatal(err)
			}
			assertDangerousBidiControlsEscaped(t, buf.String())
			if !strings.Contains(buf.String(), safeShapingText) {
				t.Fatalf("%s changed ZWJ or combining text: %q", format, buf.String())
			}
		})
	}
}

func TestTableEscapesDangerousBidiControlsInKeysAndHeaders(t *testing.T) {
	key := "field" + dangerousBidiControls + safeShapingText
	cases := []struct {
		name string
		data interface{}
	}{
		{name: "key", data: map[string]interface{}{key: "value"}},
		{name: "header", data: []interface{}{map[string]interface{}{key: "value"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteWithOptions(&buf, FormatTable, tc.data, Options{TerminalWidth: -1}); err != nil {
				t.Fatal(err)
			}
			out := buf.String()
			assertDangerousBidiControlsEscaped(t, out)
			if !strings.Contains(out, "field"+escapedBidiControls+safeShapingText) {
				t.Fatalf("table changed the escaped key/header: %q", out)
			}
		})
	}
}

func TestTableEscapesDangerousBidiControlsInSectionTitle(t *testing.T) {
	key := "nested" + dangerousBidiControls + safeShapingText
	data := map[string]interface{}{
		key: []interface{}{map[string]interface{}{"K": "v"}},
	}

	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, data, Options{TerminalWidth: -1}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	assertDangerousBidiControlsEscaped(t, out)
	if !strings.Contains(out, "nested"+escapedBidiControls+safeShapingText+"\n") {
		t.Fatalf("table changed the escaped section title: %q", out)
	}
}

func TestTextEscapesDangerousBidiControlsInNestedValues(t *testing.T) {
	unsafeText := "before" + dangerousBidiControls + safeShapingText + "after"
	safeText := "before" + escapedBidiControls + safeShapingText + "after"
	cases := []struct {
		name string
		cell interface{}
		want string
	}{
		{
			// A nested object is flattened onto one row prefixed by its key.
			name: "map",
			cell: map[string]interface{}{"literal": "slash\\tab\t", "value": unsafeText},
			want: "NESTED\tslash\\\\tab\\t\t" + safeText,
		},
		{
			// A nested scalar list emits one prefixed line per element.
			name: "slice",
			cell: []interface{}{"slash\\tab\t", unsafeText},
			want: "NESTED\tslash\\\\tab\\t\nNESTED\t" + safeText,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Write(&buf, FormatText, map[string]interface{}{"Nested": tc.cell}); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSuffix(buf.String(), "\n"); got != tc.want {
				t.Fatalf("nested value = %q, want %q", got, tc.want)
			}
			assertDangerousBidiControlsEscaped(t, buf.String())
		})
	}
}

func TestTextEscapesControlCharactersInNestedFieldPrefixes(t *testing.T) {
	unsafeKey := "nested\ncolumn\tosc\x1b]52;c;payload\a" + dangerousBidiControls
	safePrefix := "NESTED\\nCOLUMN\\tOSC\\x1B]52;C;PAYLOAD\\x07" + escapedBidiControls
	cases := []struct {
		name string
		cell interface{}
		want string
	}{
		{name: "scalar list", cell: []interface{}{"one", "two"}, want: safePrefix + "\tone\n" + safePrefix + "\ttwo\n"},
		{name: "object", cell: map[string]interface{}{"A": "one", "B": "two"}, want: safePrefix + "\tone\ttwo\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Write(&buf, FormatText, map[string]interface{}{unsafeKey: tc.cell}); err != nil {
				t.Fatal(err)
			}
			if got := buf.String(); got != tc.want {
				t.Fatalf("nested field output = %q, want %q", got, tc.want)
			}
			assertNoRawTerminalControls(t, buf.String())
		})
	}
}

func TestTextEscapesControlCharactersInQuotedQueryAliasPrefix(t *testing.T) {
	query, err := CompileQuery(`{"nested\ncolumn\tosc\u001b]52;c;payload\u0007bidi\u202e": Source}`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := query.Search(map[string]interface{}{"Source": []interface{}{"value"}})
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatText, result, Options{Columns: query.Columns(), Queried: true}); err != nil {
		t.Fatal(err)
	}
	want := "NESTED\\nCOLUMN\\tOSC\\x1B]52;C;PAYLOAD\\x07BIDI\\u202E\tvalue\n"
	if got := buf.String(); got != want {
		t.Fatalf("quoted alias output = %q, want %q", got, want)
	}
	assertNoRawTerminalControls(t, buf.String())
}

func assertNoRawTerminalControls(t *testing.T, output string) {
	t.Helper()
	for _, raw := range []string{"\r", "\x1b", "\a", "\u009b"} {
		if strings.Contains(output, raw) {
			t.Fatalf("output contains raw terminal control %q: %q", raw, output)
		}
	}
	for _, r := range dangerousBidiControls {
		if strings.ContainsRune(output, r) {
			t.Fatalf("output contains raw bidi control U+%04X: %q", r, output)
		}
	}
}

func TestTableEscapesDangerousBidiControlsInMaxDepthJSONCell(t *testing.T) {
	unsafeText := "before" + dangerousBidiControls + safeShapingText + "after"
	var data interface{} = map[string]interface{}{
		"literal": "slash\\tab\t",
		"value":   unsafeText,
	}
	// buildSections falls back to a compact JSON cell once depth exceeds 8.
	for i := 0; i < 9; i++ {
		data = map[string]interface{}{"Nested": data}
	}

	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, data, Options{TerminalWidth: -1}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	assertDangerousBidiControlsEscaped(t, out)
	if !strings.Contains(out, `"literal":"slash\\tab\t"`) {
		t.Fatalf("table double-escaped ordinary JSON escapes: %q", out)
	}
	if !strings.Contains(out, escapedBidiControls+safeShapingText) {
		t.Fatalf("table changed escaped bidi or safe shaping text: %q", out)
	}
}

func TestCompactJSONEscapesRawTerminalControlsWithoutDoubleEscaping(t *testing.T) {
	got := compactJSON(map[string]interface{}{
		"value": "line\ncolumn\tosc\x1b]52;c;x" +
			"\u0085\u009b\u202e" +
			`slash\literal`,
	})
	want := `{"value":"line\ncolumn\tosc\u001b]52;c;x\u0085\u009B\u202Eslash\\literal"}`

	if got != want {
		t.Fatalf("compactJSON() = %q, want %q", got, want)
	}
}

func assertDangerousBidiControlsEscaped(t *testing.T, output string) {
	t.Helper()
	for _, r := range dangerousBidiControls {
		if strings.ContainsRune(output, r) {
			t.Fatalf("output contains raw bidi control U+%04X: %q", r, output)
		}
	}
	if !strings.Contains(output, escapedBidiControls) {
		t.Fatalf("output is missing escaped bidi controls %q: %q", escapedBidiControls, output)
	}
}
