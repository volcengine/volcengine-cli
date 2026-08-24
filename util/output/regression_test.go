package output

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// --- Bug 1: width probing must survive the checkedWriter wrapper -----------

// WriteWithOptions wraps the caller's writer, so a plain *os.File assertion
// inside writeTable always failed and width detection silently never ran.
func TestStdoutForWidthUnwrapsWrapper(t *testing.T) {
	if got := stdoutForWidth(&checkedWriter{Writer: os.Stdout}); got != os.Stdout {
		t.Fatalf("wrapped os.Stdout not detected, got %v", got)
	}
	// Nested wrappers must also resolve.
	nested := &checkedWriter{Writer: &checkedWriter{Writer: os.Stdout}}
	if got := stdoutForWidth(nested); got != os.Stdout {
		t.Fatalf("nested wrapper not detected, got %v", got)
	}
	// A buffer has no terminal width.
	if got := stdoutForWidth(&checkedWriter{Writer: &bytes.Buffer{}}); got != nil {
		t.Fatalf("buffer should yield nil, got %v", got)
	}
	// The chain the renderers actually receive must resolve too. Buffering the
	// output added a layer that carries no Unwrap of its own, which would end
	// the walk early and disable column fitting on a real terminal.
	buffered, _ := renderWriters(os.Stdout)
	if got := stdoutForWidth(buffered); got != os.Stdout {
		t.Fatalf("production writer chain hides os.Stdout, got %v", got)
	}
}

// The renderers write a line at a time; without buffering that is one syscall
// per line at an unbuffered os.Stdout.
func TestRenderedOutputIsBatchedIntoFewWrites(t *testing.T) {
	list := make([]interface{}, 0, 500)
	for i := 0; i < 500; i++ {
		list = append(list, map[string]interface{}{
			"Id": "i-" + itoa(i), "Status": "RUNNING", "Zone": "cn-beijing-a",
		})
	}

	for _, format := range []Format{FormatText, FormatTable} {
		counter := &writeCounter{}
		if err := WriteWithOptions(counter, format, list,
			Options{TerminalWidth: -1}); err != nil {
			t.Fatal(err)
		}
		if counter.lines < 500 {
			t.Fatalf("%s wrote %d bytes, expected a large rendering",
				format, counter.bytes)
		}
		if counter.writes > 32 {
			t.Fatalf("%s made %d write calls for %d bytes, expected them batched",
				format, counter.writes, counter.bytes)
		}
	}
}

type writeCounter struct {
	writes, bytes, lines int
}

func (c *writeCounter) Write(p []byte) (int, error) {
	c.writes++
	c.bytes += len(p)
	c.lines += bytes.Count(p, []byte("\n"))
	return len(p), nil
}

// Unknown width (buffer, pipe, failed probe) must keep the horizontal layout.
// Previously termWidth<=0 was treated as "verticalize", so every single-record
// result turned into a Field|Value table on the real production path.
func TestUnknownWidthKeepsHorizontalLayout(t *testing.T) {
	rows := []interface{}{
		map[string]interface{}{"A": "1", "B": "2", "C": "3", "D": "4"},
	}
	for _, width := range []int{0, -1} {
		var buf bytes.Buffer
		if err := WriteWithOptions(&buf, FormatTable, rows,
			Options{TerminalWidth: width}); err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		if strings.Contains(out, "Field") {
			t.Fatalf("width=%d must not verticalize:\n%s", width, out)
		}
		if !strings.Contains(out, "| A ") {
			t.Fatalf("width=%d lost the horizontal header:\n%s", width, out)
		}
	}
}

// Unknown width must also skip truncation, so redirected output stays complete.
func TestUnknownWidthSkipsTruncation(t *testing.T) {
	long := strings.Repeat("x", 300)
	rows := []interface{}{map[string]interface{}{"L": long, "S": "a"}}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, rows, Options{TerminalWidth: 0}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), long) {
		t.Fatalf("content was truncated without a known width:\n%s", buf.String())
	}
}

// --- Bug 2: the column-order hint must reach single objects ---------------

// `--query '{Name:A,Id:B}'` on a non-list result produces one map. Scripts read
// text output positionally, so alphabetical order hands them the wrong field.
func TestSingleObjectTextFollowsColumnHint(t *testing.T) {
	obj := map[string]interface{}{"Name": "alice", "Id": "u-1"}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatText, obj,
		Options{Columns: []string{"Name", "Id"}}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != "alice\tu-1" {
		t.Fatalf("hint ignored: got %q, want %q", got, "alice\tu-1")
	}
}

