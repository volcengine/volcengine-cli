package output

// Nested section rendering.
//
// A response often nests lists/objects inside a row (SecurityGroupIds, Tags,
// NetworkInterfaces...). Rendering those cells as compact JSON keeps the table
// flat but makes nested data unreadable. Structured values are therefore split
// into titled sections, one per nested field. Simple scalar responses keep the
// compact flat layout.

// section is one titled grid in the rendered output.
type section struct {
	title   string
	headers []string
	rows    [][]string
	// numbered marks the section as a record list eligible for a "#" column.
	numbered bool
}

// isNested reports whether v needs its own section rather than a cell.
func isNested(v interface{}) bool {
	switch x := v.(type) {
	case map[string]interface{}:
		return len(x) > 0
	case []interface{}:
		// A list of scalars still fits in one cell; only structured content
		// deserves its own section.
		return len(x) > 0 && !allScalars(x)
	default:
		return false
	}
}

func allScalars(list []interface{}) bool {
	for _, v := range list {
		switch v.(type) {
		case map[string]interface{}, []interface{}:
			return false
		}
	}
	return true
}

// nestedPlaceholder marks a cell whose value is rendered as its own section.
// It is not a value from the response, so it is distinct from noneValue ("None",
// meaning null/absent) and from "(empty)".
const nestedPlaceholder = "(see section)"

// orderedSubset returns the members of want, ordered as they appear in order.
func orderedSubset(order, want []string) []string {
	wanted := make(map[string]struct{}, len(want))
	for _, k := range want {
		wanted[k] = struct{}{}
	}
	out := make([]string, 0, len(want))
	for _, k := range order {
		if _, ok := wanted[k]; ok {
			out = append(out, k)
			delete(wanted, k)
		}
	}
	return out
}

// splitNestedKeys separates scalar-ish keys (rendered as columns) from keys
// whose values need their own section. Both slices keep the incoming key order.
func splitNestedKeys(list []interface{}, keys []string) (scalar, nested []string) {
	nestedSeen := make(map[string]bool, len(keys))
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		for _, k := range keys {
			if isNested(m[k]) {
				nestedSeen[k] = true
			}
		}
	}
	for _, k := range keys {
		if nestedSeen[k] {
			nested = append(nested, k)
		} else {
			scalar = append(scalar, k)
		}
	}
	return scalar, nested
}

// buildSections turns data into one or more sections.
//
// The first section holds the scalar columns; each nested field contributes
// further sections, titled with the field path so the origin stays clear.
// depth caps recursion so a pathological response cannot exhaust the stack.
func buildSections(data interface{}, opts Options, title string, depth int) []section {
	const maxDepth = 8
	if depth > maxDepth {
		return []section{{title: title, headers: []string{"Value"},
			rows: [][]string{{scalarString(data)}}}}
	}

	switch v := data.(type) {
	case []interface{}:
		if len(v) == 0 {
			return nil
		}
		if allMaps(v) {
			return objectListSections(v, opts, title, depth)
		}
		headers, rows := tableFromSlice(v)
		return []section{{title: title, headers: headers, rows: rows, numbered: true}}

	case map[string]interface{}:
		if len(v) == 0 {
			return []section{{title: title, headers: []string{"Key", "Value"}}}
		}
		return objectSections(v, opts, title, depth)

	default:
		return []section{{title: title, headers: []string{"Value"},
			rows: [][]string{{scalarString(data)}}}}
	}
}

