package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/jmespath/go-jmespath"
)

// Query is a validated JMESPath expression.
type Query struct {
	compiled *jmespath.JMESPath
	// columns is the multiselect-hash key order written in the expression, used
	// as a best-effort column-order hint for table/text. Empty when the
	// expression has no usable top-level hash. See column_order.go.
	columns []string
}

// Columns returns the column-order hint recovered from the expression.
// Nil means "no hint"; renderers then fall back to alphabetical order.
func (q *Query) Columns() []string {
	if q == nil {
		return nil
	}
	return q.columns
}

// CompileQuery validates expr before an API request is sent.
// Empty expressions do not require a query.
func CompileQuery(expr string) (*Query, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, nil
	}
	compiled, err := jmespath.Compile(expr)
	if err != nil {
		return nil, newQueryError(expr, err)
	}
	if err := validateQueryExpression(expr); err != nil {
		return nil, newQueryError(expr, err)
	}
	query := &Query{compiled: compiled, columns: columnOrder(expr)}

	// go-jmespath accepts a few incomplete ASTs at compile time. The lexical
	// validation above catches every function regardless of whether a projection
	// would execute it; this final probe is retained only for those malformed ASTs.
	if err := query.probe(); err != nil {
		return nil, newQueryError(expr, err)
	}
	return query, nil
}

// probe surfaces expression faults that go-jmespath defers to evaluation.
// Type errors from the empty probe object are data-dependent and are ignored.
func (q *Query) probe() (err error) {
	defer func() {
		if recover() != nil {
			err = nil
		}
	}()
	_, searchErr := q.compiled.Search(map[string]interface{}{})
	if searchErr == nil || isDataDependentError(searchErr) {
		return nil
	}
	return searchErr
}

func isDataDependentError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "Invalid type for") ||
		strings.Contains(msg, "invalid type for") ||
		strings.Contains(msg, "cannot be sorted")
}

// Search evaluates a previously validated query against the original response.
// In particular, json.Number leaves are never normalized to float64 or restored
// by value: structural projections therefore preserve their exact source digits.
func (q *Query) Search(data interface{}) (result interface{}, err error) {
	if q == nil || q.compiled == nil {
		return data, nil
	}
	defer func() {
		if r := recover(); r != nil {
			result, err = nil, fmt.Errorf("--query could not be evaluated on this response: %v", r)
		}
	}()
	return q.compiled.Search(data)
}

// ApplyQuery evaluates a JMESPath expression against data.
// Empty expr returns data unchanged.
func ApplyQuery(data interface{}, expr string) (interface{}, error) {
	query, err := CompileQuery(expr)
	if err != nil {
		return nil, err
	}
	return query.Search(data)
}

type queryFunctionSpec struct {
	minArgs       int
	maxArgs       int // -1 means variadic.
	numericUnsafe bool
}

// The stock interpreter dispatches functions only when evaluation reaches the
// node, so an empty-object probe misses typos inside projections. Keep the
// public JMESPath function contract here and validate calls lexically instead.
var queryFunctionSpecs = map[string]queryFunctionSpec{
	"abs":         {1, 1, true},
	"avg":         {1, 1, true},
	"ceil":        {1, 1, true},
	"contains":    {2, 2, false},
	"ends_with":   {2, 2, false},
	"floor":       {1, 1, true},
	"join":        {2, 2, false},
	"keys":        {1, 1, false},
	"length":      {1, 1, false},
	"map":         {2, 2, false},
	"max":         {1, 1, false},
	"max_by":      {2, 2, false},
	"merge":       {1, -1, false},
	"min":         {1, 1, false},
	"min_by":      {2, 2, false},
	"not_null":    {1, -1, false},
	"reverse":     {1, 1, false},
	"sort":        {1, 1, false},
	"sort_by":     {2, 2, false},
	"starts_with": {2, 2, false},
	"sum":         {1, 1, true},
	"to_array":    {1, 1, false},
	"to_number":   {1, 1, true},
	"to_string":   {1, 1, false},
	// type(json.Number) returns an explicit evaluation error; it never silently
	// classifies an exact response number as a float-backed JMESPath number.
	"type":   {1, 1, false},
	"values": {1, 1, false},
}

// queryValidationError carries a source byte offset into QueryError.
type queryValidationError struct {
	message string
	offset  int
}

func (e *queryValidationError) Error() string    { return e.message }
func (e *queryValidationError) queryOffset() int { return e.offset }

