package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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
		{"yaml-stream", "", true},
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
	for _, format := range []Format{FormatJSON, FormatTable, FormatText, FormatYAML} {
		err := Write(shortWriter{}, format, map[string]interface{}{"A": "long value"})
		if !errors.Is(err, io.ErrShortWrite) {
			t.Fatalf("%s error = %v, want io.ErrShortWrite", format, err)
		}
	}
}

func TestWritePropagatesWriterErrors(t *testing.T) {
	for _, format := range []Format{FormatJSON, FormatTable, FormatText, FormatYAML} {
		err := Write(errorWriter{}, format, map[string]interface{}{"A": "long value"})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("%s error = %v, want write failure", format, err)
		}
	}
}

// Without --query, the envelope's ResponseMetadata is dropped and nested
// payload is split into titled sections (aligned with AWS CLI). The section
// title keeps the origin path visible so users still know where data came from.
func TestTableStripsMetadataAndSplitsNestedPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, FormatTable, sampleEnvelope()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "ResponseMetadata") || strings.Contains(out, "req-1") {
		t.Fatalf("ResponseMetadata must not reach table output:\n%s", out)
	}
	if !strings.Contains(out, "Result.Instances") {
		t.Fatalf("expected a titled section for the nested list:\n%s", out)
	}
	// The nested list must render as a real record table, not one JSON cell.
	if !strings.Contains(out, "InstanceId") || !strings.Contains(out, "i-1") {
		t.Fatalf("nested list should render as columns:\n%s", out)
	}
	if strings.Contains(out, `{"InstanceId"`) {
		t.Fatalf("nested list should not be dumped as JSON:\n%s", out)
	}
}

// json keeps the full envelope: scripts still need RequestId.
func TestJSONKeepsResponseMetadata(t *testing.T) {
	for _, format := range []Format{FormatJSON, FormatYAML} {
		var buf bytes.Buffer
		if err := Write(&buf, format, sampleEnvelope()); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "ResponseMetadata") {
			t.Fatalf("%s must keep ResponseMetadata:\n%s", format, buf.String())
		}
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

func TestWriteYAML(t *testing.T) {
	data := map[string]interface{}{"AccountId": "123"}
	var buf bytes.Buffer
	if err := Write(&buf, FormatYAML, data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "AccountId:") {
		t.Fatalf("yaml unexpected: %s", buf.String())
	}
}

func TestWriteYAMLSortsObjectKeys(t *testing.T) {
	data := map[string]interface{}{
		"Zed":   "z",
		"Alpha": "a",
		"Nested": map[string]interface{}{
			"b": json.Number("1"),
			"a": json.Number("2"),
		},
		"List": []interface{}{
			map[string]interface{}{"Id": "i-2", "Arn": "arn-2"},
			map[string]interface{}{"Id": "i-1", "Arn": "arn-1"},
		},
	}
	want := `Alpha: a
List:
  - Arn: arn-2
    Id: i-2
  - Arn: arn-1
    Id: i-1
Nested:
  a: 2
  b: 1
Zed: z
`

	for i := 0; i < 32; i++ {
		var buf bytes.Buffer
		if err := Write(&buf, FormatYAML, data); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != want {
			t.Fatalf("run %d yaml =\n%q\nwant\n%q", i+1, got, want)
		}
	}
}

func TestWriteYAMLEmptyObject(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, FormatYAML, map[string]interface{}{}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "{}\n" {
		t.Fatalf("empty object yaml = %q, want {}\\n", buf.String())
	}
}

func TestWriteYAMLEmptySlice(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, FormatYAML, []interface{}{}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "[]\n" {
		t.Fatalf("empty slice yaml = %q, want []\\n", buf.String())
	}
}

func TestWriteYAMLNull(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, FormatYAML, nil); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "null\n" {
		t.Fatalf("nil yaml = %q, want null\\n", buf.String())
	}
}

func TestWriteYAMLNilMapIsNull(t *testing.T) {
	var m map[string]interface{}
	var buf bytes.Buffer
	if err := Write(&buf, FormatYAML, m); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "null\n" {
		t.Fatalf("nil map yaml = %q, want null\\n", buf.String())
	}
}

