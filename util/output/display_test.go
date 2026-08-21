package output

import (
	"bytes"
	"strings"
	"testing"
)

// --- 1. ResponseMetadata stripping ---------------------------------------

func TestStripResponseMetadataOnlyAtTopLevel(t *testing.T) {
	data := map[string]interface{}{
		"ResponseMetadata": map[string]interface{}{"RequestId": "req-1"},
		"Result": map[string]interface{}{
			// A nested field of the same name is payload, not envelope.
			"ResponseMetadata": "keep-me",
		},
	}
	got, ok := stripResponseMetadata(data).(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected type %T", got)
	}
	if _, present := got["ResponseMetadata"]; present {
		t.Fatal("top-level ResponseMetadata should be removed")
	}
	inner := got["Result"].(map[string]interface{})
	if inner["ResponseMetadata"] != "keep-me" {
		t.Fatal("nested ResponseMetadata must be preserved")
	}
}

// Write APIs often return nothing but ResponseMetadata. Stripping it would turn
// a successful call into "(empty)", which reads like a failure.
func TestStripKeepsSoleResponseMetadata(t *testing.T) {
	data := map[string]interface{}{
		"ResponseMetadata": map[string]interface{}{"RequestId": "req-1"},
	}
	var buf bytes.Buffer
	if err := Write(&buf, FormatTable, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "(empty)") {
		t.Fatalf("metadata-only response must not render as empty:\n%s", out)
	}
	if !strings.Contains(out, "req-1") {
		t.Fatalf("metadata-only response should still show RequestId:\n%s", out)
	}
}

func TestTextStripsResponseMetadata(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, FormatText, sampleEnvelope()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "req-1") {
		t.Fatalf("text must not contain RequestId:\n%s", buf.String())
	}
}

// --- 2. Single-row verticalization ---------------------------------------

func TestSingleWideRowIsVerticalized(t *testing.T) {
	rows := []interface{}{
		map[string]interface{}{
			"AccountId": "2100000000", "Arn": "trn:iam::2100000000:user/alice",
			"UserId": "u-123456", "Region": "cn-beijing",
		},
	}
	var buf bytes.Buffer
	// A narrow terminal forces the transpose.
	if err := WriteWithOptions(&buf, FormatTable, rows, Options{TerminalWidth: 40}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Field") || !strings.Contains(out, "Value") {
		t.Fatalf("expected Field|Value transpose:\n%s", out)
	}
	// Every field name becomes a row label.
	for _, k := range []string{"AccountId", "Arn", "UserId", "Region"} {
		if !strings.Contains(out, k) {
			t.Fatalf("missing field %s:\n%s", k, out)
		}
	}
}

// Wide terminals keep the horizontal layout: transposing is only a fallback.
func TestSingleRowStaysHorizontalWhenItFits(t *testing.T) {
	rows := []interface{}{
		map[string]interface{}{"A": "1", "B": "2", "C": "3"},
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, rows, Options{TerminalWidth: 200}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Field") {
		t.Fatalf("wide terminal should keep horizontal layout:\n%s", buf.String())
	}
}

func TestMultiRowIsNeverVerticalized(t *testing.T) {
	rows := []interface{}{
		map[string]interface{}{"A": "1", "B": "2", "C": "3"},
		map[string]interface{}{"A": "4", "B": "5", "C": "6"},
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, rows, Options{TerminalWidth: 10}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Field") {
		t.Fatalf("multi-row table must not transpose:\n%s", buf.String())
	}
}

// Row numbers imply a record list, so numbering wins over transposing.
func TestTableNumSkipsVerticalization(t *testing.T) {
	rows := []interface{}{
		map[string]interface{}{"A": "1", "B": "2", "C": "3"},
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTableNum, rows, Options{TerminalWidth: 20}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "Field") {
		t.Fatalf("table-num must not transpose:\n%s", out)
	}
	if !strings.Contains(out, "#") {
		t.Fatalf("table-num should keep the # column:\n%s", out)
	}
}

// --- 3. Terminal width fitting -------------------------------------------

func TestWidthFittingWrapsWithoutLosingData(t *testing.T) {
	rows := []interface{}{
		map[string]interface{}{
			"Short": "a",
			"Long":  strings.Repeat("x", 200),
		},
		map[string]interface{}{
			"Short": "b",
			"Long":  strings.Repeat("y", 200),
		},
	}
	const width = 60
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, rows, Options{TerminalWidth: width}); err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n") {
		if got := displayWidth(line); got > width {
			t.Fatalf("line %d width %d exceeds terminal %d:\n%s", i+1, got, width, buf.String())
		}
	}
	if got := strings.Count(buf.String(), "x"); got != 200 {
		t.Fatalf("wrapped table lost x data: got %d of 200:\n%s", got, buf.String())
	}
	if got := strings.Count(buf.String(), "y"); got != 200 {
		t.Fatalf("wrapped table lost y data: got %d of 200:\n%s", got, buf.String())
	}
	if strings.Contains(buf.String(), "...") {
		t.Fatalf("wrapped cells must not use lossy ellipses:\n%s", buf.String())
	}
}

