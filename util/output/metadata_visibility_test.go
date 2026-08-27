package output

import (
	"bytes"
	"strings"
	"testing"
)

// ResponseMetadata is rendered like any other top-level field: in every format,
// with or without a --query.
//
// table/text used to drop the envelope unless a query had been applied, so one
// format answered "does this field exist?" two different ways — a bare
// `--output table` claimed RequestId was absent while
// `--query 'ResponseMetadata.RequestId' --output table` printed its value.

// A bare render keeps the whole response in every format.
func TestAllFormatsKeepResponseMetadata(t *testing.T) {
	for _, format := range []Format{
		FormatJSON, FormatYAML, FormatTable, FormatTableNum, FormatText,
	} {
		var buf bytes.Buffer
		if err := WriteWithOptions(&buf, format, sampleEnvelope(),
			Options{TerminalWidth: -1}); err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		if !strings.Contains(out, "req-1") {
			t.Fatalf("%s dropped RequestId:\n%s", format, out)
		}
		// The payload is rendered alongside the envelope, not instead of it.
		if !strings.Contains(out, "i-1") {
			t.Fatalf("%s dropped the payload:\n%s", format, out)
		}
	}
}

// The envelope reaches table as a titled section with real columns, so it is
// readable rather than a JSON blob in a cell.
func TestTableRendersResponseMetadataAsSection(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, sampleEnvelope(),
		Options{TerminalWidth: -1}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "ResponseMetadata\n") {
		t.Fatalf("expected a ResponseMetadata section title:\n%s", out)
	}
	if !strings.Contains(out, "RequestId") {
		t.Fatalf("expected RequestId as a column:\n%s", out)
	}
	if strings.Contains(out, `{"RequestId"`) {
		t.Fatalf("envelope should not be dumped as JSON:\n%s", out)
	}
}

// A --query result is rendered exactly as selected, including a selection that
// happens to look like an envelope.
func TestQueriedMetadataIsRendered(t *testing.T) {
	// Simulates the output of --query 'ResponseMetadata'.
	selected := map[string]interface{}{
		"RequestId": "req-1",
		"Action":    "DescribeInstances",
	}
	for _, format := range []Format{FormatTable, FormatTableNum, FormatText} {
		var buf bytes.Buffer
		if err := WriteWithOptions(&buf, format, selected,
			Options{TerminalWidth: -1}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "req-1") {
			t.Fatalf("%s dropped a queried field:\n%s", format, buf.String())
		}
	}
}

// Write APIs often return nothing but the envelope. It must render its fields
// rather than "(empty)", which reads like a failed call.
func TestMetadataOnlyResponseRendersFields(t *testing.T) {
	data := map[string]interface{}{
		"ResponseMetadata": map[string]interface{}{"RequestId": "req-1"},
	}
	for _, format := range []Format{FormatTable, FormatTableNum, FormatText} {
		var buf bytes.Buffer
		if err := WriteWithOptions(&buf, format, data,
			Options{TerminalWidth: -1}); err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		if strings.Contains(out, "(empty)") {
			t.Fatalf("%s rendered a successful call as empty:\n%s", format, out)
		}
		if !strings.Contains(out, "req-1") {
			t.Fatalf("%s lost the RequestId:\n%s", format, out)
		}
	}
}
