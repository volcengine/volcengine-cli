package output

import (
	"bytes"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// A text row is labelled with the full field path it came from, so a line can
// be traced back to its node in the response without counting lines and without
// knowing the response shape in advance.
//
// The label used to be the leaf key only: `INSTANCELIST` rather than
// `RESULT.INSTANCELIST`, which lost the depth and collided whenever two parents
// held a same-named field.

func nestedListResponse() map[string]interface{} {
	return map[string]interface{}{
		"Result": map[string]interface{}{
			"InstanceList": []interface{}{
				map[string]interface{}{
					"InstanceID": "i-1",
					"Tags": []interface{}{
						map[string]interface{}{"Key": "env", "Value": "prod"},
					},
				},
				map[string]interface{}{
					"InstanceID": "i-2",
					"Tags": []interface{}{
						map[string]interface{}{"Key": "env", "Value": "test"},
					},
				},
			},
		},
	}
}

func textOf(t *testing.T, data interface{}) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Write(&buf, FormatText, data); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestTextPrefixIsTheFullFieldPath(t *testing.T) {
	out := textOf(t, nestedListResponse())
	if !strings.Contains(out, "RESULT.INSTANCELIST\ti-1\n") {
		t.Fatalf("expected a full-path label for the record rows:\n%s", out)
	}
	// The leaf-only label must not survive anywhere.
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if strings.HasPrefix(line, "INSTANCELIST\t") || strings.HasPrefix(line, "TAGS") {
			t.Fatalf("line kept a leaf-only label: %q", line)
		}
	}
}

// Every record of one list shares an identical label, so `$1 == "..."` still
// selects all of them.
func TestTextRecordRowsShareOneLabel(t *testing.T) {
	out := textOf(t, nestedListResponse())
	records := 0
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if strings.HasPrefix(line, "RESULT.INSTANCELIST\t") {
			records++
		}
	}
	if records != 2 {
		t.Fatalf("expected 2 record rows under one label, got %d:\n%s", records, out)
	}
}

// A nested value is attributed by line order, the way the AWS CLI text
// formatter does it: the tags of an instance are emitted immediately after that
// instance's own row, so a script tracks the last record row it saw.
func TestTextNestedLinesFollowTheirRecordRow(t *testing.T) {
	want := "RESULT.INSTANCELIST\ti-1\n" +
		"RESULT.INSTANCELIST.TAGS\tenv\tprod\n" +
		"RESULT.INSTANCELIST\ti-2\n" +
		"RESULT.INSTANCELIST.TAGS\tenv\ttest\n"
	if got := textOf(t, nestedListResponse()); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

// text labels and table section titles must name the same node, differing only
// in case and in the record number. Otherwise the two formats describe the same
// nesting two ways.
//
// Only table numbers its sections: it prints every record before any nested
// section, so a section has no row to sit next to and needs the number to stay
// traceable. text has that adjacency and keeps the label stable instead.
func TestTextLabelsMatchTableSectionTitles(t *testing.T) {
	data := nestedListResponse()

	var table bytes.Buffer
	if err := WriteWithOptions(&table, FormatTable, data,
		Options{TerminalWidth: -1}); err != nil {
		t.Fatal(err)
	}
	titles := map[string]struct{}{}
	for _, line := range strings.Split(table.String(), "\n") {
		// Section titles are the only lines that are neither borders nor rows.
		if line == "" || strings.HasPrefix(line, "+") || strings.HasPrefix(line, "|") {
			continue
		}
		titles[stripRecordNumber(strings.ToUpper(line))] = struct{}{}
	}
	if len(titles) == 0 {
		t.Fatalf("no table section titles found:\n%s", table.String())
	}

	labels := labelsOf(textOf(t, data))
	for title := range titles {
		if _, ok := labels[title]; !ok {
			t.Fatalf("table section %q has no matching text label %v", title, labels)
		}
	}
}

// stripRecordNumber drops the trailing "[n]" a table section title carries.
func stripRecordNumber(title string) string {
	open := strings.LastIndexByte(title, '[')
	if open < 0 || !strings.HasSuffix(title, "]") {
		return title
	}
	if _, err := strconv.Atoi(title[open+1 : len(title)-1]); err != nil {
		return title
	}
	return title[:open]
}

// A label must not depend on how many records came back. It used to carry the
// record number as soon as a list held more than one element, so
// `$1 == "RESULT.INSTANCELIST.TAGS"` matched a one-record response and then
// silently matched nothing once a second instance existed.
func TestTextLabelsDoNotDependOnRecordCount(t *testing.T) {
	single := map[string]interface{}{
		"Result": map[string]interface{}{
			"InstanceList": []interface{}{
				map[string]interface{}{
					"InstanceID": "i-1",
					"Tags": []interface{}{
						map[string]interface{}{"Key": "env", "Value": "prod"},
					},
				},
			},
		},
	}
	oneRecord := labelsOf(textOf(t, single))
	twoRecords := labelsOf(textOf(t, nestedListResponse()))
	if !reflect.DeepEqual(oneRecord, twoRecords) {
		t.Fatalf("labels changed with the record count: one=%v two=%v",
			oneRecord, twoRecords)
	}
	if _, ok := oneRecord["RESULT.INSTANCELIST.TAGS"]; !ok {
		t.Fatalf("expected a plain path label, got %v", oneRecord)
	}
}

func labelsOf(text string) map[string]struct{} {
	labels := map[string]struct{}{}
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if label, _, found := strings.Cut(line, "\t"); found {
			labels[label] = struct{}{}
		}
	}
	return labels
}

// Response keys reach the terminal inside the label now, so a key carrying a Tab
// or a newline must not add a column or a line: the label is one TSV field, and
// `awk -F'\t'` must keep seeing the value in $2.
func TestTextLabelEscapesControlsToStayOneColumn(t *testing.T) {
	data := map[string]interface{}{
		"Result": map[string]interface{}{
			"Ta\tgs\nnext\x1b[31m": map[string]interface{}{"K": "v"},
		},
	}
	out := textOf(t, data)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("a key newline split the row: %q", out)
	}
	if got := strings.Count(lines[0], "\t"); got != 1 {
		t.Fatalf("label added %d extra column(s): %q", got-1, out)
	}
	// The label is uppercased before it is escaped, so the (by then inert) escape
	// sequence is uppercased too. Escaping first would instead mangle the escape
	// notation itself, rendering a Tab as `\T`.
	if want := `RESULT.TA\tGS\nNEXT\x1B[31M` + "\tv"; lines[0] != want {
		t.Fatalf("label = %q, want %q", lines[0], want)
	}
}

// A scalar list keeps one line per value, now labelled with its full path.
func TestTextScalarListUnderNestedKeyUsesFullPath(t *testing.T) {
	data := map[string]interface{}{
		"Result": map[string]interface{}{
			"Vpc": map[string]interface{}{
				"SubnetIds": []interface{}{"subnet-1", "subnet-2"},
			},
		},
	}
	want := "RESULT.VPC.SUBNETIDS\tsubnet-1\nRESULT.VPC.SUBNETIDS\tsubnet-2\n"
	if got := textOf(t, data); got != want {
		t.Fatalf("scalar list label = %q, want %q", got, want)
	}
}