func TestSingleObjectTableFollowsColumnHint(t *testing.T) {
	obj := map[string]interface{}{"Name": "alice", "Id": "u-1"}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, obj,
		Options{Columns: []string{"Name", "Id"}, TerminalWidth: -1}); err != nil {
		t.Fatal(err)
	}
	nameAt := strings.Index(buf.String(), "Name")
	idAt := strings.Index(buf.String(), "Id")
	if nameAt < 0 || idAt < 0 {
		t.Fatalf("missing keys:\n%s", buf.String())
	}
	if nameAt > idAt {
		t.Fatalf("Name should precede Id:\n%s", buf.String())
	}
}

// A hint that does not match the data must be discarded, never applied
// partially and never able to drop a field.
func TestSingleObjectRejectsMismatchedHint(t *testing.T) {
	obj := map[string]interface{}{"Name": "alice", "Id": "u-1"}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatText, obj,
		Options{Columns: []string{"Nope", "Zzz"}}); err != nil {
		t.Fatal(err)
	}
	// Falls back to alphabetical: Id then Name.
	if got := strings.TrimSpace(buf.String()); got != "u-1\talice" {
		t.Fatalf("bad hint should fall back to alphabetical, got %q", got)
	}
}

// --- Bug 3: a heterogeneous column must not lose values ------------------

