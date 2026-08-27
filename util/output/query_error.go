package output

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"github.com/volcengine/volcengine-cli/internal/jmespath"
)

// Error reporting for --query compilation.
//
// go-jmespath returns a SyntaxError carrying the byte Offset of the failure and
// the expression itself, but its Error() only prints the bare message: the
// library's own comment on SyntaxError.Error says underlining the location
// would be good "in the future". A bare "SyntaxError: Expected tStar,
// received: tEOF" is close to useless on a long expression — the user cannot
// tell which bracket is unbalanced, and tStar/tEOF are internal token names
// that never appear in the expression they wrote.
//
// So we render the caret ourselves (HighlightLocation() assumes one space per
// byte, which misplaces the marker on non-ASCII expressions) and translate the
// token vocabulary into what the user should type.

// QueryError explains why an expression failed to compile.
type QueryError struct {
	Expression string
	Offset     int // byte offset of the failure; -1 when unknown
	Err        error
}

func (e *QueryError) Error() string {
	var b strings.Builder
	b.WriteString("invalid --query ")
	b.WriteString(quoteExpr(e.Expression))
	b.WriteString(": ")
	b.WriteString(humanizeQueryMessage(e.Err))

	// Point at the failure only when the offset is inside the expression.
	if e.Offset >= 0 && e.Offset <= len(e.Expression) && e.Expression != "" {
		b.WriteString("\n\n  ")
		b.WriteString(e.Expression)
		b.WriteString("\n  ")
		b.WriteString(caretPad(e.Expression, e.Offset))
		b.WriteString("^")
	}
	if hint := queryHint(e.Expression, e.Err); hint != "" {
		b.WriteString("\n\nhint: ")
		b.WriteString(hint)
	}
	return b.String()
}

func (e *QueryError) Unwrap() error { return e.Err }

// newQueryError extracts the offset from a jmespath SyntaxError when present.
func newQueryError(expr string, err error) *QueryError {
	qe := &QueryError{Expression: expr, Offset: -1, Err: err}
	if se, ok := err.(jmespath.SyntaxError); ok {
		qe.Offset = se.Offset
	} else if located, ok := err.(interface{ queryOffset() int }); ok {
		qe.Offset = located.queryOffset()
	}
	return qe
}

// caretPad builds the indent that places "^" under the offset byte.
//
// SyntaxError.Offset is a byte offset, while alignment must be counted in
// terminal cells: a CJK identifier is 3 bytes but 2 cells wide, so both
// HighlightLocation() (one space per byte) and naive slicing put the caret in
// the wrong place. Slice on a rune boundary at or before the offset, then
// measure display width.
func caretPad(expr string, offset int) string {
	if offset > len(expr) {
		offset = len(expr)
	}
	// Back off to the start of the rune containing the offset so the slice is
	// never cut mid-character. offset == len(expr) is a valid end position.
	for offset > 0 && offset < len(expr) && !utf8.RuneStart(expr[offset]) {
		offset--
	}
	return strings.Repeat(" ", runewidth.StringWidth(expr[:offset]))
}

// tokenNames maps go-jmespath's internal token identifiers to the characters a
// user actually types. Leaking "tStar" or "tRbracket" into a CLI error forces
// the reader to guess.
var tokenNames = map[string]string{
	"tStar":               "'*'",
	"tDot":                "'.'",
	"tFilter":             "'[?'",
	"tFlatten":            "'[]'",
	"tLparen":             "'('",
	"tRparen":             "')'",
	"tLbracket":           "'['",
	"tRbracket":           "']'",
	"tLbrace":             "'{'",
	"tRbrace":             "'}'",
	"tOr":                 "'||'",
	"tPipe":               "'|'",
	"tComma":              "','",
	"tColon":              "':'",
	"tAnd":                "'&&'",
	"tNot":                "'!'",
	"tEQ":                 "'=='",
	"tNE":                 "'!='",
	"tLT":                 "'<'",
	"tLTE":                "'<='",
	"tGT":                 "'>'",
	"tGTE":                "'>='",
	"tJSONLiteral":        "a JSON literal (`...`)",
	"tStringLiteral":      "a string literal ('...')",
	"tNumber":             "a number",
	"tUnquotedIdentifier": "a field name",
	"tQuotedIdentifier":   `a quoted field name ("...")`,
	"tCurrent":            "'@'",
	"tExpref":             "'&'",
	"tEOF":                "end of expression",
	"tUnknown":            "an unrecognized character",
}

