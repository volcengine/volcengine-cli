package output

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
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
//
// go-jmespath compares values with reflect.DeepEqual, which treats the textual
// json.Number representations "1" and "1.0" as different. Evaluate against a
// copy whose numbers are pointers to their exact, canonical JSON value instead.
// DeepEqual follows those pointers for ==/!=, while the pointer-to-token table
// lets structural projections recover the original spelling without guessing.
func (q *Query) Search(data interface{}) (result interface{}, err error) {
	if q == nil || q.compiled == nil {
		return data, nil
	}
	defer func() {
		if r := recover(); r != nil {
			result, err = nil, fmt.Errorf("--query could not be evaluated on this response: %v", r)
		}
	}()
	if !queryContainsJSONNumber(data) {
		return q.compiled.Search(data)
	}
	compatible, registry, wrapErr := queryWrapJSONNumbers(data)
	if wrapErr != nil {
		return nil, wrapErr
	}
	result, err = q.compiled.Search(compatible)
	if err != nil {
		return nil, err
	}
	return queryUnwrapJSONNumbers(result, registry), nil
}

// exactJSONNumber deliberately contains only the canonical value. Distinct
// pointers with equal pointees compare equal through reflect.DeepEqual.
type exactJSONNumber struct {
	canonical string
	registry  *exactJSONNumberRegistry
}

type exactJSONNumberRegistry struct {
	originals map[*exactJSONNumber]json.Number
}

// MarshalJSON keeps functions such as to_string() exact if they see the
// wrapper. Prefer the source token; canonical is a valid JSON-number fallback.
func (n *exactJSONNumber) MarshalJSON() ([]byte, error) {
	if n != nil && n.registry != nil {
		if original, ok := n.registry.originals[n]; ok {
			return []byte(original.String()), nil
		}
	}
	return []byte(n.canonical), nil
}

func queryContainsJSONNumber(value interface{}) bool {
	switch value := value.(type) {
	case json.Number:
		return true
	case []interface{}:
		for _, item := range value {
			if queryContainsJSONNumber(item) {
				return true
			}
		}
	case map[string]interface{}:
		for _, item := range value {
			if queryContainsJSONNumber(item) {
				return true
			}
		}
	}
	return false
}

// queryWrapJSONNumbers copies a decoded JSON tree. Equal source tokens reuse a
// pointer so go-jmespath's contains() (which uses Go == for array elements)
// retains its existing same-token behavior. Different spellings get distinct
// pointers whose pointees still compare equal recursively for == and !=.
func queryWrapJSONNumbers(data interface{}) (interface{}, *exactJSONNumberRegistry, error) {
	byToken := make(map[string]*exactJSONNumber)
	registry := &exactJSONNumberRegistry{originals: make(map[*exactJSONNumber]json.Number)}

	var wrap func(interface{}) (interface{}, error)
	wrap = func(value interface{}) (interface{}, error) {
		switch value := value.(type) {
		case json.Number:
			raw := value.String()
			if existing, ok := byToken[raw]; ok {
				return existing, nil
			}
			canonical, ok := canonicalJSONNumber(raw)
			if !ok {
				// API response numbers come from a JSON decoder and are valid. If a
				// caller manually supplies an invalid json.Number, failing explicitly
				// is safer than silently falling back to textual equality.
				return nil, fmt.Errorf("--query cannot compare invalid JSON number %q exactly", raw)
			}
			number := &exactJSONNumber{canonical: canonical, registry: registry}
			byToken[raw] = number
			registry.originals[number] = value
			return number, nil
		case []interface{}:
			result := make([]interface{}, len(value))
			for i, item := range value {
				wrapped, err := wrap(item)
				if err != nil {
					return nil, err
				}
				result[i] = wrapped
			}
			return result, nil
		case map[string]interface{}:
			result := make(map[string]interface{}, len(value))
			for key, item := range value {
				wrapped, err := wrap(item)
				if err != nil {
					return nil, err
				}
				result[key] = wrapped
			}
			return result, nil
		default:
			return value, nil
		}
	}

	wrapped, err := wrap(data)
	return wrapped, registry, err
}

func queryUnwrapJSONNumbers(data interface{}, registry *exactJSONNumberRegistry) interface{} {
	switch value := data.(type) {
	case *exactJSONNumber:
		if original, ok := registry.originals[value]; ok {
			return original
		}
		return data
	case []interface{}:
		result := make([]interface{}, len(value))
		for i, item := range value {
			result[i] = queryUnwrapJSONNumbers(item, registry)
		}
		return result
	case map[string]interface{}:
		result := make(map[string]interface{}, len(value))
		for key, item := range value {
			result[key] = queryUnwrapJSONNumbers(item, registry)
		}
		return result
	default:
		return data
	}
}

