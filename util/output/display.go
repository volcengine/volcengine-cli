package output

// Display-layer shaping for table/text. Only layout is decided here: no key is
// ever added to or removed from the response, so `--output table` and
// `--output json` can never disagree about which fields exist.

// verticalize transposes a one-row table into Field | Value pairs, because one
// wide row forces horizontal scrolling while a tall two-column table fits on
// screen.
//
// Returns ok=false when the shape is not a single headed row, so callers keep
// the horizontal layout. Row-numbered output is never verticalized: the "#"
// column only makes sense for a list of records.
func verticalize(headers []string, rows [][]string) (vHeaders []string, vRows [][]string, ok bool) {
	if len(headers) == 0 || len(rows) != 1 {
		return nil, nil, false
	}
	row := rows[0]
	if len(row) != len(headers) {
		return nil, nil, false
	}
	// A two-column table is already readable; transposing Key|Value tables
	// would just relabel them and lose the original header names.
	if len(headers) <= 2 {
		return nil, nil, false
	}
	vRows = make([][]string, 0, len(headers))
	for i, h := range headers {
		vRows = append(vRows, []string{h, row[i]})
	}
	return []string{"Field", "Value"}, vRows, true
}