// objectListSections renders a list of records: one grid of scalar columns plus
// a section per nested field, per record.
func objectListSections(list []interface{}, opts Options, title string, depth int) []section {
	keys := applyColumnOrder(unionMapKeys(list), opts.Columns)
	if len(keys) == 0 {
		// The list is not empty: it contains records whose objects happen to
		// have no fields. Keep one visible row per record so table-num can still
		// number them and a parent "(see section)" never points at a section
		// that disappeared.
		rows := make([][]string, len(list))
		for i := range rows {
			rows[i] = []string{"{}"}
		}
		return []section{{
			title: title, headers: []string{"Value"}, rows: rows, numbered: true,
		}}
	}
	scalarKeys, nestedKeys := splitNestedKeys(list, keys)

	// A key is "nested" as soon as ONE row nests it, but the other rows may hold
	// a scalar, null or an empty list. Those values must stay visible: dropping
	// the whole column would silently discard real data for every row that is
	// not nested. So nested keys are kept as main-table columns too, where a
	// genuinely nested cell is replaced by a pointer to its section.
	mainKeys := make([]string, 0, len(keys))
	mainKeys = append(mainKeys, scalarKeys...)
	mainKeys = append(mainKeys, nestedKeys...)
	// Restore the original key order so the hint and union order still hold.
	mainKeys = orderedSubset(keys, mainKeys)

	out := make([]section, 0, 1+len(nestedKeys))
	if len(mainKeys) > 0 {
		headers := make([]string, len(mainKeys))
		for i, k := range mainKeys {
			headers[i] = escapeCellString(k)
		}
		rows := make([][]string, 0, len(list))
		for _, item := range list {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			row := make([]string, len(mainKeys))
			for i, k := range mainKeys {
				val, present := m[k]
				switch {
				case !present:
					row[i] = noneValue
				case isNested(val):
					// Rendered below as its own section.
					row[i] = nestedPlaceholder
				default:
					row[i] = scalarString(val)
				}
			}
			rows = append(rows, row)
		}
		out = append(out, section{title: title, headers: headers, rows: rows, numbered: true})
	}

	// Nested fields follow, one section per record that actually has the field.
	for _, key := range nestedKeys {
		for i, item := range list {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			val, present := m[key]
			if !present || !isNested(val) {
				continue
			}
			out = append(out, buildSections(val, Options{},
				sectionTitle(title, key, i, len(list)), depth+1)...)
		}
	}
	return out
}

// objectSections renders a single object as one horizontal record: the scalar
// field names form the header row and their values form a single data row, so a
// lone object looks like a one-row table (matching the AWS CLI, which renders a
// dict as a header row plus a value row). Nested fields become their own titled
// sections. renderSection transposes this single row to Field | Value only when
// a known terminal width is exceeded, and table-num numbers it as record 1.
func objectSections(m map[string]interface{}, opts Options, title string, depth int) []section {
	// The column-order hint applies to a single object too: `--query
	// '{Name:A,Id:B}'` on a non-list result lands here, and the header columns
	// should follow what was written rather than alphabetical order.
	keys := applyColumnOrder(sortedMapKeys(m), opts.Columns)
	var scalarKeys, nestedKeys []string
	for _, k := range keys {
		if isNested(m[k]) {
			nestedKeys = append(nestedKeys, k)
		} else {
			scalarKeys = append(scalarKeys, k)
		}
	}

	out := make([]section, 0, 1+len(nestedKeys))
	if len(scalarKeys) > 0 {
		headers := make([]string, len(scalarKeys))
		row := make([]string, len(scalarKeys))
		for i, k := range scalarKeys {
			headers[i] = escapeCellString(k)
			row[i] = scalarString(m[k])
		}
		out = append(out, section{
			title: title, headers: headers, rows: [][]string{row}, numbered: true,
		})
	}
	for _, k := range nestedKeys {
		out = append(out, buildSections(m[k], Options{},
			joinTitle(title, k), depth+1)...)
	}
	return out
}

func joinTitle(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

// sectionTitle labels a nested section, adding the record number only when the
// parent list has more than one element (avoids a noisy "[1]" on single
// results). The number is 1-based to match the "#" column of --output
// table-num, so a section can be traced back to its row.
func sectionTitle(parent, key string, index, total int) string {
	base := joinTitle(parent, key)
	if total <= 1 {
		return base
	}
	return base + "[" + itoa(index+1) + "]"
}
