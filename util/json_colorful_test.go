package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