// One nested row used to remove the whole column, silently discarding the
// scalar/null/empty values of every other row.
func TestHeterogeneousNestedColumnKeepsAllValues(t *testing.T) {
	list := []interface{}{
		map[string]interface{}{"Id": "i-1", "Tags": []interface{}{
			map[string]interface{}{"K": "env", "V": "prod"}}},
		map[string]interface{}{"Id": "i-2", "Tags": nil},
		map[string]interface{}{"Id": "i-3", "Tags": []interface{}{}},
		map[string]interface{}{"Id": "i-4", "Tags": "plain-string"},
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, list,
		Options{TerminalWidth: -1}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	// The column must survive.
	if !strings.Contains(out, "Tags") {
		t.Fatalf("Tags column disappeared:\n%s", out)
	}
	// The scalar value must be visible, not dropped.
	if !strings.Contains(out, "plain-string") {
		t.Fatalf("scalar value in a nested column was lost:\n%s", out)
	}
	// null and [] must be represented rather than blank.
	if !strings.Contains(out, noneValue) {
		t.Fatalf("null value not shown:\n%s", out)
	}
	if !strings.Contains(out, "[]") {
		t.Fatalf("empty list not shown:\n%s", out)
	}
	// The genuinely nested row points at its section instead of dumping JSON.
	if !strings.Contains(out, nestedPlaceholder) {
		t.Fatalf("nested cell should reference its section:\n%s", out)
	}
	if !strings.Contains(out, "prod") {
		t.Fatalf("nested content missing from its section:\n%s", out)
	}
}

// Section numbering is 1-based so it lines up with the "#" column.
func TestNestedSectionIndexIsOneBasedAndTraceable(t *testing.T) {
	list := []interface{}{
		map[string]interface{}{"Id": "i-1", "Tags": []interface{}{
			map[string]interface{}{"K": "a"}}},
		map[string]interface{}{"Id": "i-2", "Tags": []interface{}{
			map[string]interface{}{"K": "b"}}},
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTableNum, list,
		Options{TerminalWidth: -1}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Tags[1]") || !strings.Contains(out, "Tags[2]") {
		t.Fatalf("expected 1-based section titles:\n%s", out)
	}
	if strings.Contains(out, "Tags[0]") {
		t.Fatalf("section numbering must not be 0-based:\n%s", out)
	}
}

// A single-element list keeps a plain title: "[1]" would be noise.
func TestSingleRecordNestedSectionHasNoIndex(t *testing.T) {
	list := []interface{}{
		map[string]interface{}{"Id": "i-1", "Tags": []interface{}{
			map[string]interface{}{"K": "a"}}},
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, list,
		Options{TerminalWidth: -1}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Tags[") {
		t.Fatalf("single record should not be indexed:\n%s", buf.String())
	}
}

// Scalar lists are readable inline and must not become a placeholder.
func TestScalarListNotTreatedAsNested(t *testing.T) {
	list := []interface{}{
		map[string]interface{}{"Id": "i-1", "Sg": []interface{}{"sg-1", "sg-2"}},
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, list,
		Options{TerminalWidth: -1}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), nestedPlaceholder) {
		t.Fatalf("scalar list must stay inline:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "sg-1") {
		t.Fatalf("scalar list content missing:\n%s", buf.String())
	}
}

func TestTextHeterogeneousScalarAndStructuredColumnRecurses(t *testing.T) {
	data := []interface{}{
		map[string]interface{}{"Id": "i-1", "Value": "plain"},
		map[string]interface{}{
			"Id": "i-2",
			"Value": map[string]interface{}{
				"Name": "nested",
			},
		},
	}

	var buf bytes.Buffer
	if err := Write(&buf, FormatText, data); err != nil {
		t.Fatal(err)
	}
	// The column must not read None: the field exists on i-2, it is just
	// rendered on the VALUE line that follows. None is reserved for a field
	// that is genuinely absent or null.
	want := "i-1\tplain\ni-2\t" + nestedPlaceholder + "\nVALUE\tnested\n"
	if got := buf.String(); got != want {
		t.Fatalf("heterogeneous object column = %q, want %q", got, want)
	}
}

// A field can be a string on one record and a list on the next. table renders
// that list inline in the cell, so text must show it too: reporting None would
// claim a field that is right there in --output json does not exist.
func TestTextSharedColumnKeepsScalarListValue(t *testing.T) {
	data := []interface{}{
		map[string]interface{}{"Id": "i-1", "Sg": "sg-only"},
		map[string]interface{}{"Id": "i-2", "Sg": []interface{}{"sg-1", "sg-2"}},
	}

	var buf bytes.Buffer
	if err := Write(&buf, FormatText, data); err != nil {
		t.Fatal(err)
	}
	want := "i-1\tsg-only\ni-2\t[\"sg-1\",\"sg-2\"]\n"
	if got := buf.String(); got != want {
		t.Fatalf("shared column = %q, want %q", got, want)
	}
}

// A cell may only point at the lines below once those lines exist. Nesting that
// bottoms out in empty containers flattens to nothing, so the pointer would name
// a line the reader can never find.
func TestTextNeverPointsAtLinesItDoesNotEmit(t *testing.T) {
	for _, contentFree := range []string{`{"inner":{}}`, `[{}]`, `[[]]`} {
		var value interface{}
		if err := json.Unmarshal([]byte(contentFree), &value); err != nil {
			t.Fatal(err)
		}
		data := []interface{}{
			map[string]interface{}{"Id": "i-1", "B": "scalar"},
			map[string]interface{}{"Id": "i-2", "B": value},
		}

		var buf bytes.Buffer
		if err := Write(&buf, FormatText, data); err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		if strings.Contains(out, nestedPlaceholder) {
			t.Fatalf("%s: cell points at lines that were never emitted:\n%s",
				contentFree, out)
		}
		// The value still has to be visible, shape and all.
		if !strings.Contains(out, contentFree) {
			t.Fatalf("%s: value lost from the row:\n%s", contentFree, out)
		}
	}
}

// The pointer must still be used when the lines really do follow.
func TestTextPointsAtNestedLinesWhenTheyExist(t *testing.T) {
	data := []interface{}{
		map[string]interface{}{"Id": "i-1", "B": "scalar"},
		map[string]interface{}{"Id": "i-2", "B": map[string]interface{}{"Inner": "v"}},
	}
	var buf bytes.Buffer
	if err := Write(&buf, FormatText, data); err != nil {
		t.Fatal(err)
	}
	// Columns are alphabetical, so B comes before Id.
	want := "scalar\ti-1\n" + nestedPlaceholder + "\ti-2\nB\tv\n"
	if got := buf.String(); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestTextNestedMixedObjectListKeepsPrefixOnScalarValues(t *testing.T) {
	data := map[string]interface{}{
		"Items": []interface{}{
			map[string]interface{}{"A": "mapped"},
			"tail",
			nil,
		},
	}

	var buf bytes.Buffer
	if err := Write(&buf, FormatText, data); err != nil {
		t.Fatal(err)
	}
	want := "ITEMS\tmapped\nITEMS\ttail\nITEMS\tNone\n"
	if got := buf.String(); got != want {
		t.Fatalf("nested mixed list = %q, want %q", got, want)
	}
}

func TestOrderedSubsetPreservesOriginalOrder(t *testing.T) {
	order := []string{"A", "B", "C", "D"}
	got := orderedSubset(order, []string{"C", "A"})
	if strings.Join(got, ",") != "A,C" {
		t.Fatalf("got %v, want [A C]", got)
	}
	// Duplicates in want must not duplicate the output.
	got = orderedSubset(order, []string{"B", "B"})
	if strings.Join(got, ",") != "B" {
		t.Fatalf("got %v, want [B]", got)
	}
}
