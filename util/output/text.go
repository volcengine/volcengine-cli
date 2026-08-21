package output

import (
	"io"
	"strings"
)

// writeText emits tab-separated values.
//
// Shape rules match table (no nested auto-unwrap — use --query for nested lists):
//   - []map           → one TSV row per object (column order from opts.Columns,
//     otherwise sorted; missing → None)
//   - map             → single TSV row of sorted values
//   - [][]             → one TSV row per projected row; deeper nested rows are
//     flattened recursively and empty nested lists do not create blank lines
//   - []scalar         → one value per line
//   - scalar / null   → single line
//
// Text stays free of headers and borders so it pipes cleanly into line tools
// (`--output text | nl` numbers data rows, awk/grep operate per record).
func writeText(w io.Writer, data interface{}, opts Options) error {
	// Same display-layer strip as table: text is for humans and line tools, so
	// the ResponseMetadata envelope would only add a noise row. A --query result
	// is left untouched — see Options.Queried.
	display := data
	if !opts.Queried {
		display = stripResponseMetadata(data)
	}
	for _, line := range textLines(display, opts) {
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			return err
		}
	}
	return nil
}

func textLines(data interface{}, opts Options) []string {
	if data == nil {
		return []string{noneValue}
	}
	if s, ok := data.([]interface{}); ok {
		if allMaps(s) {
			return textFromObjectList(s, opts)
		}
		return textFromSlice(s, opts)
	}
	if m, ok := data.(map[string]interface{}); ok {
		return textFromKeyValue(m, opts)
	}
	return []string{scalarString(data)}
}

// textFromKeyValue emits one TSV line of values.
//
// The --query column-order hint applies here too: a single object is exactly
// the shape produced by `--query '{Name:A,Id:B}'` on a non-list result, and
// scripts read those fields positionally ($1, $2). Falling back to alphabetical
// order here would hand them the wrong column.
func textFromKeyValue(m map[string]interface{}, opts Options) []string {
	keys := applyColumnOrder(sortedMapKeys(m), opts.Columns)
	if len(keys) == 0 {
		return nil
	}
	cols := make([]string, 0, len(keys))
	for _, k := range keys {
		cols = append(cols, scalarString(m[k]))
	}
	return []string{strings.Join(cols, "\t")}
}

func textFromObjectList(list []interface{}, opts Options) []string {
	headers, rows := objectListMatrix(list, opts)
	// A list containing only empty objects has no fields and therefore no TSV
	// records. Emitting one empty line per object would turn structural emptiness
	// into phantom data rows and disagree with a standalone empty object.
	if len(headers) == 0 || len(rows) == 0 {
		return nil
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, strings.Join(row, "\t"))
	}
	return out
}

func textFromSlice(s []interface{}, opts Options) []string {
	if len(s) == 0 {
		return nil
	}
	if hasNestedTextValue(s) {
		return textFromNestedSlice(s, opts)
	}
	out := make([]string, 0, len(s))
	for _, v := range s {
		out = append(out, scalarString(v))
	}
	return out
}

// textFromNestedSlice formats positional projections recursively. JMESPath can
// produce shapes deeper than [][] (for example Reservations[].Instances[].[]),
// and an empty inner projection must be truly empty rather than a phantom blank
// record. Scalar siblings stay on one TSV row; child lists continue below it.
// The top-level []scalar rule intentionally remains one value per line.
func textFromNestedSlice(s []interface{}, opts Options) []string {
	if len(s) == 0 {
		return nil
	}
	if allMaps(s) {
		return textFromObjectList(s, opts)
	}
	// A heterogeneous list containing objects still needs to expand those
	// objects instead of serializing them as JSON. Use the union of object keys
	// so every object row has a stable positional schema.
	if hasMapValue(s) {
		keys := applyColumnOrder(unionMapKeys(s), opts.Columns)
		var out []string
		for _, value := range s {
			switch value := value.(type) {
			case map[string]interface{}:
				if len(keys) == 0 {
					// Empty objects do not create blank records. Scalars and child
					// lists in the same heterogeneous projection still render.
					continue
				}
				row := make([]string, len(keys))
				for i, key := range keys {
					item, present := value[key]
					if !present {
						row[i] = noneValue
					} else {
						row[i] = scalarString(item)
					}
				}
				out = append(out, strings.Join(row, "\t"))
			case []interface{}:
				out = append(out, textFromNestedSlice(value, opts)...)
			default:
				out = append(out, scalarString(value))
			}
		}
		return out
	}
	var scalars []string
	var nested [][]interface{}
	for _, value := range s {
		if child, ok := value.([]interface{}); ok {
			nested = append(nested, child)
			continue
		}
		scalars = append(scalars, scalarString(value))
	}
	out := make([]string, 0, 1+len(nested))
	if len(scalars) > 0 {
		out = append(out, strings.Join(scalars, "\t"))
	}
	for _, child := range nested {
		out = append(out, textFromNestedSlice(child, opts)...)
	}
	return out
}

func hasNestedTextValue(s []interface{}) bool {
	for _, value := range s {
		switch value.(type) {
		case map[string]interface{}, []interface{}:
			return true
		}
	}
	return false
}

func hasMapValue(s []interface{}) bool {
	for _, value := range s {
		if _, ok := value.(map[string]interface{}); ok {
			return true
		}
	}
	return false
}
