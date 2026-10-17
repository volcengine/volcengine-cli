package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// expandFlatToJSON converts a flat dotted-key parameter map into a nested JSON
// document suitable for an application/json request body. Leaf values are typed
// strictly from metadata (typeset via apiMeta); keys absent from metadata are
// kept as raw strings. Numeric path segments denote 1-based array indices.
func expandFlatToJSON(flat map[string]string, apiMeta *ApiMeta) (map[string]interface{}, error) {
	// Phase 1: build a string-keyed tree (numeric segments stay as string keys).
	tree := map[string]interface{}{}

	keys := make([]string, 0, len(flat))
	for k := range flat {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		segs := strings.Split(key, ".")
		if err := validateIndexSegments(key, segs); err != nil {
			return nil, err
		}
		leaf, err := convertLeaf(apiMeta, key, flat[key])
		if err != nil {
			return nil, err
		}
		if err := insertLeaf(tree, segs, key, leaf); err != nil {
			return nil, err
		}
	}

	// Phase 2: collapse all-numeric-keyed maps into ordered slices.
	out, err := collapseNode(tree, "")
	if err != nil {
		return nil, err
	}
	m, _ := out.(map[string]interface{})
	return m, nil
}

// validateIndexSegments rejects malformed array indices early so they cannot be
// silently turned into object fields. A segment that parses as an integer (so it
// was intended as an index) must be a clean positive 1-based integer; signed or
// zero indices (e.g. "-1", "+1", "0") are errors.
func validateIndexSegments(fullKey string, segs []string) error {
	for _, seg := range segs {
		n, err := strconv.Atoi(seg)
		if err != nil {
			continue // not integer-like -> an object field name
		}
		if !isNumericSeg(seg) || n < 1 {
			return fmt.Errorf("parameter %q: invalid array index %q (array indices must be positive 1-based integers)", fullKey, seg)
		}
	}
	return nil
}

// convertLeaf types a single leaf value from metadata. Reuses the existing
// metadata helpers so indexed elements of scalar arrays stay typed by their
// element type rather than being parsed as a whole JSON array.
func convertLeaf(apiMeta *ApiMeta, fullKey, raw string) (interface{}, error) {
	mt, matchedKey, ok := resolveRequestMetaType(apiMeta, fullKey)
	if !ok {
		return raw, nil // unknown key -> string
	}
	tn := mt.TypeName

	// An indexed element of a scalar array (matched key ends with ".N" and the
	// declared type is an array). The leaf is a single element, so type it by
	// the element type, NOT by the array container.
	// Covers both the "array" + TypeOf form and the legacy "array[xxx]" form.
	if isIndexedStringArrayElement(matchedKey) && isArrayType(tn) {
		return convertScalar(fullKey, raw, arrayElemType(mt))
	}

	switch {
	case tn == "object" || tn == "map" || isArrayType(tn):
		// Whole composite passed as a JSON string.
		var v interface{}
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, fmt.Errorf("parameter %q: expected JSON for %s, got %q", fullKey, tn, raw)
		}
		return v, nil
	default:
		return convertScalar(fullKey, raw, tn)
	}
}

// resolveRequestMetaType resolves the metadata type for a (possibly nested)
// dotted key. It first tries the flat MetaTypes lookup (which handles repeated
// ".N" array keys via normalization), then walks ChildMetas for nested object
// fields whose types are not present in the top-level MetaTypes.
func resolveRequestMetaType(apiMeta *ApiMeta, name string) (*MetaType, string, bool) {
	if mt, matched, ok := getRequestMetaType(apiMeta, name); ok {
		return mt, matched, true
	}
	if apiMeta == nil || apiMeta.Request == nil {
		return nil, "", false
	}

	segs := strings.Split(name, ".")
	meta := apiMeta.Request
	matched := make([]string, 0, len(segs))
	for i := 0; i < len(segs); i++ {
		if meta == nil || meta.MetaTypes == nil {
			return nil, "", false
		}
		seg := segs[i]
		mt, ok := meta.MetaTypes[seg]
		if !ok {
			return nil, "", false
		}
		matched = append(matched, seg)

		// An array field may be followed by a numeric index segment. The index
		// is not a field name, so it must be consumed rather than looked up.
		if isArrayType(mt.TypeName) && i+1 < len(segs) && isNumericSeg(segs[i+1]) {
			matched = append(matched, "N")
			if i+1 == len(segs)-1 {
				// Scalar array element (e.g. Ports.1): caller types it by the
				// element type via the trailing ".N" in the matched key.
				return mt, strings.Join(matched, "."), true
			}
			// array<object>: descend into the element meta (ChildMetas[field])
			// and skip past the index to resolve the element's sub-field.
			if meta.ChildMetas == nil {
				return nil, "", false
			}
			meta = meta.ChildMetas[seg]
			i++ // consume the numeric index segment
			continue
		}

		if i == len(segs)-1 {
			return mt, strings.Join(matched, "."), true
		}
		if meta.ChildMetas == nil {
			return nil, "", false
		}
		meta = meta.ChildMetas[seg]
	}
	return nil, "", false
}