// humanizeQueryMessage rewrites the library message into user vocabulary.
func humanizeQueryMessage(err error) string {
	msg := err.Error()
	msg = strings.TrimPrefix(msg, "SyntaxError: ")

	// Evaluation-time messages leak internal AST names. "Unknown AST node:
	// ASTEmpty" means a construct was left incomplete, most often a dangling
	// pipe or an operator with a missing right-hand side.
	if strings.Contains(msg, "Unknown AST node") {
		return "the expression is incomplete"
	}
	if strings.Contains(msg, "incorrect number of args") ||
		strings.Contains(msg, " argument(s); expected ") {
		return "a function was called with the wrong number of arguments"
	}
	// The lexer reports a non-ASCII rune as an escape ("Unknown char:
	// '\u5b9e'"), which the user cannot match against what they typed.
	if rest := strings.TrimPrefix(msg, "Unknown char: "); rest != msg {
		if ch, ok := decodeQuotedRune(rest); ok {
			return fmt.Sprintf("unexpected character %q", ch)
		}
	}

	// Replace longest names first so tUnquotedIdentifier is not partially
	// matched by a shorter key.
	for _, name := range tokenNamesByLengthDesc() {
		msg = strings.ReplaceAll(msg, name, tokenNames[name])
	}
	msg = strings.ReplaceAll(msg, "Expected ", "expected ")
	msg = strings.ReplaceAll(msg, ", received: ", ", but found ")
	return msg
}

func tokenNamesByLengthDesc() []string {
	names := make([]string, 0, len(tokenNames))
	for name := range tokenNames {
		names = append(names, name)
	}
	// Insertion sort by descending length: the map is small and this avoids
	// pulling in sort just for stable ordering of a fixed table.
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && len(names[j]) > len(names[j-1]); j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}

// builtinFunctions is the JMESPath function set supported by go-jmespath.
// Kept here only to suggest a correction for a misspelled name.
// ("map" is registered internally as "amp"; users type "map".)
var builtinFunctions = []string{
	"abs", "avg", "ceil", "contains", "ends_with", "floor", "join", "keys",
	"length", "map", "max", "max_by", "merge", "min", "min_by", "not_null",
	"reverse", "sort", "sort_by", "starts_with", "sum", "to_array",
	"to_number", "to_string", "type", "values",
}

// nearestFunction returns the closest builtin name within a small edit
// distance, so "lenght" suggests "length" but "frobnicate" suggests nothing.
func nearestFunction(name string) string {
	best := ""
	bestDist := 0
	for _, candidate := range builtinFunctions {
		// Allow more slack for longer names, but never more than 2 edits.
		limit := 1
		if len(candidate) > 5 {
			limit = 2
		}
		d := editDistance(name, candidate)
		if d == 0 || d > limit {
			continue
		}
		if best == "" || d < bestDist {
			best, bestDist = candidate, d
		}
	}
	return best
}

