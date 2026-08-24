package output

import (
	"fmt"
	"strings"

	"github.com/volcengine/volcengine-cli/internal/jmespath"
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
	minArgs int
	maxArgs int // -1 means variadic.
}

// The stock interpreter dispatches functions only when evaluation reaches the
// node, so an empty-object probe misses typos inside projections. Keep the
// public JMESPath function contract here and validate calls lexically instead.
var queryFunctionSpecs = map[string]queryFunctionSpec{
	"abs": {1, 1}, "avg": {1, 1}, "ceil": {1, 1}, "contains": {2, 2},
	"ends_with": {2, 2}, "floor": {1, 1}, "join": {2, 2}, "keys": {1, 1},
	"length": {1, 1}, "map": {2, 2}, "max": {1, 1}, "max_by": {2, 2},
	"merge": {1, -1}, "min": {1, 1}, "min_by": {2, 2}, "not_null": {1, -1},
	"reverse": {1, 1}, "sort": {1, 1}, "sort_by": {2, 2}, "starts_with": {2, 2},
	"sum": {1, 1}, "to_array": {1, 1}, "to_number": {1, 1}, "to_string": {1, 1},
	"type": {1, 1}, "values": {1, 1},
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
			i = next + 1
			continue
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
			// Do not jump to the closing parenthesis. Continue at the opening
			// parenthesis so nested calls are validated independently too.
			continue
		}
		i++
	}
	return nil
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

func isQuerySpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

func isQueryIdentifierStart(c byte) bool {
	return c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func isQueryIdentifierPart(c byte) bool {
	return isQueryIdentifierStart(c) || c >= '0' && c <= '9'
}
