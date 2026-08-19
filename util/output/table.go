package output

import (
	"io"
	"strings"

	"github.com/mattn/go-runewidth"
)

// writeTable emits an ASCII table (practical subset).
//
// Shape rules (no nested auto-unwrap — use --query for Result.Instances etc.):
//   - []map           → multi-column object table
//   - map             → Key | Value
//   - [][] / []scalar → projection / single column
//   - scalar / null   → single cell
func writeTable(w io.Writer, data interface{}) error {
	headers, rows := tableMatrix(data)
	if len(headers) == 0 && len(rows) == 0 {
		_, err := io.WriteString(w, "(empty)\n")
		return err
	}
	return renderTable(w, headers, rows)
}

func tableMatrix(data interface{}) (headers []string, rows [][]string) {
	if data == nil {
		return []string{"Value"}, [][]string{{noneValue}}
	}
	if s, ok := data.([]interface{}); ok {
		if allMaps(s) {
			return objectListMatrix(s)
		}
		return tableFromSlice(s)
	}
	if m, ok := data.(map[string]interface{}); ok {
		return tableFromKeyValue(m)
	}
	return []string{"Value"}, [][]string{{scalarString(data)}}
}

func tableFromKeyValue(m map[string]interface{}) (headers []string, rows [][]string) {
	keys := sortedMapKeys(m)
	headers = []string{"Key", "Value"}
	rows = make([][]string, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, []string{escapeCellString(k), scalarString(m[k])})
	}
	return headers, rows
}

func tableFromSlice(s []interface{}) (headers []string, rows [][]string) {
	if len(s) == 0 {
		return nil, nil
	}
	if allSlices(s) {
		maxCols := 0
		for _, row := range s {
			arr, ok := row.([]interface{})
			if !ok {
				continue
			}
			if len(arr) > maxCols {
				maxCols = len(arr)
			}
		}
		rows = make([][]string, 0, len(s))
		for _, row := range s {
			arr, ok := row.([]interface{})
			if !ok {
				continue
			}
			cells := make([]string, maxCols)
			for i := 0; i < maxCols; i++ {
				if i < len(arr) {
					cells[i] = scalarString(arr[i])
				} else {
					cells[i] = noneValue
				}
			}
			rows = append(rows, cells)
		}
		return nil, rows
	}
	headers = []string{"Value"}
	rows = make([][]string, 0, len(s))
	for _, v := range s {
		rows = append(rows, []string{scalarString(v)})
	}
	return headers, rows
}

func renderTable(w io.Writer, headers []string, rows [][]string) error {
	cols := 0
	if len(headers) > cols {
		cols = len(headers)
	}
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		_, err := io.WriteString(w, "(empty)\n")
		return err
	}

	norm := make([][]string, len(rows))
	for i, r := range rows {
		nr := make([]string, cols)
		copy(nr, r)
		norm[i] = nr
	}
	widths := make([]int, cols)
	for i := 0; i < cols && i < len(headers); i++ {
		widths[i] = displayWidth(headers[i])
	}
	for _, r := range norm {
		for i := 0; i < cols; i++ {
			if dw := displayWidth(r[i]); dw > widths[i] {
				widths[i] = dw
			}
		}
	}
	for i := range widths {
		if widths[i] < 1 {
			widths[i] = 1
		}
	}

	border := tableBorder(widths)
	if err := writeTableLine(w, border); err != nil {
		return err
	}
	if len(headers) > 0 {
		h := make([]string, cols)
		copy(h, headers)
		if err := writeTableLine(w, tableRow(h, widths)); err != nil {
			return err
		}
		if err := writeTableLine(w, border); err != nil {
			return err
		}
	}
	for _, r := range norm {
		if err := writeTableLine(w, tableRow(r, widths)); err != nil {
			return err
		}
	}
	return writeTableLine(w, border)
}

func writeTableLine(w io.Writer, line string) error {
	_, err := io.WriteString(w, line+"\n")
	return err
}

func tableBorder(widths []int) string {
	var b strings.Builder
	b.WriteByte('+')
	for _, w := range widths {
		b.WriteByte('-')
		b.WriteString(strings.Repeat("-", w))
		b.WriteByte('-')
		b.WriteByte('+')
	}
	return b.String()
}

func tableRow(cells []string, widths []int) string {
	var b strings.Builder
	b.WriteByte('|')
	for i, w := range widths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		b.WriteByte(' ')
		b.WriteString(padRight(cell, w))
		b.WriteByte(' ')
		b.WriteByte('|')
	}
	return b.String()
}

func displayWidth(s string) int {
	return runewidth.StringWidth(s)
}

func padRight(s string, width int) string {
	pad := width - displayWidth(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}