// canonicalJSONNumber normalizes a JSON number without expanding its decimal
// exponent. For example, 1, 1.0 and 10e-1 all become "1e0". Only the exponent
// arithmetic uses big.Int, so a compact token such as 1e1000000000 stays compact
// in time and memory instead of allocating a billion-digit numerator.
func canonicalJSONNumber(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}

	i := 0
	negative := false
	if raw[i] == '-' {
		negative = true
		i++
		if i == len(raw) {
			return "", false
		}
	}

	integerStart := i
	if raw[i] == '0' {
		i++
		if i < len(raw) && raw[i] >= '0' && raw[i] <= '9' {
			return "", false
		}
	} else if raw[i] >= '1' && raw[i] <= '9' {
		for i < len(raw) && raw[i] >= '0' && raw[i] <= '9' {
			i++
		}
	} else {
		return "", false
	}
	integerEnd := i

	fractionStart, fractionEnd := i, i
	if i < len(raw) && raw[i] == '.' {
		i++
		fractionStart = i
		for i < len(raw) && raw[i] >= '0' && raw[i] <= '9' {
			i++
		}
		fractionEnd = i
		if fractionStart == fractionEnd {
			return "", false
		}
	}

	exponent := new(big.Int)
	if i < len(raw) && (raw[i] == 'e' || raw[i] == 'E') {
		i++
		exponentNegative := false
		if i < len(raw) && (raw[i] == '+' || raw[i] == '-') {
			exponentNegative = raw[i] == '-'
			i++
		}
		exponentStart := i
		for i < len(raw) && raw[i] >= '0' && raw[i] <= '9' {
			i++
		}
		if exponentStart == i {
			return "", false
		}
		if _, ok := exponent.SetString(raw[exponentStart:i], 10); !ok {
			return "", false
		}
		if exponentNegative {
			exponent.Neg(exponent)
		}
	}
	if i != len(raw) {
		return "", false
	}

	digits := make([]byte, 0, integerEnd-integerStart+fractionEnd-fractionStart)
	digits = append(digits, raw[integerStart:integerEnd]...)
	digits = append(digits, raw[fractionStart:fractionEnd]...)
	first := 0
	for first < len(digits) && digits[first] == '0' {
		first++
	}
	if first == len(digits) {
		return "0", true
	}
	last := len(digits)
	for last > first && digits[last-1] == '0' {
		last--
	}

	exponent.Sub(exponent, big.NewInt(int64(fractionEnd-fractionStart)))
	exponent.Add(exponent, big.NewInt(int64(len(digits)-last)))
	var canonical strings.Builder
	canonical.Grow(last - first + len(exponent.String()) + 2)
	if negative {
		canonical.WriteByte('-')
	}
	canonical.Write(digits[first:last])
	canonical.WriteByte('e')
	canonical.WriteString(exponent.String())
	return canonical.String(), true
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
	safeOrdering := queryOrderingExpressionIsSafe(expr)
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
			if !safeOrdering {
				return unsupportedNumericQuery(expr, i, "numeric ordering comparisons")
			}
		case '=', '!':
			if i+1 < len(expr) && expr[i+1] == '=' && queryEqualityAtMixesSafeAndUnknownNumbers(expr, i) {
				return unsupportedNumericQuery(expr, i, "equality between a derived number and a response value")
			}
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
			argc, close, ok := queryCallArity(expr, open)
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
			if spec.numericUnsafe && !queryNumericFunctionIsSafe(name, expr[open+1:close]) {
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

// queryNumericFunctionIsSafe proves that an exact-number-sensitive function
// cannot consume a json.Number from the response. Keep this deliberately
// conservative: an expression that is not proven safe remains rejected.
func queryNumericFunctionIsSafe(name, argument string) bool {
	switch name {
	case "abs", "ceil", "floor":
		return queryExpressionProducesSafeNumber(argument)
	case "to_number":
		return queryExpressionProducesSafeNumber(argument) || queryExpressionIsStringLiteral(argument)
	default:
		// avg() and sum() can consume response arrays containing json.Number.
		return false
	}
}

// queryExpressionProducesSafeNumber recognizes the small closed set of
// expressions whose numeric result is created by JMESPath itself rather than
// read from the response. This is sufficient for compositions such as
// ceil(abs(length(Items))) without attempting to duplicate the full parser.
func queryExpressionProducesSafeNumber(expr string) bool {
	expr = strings.TrimSpace(expr)
	for queryHasSingleOuterParens(expr) {
		expr = strings.TrimSpace(expr[1 : len(expr)-1])
	}
	if expr == "" || !isQueryIdentifierStart(expr[0]) {
		return false
	}

	i := 1
	for i < len(expr) && isQueryIdentifierPart(expr[i]) {
		i++
	}
	name := expr[:i]
	open := skipQuerySpaces(expr, i)
	if open >= len(expr) || expr[open] != '(' {
		return false
	}
	argc, close, ok := queryCallArity(expr, open)
	if !ok || argc != 1 || skipQuerySpaces(expr, close+1) != len(expr) {
		return false
	}
	argument := expr[open+1 : close]
	switch name {
	case "length":
		return true
	case "abs", "ceil", "floor":
		return queryExpressionProducesSafeNumber(argument)
	case "to_number":
		return queryExpressionProducesSafeNumber(argument) || queryExpressionIsStringLiteral(argument)
	default:
		return false
	}
}

func queryExpressionIsStringLiteral(expr string) bool {
	expr = strings.TrimSpace(expr)
	for queryHasSingleOuterParens(expr) {
		expr = strings.TrimSpace(expr[1 : len(expr)-1])
	}
	if len(expr) < 2 || (expr[0] != '\'' && expr[0] != '`') {
		return false
	}
	end, ok := skipQueryQuoted(expr, 0)
	if !ok || end != len(expr)-1 {
		return false
	}
	if expr[0] == '\'' {
		return true
	}
	decoder := json.NewDecoder(strings.NewReader(strings.ReplaceAll(expr[1:end], "\\`", "`")))
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return false
	}
	_, ok = value.(string)
	return ok
}

