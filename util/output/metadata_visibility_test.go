package output

import (
	"bytes"
	"strings"
	"testing"
)

// Visibility of ResponseMetadata must depend only on whether a --query was
// applied — never on the same format behaving two different ways.
//
// Before Options.Queried existed, stripping happened unconditionally in the
// renderer while --query ran before it, so `--output table` claimed the field
// did not exist while `--query 'ResponseMetadata.RequestId' --output table`
// happily printed its value.

// Without a query, the envelope is display noise and is dropped.
func TestBareTableHidesResponseMetadata(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, sampleEnvelope(),
		Options{TerminalWidth: -1}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "req-1") {
		t.Fatalf("bare table must not show RequestId:\n%s", buf.String())
	}
}

// With a query, the result is exactly what the user selected: nothing is
// removed from it, even when it looks like an envelope.
func TestQueriedResultKeepsSelectedMetadata(t *testing.T) {
	// Simulates the output of --query 'ResponseMetadata'.
	selected := map[string]interface{}{
		"RequestId": "req-1",
		"Action":    "DescribeInstances",
	}
	for _, format := range []Format{FormatTable, FormatTableNum, FormatText} {
		var buf bytes.Buffer
		if err := WriteWithOptions(&buf, format, selected,
			Options{TerminalWidth: -1, Queried: true}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "req-1") {
			t.Fatalf("%s dropped a queried field:\n%s", format, buf.String())
		}
	}
}

// `--query '@'` asks for the whole response verbatim; the renderer must not
// second-guess that and delete a top-level key.
func TestQueriedIdentityShowsWholeResponse(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteWithOptions(&buf, FormatTable, sampleEnvelope(),
		Options{TerminalWidth: -1, Queried: true}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "req-1") {
		t.Fatalf("--query '@' must keep ResponseMetadata:\n%s", out)
	}
	// The payload is still rendered normally alongside it.
	if !strings.Contains(out, "i-1") {
		t.Fatalf("payload missing:\n%s", out)
	}
}

// The same rule applies to text, which scripts read positionally.
func TestQueriedTextKeepsMetadata(t *testing.T) {
	var stripped, kept bytes.Buffer
	if err := WriteWithOptions(&stripped, FormatText, sampleEnvelope(),
		Options{}); err != nil {
		t.Fatal(err)
	}
	if err := WriteWithOptions(&kept, FormatText, sampleEnvelope(),
		Options{Queried: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stripped.String(), "req-1") {
		t.Fatalf("bare text must not show RequestId:\n%s", stripped.String())
	}
	if !strings.Contains(kept.String(), "req-1") {
		t.Fatalf("queried text must keep RequestId:\n%s", kept.String())
	}
}

// json/yaml are untouched by this flag: they always carry the full response.
func TestQueriedFlagDoesNotAffectJSONOrYAML(t *testing.T) {
	for _, format := range []Format{FormatJSON, FormatYAML} {
		for _, queried := range []bool{false, true} {
			var buf bytes.Buffer
			if err := WriteWithOptions(&buf, format, sampleEnvelope(),
				Options{Queried: queried}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(buf.String(), "req-1") {
				t.Fatalf("%s (queried=%v) must keep the full response:\n%s",
					format, queried, buf.String())
			}
		}
	}
}

// A metadata-only response must stay visible in both modes: it is a successful
// write call, not an empty result.
func TestMetadataOnlyResponseVisibleEitherWay(t *testing.T) {
	data := map[string]interface{}{
		"ResponseMetadata": map[string]interface{}{"RequestId": "req-1"},
	}
	for _, queried := range []bool{false, true} {
		var buf bytes.Buffer
		if err := WriteWithOptions(&buf, FormatTable, data,
			Options{TerminalWidth: -1, Queried: queried}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(buf.String(), "(empty)") {
			t.Fatalf("queried=%v rendered a successful call as empty:\n%s",
				queried, buf.String())
		}
		if !strings.Contains(buf.String(), "req-1") {
			t.Fatalf("queried=%v lost the RequestId:\n%s", queried, buf.String())
		}
	}
}
