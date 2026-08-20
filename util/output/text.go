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
//   - [][] / []scalar → projection / one value per line
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
		return textFromSlice(s)
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
	_, rows := objectListMatrix(list, opts)
	if len(rows) == 0 {
		return nil
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, strings.Join(row, "\t"))
	}
	return out
}

func textFromSlice(s []interface{}) []string {
	if len(s) == 0 {
		return nil
	}
	if allSlices(s) {
		out := make([]string, 0, len(s))
		for _, row := range s {
			arr, ok := row.([]interface{})
			if !ok {
				continue
			}
			cols := make([]string, len(arr))
			for i, c := range arr {
				cols[i] = scalarString(c)
			}
			out = append(out, strings.Join(cols, "\t"))
		}
		return out
	}
	out := make([]string, 0, len(s))
	for _, v := range s {
		out = append(out, scalarString(v))
	}
	return out
}