func TestWriteYAMLNilSliceIsNull(t *testing.T) {
	var s []interface{}
	var buf bytes.Buffer
	if err := Write(&buf, FormatYAML, s); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "null\n" {
		t.Fatalf("nil slice yaml = %q, want null\\n", buf.String())
	}
}

func TestWriteYAMLSortsQueriedObjectKeys(t *testing.T) {
	data := map[string]interface{}{"Z": "z", "A": "a"}
	got, err := ApplyQuery(data, "{Zed:Z,Alpha:A}")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Write(&buf, FormatYAML, got); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "Alpha: a\nZed: z\n" {
		t.Fatalf("queried yaml = %q", buf.String())
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

func TestApplyQueryPreservesJSONNumbersInStructuralProjection(t *testing.T) {
	data := map[string]interface{}{
		"Large":     json.Number("9223372036854775807"),
		"VeryLarge": json.Number("18446744073709551615"),
		"Decimal":   json.Number("0.123456789012345678901"),
	}
	got, err := ApplyQuery(data, "{Large:Large,VeryLarge:VeryLarge,Decimal:Decimal}")
	if err != nil {
		t.Fatal(err)
	}
	result, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("query result = %#v, want map", got)
	}
	if large, ok := result["Large"].(json.Number); !ok || large.String() != "9223372036854775807" {
		t.Fatalf("Large = %#v, want exact json.Number", result["Large"])
	}
	if veryLarge, ok := result["VeryLarge"].(json.Number); !ok || veryLarge.String() != "18446744073709551615" {
		t.Fatalf("VeryLarge = %#v, want exact json.Number", result["VeryLarge"])
	}
	if decimal, ok := result["Decimal"].(json.Number); !ok || decimal.String() != "0.123456789012345678901" {
		t.Fatalf("Decimal = %#v, want exact json.Number", result["Decimal"])
	}
}

func TestApplyQueryRejectsUnsafeJSONNumberOperations(t *testing.T) {
	account := json.Number("2106494982")
	data := map[string]interface{}{
		"Result": map[string]interface{}{
			"AccountId": account,
			"Items": []interface{}{
				map[string]interface{}{"Id": "a", "Cpu": json.Number("8")},
				map[string]interface{}{"Id": "b", "Cpu": json.Number("4")},
			},
		},
	}

	for _, expr := range []string{
		"Result.AccountId == `2106494982`",
		"Result.AccountId != `2106494982`",
		"Result.Items[?Cpu == `8`].Id",
		"Result.Items[?Cpu != `8`].Id",
		"Result.Items[?Cpu > `4`].Id",
	} {
		if _, err := ApplyQuery(data, expr); err == nil {
			t.Errorf("unsafe numeric query %q was accepted", expr)
		}
	}

	projected, err := ApplyQuery(data, "Result.AccountId")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := projected.(json.Number); !ok || got.String() != account.String() {
		t.Fatalf("AccountId projection = %#v, want exact json.Number %s", projected, account)
	}
}

func TestApplyQueryLargeIntegerOperationsRemainExact(t *testing.T) {
	data := map[string]interface{}{
		"N":         json.Number("9223372036854775807"),
		"Left":      json.Number("9007199254740993"),
		"SameLeft":  json.Number("9007199254740993"),
		"Different": json.Number("9007199254740992"),
	}
	if _, err := ApplyQuery(data, "N > `0`"); err == nil {
		t.Fatal("unsafe large-integer ordering query was accepted")
	}
	for _, tc := range []struct {
		expr string
		want bool
	}{
		{"Left == SameLeft", true},
		{"Left == Different", false},
		{"Left != Different", true},
	} {
		got, err := ApplyQuery(data, tc.expr)
		if err != nil || got != tc.want {
			t.Errorf("ApplyQuery(%q) = %#v, %v; want %v", tc.expr, got, err, tc.want)
		}
	}
}

func TestApplyQueryDoesNotRewriteNumericLookingStrings(t *testing.T) {
	data := map[string]interface{}{
		"Code": json.Number("1"),
		"Id":   "01",
	}
	got, err := ApplyQuery(data, "Id")
	if err != nil {
		t.Fatal(err)
	}
	if got != "01" {
		t.Fatalf("string Id rewritten: %#v", got)
	}
}

func TestApplyQueryLengthRemainsAvailableForCollections(t *testing.T) {
	data := map[string]interface{}{
		"Cpu":   json.Number("2.0"),
		"Items": []interface{}{"a", "b"},
	}
	got, err := ApplyQuery(data, "length(Items)")
	if err != nil {
		t.Fatal(err)
	}
	if got != float64(2) {
		t.Fatalf("length restored onto Cpu digits: %#v", got)
	}
}

func TestWriteYAMLKeepsQueriedIntegerDigits(t *testing.T) {
	data := map[string]interface{}{"AccountId": json.Number("2106494982")}
	got, err := ApplyQuery(data, "AccountId")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Write(&buf, FormatYAML, got); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "2106494982") {
		t.Fatalf("yaml lost queried integer digits: %q", out)
	}
	if strings.Contains(out, "e+") || strings.Contains(out, "E+") {
		t.Fatalf("yaml used scientific notation: %q", out)
	}
}

