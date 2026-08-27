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
// blob. Nested objects and lists are prefixed with their UPPERCASED field path
// (`RESULT.INSTANCELIST`), naming the same node as the matching table section
// title, so a line can be traced back to its origin without counting lines. The
// label is the path only: unlike a table section title it carries no record
// number, so it does not change when the response returns one record or ten.
//
// Shape rules:
//   - map               → one TSV row of its scalar fields; each nested field
//     recurses on its own line(s), prefixed with the UPPERCASED path
//   - []map             → one row per object, sharing the union of scalar keys
//     (missing field → None); nested fields recurse, prefixed with the path. A
//     shared column that is structured on some record shows the value inline
//     or points at the lines below — see partitionDict
//   - [][] / [] mixed   → scalar siblings join into one row; child lists/objects
//     recurse; empty lists/objects produce no phantom rows
//   - []scalar          → a single TSV row (values joined by Tab)
//   - scalar / null     → single line
//   - past maxNestDepth → the remainder as compact JSON, as table does
//
// The --query multiselect-hash column order (opts.Columns) is honored for the
// level the hash projected, and dropped below it, exactly as buildSections does.
// Text stays free of headers and borders so it pipes cleanly into line tools
// (awk/grep/nl operate per line).
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
	formatText(data, textNode{}, nil, opts, &out)
	return out
}

// textNode locates the value being formatted inside the response.
//
// path is the dotted field path the value was reached through and becomes the
// UPPERCASED row prefix; the empty path means "top level" (no prefix).
type textNode struct {
	path string
	// depth counts the nested fields descended through, counted the way
	// buildSections counts it so both formats stop flattening at the same node.
	depth int
}

// prefix is the row label. Response keys are untrusted terminal text, so
// controls are escaped here exactly as they are in values.
func (n textNode) prefix() string {
	if n.path == "" {
		return ""
	}
	return escapeCellString(strings.ToUpper(n.path))
}

// child is the node reached by descending into key.
//
// The label is the field path alone, never the position of the record it came
// from: a label that carried `[2]` would depend on how many records the response
// happened to contain, so `$1 == "RESULT.INSTANCELIST.TAGS"` would match a
// one-record response and silently match nothing once a second record appeared.
//
// Attribution comes from line order instead, as it does in the AWS CLI text
// formatter: a record's nested lines are emitted immediately after that record's
// own row, so a script tracks the last record row it saw. `--output table` still
// numbers its sections, because a table prints every record before any nested
// section and so has no adjacency to rely on.
func (n textNode) child(key string) textNode {
	return textNode{path: joinTitle(n.path, key), depth: n.depth + 1}
}

// formatText dispatches by JSON kind. scalarKeys, when non-nil, is the shared
// column set computed for a list of objects so every row lines up even when a
// field is missing.
func formatText(item interface{}, node textNode, scalarKeys []string, opts Options, out *[]string) {
	if node.depth > maxNestDepth {
		formatScalarList([]interface{}{item}, node, out)
		return
	}
	switch v := item.(type) {
	case map[string]interface{}:
		formatDict(v, node, scalarKeys, opts, out)
	case []interface{}:
		formatList(v, node, opts, out)
	default:
		*out = append(*out, scalarString(item))
	}
}

// formatList flattens a list. A list with any object shares one column set
// across its objects; a list with nested lists emits its scalar siblings on one
// row and recurses into the nested lists; a pure scalar list joins into a row.
func formatList(list []interface{}, node textNode, opts Options, out *[]string) {
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
				formatScalarList([]interface{}{element}, node, out)
				continue
			}
			formatText(element, node, keys, listOpts, out)
		}
		return
	}
	if listHasList(list) {
		var scalars []interface{}
		for _, element := range list {
			if !isStructuredValue(element) {
				scalars = append(scalars, element)
			}
		}
		if len(scalars) > 0 {
			formatScalarList(scalars, node, out)
		}
		for _, element := range list {
			if isStructuredValue(element) {
				// List nesting is transparent flattening, not a step down into
				// a field: `[[{...}]]` still renders the rows the query
				// projected, so the column-order hint stays in effect.
				formatText(element, node, nil, opts, out)
			}
		}
		return
	}
	formatScalarList(list, node, out)
}

// formatScalarList writes a flat list. Below the top level every value gets its
// own prefixed line; at the top level the values join into a single
// Tab-separated row.
func formatScalarList(elements []interface{}, node textNode, out *[]string) {
	if prefix := node.prefix(); prefix != "" {
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

// formatDict writes an object's scalar fields as one row (prefixed by its path
// when nested), then recurses into each structured field under that field's
// path.
func formatDict(m map[string]interface{}, node textNode, scalarKeys []string, opts Options, out *[]string) {
	scalars, nested := partitionDict(m, scalarKeys, opts)
	if len(scalars) > 0 {
		if prefix := node.prefix(); prefix != "" {
			scalars = append([]string{prefix}, scalars...)
		}
		*out = append(*out, strings.Join(scalars, "\t"))
	}
	for _, field := range nested {
		// The column-order hint describes the one level the --query multiselect
		// hash projected, so it is dropped on the way down. buildSections does
		// the same, keeping the two formats on the same key order.
		formatText(field.value, node.child(field.key), nil, Options{}, out)
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
//
// A shared column is not necessarily scalar in every record: a field can be a
// string on one instance and a list on the next. That cell must still show the
// value, and must not read None, which is how this row reports a field that is
// genuinely absent or null. It points at the flattened lines below only when
// those lines exist (see producesTextLines); otherwise the value is rendered
// inline, which is also what table puts in the same cell.
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
		case isNested(value) && producesTextLines(value):
			scalars = append(scalars, nestedPlaceholder)
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

// producesTextLines reports whether flattening v emits at least one line.
//
// text has no cell to park an empty container in: nesting that bottoms out in
// `{}` or `[]` alone flattens to nothing at all. A cell may only point at the
// lines below (nestedPlaceholder) once those lines are known to exist, otherwise
// the row would name a line the reader can never find. Such a value is rendered
// inline instead, which at least shows its shape.
func producesTextLines(v interface{}) bool {
	switch x := v.(type) {
	case map[string]interface{}:
		for _, field := range x {
			if producesTextLines(field) {
				return true
			}
		}
		return false
	case []interface{}:
		for _, element := range x {
			if producesTextLines(element) {
				return true
			}
		}
		return false
	default:
		return true
	}
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