// convertScalar coerces a raw string into a scalar JSON value per typeName.
// Unknown / "string" types are kept as raw strings.
func convertScalar(fullKey, raw, typeName string) (interface{}, error) {
	switch typeName {
	case "integer", "long":
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parameter %q: expected %s, got %q", fullKey, typeName, raw)
		}
		return n, nil
	case "number", "float", "double":
		f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return nil, fmt.Errorf("parameter %q: expected %s, got %q", fullKey, typeName, raw)
		}
		return f, nil
	case "boolean":
		b, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("parameter %q: expected boolean, got %q", fullKey, raw)
		}
		return b, nil
	default:
		// "string" and any unknown element type -> literal string.
		return raw, nil
	}
}

// isArrayType reports whether a metadata TypeName denotes an array, covering
// both the "array" form (with TypeOf) and the legacy "array[xxx]" form.
func isArrayType(typeName string) bool {
	return typeName == "array" || strings.HasPrefix(typeName, "array[")
}

// arrayElemType returns the scalar element type of an array metadata entry,
// preferring TypeOf and falling back to the legacy "array[xxx]" form.
func arrayElemType(mt *MetaType) string {
	if mt.TypeOf != "" {
		return mt.TypeOf
	}
	tn := mt.TypeName
	if strings.HasPrefix(tn, "array[") && strings.HasSuffix(tn, "]") {
		return tn[len("array[") : len(tn)-1]
	}
	return "string"
}

// insertLeaf places leaf at the path described by segs into the string-keyed tree.
func insertLeaf(tree map[string]interface{}, segs []string, fullKey string, leaf interface{}) error {
	cur := tree
	for i := 0; i < len(segs)-1; i++ {
		seg := segs[i]
		switch child := cur[seg].(type) {
		case nil:
			next := map[string]interface{}{}
			cur[seg] = next
			cur = next
		case map[string]interface{}:
			cur = child
		default:
			return fmt.Errorf("parameter %q: conflicting paths at segment %q", fullKey, seg)
		}
	}
	last := segs[len(segs)-1]
	if _, exists := cur[last]; exists {
		return fmt.Errorf("parameter %q: conflicting paths at segment %q", fullKey, last)
	}
	cur[last] = leaf
	return nil
}

func isNumericSeg(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// collapseNode converts every all-numeric-keyed map into an ordered slice,
// validating 1-based contiguous indices. path is used for error messages.
func collapseNode(v interface{}, path string) (interface{}, error) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return v, nil // leaf
	}
	if len(m) == 0 {
		return m, nil
	}

	numeric := 0
	for k := range m {
		if isNumericSeg(k) {
			numeric++
		}
	}

	// Recurse into children first.
	for k := range m {
		childPath := k
		if path != "" {
			childPath = path + "." + k
		}
		nc, err := collapseNode(m[k], childPath)
		if err != nil {
			return nil, err
		}
		m[k] = nc
	}

	if numeric == 0 {
		return m, nil // plain object
	}
	if numeric != len(m) {
		return nil, fmt.Errorf("parameter path %q: mixes object fields and array indices", path)
	}

	arr := make([]interface{}, len(m))
	seen := make([]bool, len(m))
	for k := range m {
		idx, err := strconv.Atoi(k)
		if err != nil || idx < 1 || idx > len(m) {
			return nil, fmt.Errorf("parameter path %q: array indices must be 1-based and contiguous", path)
		}
		if seen[idx-1] {
			return nil, fmt.Errorf("parameter path %q: duplicate array index %d", path, idx)
		}
		seen[idx-1] = true
		arr[idx-1] = m[k]
	}
	return arr, nil
}
