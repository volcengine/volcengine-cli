package output

// Display-layer shaping for table/text.
//
// Two strictly presentational transforms happen here. They never run for
// json/yaml, so scripted consumers keep the exact API response.
//
//  1. stripResponseMetadata — drop the ResponseMetadata envelope so a bare
//     `--output table` shows the payload instead of a two-row Key/Value table
//     whose Result cell holds the entire response as JSON.
//
//  2. verticalize — a single-row table is transposed to Field | Value, because
//     one wide row forces horizontal scrolling while a tall two-column table
//     fits on screen.

// responseMetadataKey is the Volcengine envelope field carrying RequestId and
// other call metadata. It is diagnostic data, not payload.
const responseMetadataKey = "ResponseMetadata"

// stripResponseMetadata removes the ResponseMetadata envelope for display.
//
// Only the top-level key of a map is considered, and only when it is not the
// sole key: a response that carries nothing but ResponseMetadata (common for
// write APIs) would otherwise render as "(empty)" and look like a failure.
// RequestId stays available in json/yaml output and in --debug logs.
func stripResponseMetadata(data interface{}) interface{} {
	m, ok := data.(map[string]interface{})
	if !ok {
		return data
	}
	if _, present := m[responseMetadataKey]; !present || len(m) == 1 {
		return data
	}
	stripped := make(map[string]interface{}, len(m)-1)
	for k, v := range m {
		if k == responseMetadataKey {
			continue
		}
		stripped[k] = v
	}
	return stripped
}

// verticalize transposes a one-row table into Field | Value pairs.
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
