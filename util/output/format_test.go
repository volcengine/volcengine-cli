package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return 1, nil
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func sampleEnvelope() map[string]interface{} {
	return map[string]interface{}{
		"ResponseMetadata": map[string]interface{}{"RequestId": "req-1"},
		"Result": map[string]interface{}{
			"Instances": []interface{}{
				map[string]interface{}{"InstanceId": "i-1", "Status": "RUNNING"},
				map[string]interface{}{"InstanceId": "i-2", "Status": "STOPPED"},
			},
		},
	}
}

func TestParseFormat(t *testing.T) {
	cases := []struct {
		in      string
		want    Format
		wantErr bool
	}{
		{"", FormatJSON, false},
		{"json", FormatJSON, false},
		{"JSON", FormatJSON, false},
		{"table", FormatTable, false},
		{"text", FormatText, false},
		{"yaml", FormatYAML, false},
		{"yaml-stream", FormatYAMLStream, false},
		{"off", FormatOff, false},
		{"xml", "", true},
	}
	for _, tc := range cases {
		got, err := ParseFormat(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("ParseFormat(%q) expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseFormat(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("ParseFormat(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, FormatJSON, map[string]interface{}{"A": "b"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"A"`) {
		t.Fatalf("json unexpected: %s", buf.String())
	}
}

func TestWriteOff(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, FormatOff, sampleEnvelope()); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("off should write nothing, got %q", buf.String())
	}
}

func TestWriteDetectsShortWrites(t *testing.T) {
	for _, format := range []Format{FormatJSON, FormatTable, FormatText, FormatYAML, FormatYAMLStream} {
		err := Write(shortWriter{}, format, map[string]interface{}{"A": "long value"})
		if !errors.Is(err, io.ErrShortWrite) {
			t.Fatalf("%s error = %v, want io.ErrShortWrite", format, err)
		}
	}
}

func TestWritePropagatesWriterErrors(t *testing.T) {
	for _, format := range []Format{FormatJSON, FormatTable, FormatText, FormatYAML, FormatYAMLStream} {
		err := Write(errorWriter{}, format, map[string]interface{}{"A": "long value"})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("%s error = %v, want write failure", format, err)
		}
	}
}

func TestTableNoAutoUnwrapEnvelope(t *testing.T) {
	// Without --query, nested Result.Instances must NOT be auto-picked.
	var buf bytes.Buffer
	if err := Write(&buf, FormatTable, sampleEnvelope()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Key") || !strings.Contains(out, "ResponseMetadata") {
		t.Fatalf("expected Key/Value envelope table, got:\n%s", out)
	}
	// Should not look like an Instances multi-column table alone.
	if strings.Contains(out, "InstanceId") && !strings.Contains(out, "Result") {
		t.Fatalf("unexpected auto-unwrap of Instances:\n%s", out)
	}
}

func TestTableAfterQuery(t *testing.T) {
	data := sampleEnvelope()
	filtered, err := ApplyQuery(data, "Result.Instances[*].{Id:InstanceId,Status:Status}")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Write(&buf, FormatTable, filtered); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Id") || !strings.Contains(out, "i-1") {
		t.Fatalf("table after query unexpected:\n%s", out)
	}
}

func TestTableAndTextMissingFieldNone(t *testing.T) {
	rows := []interface{}{
		map[string]interface{}{"A": "1"},
		map[string]interface{}{"A": "2", "B": "x"},
	}
	var tableBuf, textBuf bytes.Buffer
	if err := Write(&tableBuf, FormatTable, rows); err != nil {
		t.Fatal(err)
	}
	if err := Write(&textBuf, FormatText, rows); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tableBuf.String(), noneValue) {
		t.Fatalf("table missing None for absent field:\n%s", tableBuf.String())
	}
	if !strings.Contains(textBuf.String(), noneValue) {
		t.Fatalf("text missing None for absent field:\n%s", textBuf.String())
	}
}

func TestEmptyObjectListTable(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, FormatTable, []interface{}{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "(empty)") {
		t.Fatalf("empty list table unexpected: %q", buf.String())
	}
}

func TestWriteTextListProjection(t *testing.T) {
	var buf bytes.Buffer
	data := []interface{}{
		[]interface{}{"i-1", "RUNNING"},
		[]interface{}{"i-2", "STOPPED"},
	}
	if err := Write(&buf, FormatText, data); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2 || lines[0] != "i-1\tRUNNING" {
		t.Fatalf("text projection unexpected: %q", buf.String())
	}
}

func TestTableAndTextEscapeControlCharacters(t *testing.T) {
	data := []interface{}{
		map[string]interface{}{"Value": "line1\nline2\t\x1b]52;c;payload\a"},
	}
	for _, format := range []Format{FormatTable, FormatText} {
		var buf bytes.Buffer
		if err := Write(&buf, format, data); err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		out := buf.String()
		for _, raw := range []string{"\nline2", "\t", "\x1b", "\a"} {
			if strings.Contains(out, raw) {
				t.Fatalf("%s output contains raw control sequence %q: %q", format, raw, out)
			}
		}
		for _, escaped := range []string{`\n`, `\t`, `\x1B`, `\x07`} {
			if !strings.Contains(out, escaped) {
				t.Fatalf("%s output missing escaped sequence %q: %q", format, escaped, out)
			}
		}
	}
}

func TestTableAlignsWideCharacters(t *testing.T) {
	var buf bytes.Buffer
	data := []interface{}{
		map[string]interface{}{"City": "中文", "Name": "first"},
		map[string]interface{}{"City": "Tokyo", "Name": "second"},
	}
	if err := Write(&buf, FormatTable, data); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("unexpected table output:\n%s", buf.String())
	}
	wantWidth := displayWidth(lines[0])
	for i, line := range lines {
		if got := displayWidth(line); got != wantWidth {
			t.Fatalf("line %d display width=%d, want %d:\n%s", i+1, got, wantWidth, buf.String())
		}
	}
}

func TestWriteYAMLAndStream(t *testing.T) {
	data := map[string]interface{}{"AccountId": "123"}
	for _, f := range []Format{FormatYAML, FormatYAMLStream} {
		var buf bytes.Buffer
		if err := Write(&buf, f, data); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if !strings.Contains(buf.String(), "AccountId:") {
			t.Fatalf("%s unexpected: %s", f, buf.String())
		}
	}
}

func TestApplyQuery(t *testing.T) {
	got, err := ApplyQuery(sampleEnvelope(), "Result.Instances[*].InstanceId")
	if err != nil {
		t.Fatal(err)
	}
	arr, ok := got.([]interface{})
	if !ok || len(arr) != 2 || arr[0] != "i-1" {
		t.Fatalf("query result = %#v", got)
	}
}

func TestApplyQueryInvalid(t *testing.T) {
	if _, err := ApplyQuery(map[string]interface{}{}, "[[["); err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyQuerySupportsJSONNumberOperationsWithoutLosingLargeIntegers(t *testing.T) {
	data := map[string]interface{}{
		"Small":     json.Number("42"),
		"Large":     json.Number("9223372036854775807"),
		"VeryLarge": json.Number("18446744073709551615"),
		"Decimal":   json.Number("0.123456789012345678901"),
		"Items":     []interface{}{json.Number("2"), json.Number("1")},
	}
	decimal, err := ApplyQuery(data, "Decimal")
	if err != nil {
		t.Fatal(err)
	}
	if exact, ok := decimal.(json.Number); !ok || exact.String() != "0.123456789012345678901" {
		t.Fatalf("Decimal = %#v, want exact json.Number", decimal)
	}

	got, err := ApplyQuery(data, "{Small:Small > `40`,Max:max(Items),Large:Large,VeryLarge:VeryLarge}")
	if err != nil {
		t.Fatal(err)
	}
	result, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("query result = %#v, want map", got)
	}
	if result["Small"] != true {
		t.Fatalf("Small comparison = %#v, want true", result["Small"])
	}
	if result["Max"] != float64(2) {
		t.Fatalf("Max = %#v, want 2", result["Max"])
	}
	if large, ok := result["Large"].(json.Number); !ok || large.String() != "9223372036854775807" {
		t.Fatalf("Large = %#v, want exact json.Number", result["Large"])
	}
	if veryLarge, ok := result["VeryLarge"].(json.Number); !ok || veryLarge.String() != "18446744073709551615" {
		t.Fatalf("VeryLarge = %#v, want exact json.Number", result["VeryLarge"])
	}
}

func TestQueryNullPath(t *testing.T) {
	got, err := ApplyQuery(sampleEnvelope(), "Result.Missing")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("want nil, got %#v", got)
	}
	var buf bytes.Buffer
	if err := Write(&buf, FormatJSON, got); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(buf.String()) != "null" {
		t.Fatalf("json null unexpected: %q", buf.String())
	}
}
