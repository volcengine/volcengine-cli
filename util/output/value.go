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
		return strconv.FormatBool(x)
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

func compactJSON(v interface{}) string {
	if v == nil {
		return "null"
	}
	buf := bytes.NewBuffer(nil)
	encoder := json.NewEncoder(buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimSuffix(buf.String(), "\n")
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

// objectListMatrix builds headers/rows for a list of maps.
// Missing fields and nulls both use noneValue so table and text match.
func objectListMatrix(list []interface{}) (headers []string, rows [][]string) {
	keys := unionMapKeys(list)
	headers = make([]string, len(keys))
	for i, key := range keys {
		headers[i] = escapeCellString(key)
	}
	rows = make([][]string, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		row := make([]string, len(keys))
		for i, key := range keys {
			if v, ok := m[key]; ok {
				row[i] = scalarString(v)
			} else {
				row[i] = noneValue
			}
		}
		rows = append(rows, row)
	}
	return headers, rows
}
