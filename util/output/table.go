package output

import (
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

func itoa(i int) string { return strconv.Itoa(i) }

// writeTable emits an ASCII table.
//
// Pipeline (all steps are presentational; json/yaml are unaffected):
//
//	data → strip ResponseMetadata → sections (nested split)
//	     → column order → [row numbers] → [verticalize] → width fit → render
//
// Shape rules:
//   - []map           → multi-column record table (+ sections for nested fields)
//   - map             → Key | Value (+ sections for nested fields)
//   - [][] / []scalar → projection / single column
//   - scalar / null   → single cell
func writeTable(w io.Writer, data interface{}, opts Options, numbered bool) error {
	// ResponseMetadata is diagnostic, not payload: dropping it here is what
	// makes a bare `--output table` readable. json/yaml keep the full envelope.
	// A --query result is left untouched — see Options.Queried.
	display := data
	if !opts.Queried {
		display = stripResponseMetadata(data)
	}

	sections := buildSections(display, opts, "", 0)
	if len(sections) == 0 {
		_, err := io.WriteString(w, "(empty)\n")
		return err
	}

	width := opts.TerminalWidth
	if width == 0 {
		width = terminalWidth(stdoutForWidth(w))
	}

	for i, sec := range sections {
		if i > 0 {
			if _, err := io.WriteString(w, "\n"); err != nil {
				return err
			}
		}
		if err := renderSection(w, sec, opts, numbered, width); err != nil {
			return err
		}
	}
	return nil
}

// stdoutForWidth reports the file to probe for terminal width.
//
// The writer reaching writeTable is wrapped (see checkedWriter), so a plain
// type assertion would always fail and width detection would never run. Unwrap
// until the real file surfaces; buffers and pipes yield nil, which means
// "unknown width".
func stdoutForWidth(w io.Writer) *os.File {
	for i := 0; i < 8 && w != nil; i++ {
		if f, ok := w.(*os.File); ok {
			return f
		}
		u, ok := w.(interface{ Unwrap() io.Writer })
		if !ok {
			return nil
		}
		w = u.Unwrap()
	}
	return nil
}

func renderSection(w io.Writer, sec section, opts Options, numbered bool, termWidth int) error {
	headers, rows := sec.headers, sec.rows

	// Row numbers apply to record lists only; a Key|Value grid lists fields.
	if numbered && sec.numbered && len(rows) > 0 {
		headers, rows = withRowNumbers(headers, rows)
	}

	// A single wide record reads better transposed — but only when the width is
	// actually known and actually exceeded. termWidth <= 0 means "unknown or
	// unlimited" (buffer, pipe, failed probe, explicitly disabled), and must
	// keep the horizontal layout rather than transpose everything.
	if !(numbered && sec.numbered) && termWidth > 0 &&
		tableTotalWidth(headers, rows) > termWidth {
		if vh, vr, ok := verticalize(headers, rows); ok {
			headers, rows = vh, vr
		}
	}

	if sec.title != "" {
		// Section titles contain response keys and query aliases. Treat them as
		// untrusted terminal text just like cells: controls must be visible. Wrap
		// long titles instead of truncating response paths.
		title := escapeCellString(sec.title)
		if termWidth > 0 {
			for _, line := range wrapToWidth(title, termWidth) {
				if _, err := io.WriteString(w, line+"\n"); err != nil {
					return err
				}
			}
		} else {
			if _, err := io.WriteString(w, title+"\n"); err != nil {
				return err
			}
		}
	}
	if len(headers) == 0 && len(rows) == 0 {
		_, err := io.WriteString(w, "(empty)\n")
		return err
	}
	return renderTable(w, headers, rows, opts, termWidth)
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

// withRowNumbers prepends the "#" column, numbering from 1.
func withRowNumbers(headers []string, rows [][]string) ([]string, [][]string) {
	if len(headers) > 0 {
		headers = append([]string{"#"}, headers...)
	} else if len(rows) > 0 {
		// Positional projections (for example a list of lists) have no named
		// data headers, but table-num still promises a visible leading # column.
		headers = make([]string, len(rows[0])+1)
		headers[0] = "#"
	}
	numbered := make([][]string, len(rows))
	for i, r := range rows {
		row := make([]string, 0, len(r)+1)
		row = append(row, strconv.Itoa(i+1))
		row = append(row, r...)
		numbered[i] = row
	}
	return headers, numbered
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
		if maxCols == 0 {
			// The outer list contains positional records even though every
			// record is empty. Preserve their count instead of rendering the
			// non-empty outer list as "(empty)".
			rows = make([][]string, len(s))
			for i := range rows {
				rows[i] = []string{"[]"}
			}
			return []string{"Value"}, rows
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

// tableTotalWidth is the rendered width of a grid: per column the content plus
// "| " and " ", then the trailing "|".
func tableTotalWidth(headers []string, rows [][]string) int {
	widths := columnWidths(headers, rows)
	total := 1
	for _, w := range widths {
		total += w + 3
	}
	return total
}

func columnWidths(headers []string, rows [][]string) []int {
	cols := len(headers)
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	widths := make([]int, cols)
	for i := 0; i < cols && i < len(headers); i++ {
		widths[i] = displayWidth(headers[i])
	}
	for _, r := range rows {
		for i, cell := range r {
			if i >= cols {
				break
			}
			if dw := displayWidth(cell); dw > widths[i] {
				widths[i] = dw
			}
		}
	}
	for i := range widths {
		if widths[i] < 1 {
			widths[i] = 1
		}
	}
	return widths
}

// fitWidths shrinks column widths so the grid fits termWidth.
//
// Cells are wrapped to these widths later; response content is never truncated.
// Every column keeps minCellWidth so a narrow terminal cannot collapse the table
// into unreadable slivers. Returns the widths unchanged when they already fit or
// when no width is known.
func fitWidths(widths []int, termWidth int) []int {
	const minCellWidth = 3
	if termWidth <= 0 || len(widths) == 0 {
		return widths
	}
	overhead := 1 + 3*len(widths)
	budget := termWidth - overhead
	if budget < minCellWidth*len(widths) {
		budget = minCellWidth * len(widths)
	}
	total := 0
	for _, w := range widths {
		total += w
	}
	if total <= budget {
		return widths
	}

	fitted := append([]int(nil), widths...)
	// Lower the widest tier in chunks. A character-at-a-time loop makes table
	// rendering proportional to the largest response string (potentially
	// millions of iterations); tiered reduction is bounded by the number of
	// distinct column widths while still preserving narrow columns.
	for total > budget {
		widest := 0
		for i, w := range fitted {
			if w > widest {
				widest = fitted[i]
			}
		}
		if widest <= minCellWidth {
			break
		}
		next, count := minCellWidth, 0
		for _, w := range fitted {
			switch {
			case w == widest:
				count++
			case w > next:
				next = w
			}
		}
		excess := total - budget
		tierCapacity := (widest - next) * count
		if tierCapacity <= excess {
			for i, w := range fitted {
				if w == widest {
					fitted[i] = next
				}
			}
			total -= tierCapacity
			continue
		}
		base, remainder := excess/count, excess%count
		for i, w := range fitted {
			if w != widest {
				continue
			}
			reduction := base
			if remainder > 0 {
				reduction++
				remainder--
			}
			fitted[i] -= reduction
			total -= reduction
		}
	}
	return fitted
}

func renderTable(w io.Writer, headers []string, rows [][]string, opts Options, termWidth int) error {
	widths := columnWidths(headers, rows)
	if len(widths) == 0 {
		_, err := io.WriteString(w, "(empty)\n")
		return err
	}
	widths = fitWidths(widths, termWidth)
	cols := len(widths)

	norm := make([][]string, len(rows))
	for i, r := range rows {
		nr := make([]string, cols)
		copy(nr, r)
		norm[i] = nr
	}

	border := tableBorder(widths)
	if err := writeTableLine(w, border); err != nil {
		return err
	}
	if len(headers) > 0 {
		h := make([]string, cols)
		copy(h, headers)
		if err := writeWrappedTableRow(w, h, widths, opts.styleHeader); err != nil {
			return err
		}
		if err := writeTableLine(w, border); err != nil {
			return err
		}
	}
	for _, r := range norm {
		if err := writeWrappedTableRow(w, r, widths, opts.styleCell); err != nil {
			return err
		}
	}
	return writeTableLine(w, border)
}

// writeWrappedTableRow renders one logical row as as many physical rows as are
// needed to preserve every cell. Styling is applied after wrapping so ANSI
// sequences never participate in display-width calculations.
func writeWrappedTableRow(w io.Writer, cells []string, widths []int, style func(string) string) error {
	wrapped := make([][]string, len(widths))
	lineCount := 1
	for i, width := range widths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		wrapped[i] = wrapToWidth(cell, width)
		if len(wrapped[i]) > lineCount {
			lineCount = len(wrapped[i])
		}
	}
	for line := 0; line < lineCount; line++ {
		physical := make([]string, len(widths))
		for col := range widths {
			if line < len(wrapped[col]) {
				physical[col] = style(wrapped[col][line])
			}
		}
		if err := writeTableLine(w, tableRow(physical, widths)); err != nil {
			return err
		}
	}
	return nil
}

// wrapToWidth splits s on display-cell boundaries without losing data. Input
// has already had control characters escaped, so every grapheme cluster is safe
// to print. Iterating clusters (rather than runes) keeps combining sequences,
// emoji joined with ZWJ, and regional-indicator flags intact.
func wrapToWidth(s string, width int) []string {
	if width <= 0 || displayWidth(s) <= width {
		return []string{s}
	}
	var lines []string
	var current strings.Builder
	used := 0
	flush := func() {
		lines = append(lines, current.String())
		current.Reset()
		used = 0
	}
	graphemes := uniseg.NewGraphemes(s)
	for graphemes.Next() {
		cluster := graphemes.Str()
		clusterWidth := runewidth.StringWidth(cluster)
		if clusterWidth > 0 && used > 0 && used+clusterWidth > width {
			flush()
		}
		current.WriteString(cluster)
		used += clusterWidth
	}
	if current.Len() > 0 || len(lines) == 0 {
		flush()
	}
	return lines
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

// padRight pads to width using the visible width, ignoring ANSI styling so
// colored cells stay aligned.
func padRight(s string, width int) string {
	pad := width - displayWidth(stripANSI(s))
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}