// Negative width disables fitting so piped output keeps full columns.
func TestNegativeWidthDisablesFitting(t *testing.T) {
	long := strings.Repeat("x", 120)
	rows := []interface{}{map[string]interface{}{"Long": long}}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, rows, Options{TerminalWidth: -1}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), long) {
		t.Fatalf("full-width content should survive:\n%s", buf.String())
	}
}

// Narrow terminals must not collapse columns below a readable minimum.
func TestWidthFittingKeepsMinimumCellWidth(t *testing.T) {
	widths := []int{50, 50, 50}
	got := fitWidths(widths, 10)
	for i, w := range got {
		if w < 3 {
			t.Fatalf("column %d width %d below minimum: %v", i, w, got)
		}
	}
}

// Fitting trims the widest column first, leaving narrow ones intact.
func TestWidthFittingTrimsWidestFirst(t *testing.T) {
	got := fitWidths([]int{3, 100}, 40)
	if got[0] != 3 {
		t.Fatalf("narrow column should be preserved, got %v", got)
	}
	if got[1] >= 100 {
		t.Fatalf("wide column should shrink, got %v", got)
	}
}

func TestWidthFittingHandlesHugeColumnsInBoundedSteps(t *testing.T) {
	got := fitWidths([]int{1 << 30, 1 << 30, 4}, 80)
	if got[2] != 4 {
		t.Fatalf("narrow column should be preserved, got %v", got)
	}
	if total := got[0] + got[1] + got[2]; total != 80-(1+3*len(got)) {
		t.Fatalf("fitted width budget mismatch: got %v", got)
	}
}

func TestWrapToWidthPreservesWideRunes(t *testing.T) {
	original := strings.Repeat("中", 10)
	got := wrapToWidth(original, 9)
	if strings.Join(got, "") != original {
		t.Fatalf("wrapped value changed: %#v", got)
	}
	for _, line := range got {
		if width := displayWidth(line); width > 9 {
			t.Fatalf("wrapped width %d exceeds 9: %q", width, line)
		}
	}
}

func TestWrapToWidthKeepsCombiningMarksWithBaseRune(t *testing.T) {
	original := "a\u0301b\u0301"
	got := wrapToWidth(original, 1)
	want := []string{"a\u0301", "b\u0301"}
	if len(got) != len(want) {
		t.Fatalf("wrapped combining sequence = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("wrapped combining sequence = %#v, want %#v", got, want)
		}
	}
}

func TestWrapToWidthKeepsEmojiGraphemeClustersIntact(t *testing.T) {
	emoji := "👩‍❤️‍💋‍👩"
	original := "A" + emoji + "B"
	got := wrapToWidth(original, 2)
	want := []string{"A", emoji, "B"}
	if len(got) != len(want) {
		t.Fatalf("wrapped emoji = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("wrapped emoji = %#v, want %#v", got, want)
		}
		if width := displayWidth(got[i]); width > 2 {
			t.Fatalf("wrapped emoji line width %d exceeds 2: %q", width, got[i])
		}
	}
}

// --- 4. Nested sections --------------------------------------------------

func TestNestedListBecomesOwnSection(t *testing.T) {
	data := map[string]interface{}{
		"Instances": []interface{}{
			map[string]interface{}{
				"InstanceId": "i-1",
				"Tags": []interface{}{
					map[string]interface{}{"Key": "env", "Value": "prod"},
				},
			},
		},
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, data, Options{TerminalWidth: -1}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Instances.Tags") {
		t.Fatalf("expected nested Tags section:\n%s", out)
	}
	// Tags render as real columns rather than a JSON blob in a cell.
	if !strings.Contains(out, "env") || !strings.Contains(out, "prod") {
		t.Fatalf("nested Tags content missing:\n%s", out)
	}
}

