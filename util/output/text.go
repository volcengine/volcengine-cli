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
// title, so a line can be traced back to its origin without counting lines.
//
// Shape rules:
//   - map               → one TSV row of its scalar fields; each nested field
//     recurses on its own line(s), prefixed with the UPPERCASED path
//   - []map             → one row per object, sharing the union of scalar keys
//     (missing field → None); nested fields recurse, prefixed with the path
//   - [][] / [] mixed   → scalar siblings join into one row; child lists/objects
//     recurse; empty lists/objects produce no phantom rows
//   - []scalar          → a single TSV row (values joined by Tab)
//   - scalar / null     → single line
//   - past maxNestDepth → the remainder as compact JSON, as table does
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
	formatText(data, textNode{}, nil, opts, &out)
	return out
}

// textNode locates the value being formatted inside the response.
//
// path is the dotted field path the value was reached through and becomes the
// UPPERCASED row prefix; the empty path means "top level" (no prefix).
//
// index/total record the value's position in its parent list. They exist so the
// nested rows of one record can be told apart from another's: without them the
// TAGS lines of every instance carry the same label and are distinguishable
// only by line order.
type textNode struct {
	path string
	// index is 1-based. Both are 0 when the value is not a list element.
	index, total int
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
// The parent's list position is appended, so `RESULT.INSTANCELIST.TAGS[2]` names
// the tags of the second record and matches how table titles that section. A
// parent list holding a single element adds no index, keeping the common
// one-record response unadorned.
func (n textNode) child(key string) textNode {
	path := joinTitle(n.path, key)
	if n.total > 1 {
		path += "[" + itoa(n.index) + "]"
	}
	return textNode{path: path, depth: n.depth + 1}
}

// element is the node for list[i] of a list of length total. The path is left
// alone: all records of one list share a row label, so a script can still
// select every record with a single exact-match comparison on the first column.
// A list element sits at the depth of its list, again matching buildSections.
func (n textNode) element(i, total int) textNode {
	return textNode{path: n.path, index: i + 1, total: total, depth: n.depth}
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
		for i, element := range list {
			if !isStructuredValue(element) {
				formatScalarList([]interface{}{element}, node, out)
				continue
			}
			formatText(element, node.element(i, len(list)), keys, listOpts, out)
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
		for i, element := range list {
			if isStructuredValue(element) {
				formatText(element, node.element(i, len(list)), nil, opts, out)
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
		formatText(field.value, node.child(field.key), nil, opts, out)
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
