package output

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestColumnOrderExtractsWrittenOrder(t *testing.T) {
	cases := []struct {
		expr string
		want []string
	}{
		// The whole point: written order, not alphabetical.
		{"Result.Instances[].{Name:InstanceName,Id:InstanceId,Status:Status}",
			[]string{"Name", "Id", "Status"}},
		{"Result.Instances[*].{Zed:A,Alpha:B}", []string{"Zed", "Alpha"}},
		// Quoted keys, including non-ASCII labels.
		{`Result.Instances[].{"实例ID":InstanceId,"状态":Status}`,
			[]string{"实例ID", "状态"}},
		{`a[].{"with space":x,"with:colon":y}`,
			[]string{"with space", "with:colon"}},
		{`a[].{"esc\"quote":x,plain:y}`, []string{`esc"quote`, "plain"}},
		{`a[].{"\u005A":x,"\u4E2D":y}`, []string{"Z", "中"}},
		{`a[].{"\uD83D\uDE80":x,plain:y}`, []string{"🚀", "plain"}},
		// Whitespace around keys and values.
		{"a[].{ First : b , Second : c }", []string{"First", "Second"}},
		// Filter before the hash still exposes the shaping hash.
		{"Result.Instances[?Status=='Running'].{I:InstanceId,S:Status}",
			[]string{"I", "S"}},
		// Commas inside nested calls/brackets must not split key pairs.
		{"a[].{N:length(b),M:c[0]}", []string{"N", "M"}},
	}
	for _, c := range cases {
		got := columnOrder(c.expr)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("columnOrder(%q) = %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestColumnOrderReturnsNilWhenNoUsableHint(t *testing.T) {
	// Each of these must fall back to alphabetical rendering.
	for _, expr := range []string{
		"",
		"Result.Instances[]",                     // no hash at all
		"Result.Instances[].[InstanceId,Status]", // multiselect list, not a hash
		"Result",
		"merge(a, b)",
		"{a:x,a:y}",      // duplicate keys collapse in the evaluated map
		"{}",             // empty hash
		"{a:x",           // unbalanced brace
		"a[].{no_colon}", // key without value
		"a[].{a b:x}",    // space inside a bare identifier
		"a[].{'sq':x}",   // raw-string literal is not a valid key form
	} {
		if got := columnOrder(expr); got != nil {
			t.Errorf("columnOrder(%q) = %v, want nil", expr, got)
		}
	}
}

func TestApplyColumnOrderOnlyWhenKeySetMatches(t *testing.T) {
	sorted := []string{"Id", "Name", "Status"}
	cases := []struct {
		name string
		hint []string
		want []string
	}{
		{"exact match reorders", []string{"Name", "Id", "Status"},
			[]string{"Name", "Id", "Status"}},
		{"nil hint keeps sorted", nil, sorted},
		{"missing key keeps sorted", []string{"Name", "Id"}, sorted},
		{"extra key keeps sorted",
			[]string{"Name", "Id", "Status", "Extra"}, sorted},
		{"unknown key keeps sorted", []string{"Name", "Id", "Nope"}, sorted},
	}
	for _, c := range cases {
		got := applyColumnOrder(sorted, c.hint)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// End-to-end: the order written in --query is the order rendered.
func TestTableFollowsQueryColumnOrder(t *testing.T) {
	q, err := CompileQuery("Result.Instances[].{Name:InstanceId,Id:InstanceId,Status:Status}")
	if err != nil {
		t.Fatalf("CompileQuery: %v", err)
	}
	rows, err := q.Search(sampleEnvelope())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, rows, Options{Columns: q.Columns()}); err != nil {
		t.Fatalf("WriteWithOptions: %v", err)
	}
	header := firstLineContaining(buf.String(), "Name")
	nameAt, idAt, statusAt := strings.Index(header, "Name"),
		strings.Index(header, "Id"), strings.Index(header, "Status")
	if nameAt < 0 || idAt < 0 || statusAt < 0 || !(nameAt < idAt && idAt < statusAt) {
		t.Fatalf("column order Name,Id,Status not preserved:\n%s", buf.String())
	}
}

func TestTableFollowsUnicodeEscapedAliasOrder(t *testing.T) {
	q, err := CompileQuery(`Result.Instances[].{"\u005A":InstanceId,A:Status}`)
	if err != nil {
		t.Fatalf("CompileQuery: %v", err)
	}
	rows, err := q.Search(sampleEnvelope())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, rows, Options{Columns: q.Columns()}); err != nil {
		t.Fatalf("WriteWithOptions: %v", err)
	}
	header := firstLineContaining(buf.String(), "Z")
	if zAt, aAt := strings.Index(header, "Z"), strings.Index(header, "A"); zAt < 0 || aAt < 0 || zAt >= aAt {
		t.Fatalf("escaped alias order Z,A not preserved:\n%s", buf.String())
	}
}

// Without a hint, behaviour must stay exactly as before: alphabetical.
func TestTableKeepsAlphabeticalWithoutHint(t *testing.T) {
	data := []interface{}{
		map[string]interface{}{"Zeta": "1", "Alpha": "2"},
	}
	var buf bytes.Buffer
	if err := Write(&buf, FormatTable, data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	header := firstLineContaining(buf.String(), "Alpha")
	if strings.Index(header, "Alpha") > strings.Index(header, "Zeta") {
		t.Fatalf("expected alphabetical fallback:\n%s", buf.String())
	}
}

// Text shares the column-order path with table.
func TestTextFollowsQueryColumnOrder(t *testing.T) {
	rows := []interface{}{
		map[string]interface{}{"Id": "i-1", "Name": "web", "Status": "Running"},
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatText, rows,
		Options{Columns: []string{"Name", "Status", "Id"}}); err != nil {
		t.Fatalf("WriteWithOptions: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "web\tRunning\ti-1" {
		t.Fatalf("text column order = %q, want web\\tRunning\\ti-1", got)
	}
}

func TestTextKeepsQueryScalarOrderWhenStructuredColumnRecurses(t *testing.T) {
	rows := []interface{}{
		map[string]interface{}{
			"Z":       "last alphabetically",
			"NestedZ": map[string]interface{}{"Value": "child-z"},
			"NestedA": map[string]interface{}{"Value": "child-a"},
			"A":       "first alphabetically",
		},
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatText, rows,
		Options{Columns: []string{"Z", "NestedZ", "NestedA", "A"}}); err != nil {
		t.Fatalf("WriteWithOptions: %v", err)
	}
	want := "last alphabetically\tfirst alphabetically\nNESTEDZ\tchild-z\nNESTEDA\tchild-a\n"
	if got := buf.String(); got != want {
		t.Fatalf("text scalar subsequence order = %q, want %q", got, want)
	}
}

func TestTextKeepsQueryNestedOrderForMixedStructuredColumn(t *testing.T) {
	rows := []interface{}{
		map[string]interface{}{
			"Z":       "z-1",
			"NestedA": map[string]interface{}{"Value": "a-1"},
			"Mixed":   "plain",
		},
		map[string]interface{}{
			"Z":       "z-2",
			"NestedA": map[string]interface{}{"Value": "a-2"},
			"Mixed":   map[string]interface{}{"Value": "mixed-2"},
		},
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatText, rows,
		Options{Columns: []string{"Z", "NestedA", "Mixed"}}); err != nil {
		t.Fatalf("WriteWithOptions: %v", err)
	}
	want := "z-1\tplain\nNESTEDA\ta-1\nz-2\tNone\nNESTEDA\ta-2\nMIXED\tmixed-2\n"
	if got := buf.String(); got != want {
		t.Fatalf("text nested subsequence order = %q, want %q", got, want)
	}
}

func TestTableNumAddsRowNumbersFromOne(t *testing.T) {
	data := []interface{}{
		map[string]interface{}{"Id": "i-1", "Status": "Running"},
		map[string]interface{}{"Id": "i-2", "Status": "Stopped"},
		map[string]interface{}{"Id": "i-3", "Status": "Running"},
	}
	var buf bytes.Buffer
	if err := Write(&buf, FormatTableNum, data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()
	if !strings.Contains(firstLineContaining(out, "Id"), "#") {
		t.Fatalf("missing # header:\n%s", out)
	}
	for i, id := range []string{"i-1", "i-2", "i-3"} {
		line := firstLineContaining(out, id)
		want := []string{"1", "2", "3"}[i]
		if !strings.Contains(line, "| "+want+" ") {
			t.Errorf("row %s numbered wrong, want %s, got %q", id, want, line)
		}
	}
}

// Row numbers must not disturb the requested column order.
func TestTableNumKeepsColumnOrder(t *testing.T) {
	rows := []interface{}{
		map[string]interface{}{"Id": "i-1", "Name": "web"},
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTableNum, rows,
		Options{Columns: []string{"Name", "Id"}}); err != nil {
		t.Fatalf("WriteWithOptions: %v", err)
	}
	header := firstLineContaining(buf.String(), "Name")
	hashAt, nameAt, idAt := strings.Index(header, "#"),
		strings.Index(header, "Name"), strings.Index(header, "Id")
	if !(hashAt < nameAt && nameAt < idAt) {
		t.Fatalf("want #,Name,Id order, got:\n%s", buf.String())
	}
}

// A single object renders as one horizontal record (field-name header row +
// value row), matching the AWS CLI. table-num therefore numbers it as record 1.
func TestTableNumNumbersSingleObjectAsOneRecord(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTableNum,
		map[string]interface{}{"Id": "i-1", "Status": "Running"},
		Options{TerminalWidth: -1}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()
	header := firstLineContaining(out, "Id")
	if !strings.Contains(header, "#") {
		t.Fatalf("single object under table-num should gain a # header:\n%s", out)
	}
	if !strings.Contains(out, "| 1 ") {
		t.Fatalf("single object should be numbered as record 1:\n%s", out)
	}
}

func TestTableNumLabelsRowNumbersForListProjection(t *testing.T) {
	data := []interface{}{
		[]interface{}{"i-1", "Running"},
		[]interface{}{"i-2", "Stopped"},
	}
	var buf bytes.Buffer
	if err := Write(&buf, FormatTableNum, data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 4 || !strings.Contains(lines[1], "| # ") {
		t.Fatalf("list projection is missing the # header:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "| 1 ") || !strings.Contains(buf.String(), "| 2 ") {
		t.Fatalf("list projection row numbers are missing:\n%s", buf.String())
	}
}

func TestTableNumOnEmptyListStaysEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, FormatTableNum, []interface{}{}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "(empty)" {
		t.Fatalf("got %q, want (empty)", buf.String())
	}
}

// Numbered tables must stay aligned, including with wide characters.
func TestTableNumStaysAligned(t *testing.T) {
	data := []interface{}{
		map[string]interface{}{"City": "中文", "Name": "first"},
		map[string]interface{}{"City": "Tokyo", "Name": "second"},
	}
	var buf bytes.Buffer
	if err := Write(&buf, FormatTableNum, data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	want := displayWidth(lines[0])
	for i, line := range lines {
		if got := displayWidth(line); got != want {
			t.Fatalf("line %d width=%d, want %d:\n%s", i+1, got, want, buf.String())
		}
	}
}

func TestParseFormatAcceptsTableNum(t *testing.T) {
	for _, in := range []string{"table-num", "TABLE-NUM", " table-num "} {
		got, err := ParseFormat(in)
		if err != nil || got != FormatTableNum {
			t.Fatalf("ParseFormat(%q) = %v, %v", in, got, err)
		}
	}
	_, err := ParseFormat("table_num")
	if err == nil || !strings.Contains(err.Error(), "table-num") {
		t.Fatalf("error should list table-num as supported, got %v", err)
	}
}

// Column order is a table/text concern only; YAML stays deterministic so
// downstream consumers (yq, k8s) see a stable key order.
func TestYAMLIgnoresColumnOrderHint(t *testing.T) {
	data := []interface{}{
		map[string]interface{}{"Zeta": "1", "Alpha": "2"},
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatYAML, data,
		Options{Columns: []string{"Zeta", "Alpha"}}); err != nil {
		t.Fatalf("WriteWithOptions: %v", err)
	}
	out := buf.String()
	if strings.Index(out, "Alpha") > strings.Index(out, "Zeta") {
		t.Fatalf("YAML keys must stay sorted:\n%s", out)
	}
}

// A malformed or exotic expression must never break rendering: no panic, and
// output falls back to alphabetical order.
func TestColumnOrderNeverBreaksRendering(t *testing.T) {
	rows := []interface{}{
		map[string]interface{}{"B": "1", "A": "2"},
	}
	for _, expr := range []string{
		"{a:x", "}{", "a[].{", `a[].{"unterminated:x}`, "{{{}}}", "",
	} {
		hint := columnOrder(expr)
		var buf bytes.Buffer
		if err := WriteWithOptions(&buf, FormatTable, rows, Options{Columns: hint}); err != nil {
			t.Fatalf("expr %q: %v", expr, err)
		}
		header := firstLineContaining(buf.String(), "A")
		if strings.Index(header, "A") > strings.Index(header, "B") {
			t.Fatalf("expr %q should fall back to sorted:\n%s", expr, buf.String())
		}
	}
}

func firstLineContaining(s, substr string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, substr) {
			return line
		}
	}
	return ""
}