func queryHasSingleOuterParens(expr string) bool {
	if len(expr) < 2 || expr[0] != '(' {
		return false
	}
	_, close, ok := queryCallArity(expr, 0)
	return ok && close == len(expr)-1
}

type queryBoundaryToken struct {
	text       string
	start, end int
}

// queryEqualityAtMixesSafeAndUnknownNumbers examines the immediate operands of
// every ==/!=, including comparators nested in filters, hashes, lists, function
// arguments, parentheses, and logical expressions. JMESPath operators with the
// same or lower binding power, plus collection separators, delimit an operand.
func queryEqualityAtMixesSafeAndUnknownNumbers(expr string, operatorStart int) bool {
	tokens := queryBoundaryTokensForExpression(expr)
	operatorIndex := -1
	for i, token := range tokens {
		if token.start == operatorStart && (token.text == "==" || token.text == "!=") {
			operatorIndex = i
			break
		}
	}
	if operatorIndex < 0 {
		return false
	}

	leftStart := queryEqualityOperandStart(tokens, operatorIndex)
	rightEnd := queryEqualityOperandEnd(tokens, operatorIndex, len(expr))
	leftSafe := queryEqualityOperandProducesSafeNumber(expr[leftStart:operatorStart])
	rightSafe := queryEqualityOperandProducesSafeNumber(expr[tokens[operatorIndex].end:rightEnd])
	return leftSafe != rightSafe
}

func queryEqualityOperandProducesSafeNumber(expr string) bool {
	expr = strings.TrimSpace(expr)
	if strings.HasPrefix(expr, "&") {
		expr = strings.TrimSpace(expr[1:])
	}
	return queryExpressionProducesSafeNumber(expr)
}

func queryBoundaryTokensForExpression(expr string) []queryBoundaryToken {
	var tokens []queryBoundaryToken
	for i := 0; i < len(expr); {
		switch expr[i] {
		case '\'', '"', '`':
			next, ok := skipQueryQuoted(expr, i)
			if !ok {
				return tokens
			}
			i = next + 1
			continue
		case '(', ')', '[', ']', '{', '}', ',', ':', '?':
			end := i + 1
			if expr[i] == '[' && end < len(expr) && expr[end] == '?' {
				end++
			}
			tokens = append(tokens, queryBoundaryToken{text: expr[i:end], start: i, end: end})
			i = end
			continue
		case '=', '!', '<', '>', '|', '&':
			end := i + 1
			if end < len(expr) && ((expr[i] == '=' && expr[end] == '=') ||
				(expr[i] == '!' && expr[end] == '=') ||
				(expr[i] == '<' && expr[end] == '=') ||
				(expr[i] == '>' && expr[end] == '=') ||
				(expr[i] == '|' && expr[end] == '|') ||
				(expr[i] == '&' && expr[end] == '&')) {
				end++
			}
			tokens = append(tokens, queryBoundaryToken{text: expr[i:end], start: i, end: end})
			i = end
			continue
		}
		i++
	}
	return tokens
}

