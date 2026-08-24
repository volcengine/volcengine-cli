package output

import (
	"io"
	"sort"
	"strings"
)

// writeText emits tab-separated values.
//
// It mirrors the AWS CLI text formatter: ANY response is recursively flattened
// into TSV lines, so a bare `--output text` (no --query) never prints a JSON
// blob. Nested objects and lists are prefixed with the UPPERCASED field name
// they came from (the "identifier"), which keeps otherwise-ambiguous nested
// records traceable to their source key.
//
// Shape rules:
//   - map               → one TSV row of its scalar fields; each nested field
//     recurses on its own line(s), prefixed with the UPPERCASED key
//   - []map             → one row per object, sharing the union of scalar keys
//     (missing field → None); nested fields recurse, prefixed with the key
//   - [][] / [] mixed   → scalar siblings join into one row; child lists/objects
//     recurse; empty lists/objects produce no phantom rows
//   - []scalar          → a single TSV row (values joined by Tab)
//   - scalar / null     → single line
//
// The --query multiselect-hash column order (opts.Columns) is still honored for
// object field ordering. Text stays free of headers and borders so it pipes
// cleanly into line tools (awk/grep/nl operate per line).
func writeText(w io.Writer, data interface{}, opts Options) error {
	for _, line := range textLines(data, opts) {
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			return err
		}
	}
	return nil
}

func textLines(data interface{}, opts Options) []string {
	var out []string
	formatText(data, "", nil, opts, &out)
	return out
}

// formatText dispatches by JSON kind. identifier is the UPPERCASED-on-write key
// name a nested value was reached through; the empty string means "top level"
// (no prefix). scalarKeys, when non-nil, is the shared column set computed for a
// list of objects so every row lines up even when a field is missing.
func formatText(item interface{}, identifier string, scalarKeys []string, opts Options, out *[]string) {
	switch v := item.(type) {
	case map[string]interface{}:
		formatDict(v, identifier, scalarKeys, opts, out)
	case []interface{}:
		formatList(v, identifier, opts, out)
	default:
		*out = append(*out, scalarString(item))
	}
}

// formatList flattens a list. A list with any object shares one column set
// across its objects; a list with nested lists emits its scalar siblings on one
// row and recurses into the nested lists; a pure scalar list joins into a row.
func formatList(list []interface{}, identifier string, opts Options, out *[]string) {
	if len(list) == 0 {
		return
	}
	if listHasMap(list) {
		orderedKeys := listObjectKeys(list, opts)
		keys := listScalarKeys(list, orderedKeys)
		listOpts := opts
		listOpts.Columns = orderedKeys
		for _, element := range list {
			if !isStructuredValue(element) {
				formatScalarList([]interface{}{element}, identifier, out)
				continue
			}
			formatText(element, identifier, keys, listOpts, out)
		}
		return
	}
	if listHasList(list) {
		var scalars []interface{}
		var nested []interface{}
		for _, element := range list {
			if isStructuredValue(element) {
				nested = append(nested, element)
			} else {
				scalars = append(scalars, element)
			}
		}
		if len(scalars) > 0 {
			formatScalarList(scalars, identifier, out)
		}
		for _, element := range nested {
			formatText(element, identifier, nil, opts, out)
		}
		return
	}
	formatScalarList(list, identifier, out)
}

// formatScalarList writes a flat list. With an identifier every value gets its
// own prefixed line; without one the values join into a single Tab-separated
// row.
func formatScalarList(elements []interface{}, identifier string, out *[]string) {
	if identifier != "" {
		prefix := escapeCellString(strings.ToUpper(identifier))
		for _, item := range elements {
			*out = append(*out, prefix+"\t"+scalarString(item))
		}
		return
	}
	cols := make([]string, len(elements))
	for i, item := range elements {
		cols[i] = scalarString(item)
	}
	*out = append(*out, strings.Join(cols, "\t"))
}

// formatDict writes an object's scalar fields as one row (prefixed by the
// identifier when nested), then recurses into each structured field using that
// field's key as the next identifier.
func formatDict(m map[string]interface{}, identifier string, scalarKeys []string, opts Options, out *[]string) {
	scalars, nested := partitionDict(m, scalarKeys, opts)
	if len(scalars) > 0 {
		if identifier != "" {
			scalars = append([]string{escapeCellString(strings.ToUpper(identifier))}, scalars...)
		}
		*out = append(*out, strings.Join(scalars, "\t"))
	}
	for _, field := range nested {
		formatText(field.value, field.key, nil, opts, out)
	}
}

type textField struct {
	key   string
	value interface{}
}

// partitionDict splits an object into a scalar row and the structured fields
// that must recurse. When scalarKeys is provided (object came from a list) every
// listed key is emitted in order so rows align, with missing fields shown as
// None; the remaining keys recurse. Otherwise the object's own keys are used,
// honoring the --query column order.
func partitionDict(m map[string]interface{}, scalarKeys []string, opts Options) (scalars []string, nested []textField) {
	if scalarKeys == nil {
		for _, key := range applyColumnOrder(sortedMapKeys(m), opts.Columns) {
			value := m[key]
			if isStructuredValue(value) {
				nested = append(nested, textField{key: key, value: value})
			} else {
				scalars = append(scalars, scalarString(value))
			}
		}
		return scalars, nested
	}

	nestedKeys := make([]string, 0, len(m))
	for _, key := range scalarKeys {
		value, ok := m[key]
		switch {
		case !ok:
			scalars = append(scalars, noneValue)
		case isStructuredValue(value):
			scalars = append(scalars, noneValue)
			nestedKeys = append(nestedKeys, key)
		default:
			scalars = append(scalars, scalarString(value))
		}
	}
	shared := make(map[string]struct{}, len(scalarKeys))
	for _, key := range scalarKeys {
		shared[key] = struct{}{}
	}
	remaining := make([]string, 0, len(m))
	for key := range m {
		if _, ok := shared[key]; !ok {
			remaining = append(remaining, key)
		}
	}
	nestedKeys = append(nestedKeys, remaining...)
	for _, key := range orderedSubset(opts.Columns, nestedKeys) {
		nested = append(nested, textField{key: key, value: m[key]})
	}
	return scalars, nested
}

// listObjectKeys returns the complete object-key union in query order when the
// hint matches that union exactly, otherwise in alphabetical order.
func listObjectKeys(list []interface{}, opts Options) []string {
	seen := make(map[string]struct{})
	for _, element := range list {
		m, ok := element.(map[string]interface{})
		if !ok {
			continue
		}
		for key := range m {
			seen[key] = struct{}{}
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return applyColumnOrder(keys, opts.Columns)
}

// listScalarKeys is the union of keys that are scalar in at least one object
// of the list, kept as a subsequence of the validated complete object order.
// It mirrors the AWS CLI's shared-column behavior.
func listScalarKeys(list []interface{}, orderedKeys []string) []string {
	seen := make(map[string]struct{})
	for _, element := range list {
		m, ok := element.(map[string]interface{})
		if !ok {
			continue
		}
		for key, value := range m {
			if !isStructuredValue(value) {
				seen[key] = struct{}{}
			}
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	return orderedSubset(orderedKeys, keys)
}

func isStructuredValue(v interface{}) bool {
	switch v.(type) {
	case map[string]interface{}, []interface{}:
		return true
	}
	return false
}

func listHasMap(list []interface{}) bool {
	for _, value := range list {
		if _, ok := value.(map[string]interface{}); ok {
			return true
		}
	}
	return false
}

func listHasList(list []interface{}) bool {
	for _, value := range list {
		if _, ok := value.([]interface{}); ok {
			return true
		}
	}
	return false
}