// editDistance is Levenshtein distance over runes with a rolling row.
func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = minInt(minInt(curr[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// queryHint suggests a fix for the mistakes that actually show up in practice.
// It only fires on unambiguous evidence, because a wrong hint costs more than
// no hint.
func queryHint(expr string, err error) string {
	msg := err.Error()

	switch {
	case strings.Contains(msg, "unknown function: "):
		name := strings.TrimPrefix(msg, "unknown function: ")
		if suggestion := nearestFunction(name); suggestion != "" {
			return fmt.Sprintf("no function named %q; did you mean %q?", name, suggestion)
		}
		return fmt.Sprintf("no function named %q; see the JMESPath built-in functions", name)
	case strings.Contains(msg, "Unknown AST node"):
		return "a construct is left unfinished; check for a trailing '|', '.' or comparison operator"
	case strings.Contains(msg, "Unclosed delimiter: '"):
		return "a quoted string is not closed; string literals use single quotes, as in [?Status=='Running']"
	case strings.Contains(msg, `Unclosed delimiter: "`):
		return `a quoted field name is not closed, as in "field-with-dash"`
	case strings.Contains(msg, "Unknown char"):
		// A non-ASCII character here almost always means an unquoted CJK (or
		// otherwise non-identifier) field name, which JMESPath requires to be
		// double-quoted. Saying only "remove it" would be wrong advice.
		if hasNonASCII(expr) {
			return `non-ASCII field names must be double-quoted, as in "实例列表"."数据"`
		}
		return "remove the unsupported character; JMESPath allows letters, digits, '_' and the operators . [ ] { } ( ) | ? * @ &"
	}

	if n := unbalanced(expr, '[', ']'); n > 0 {
		return fmt.Sprintf("%d unclosed '['; every '[' needs a matching ']'", n)
	}
	if n := unbalanced(expr, '{', '}'); n > 0 {
		return fmt.Sprintf("%d unclosed '{'; a multiselect hash looks like {Name:Field,Id:Other}", n)
	}
	if n := unbalanced(expr, '(', ')'); n > 0 {
		return fmt.Sprintf("%d unclosed '('", n)
	}
	if strings.Contains(expr, "..") {
		return "'..' is not valid; use a single '.' between field names"
	}
	if strings.Contains(msg, "a field name") || strings.Contains(msg, "tUnquotedIdentifier") {
		return "field names cannot be empty; check for a trailing '.' or ',' "
	}
	return ""
}

// decodeQuotedRune reads the rune out of the lexer's quoted form, which may be
// a literal ("'#'") or a Go escape ("'\u5b9e'").
func decodeQuotedRune(quoted string) (rune, bool) {
	quoted = strings.TrimSpace(quoted)
	if len(quoted) < 3 || quoted[0] != '\'' || quoted[len(quoted)-1] != '\'' {
		return 0, false
	}
	if r, _, _, err := strconv.UnquoteChar(quoted[1:len(quoted)-1], '\''); err == nil {
		return r, true
	}
	return 0, false
}

func hasNonASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return true
		}
	}
	return false
}

// unbalanced counts openers left without a closer, ignoring quoted text so a
// bracket inside 'a[b' is not miscounted.
func unbalanced(expr string, open, close rune) int {
	depth := 0
	var quote rune
	for _, r := range expr {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"' || r == '`':
			quote = r
		case r == open:
			depth++
		case r == close:
			if depth > 0 {
				depth--
			}
		}
	}
	return depth
}

// quoteExpr wraps the expression in a delimiter that is not also part of it.
//
// %q would escape the single quotes that JMESPath string literals rely on,
// turning [?S=='R'] into "[?S==\\'R\\']". Wrapping the whole expression
// in single quotes also makes a common filter such as [?Status=='Running']
// ambiguous because the inner and outer delimiters become adjacent.
// So pick a delimiter the expression does not contain, and fall back to %q only
// when it contains all of them or is not safely printable.
func quoteExpr(expr string) string {
	if strings.ContainsAny(expr, "\n\r\t\x00") || !isPrintableASCIIsafe(expr) {
		return fmt.Sprintf("%q", expr)
	}
	for _, delim := range []string{"'", "`", `"`} {
		if !strings.Contains(expr, delim) {
			return delim + expr + delim
		}
	}
	return fmt.Sprintf("%q", expr)
}

func isPrintableASCIIsafe(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