func queryEqualityOperandStart(tokens []queryBoundaryToken, operatorIndex int) int {
	depth := 0
	for i := operatorIndex - 1; i >= 0; i-- {
		token := tokens[i]
		switch token.text {
		case ")", "]", "}":
			depth++
		case "(", "[", "[?", "{":
			if depth > 0 {
				depth--
				continue
			}
			return token.end
		default:
			if depth == 0 && queryTokenSeparatesEqualityOperand(token.text) {
				return token.end
			}
		}
	}
	return 0
}

func queryEqualityOperandEnd(tokens []queryBoundaryToken, operatorIndex, expressionEnd int) int {
	depth := 0
	for i := operatorIndex + 1; i < len(tokens); i++ {
		token := tokens[i]
		switch token.text {
		case "(", "[", "[?", "{":
			depth++
		case ")", "]", "}":
			if depth > 0 {
				depth--
				continue
			}
			return token.start
		default:
			if depth == 0 && queryTokenSeparatesEqualityOperand(token.text) {
				return token.start
			}
		}
	}
	return expressionEnd
}

func queryTokenSeparatesEqualityOperand(token string) bool {
	switch token {
	case ",", ":", "?", "&", "|", "||", "&&", "==", "!=", "<", "<=", ">", ">=":
		return true
	}
	return false
}

// queryOrderingExpressionIsSafe intentionally accepts only a complete
// safe-number comparison (allowing redundant outer parentheses). This avoids
// treating a safe-looking suffix as the operand in expressions such as
// "N || length(A) > length(B)". Comparisons nested in filters, pipes, logical
// expressions, collections, or function arguments remain conservatively
// rejected unless a future AST-aware implementation can prove their operands.
func queryOrderingExpressionIsSafe(expr string) bool {
	expr = strings.TrimSpace(expr)
	for queryHasSingleOuterParens(expr) {
		expr = strings.TrimSpace(expr[1 : len(expr)-1])
	}
	operatorStart, operatorEnd := -1, -1
	roundDepth, squareDepth, curlyDepth := 0, 0, 0
	for i := 0; i < len(expr); i++ {
		switch expr[i] {
		case '\'', '"', '`':
			next, ok := skipQueryQuoted(expr, i)
			if !ok {
				return false
			}
			i = next
		case '(':
			roundDepth++
		case ')':
			if roundDepth == 0 {
				return false
			}
			roundDepth--
		case '[':
			squareDepth++
		case ']':
			if squareDepth == 0 {
				return false
			}
			squareDepth--
		case '{':
			curlyDepth++
		case '}':
			if curlyDepth == 0 {
				return false
			}
			curlyDepth--
		case '<', '>':
			// More than one ordering operation is outside the deliberately
			// small proof surface, even if both individually look safe.
			if operatorStart >= 0 || roundDepth != 0 || squareDepth != 0 || curlyDepth != 0 {
				return false
			}
			operatorStart, operatorEnd = i, i+1
			if operatorEnd < len(expr) && expr[operatorEnd] == '=' {
				operatorEnd++
				i++
			}
		}
	}
	return operatorStart >= 0 &&
		queryExpressionProducesSafeNumber(expr[:operatorStart]) &&
		queryExpressionProducesSafeNumber(expr[operatorEnd:])
}

// A backtick JSON literal is decoded by go-jmespath with json.Unmarshal, so
// every contained number becomes float64 before evaluation. Reject such a
// literal unless its value is consumed only for its shape.
func queryJSONLiteralContainsNumber(raw string) bool {
	value, ok := queryDecodeJSONLiteral(raw)
	return ok && queryValueContainsJSONNumber(value)
}

func queryDecodeJSONLiteral(raw string) (interface{}, bool) {
	decoder := json.NewDecoder(strings.NewReader(strings.ReplaceAll(raw, "\\`", "`")))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return nil, false // The upstream parser will report malformed JSON.
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return nil, false
	}
	return value, true
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
	right := skipQuerySpaces(expr, end)
	for ; openCount > 0; openCount-- {
		if right >= len(expr) || expr[right] != ')' {
			return false
		}
		right = skipQuerySpaces(expr, right+1)
	}

	literal, ok := queryDecodeJSONLiteral(expr[start+1 : end-1])
	if !ok {
		return false
	}
	switch name {
	case "length":
		switch literal.(type) {
		case string, []interface{}, map[string]interface{}:
			return true
		}
	case "keys":
		_, ok := literal.(map[string]interface{})
		return ok
	}
	return false
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