func TestWriteYAMLKeepsNonIntegerFloat(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, FormatYAML, map[string]interface{}{"Ratio": 1.5}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "1.5") {
		t.Fatalf("yaml dropped non-integer float: %s", buf.String())
	}
}

// TestWriteYAMLJSONNumberDecimalsAreNumbers covers the production path:
// cmd/sdk_client.go enables WithForceJsonNumberDecode, so every response
// number is a json.Number. Non-integer json.Number values must still render
// as real YAML numbers, not quoted strings, or downstream YAML consumers
// (yq, k8s, etc.) lose numeric typing.
func TestWriteYAMLJSONNumberDecimalsAreNumbers(t *testing.T) {
	data := map[string]interface{}{
		"Ratio": json.Number("0.1"),
		"Half":  json.Number("1.5"),
	}
	var buf bytes.Buffer
	if err := Write(&buf, FormatYAML, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// Production json.Number decimals must remain real YAML numbers rather than
	// quoted strings. Compare deterministic output to catch quoting or key loss.
	want := "Half: 1.5\nRatio: 0.1\n"
	if out != want {
		t.Fatalf("json.Number decimal yaml = %q, want %q", out, want)
	}
}

func TestWriteYAMLJSONNumbersPreserveTagAndLiteral(t *testing.T) {
	data := map[string]interface{}{
		"Exponent":     json.Number("1e3"),
		"ExponentPlus": json.Number("1E+3"),
		"TrailingZero": json.Number("1.0"),
		"LongDecimal":  json.Number("0.123456789012345678901"),
		"HugeInteger":  json.Number("123456789012345678901234567890"),
	}
	var buf bytes.Buffer
	if err := Write(&buf, FormatYAML, data); err != nil {
		t.Fatal(err)
	}

	var document yaml.Node
	if err := yaml.Unmarshal(buf.Bytes(), &document); err != nil {
		t.Fatalf("parse rendered YAML: %v\n%s", err, buf.String())
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		t.Fatalf("rendered YAML root = %#v, want mapping", document.Content)
	}
	values := make(map[string]*yaml.Node)
	content := document.Content[0].Content
	for i := 0; i+1 < len(content); i += 2 {
		values[content[i].Value] = content[i+1]
	}
	for key, want := range map[string]struct {
		tag, value string
	}{
		"Exponent":     {tag: "!!float", value: "1e3"},
		"ExponentPlus": {tag: "!!float", value: "1E+3"},
		"TrailingZero": {tag: "!!float", value: "1.0"},
		"LongDecimal":  {tag: "!!float", value: "0.123456789012345678901"},
		"HugeInteger":  {tag: "!!int", value: "123456789012345678901234567890"},
	} {
		node := values[key]
		if node == nil {
			t.Fatalf("missing %s in rendered YAML:\n%s", key, buf.String())
		}
		if node.Tag != want.tag || node.Value != want.value {
			t.Errorf("%s = tag %q value %q, want tag %q value %q\n%s",
				key, node.Tag, node.Value, want.tag, want.value, buf.String())
		}
	}
}

func TestWriteYAMLKeepsUint64MaxDigits(t *testing.T) {
	data := map[string]interface{}{"N": json.Number("18446744073709551615")}
	var buf bytes.Buffer
	if err := Write(&buf, FormatYAML, data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "18446744073709551615") {
		t.Fatalf("yaml lost uint64 digits: %s", buf.String())
	}
	if strings.Contains(buf.String(), "1.844") {
		t.Fatalf("yaml used scientific float: %s", buf.String())
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
