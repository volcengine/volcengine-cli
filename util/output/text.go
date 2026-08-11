package output

import (
	"io"
	"strings"
)

// writeText emits tab-separated values (AWS-inspired).
//
// Shape rules match table (no nested auto-unwrap — use --query for nested lists):
//   - []map           → one TSV row per object (sorted columns; missing → None)
//   - map             → single TSV row of sorted values
//   - [][] / []scalar → projection / one value per line
//   - scalar / null   → single line
func writeText(w io.Writer, data interface{}) error {
	for _, line := range textLines(data) {
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			return err
		}
	}
	return nil
}

func textLines(data interface{}) []string {
	if data == nil {
		return []string{noneValue}
	}
	if s, ok := data.([]interface{}); ok {
		if allMaps(s) {
			return textFromObjectList(s)
		}
		return textFromSlice(s)
	}
	if m, ok := data.(map[string]interface{}); ok {
		return textFromKeyValue(m)
	}
	return []string{scalarString(data)}
}

func textFromKeyValue(m map[string]interface{}) []string {
	keys := sortedMapKeys(m)
	if len(keys) == 0 {
		return nil
	}
	cols := make([]string, 0, len(keys))
	for _, k := range keys {
		cols = append(cols, scalarString(m[k]))
	}
	return []string{strings.Join(cols, "\t")}
}

func textFromObjectList(list []interface{}) []string {
	_, rows := objectListMatrix(list)
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