func validateQueryExpression(expr string) error {
	for i := 0; i < len(expr); {
		switch expr[i] {
		case '\'', '"', '`':
			next, ok := skipQueryQuoted(expr, i)
			if !ok {
				// The upstream parser reports the more specific delimiter error.
				return nil
			}
			if expr[i] == '`' && queryJSONLiteralContainsNumber(expr[i+1:next]) &&
				!queryNumericLiteralIsShapeOnly(expr, i, next+1) {
				return unsupportedNumericQuery(expr, i, "JSON literal containing a number")
			}
			i = next + 1
			continue
		case '<', '>':
			return unsupportedNumericQuery(expr, i, "numeric ordering comparisons")
		}

		if isQueryIdentifierStart(expr[i]) {
			start := i
			for i < len(expr) && isQueryIdentifierPart(expr[i]) {
				i++
			}
			name := expr[start:i]
			open := skipQuerySpaces(expr, i)
			if open >= len(expr) || expr[open] != '(' {
				continue
			}
			argc, _, ok := queryCallArity(expr, open)
			if !ok {
				return nil // Let the upstream syntax error describe it.
			}
			spec, exists := queryFunctionSpecs[name]
			if !exists {
				return &queryValidationError{message: "unknown function: " + name, offset: start}
			}
			if argc < spec.minArgs || (spec.maxArgs >= 0 && argc > spec.maxArgs) {
				return &queryValidationError{
					message: fmt.Sprintf("function %s called with %d argument(s); expected %s", name, argc, queryArityText(spec)),
					offset:  start,
				}
			}
			if spec.numericUnsafe {
				return unsupportedNumericQuery(expr, start, fmt.Sprintf("function %s()", name))
			}
			// Do not jump to the closing parenthesis. Continue at the opening
			// parenthesis so nested calls are validated independently too.
			continue
		}
		i++
	}
	return nil
}

// A backtick JSON literal is decoded by go-jmespath with json.Unmarshal, so
// every contained number becomes float64 before evaluation. Reject such a
// literal unless its value is consumed only for its shape.
func queryJSONLiteralContainsNumber(raw string) bool {
	decoder := json.NewDecoder(strings.NewReader(strings.ReplaceAll(raw, "\\`", "`")))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return false // The upstream parser will report malformed JSON.
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return false
	}
	return queryValueContainsJSONNumber(value)
}

func queryValueContainsJSONNumber(value interface{}) bool {
	switch value := value.(type) {
	case json.Number:
		return true
	case []interface{}:
		for _, item := range value {
			if queryValueContainsJSONNumber(item) {
				return true
			}
		}
	case map[string]interface{}:
		for _, item := range value {
			if queryValueContainsJSONNumber(item) {
				return true
			}
		}
	}
	return false
}

// length() and keys() inspect only collection shape. This deliberately accepts
// only a direct, sole literal argument; projections and value-returning
// functions must not acquire a broad exemption merely by being nested here.
func queryNumericLiteralIsShapeOnly(expr string, start, end int) bool {
	left := skipQuerySpacesBackward(expr, start-1)
	openCount := 0
	for left >= 0 && expr[left] == '(' {
		openCount++
		left = skipQuerySpacesBackward(expr, left-1)
	}
	if openCount == 0 || left < 0 {
		return false
	}
	nameEnd := left
	nameStart := nameEnd
	for nameStart >= 0 && isQueryIdentifierPart(expr[nameStart]) {
		nameStart--
	}
	name := expr[nameStart+1 : nameEnd+1]
	if name != "length" && name != "keys" {
		return false
	}

	right := skipQuerySpaces(expr, end)
	for ; openCount > 0; openCount-- {
		if right >= len(expr) || expr[right] != ')' {
			return false
		}
		right = skipQuerySpaces(expr, right+1)
	}
	return true
}

func unsupportedNumericQuery(expr string, offset int, operation string) error {
	return &queryValidationError{
		message: fmt.Sprintf("%s is not supported because exact JSON-number semantics cannot be guaranteed", operation),
		offset:  offset,
	}
}

func queryArityText(spec queryFunctionSpec) string {
	if spec.maxArgs < 0 {
		return fmt.Sprintf("at least %d", spec.minArgs)
	}
	if spec.minArgs == spec.maxArgs {
		return fmt.Sprintf("%d", spec.minArgs)
	}
	return fmt.Sprintf("%d to %d", spec.minArgs, spec.maxArgs)
}

func queryCallArity(expr string, open int) (argc, close int, ok bool) {
	depth := 0
	hasArgument := false
	for i := open + 1; i < len(expr); i++ {
		switch expr[i] {
		case '\'', '"', '`':
			next, quoted := skipQueryQuoted(expr, i)
			if !quoted {
				return 0, 0, false
			}
			hasArgument = true
			i = next
		case '(', '[', '{':
			depth++
			hasArgument = true
		case ']', '}':
			if depth > 0 {
				depth--
			}
		case ')':
			if depth == 0 {
				if hasArgument {
					argc++
				}
				return argc, i, true
			}
			depth--
		case ',':
			if depth == 0 {
				argc++
				hasArgument = false
			}
		default:
			if expr[i] != ' ' && expr[i] != '\t' && expr[i] != '\r' && expr[i] != '\n' {
				hasArgument = true
			}
		}
	}
	return 0, 0, false
}

func skipQueryQuoted(expr string, start int) (int, bool) {
	quote := expr[start]
	for i := start + 1; i < len(expr); i++ {
		if expr[i] == '\\' {
			i++
			continue
		}
		if expr[i] == quote {
			return i, true
		}
	}
	return 0, false
}

func skipQuerySpaces(expr string, i int) int {
	for i < len(expr) && isQuerySpace(expr[i]) {
		i++
	}
	return i
}

func skipQuerySpacesBackward(expr string, i int) int {
	for i >= 0 && isQuerySpace(expr[i]) {
		i--
	}
	return i
}

func isQuerySpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

func isQueryIdentifierStart(c byte) bool {
	return c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func isQueryIdentifierPart(c byte) bool {
	return isQueryIdentifierStart(c) || c >= '0' && c <= '9'
}
