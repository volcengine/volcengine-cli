package output

// Column order for table/text rendering.
//
// JMESPath multiselect hashes are written in a meaningful order
// (`{Name:InstanceName,Id:InstanceId}`), but evaluation stores the result in a
// Go map, which is unordered. Without a hint, table/text can only fall back to
// alphabetical keys, so the rendered columns do not match what the user wrote.
//
// columnOrder recovers that written order by scanning the expression text for
// the LAST top-level multiselect hash — the projection that shapes each output
// row. The AST itself cannot be used: go-jmespath keeps nodeType/value/children
// unexported, and PrettyPrint is documented as an unstable debugging aid.
//
// This is a best-effort hint, never a correctness requirement:
//   - unparsable or ambiguous expressions return nil, and rendering falls back
//     to alphabetical order;
//   - the hint is applied only when it exactly matches the keys actually present
//     in the data (see applyColumnOrder), so a stale or wrong hint can never
//     drop, invent or reorder real data.

import "strings"

// columnOrder extracts the key order of the multiselect hash that shapes each
// row. Returns nil when the expression has no usable top-level hash.
func columnOrder(expr string) []string {
	start, end, ok := lastTopLevelHash(expr)
	if !ok {
		return nil
	}
	keys, ok := hashKeys(expr[start+1 : end])
	if !ok {
		return nil
	}
	return keys
}

// lastTopLevelHash finds the outermost-depth `{...}` that appears last in expr.
// Braces inside quotes or raw literals are ignored.
func lastTopLevelHash(expr string) (start, end int, ok bool) {
	depth := 0
	bestStart, bestEnd := -1, -1
	openStack := make([]int, 0, 8)
	for i := 0; i < len(expr); i++ {
		switch expr[i] {
		case '\'', '"', '`':
			next, ok := skipQuoted(expr, i)
			if !ok {
				return 0, 0, false
			}
			i = next
		case '(', '[':
			depth++
		case ')', ']':
			depth--
			if depth < 0 {
				return 0, 0, false
			}
		case '{':
			openStack = append(openStack, i)
		case '}':
			if len(openStack) == 0 {
				return 0, 0, false
			}
			open := openStack[len(openStack)-1]
			openStack = openStack[:len(openStack)-1]
			// Only consider hashes that are not nested inside another hash.
			if len(openStack) == 0 {
				bestStart, bestEnd = open, i
			}
		}
	}
	if len(openStack) != 0 || bestStart < 0 {
		return 0, 0, false
	}
	return bestStart, bestEnd, true
}

// hashKeys splits `Key:Expr, Key2:Expr2` and returns the keys in written order.
func hashKeys(body string) ([]string, bool) {
	if strings.TrimSpace(body) == "" {
		return nil, false
	}
	parts, ok := splitTopLevel(body, ',')
	if !ok {
		return nil, false
	}
	keys := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		pair, ok := splitTopLevel(part, ':')
		if !ok || len(pair) < 2 {
			return nil, false
		}
		key, ok := unquoteKey(strings.TrimSpace(pair[0]))
		if !ok {
			return nil, false
		}
		// Duplicate keys collapse in the evaluated map; the hint would not
		// match the data, so give up and let rendering sort alphabetically.
		if _, dup := seen[key]; dup {
			return nil, false
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys, true
}

// splitTopLevel splits s on sep, ignoring separators inside quotes or brackets.
func splitTopLevel(s string, sep byte) ([]string, bool) {
	var parts []string
	depth := 0
	last := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\'', '"', '`':
			next, ok := skipQuoted(s, i)
			if !ok {
				return nil, false
			}
			i = next
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth < 0 {
				return nil, false
			}
		case sep:
			if depth == 0 {
				parts = append(parts, s[last:i])
				last = i + 1
			}
		}
	}
	if depth != 0 {
		return nil, false
	}
	return append(parts, s[last:]), true
}

// skipQuoted returns the index of the closing quote that matches the opener at i.
func skipQuoted(s string, i int) (int, bool) {
	quote := s[i]
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case quote:
			return j, true
		}
	}
	return 0, false
}

// unquoteKey accepts a bare identifier or a "quoted key" and returns its literal
// name. Escapes inside quoted keys are resolved the same way JMESPath does for
// the common cases (\" and \\); anything else is rejected so the hint stays safe.
func unquoteKey(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	if raw[0] != '"' {
		// Bare identifier: JMESPath allows [A-Za-z0-9_] plus non-ASCII.
		for i := 0; i < len(raw); i++ {
			c := raw[i]
			if c == '_' || c >= 0x80 ||
				(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
				(c >= '0' && c <= '9') {
				continue
			}
			return "", false
		}
		return raw, true
	}
	if len(raw) < 2 || raw[len(raw)-1] != '"' {
		return "", false
	}
	var b strings.Builder
	for i := 1; i < len(raw)-1; i++ {
		c := raw[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(raw)-1 {
			return "", false
		}
		switch raw[i] {
		case '"', '\\':
			b.WriteByte(raw[i])
		default:
			return "", false
		}
	}
	return b.String(), true
}

// applyColumnOrder reorders keys to match the hint.
//
// The hint is honoured only when it describes exactly the same key set that is
// present in the data. Any mismatch (extra key, missing key, different count)
// discards the hint and keeps the alphabetical order, so rendering never loses
// or fabricates a column because of a bad hint.
func applyColumnOrder(sorted, hint []string) []string {
	if len(hint) == 0 || len(hint) != len(sorted) {
		return sorted
	}
	present := make(map[string]struct{}, len(sorted))
	for _, k := range sorted {
		present[k] = struct{}{}
	}
	for _, k := range hint {
		if _, ok := present[k]; !ok {
			return sorted
		}
		delete(present, k)
	}
	if len(present) != 0 {
		return sorted
	}
	ordered := make([]string, len(hint))
	copy(ordered, hint)
	return ordered
}
