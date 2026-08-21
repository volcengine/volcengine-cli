package output

// Shared table/text helpers: scalar/cell rendering, key order, and list shape.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const noneValue = "None"

func sortedMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// scalarString renders a value for table/text cells.
// Nested maps/slices become compact JSON; nil becomes noneValue.
func scalarString(v interface{}) string {
	if v == nil {
		return noneValue
	}
	switch x := v.(type) {
	case string:
		return escapeCellString(x)
	case bool:
		// Human-readable formats use title-case booleans. JSON and YAML retain
		// their native lowercase boolean syntax in their own encoders.
		if x {
			return "True"
		}
		return "False"
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case json.Number:
		return x.String()
	case map[string]interface{}, []interface{}:
		return compactJSON(x)
	default:
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Map, reflect.Slice, reflect.Array:
			return compactJSON(v)
		default:
			return fmt.Sprint(v)
		}
	}
}

func escapeCellString(value string) string {
	var escaped strings.Builder
	for _, r := range value {
		switch r {
		case '\n':
			escaped.WriteString(`\n`)
		case '\r':
			escaped.WriteString(`\r`)
		case '\t':
			escaped.WriteString(`\t`)
		case '\\':
			escaped.WriteString(`\\`)
		default:
			if isDangerousBidiControl(r) {
				fmt.Fprintf(&escaped, `\u%04X`, r)
				continue
			}
			if unicode.IsControl(r) {
				if r <= 0xff {
					fmt.Fprintf(&escaped, `\x%02X`, r)
				} else {
					fmt.Fprintf(&escaped, `\u%04X`, r)
				}
				continue
			}
			escaped.WriteRune(r)
		}
	}
	return escaped.String()
}

func isDangerousBidiControl(r rune) bool {
	switch r {
	case '\u061c', '\u200e', '\u200f',
		'\u202a', '\u202b', '\u202c', '\u202d', '\u202e',
		'\u2066', '\u2067', '\u2068', '\u2069':
		// These format controls can visually reorder terminal output but are
		// not matched by unicode.IsControl (they are category Cf).
		return true
	default:
		return false
	}
}

func isRawTerminalControl(r rune) bool {
	return unicode.IsControl(r) || isDangerousBidiControl(r)
}

// escapeRawTerminalControls makes raw terminal controls visible without
// touching backslash escapes that JSON has already produced.
func escapeRawTerminalControls(value string) string {
	if strings.IndexFunc(value, isRawTerminalControl) < 0 {
		return value
	}
	var escaped strings.Builder
	escaped.Grow(len(value))
	for _, r := range value {
		if isRawTerminalControl(r) {
			fmt.Fprintf(&escaped, `\u%04X`, r)
			continue
		}
		escaped.WriteRune(r)
	}
	return escaped.String()
}

func compactJSON(v interface{}) string {
	if v == nil {
		return "null"
	}
	buf := bytes.NewBuffer(nil)
	encoder := json.NewEncoder(buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return escapeRawTerminalControls(fmt.Sprint(v))
	}
	return escapeRawTerminalControls(strings.TrimSuffix(buf.String(), "\n"))
}

func allMaps(rows []interface{}) bool {
	if len(rows) == 0 {
		return true // empty list is a valid empty object-list
	}
	for _, r := range rows {
		if _, ok := r.(map[string]interface{}); !ok {
			return false
		}
	}
	return true
}

func allSlices(rows []interface{}) bool {
	if len(rows) == 0 {
		return false
	}
	for _, r := range rows {
		if _, ok := r.([]interface{}); !ok {
			return false
		}
	}
	return true
}

func unionMapKeys(rows []interface{}) []string {
	seen := make(map[string]struct{})
	for _, r := range rows {
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		for k := range m {
			seen[k] = struct{}{}
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