// Scalar lists are readable in one cell and must not spawn a section.
func TestScalarListStaysInCell(t *testing.T) {
	data := []interface{}{
		map[string]interface{}{
			"Id":  "i-1",
			"Ids": []interface{}{"sg-1", "sg-2"},
		},
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, data, Options{TerminalWidth: -1}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Ids\n") {
		t.Fatalf("scalar list should stay inline:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "sg-1") {
		t.Fatalf("scalar list content missing:\n%s", buf.String())
	}
}

// Deeply nested data must terminate instead of recursing without bound.
func TestDeepNestingTerminates(t *testing.T) {
	deep := map[string]interface{}{"leaf": "v"}
	for i := 0; i < 40; i++ {
		deep = map[string]interface{}{"n": deep}
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, deep, Options{TerminalWidth: -1}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("deep nesting produced no output")
	}
}

func TestNestedSectionTitleEscapesTerminalControls(t *testing.T) {
	key := "Tags\nnext\t\x1b[31mred\x1b[0m\a\u009b2J"
	data := map[string]interface{}{
		key: []interface{}{map[string]interface{}{"K": "v"}},
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, data,
		Options{TerminalWidth: -1}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.ContainsAny(out, "\r\t\x1b\a\u009b") {
		t.Fatalf("section title leaked terminal controls: %q", out)
	}
	if !strings.Contains(out, `Tags\nnext\t\x1B[31mred\x1B[0m\x07\x9B2J`) {
		t.Fatalf("section title controls were not rendered visibly: %q", out)
	}
}

func TestNestedSectionTitleRespectsTerminalWidthWithWideRunes(t *testing.T) {
	const width = 12
	key := strings.Repeat("中", 12)
	data := map[string]interface{}{
		key: []interface{}{map[string]interface{}{"K": "v"}},
	}
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, data,
		Options{TerminalWidth: width}); err != nil {
		t.Fatal(err)
	}
	foundTitle := false
	var title strings.Builder
	for _, line := range strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n") {
		if strings.Contains(line, "中") {
			foundTitle = true
			title.WriteString(line)
			if got := displayWidth(line); got > width {
				t.Fatalf("section title width %d exceeds terminal %d: %q", got, width, line)
			}
		}
	}
	if !foundTitle {
		t.Fatalf("nested section title missing: %q", buf.String())
	}
	if title.String() != key {
		t.Fatalf("wrapped section title changed: got %q, want %q", title.String(), key)
	}
}

// --- 5. Color ------------------------------------------------------------

// Color must not change layout: a colored table aligns exactly like a plain one.
func TestColorDoesNotAffectAlignment(t *testing.T) {
	rows := []interface{}{
		map[string]interface{}{"City": "中文", "Name": "first"},
		map[string]interface{}{"City": "Tokyo", "Name": "second"},
	}
	var plain, colored bytes.Buffer
	if err := WriteWithOptions(&plain, FormatTable, rows, Options{TerminalWidth: -1}); err != nil {
		t.Fatal(err)
	}
	if err := WriteWithOptions(&colored, FormatTable, rows,
		Options{TerminalWidth: -1, Color: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(colored.String(), "\x1b[") {
		t.Fatalf("color mode should emit ANSI:\n%q", colored.String())
	}
	// After removing styling, both renderings must be byte-identical.
	if stripANSI(colored.String()) != plain.String() {
		t.Fatalf("color changed layout:\nplain=%q\ncolored=%q",
			plain.String(), stripANSI(colored.String()))
	}
}

func TestColorDoesNotAffectWrappedLayout(t *testing.T) {
	rows := []interface{}{
		map[string]interface{}{"City": "中文城市", "Name": strings.Repeat("x", 30)},
		map[string]interface{}{"City": "Tokyo", "Name": strings.Repeat("y", 30)},
	}
	var plain, colored bytes.Buffer
	if err := WriteWithOptions(&plain, FormatTable, rows, Options{TerminalWidth: 24}); err != nil {
		t.Fatal(err)
	}
	if err := WriteWithOptions(&colored, FormatTable, rows,
		Options{TerminalWidth: 24, Color: true}); err != nil {
		t.Fatal(err)
	}
	if stripANSI(colored.String()) != plain.String() {
		t.Fatalf("color changed wrapped layout:\nplain=%q\ncolored=%q",
			plain.String(), stripANSI(colored.String()))
	}
	for i, line := range strings.Split(strings.TrimSuffix(plain.String(), "\n"), "\n") {
		if got := displayWidth(line); got > 24 {
			t.Fatalf("wrapped line %d width %d exceeds 24:\n%s", i+1, got, plain.String())
		}
	}
}

func TestColorOffByDefault(t *testing.T) {
	rows := []interface{}{map[string]interface{}{"A": "1"}}
	var buf bytes.Buffer
	if err := Write(&buf, FormatTable, rows); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "\x1b[") {
		t.Fatalf("default output must be uncolored:\n%q", buf.String())
	}
}

func TestStripANSI(t *testing.T) {
	cases := map[string]string{
		"\x1b[1mbold\x1b[0m":      "bold",
		"\x1b[36mcyan\x1b[0m":     "cyan",
		"plain":                   "plain",
		"\x1b[1m\x1b[36mx\x1b[0m": "x",
	}
	for in, want := range cases {
		if got := stripANSI(in); got != want {
			t.Errorf("stripANSI(%q) = %q, want %q", in, got, want)
		}
	}
}
