package output

import (
	"bytes"
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

// A nested value carries the position of the record it belongs to, so the tags
// of instance 1 and instance 2 are not interchangeable.
func TestTextNestedRowsCarryRecordPosition(t *testing.T) {
	out := textOf(t, nestedListResponse())
	for _, want := range []string{
		"RESULT.INSTANCELIST.TAGS[1]\tenv\tprod\n",
		"RESULT.INSTANCELIST.TAGS[2]\tenv\ttest\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}

// text labels and table section titles must name the same node, differing only
// in case. Otherwise the two formats describe the same nesting two ways.
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
		titles[strings.ToUpper(line)] = struct{}{}
	}
	if len(titles) == 0 {
		t.Fatalf("no table section titles found:\n%s", table.String())
	}

	labels := map[string]struct{}{}
	for _, line := range strings.Split(strings.TrimSuffix(textOf(t, data), "\n"), "\n") {
		if label, _, found := strings.Cut(line, "\t"); found {
			labels[label] = struct{}{}
		}
	}
	for title := range titles {
		if _, ok := labels[title]; !ok {
			t.Fatalf("table section %q has no matching text label %v", title, labels)
		}
	}
}

// A parent list of one element adds no position, keeping the common
// single-record response unadorned.
func TestTextSingleRecordNeedsNoPosition(t *testing.T) {
	data := map[string]interface{}{
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
	out := textOf(t, data)
	if !strings.Contains(out, "RESULT.INSTANCELIST.TAGS\tenv\tprod\n") {
		t.Fatalf("single record should carry no [n]:\n%s", out)
	}
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
