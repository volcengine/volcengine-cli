package util

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"testing"
)

var (
	empty   = map[string]interface{}{}
	simple1 = map[string]interface{}{
		"k1": "v1",
	}
	simple2 = map[string]interface{}{
		"k1": 3.14,
	}
	simple3 = map[string]interface{}{
		"k1": 1 << 31,
	}
	simple4 = map[string]interface{}{
		"k1": true,
	}
	simple5 = map[string]interface{}{
		"k1": json.Number("3"),
		"k2": json.Number("4.1313"),
	}

	nestedMap = map[string]interface{}{
		"k1": map[string]interface{}{
			"k2": "v2",
		},
	}
	nestedArray = map[string]interface{}{
		"k1": []interface{}{
			"string",
			3.14159,
			1 << 31,
			true,
		},
	}

	complicated = map[string]interface{}{
		"a1": []interface{}{
			"string",
			3.14159,
			1 << 31,
			true,
			map[string]interface{}{
				"m1": map[string]interface{}{
					"s1": "v1",
					"i1": 33333,
					"f1": 1231.5241,
					"b":  true,
				},
			},
		},
		"m2": map[string]interface{}{
			"a3": []interface{}{
				"string",
				3.14159,
				1 << 31,
				true,
				map[string]interface{}{
					"m1": map[string]interface{}{
						"s1": "v1",
						"i1": 33333,
						"f1": 1231.5241,
						"b":  true,
					},
				},
			},
		},
	}
)

func TestColorfulJson(t *testing.T) {
	checkValid(nil)
	checkValid(empty)

	checkValid(simple1)
	checkValid(simple2)
	checkValid(simple3)
	checkValid(simple4)
	checkValid(simple5)

	checkValid(nestedMap)
	checkValid(nestedArray)

	checkValid(complicated)
}

func TestWriteJsonColorOutputRemainsValidAfterRemovingANSI(t *testing.T) {
	data := map[string]interface{}{
		"control": "a\x1bb\nc",
		"quoted":  `"value"`,
		"html":    "<x>",
		"number":  json.Number("9223372036854775807"),
		"boolean": true,
		"null":    nil,
	}
	var output bytes.Buffer
	if err := WriteJson(&output, data, true); err != nil {
		t.Fatal(err)
	}
	stripped := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAll(output.Bytes(), nil)
	if !json.Valid(stripped) {
		t.Fatalf("colored output is not valid JSON after ANSI removal:\n%s", stripped)
	}
	if !bytes.Contains(stripped, []byte(`"a\u001bb\nc"`)) {
		t.Fatalf("control characters were not JSON-escaped: %q", stripped)
	}
}

func TestWriteJsonPropagatesWriterErrors(t *testing.T) {
	writerErr := errors.New("write failed")
	writer := writerFunc(func([]byte) (int, error) {
		return 0, writerErr
	})
	if err := WriteJson(writer, map[string]interface{}{"A": "b"}, true); !errors.Is(err, writerErr) {
		t.Fatalf("error = %v, want %v", err, writerErr)
	}
}

func TestWriteJsonDetectsShortWrite(t *testing.T) {
	writer := writerFunc(func(p []byte) (int, error) {
		if len(p) == 0 {
			return 0, nil
		}
		return 1, nil
	})
	if err := WriteJson(writer, map[string]interface{}{"A": "b"}, true); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("error = %v, want io.ErrShortWrite", err)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}

func checkValid(data interface{}) {
	stdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	os.Stdout = w

	colorfulJsonTest(data, 0, false, true)

	w.Close()
	os.Stdout = stdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	if data == nil && !(buf.String() == "null") {
		panic("invalid json output")
	}

	if !json.Valid(buf.Bytes()) {
		panic("invalid json output")
	}
}

// test colorfulJson, not to print color character to check json
func colorfulJsonTest(data interface{}, indent int, indentValue, lastValue bool) {
	if data == nil {
		if !lastValue {
			printfWithIndent(0, "null,")
		} else {
			printfWithIndent(0, "null")
		}
		return
	}

	switch v := data.(type) {
	case map[string]interface{}:
		if !indentValue {
			printlnWithIndent(0, "{")
		} else {
			printlnWithIndent(indent, "{")
		}
		defer func() {
			printWithIndent(indent, "}")
			if !lastValue {
				fmt.Print(",\n")
			} else {
				fmt.Print("\n")
			}
		}()

		loop, mapLen := 1, len(v)
		for k1, v1 := range v {
			printfWithIndent(indent+1, "%q", k1)
			fmt.Print(": ")
			colorfulJsonTest(v1, indent+1, false, loop == mapLen)
			loop++
		}
	case []interface{}:
		if !indentValue {
			printlnWithIndent(0, "[")
		} else {
			printlnWithIndent(indent, "[")
		}
		defer func() {
			printWithIndent(indent, "]")
			if !lastValue {
				fmt.Print(",\n")
			} else {
				fmt.Print("\n")
			}
		}()

		loop, arrLen := 1, len(v)
		for _, v1 := range v {
			colorfulJsonTest(v1, indent+1, true, loop == arrLen)
			loop++
		}
	case string:
		if indentValue {
			printfWithIndent(indent, "%q", v)
		} else {
			printfWithIndent(0, "%q", v)
		}
		if !lastValue {
			fmt.Print(",\n")
		} else {
			fmt.Print("\n")
		}
	case json.Number:
		if indentValue {
			printfWithIndent(indent, "%v", v)
		} else {
			printfWithIndent(0, "%v", v)
		}
		if !lastValue {
			fmt.Print(",\n")
		} else {
			fmt.Print("\n")
		}
	case bool:
		if indentValue {
			printfWithIndent(indent, "%v", v)
		} else {
			printfWithIndent(0, "%v", v)
		}
		if !lastValue {
			fmt.Print(",\n")
		} else {
			fmt.Print("\n")
		}
	default:
		if indentValue {
			printfWithIndent(indent, "%v", v)
		} else {
			printfWithIndent(0, "%v", v)
		}
		if !lastValue {
			fmt.Print(",\n")
		} else {
			fmt.Print("\n")
		}
	}
}
