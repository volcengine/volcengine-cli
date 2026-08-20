package output

import (
	"strings"
	"testing"
)

func compileErr(t *testing.T, expr string) string {
	t.Helper()
	_, err := CompileQuery(expr)
	if err == nil {
		t.Fatalf("expected %q to fail", expr)
	}
	return err.Error()
}

// The message must name the expression, not just say "syntax error".
func TestQueryErrorIncludesExpression(t *testing.T) {
	msg := compileErr(t, "Result.Instances[")
	if !strings.Contains(msg, "Result.Instances[") {
		t.Fatalf("expression missing from error:\n%s", msg)
	}
	if !strings.Contains(msg, "--query") {
		t.Fatalf("error should mention --query:\n%s", msg)
	}
}

// A caret must sit under the reported failure offset.
func TestQueryErrorShowsCaret(t *testing.T) {
	msg := compileErr(t, "Result..Instances")
	lines := strings.Split(msg, "\n")
	var exprLine, caretLine string
	for i, l := range lines {
		if strings.TrimSpace(l) == "Result..Instances" && i+1 < len(lines) {
			exprLine, caretLine = l, lines[i+1]
			break
		}
	}
	if caretLine == "" {
		t.Fatalf("no caret line found:\n%s", msg)
	}
	if !strings.Contains(caretLine, "^") {
		t.Fatalf("caret missing:\n%s", msg)
	}
	// The caret must point at the second dot (index 7 within the expression).
	want := strings.Index(exprLine, "Result..Instances") + 7
	if got := strings.Index(caretLine, "^"); got != want {
		t.Fatalf("caret at %d, want %d:\n%s", got, want, msg)
	}
}

// Offset is a byte offset; a CJK expression must not shift the caret or slice
// a rune in half.
func TestQueryErrorCaretHandlesWideRunes(t *testing.T) {
	msg := compileErr(t, "实例列表.数据[")
	if strings.Contains(msg, `\u`) {
		t.Fatalf("error leaks an escape sequence instead of the character:\n%s", msg)
	}
	if !strings.Contains(msg, "'实'") {
		t.Fatalf("expected the offending character to be shown:\n%s", msg)
	}
	// Advice must be to quote the name, not to delete it.
	if !strings.Contains(msg, "double-quoted") {
		t.Fatalf("expected quoting advice for a CJK field name:\n%s", msg)
	}
}

// Internal token names must never reach the user.
func TestQueryErrorHidesInternalTokenNames(t *testing.T) {
	for _, expr := range []string{
		"Result.Instances[",
		"Result.Instances[].{Id:InstanceId",
		"Result.Instances | [0",
	} {
		msg := compileErr(t, expr)
		for _, leak := range []string{
			"tStar", "tEOF", "tRbracket", "tUnquotedIdentifier",
			"tQuotedIdentifier", "ASTEmpty", "Unknown AST node",
		} {
			if strings.Contains(msg, leak) {
				t.Errorf("%q leaks %q:\n%s", expr, leak, msg)
			}
		}
	}
}

func TestQueryErrorHintsUnbalancedBrackets(t *testing.T) {
	if msg := compileErr(t, "Result.Instances["); !strings.Contains(msg, "unclosed '['") {
		t.Fatalf("missing bracket hint:\n%s", msg)
	}
	if msg := compileErr(t, "Result.Instances[].{Id:InstanceId"); !strings.Contains(msg, "unclosed '{'") {
		t.Fatalf("missing brace hint:\n%s", msg)
	}
}

// A bracket inside a string literal is not an unbalanced bracket.
func TestUnbalancedIgnoresQuotedText(t *testing.T) {
	if got := unbalanced("[?Name=='a[b']", '[', ']'); got != 0 {
		t.Fatalf("quoted bracket counted as unbalanced: %d", got)
	}
	if got := unbalanced("Result.Instances[", '[', ']'); got != 1 {
		t.Fatalf("expected 1 unclosed bracket, got %d", got)
	}
}

func TestQueryErrorHintsUnclosedString(t *testing.T) {
	msg := compileErr(t, "Result.Instances[?Status=='Running]")
	if !strings.Contains(msg, "not closed") {
		t.Fatalf("expected unclosed-string hint:\n%s", msg)
	}
}

// Unknown functions are only detected at evaluation time by go-jmespath;
// CompileQuery must surface them before the API request is sent.
func TestQueryErrorCatchesUnknownFunctionAtCompileTime(t *testing.T) {
	msg := compileErr(t, "nosuchfunc(@)")
	if !strings.Contains(msg, "nosuchfunc") {
		t.Fatalf("expected the function name:\n%s", msg)
	}
}

func TestQueryErrorSuggestsNearestFunction(t *testing.T) {
	msg := compileErr(t, "lenght(@)")
	if !strings.Contains(msg, `did you mean "length"`) {
		t.Fatalf("expected a spelling suggestion:\n%s", msg)
	}
}

// A name nowhere near a builtin must not get a bogus suggestion.
func TestQueryErrorSkipsSuggestionWhenTooFar(t *testing.T) {
	msg := compileErr(t, "frobnicate(@)")
	if strings.Contains(msg, "did you mean") {
		t.Fatalf("unexpected suggestion:\n%s", msg)
	}
}

func TestQueryErrorCatchesWrongArity(t *testing.T) {
	msg := compileErr(t, "length(@, @)")
	if !strings.Contains(msg, "wrong number of arguments") {
		t.Fatalf("expected an arity message:\n%s", msg)
	}
}

// "a | [0" compiles but fails at evaluation with "Unknown AST node".
func TestQueryErrorCatchesIncompleteExpression(t *testing.T) {
	msg := compileErr(t, "Result.Instances | [0")
	if !strings.Contains(msg, "incomplete") {
		t.Fatalf("expected an incompleteness message:\n%s", msg)
	}
}

// The probe must not reject expressions that are merely data-dependent.
func TestValidQueriesStillCompile(t *testing.T) {
	for _, expr := range []string{
		"Result.Instances[].{Id:InstanceId,Status:Status}",
		"Result.Instances[?Status=='Running'].InstanceId",
		"length(Result.Instances)",
		"Left == Right",
		"Left != Right",
		"length(A) == length(B)",
		"sort_by(Result.Instances, &InstanceId)[].InstanceId",
		"max_by(Result.Instances, &InstanceId).InstanceId",
		"min_by(Result.Instances, &InstanceId).InstanceId",
		"type('web')",
		"sort(`[\"db\",\"web\"]`)",
		"max(`[\"db\",\"web\"]`)",
		"min(`[\"db\",\"web\"]`)",
		"Result.Instances[0]",
		`"odd-name".value`,
		"Result.Instances | [0].InstanceId",
		"merge(Result, ResponseMetadata)",
		"not_null(Result.Missing, Result.TotalCount)",
		"starts_with(Result.Name, 'web-')",
		"contains(Result.Name, 'web')",
		"contains(Result.Tags, 'web')",
		"length(`[1]`)",
		"length(`[9007199254740993]`)",
		"keys(`{\"N\":9007199254740993}`)",
		"merge(A,B,C)",
		"not_null(A,B,C)",
		"merge({A:not_null(Bar, Baz),B:[One,Two]}, {CommaKey:'x,y'})",
	} {
		if _, err := CompileQuery(expr); err != nil {
			t.Errorf("valid expression %q rejected: %v", expr, err)
		}
	}
}

func TestBooleanAndNullEqualityQueriesCompile(t *testing.T) {
	for _, expr := range []string{
		"Enabled == `true`",
		"`false` != Enabled",
		"DeletedAt == `null`",
		"`null` != DeletedAt",
		"Items[?Enabled == `true`].Name",
		"Items[?DeletedAt != `null`].Name",
		"Enabled\t==\n( `true` )",
		"(( `false` )) != Enabled",
	} {
		if _, err := CompileQuery(expr); err != nil {
			t.Errorf("safe boolean/null comparison %q rejected: %v", expr, err)
		}
	}
}

func TestQueryErrorCatchesUnknownFunctionInsideProjection(t *testing.T) {
	msg := compileErr(t, "Result.Instances[].nosuchfunc(@)")
	if !strings.Contains(msg, "nosuchfunc") {
		t.Fatalf("expected projection function name before evaluation:\n%s", msg)
	}
	if !strings.Contains(msg, "^") {
		t.Fatalf("expected a caret at the function call:\n%s", msg)
	}
}

func TestQueryErrorCatchesArityInsideFilter(t *testing.T) {
	msg := compileErr(t, "Result.Instances[?starts_with(Name)].Id")
	if !strings.Contains(msg, "wrong number of arguments") {
		t.Fatalf("expected filter arity error before evaluation:\n%s", msg)
	}
}

func TestQueryValidationRecursesIntoFunctionArguments(t *testing.T) {
	for _, expr := range []string{
		"not_null(A, nosuchfunc(Value))",
		"map(&nosuchfunc(@), Items)",
	} {
		msg := compileErr(t, expr)
		if !strings.Contains(msg, "nosuchfunc") {
			t.Errorf("nested unknown function in %q was missed:\n%s", expr, msg)
		}
	}

	msg := compileErr(t, "not_null(A, length(B, C))")
	if !strings.Contains(msg, "wrong number of arguments") {
		t.Fatalf("nested arity error was missed:\n%s", msg)
	}
}

func TestQueryArityHandlesNestedDelimitersAndQuotedCommas(t *testing.T) {
	for _, expr := range []string{
		"merge(A, {Nested:not_null(B,C), List:[D,E]}, F)",
		"not_null('a,b', {X:'c,d'}, [A,B])",
		"map(&not_null(@, 'fallback,value'), Items)",
	} {
		if _, err := CompileQuery(expr); err != nil {
			t.Errorf("valid nested call %q rejected: %v", expr, err)
		}
	}
}

func TestQueryValidationSkipsQuotedContent(t *testing.T) {
	for _, expr := range []string{
		`"nosuchfunc(@)"`,
		"Result[?Name=='nosuchfunc(@)']",
		"to_string(`{\"call\":\"nosuchfunc(@)\",\"cmp\":\"1 > 0\"}`)",
		"'Field == `true` && N == `1`'",
		"\"field==`true`\"",
		"to_string(`{\"cmp\":\"Left == Right\",\"enabled\":true}`)",
		"`\"9007199254740993\"`",
		"to_string(`{\"digits\":\"9007199254740993\"}`)",
	} {
		if _, err := CompileQuery(expr); err != nil {
			t.Errorf("quoted content in %q was scanned as code: %v", expr, err)
		}
	}
}

func TestUnsafeNumericQueriesAreRejected(t *testing.T) {
	for _, expr := range []string{
		"N == `9007199254740993`",
		"`9007199254740993` == N",
		"N != (`0.10000000000000001`)",
		"(`-1`) != N",
		"`true` == `1`",
		"`0` != `null`",
		"'text' == `1`",
		"N > `0`",
		"Items[?Cpu != `8`]",
		"avg(Items)",
		"contains(Items, `8`)",
		"contains(not_null(`[9007199254740993]`, 'unused'), '9007199254740992')",
	} {
		msg := compileErr(t, expr)
		if !strings.Contains(msg, "exact JSON-number semantics") {
			t.Errorf("%q did not explain the exact-number restriction:\n%s", expr, msg)
		}
	}
}

// go-jmespath panics inside jpfMerge when an argument is absent. Neither
// compiling nor evaluating such an expression may take the process down.
func TestQuerySurvivesLibraryPanic(t *testing.T) {
	q, err := CompileQuery("merge(Result, ResponseMetadata)")
	if err != nil {
		t.Fatalf("valid expression rejected: %v", err)
	}
	// Missing keys are what triggers the library's nil type assertion.
	if _, err := q.Search(map[string]interface{}{"Result": map[string]interface{}{}}); err == nil {
		t.Log("no error returned; library handled it")
	}
	// Present keys must still work normally.
	got, err := q.Search(map[string]interface{}{
		"Result":           map[string]interface{}{"a": "1"},
		"ResponseMetadata": map[string]interface{}{"b": "2"},
	})
	if err != nil {
		t.Fatalf("merge on complete data failed: %v", err)
	}
	m, ok := got.(map[string]interface{})
	if !ok || m["a"] != "1" || m["b"] != "2" {
		t.Fatalf("unexpected merge result: %#v", got)
	}
}

func TestEditDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"length", "length", 0},
		{"lenght", "length", 2},
		{"sort", "sort_by", 3},
		{"", "abs", 3},
	}
	for _, c := range cases {
		if got := editDistance(c.a, c.b); got != c.want {
			t.Errorf("editDistance(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// Single quotes in the expression must stay readable, not become \'.
func TestQuoteExprKeepsStringLiteralsReadable(t *testing.T) {
	got := quoteExpr("Result[?S=='R']")
	if strings.Contains(got, `\'`) {
		t.Fatalf("escaped quotes hurt readability: %s", got)
	}
	if !strings.Contains(got, "'R'") {
		t.Fatalf("literal lost: %s", got)
	}
}

// A raw example containing nested single quotes would make the wrapping
// delimiter ambiguous, so choose a different delimiter for the expression.
func TestQuoteExprPicksNonConflictingDelimiter(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		// No quotes inside: plain single quotes are fine.
		{"Result.Instances[", "'Result.Instances['"},
		// Contains ': must not be wrapped in ' again.
		{"Result[?S=='R'", "`Result[?S=='R'`"},
		{"Result[?Status=='Running'", "`Result[?Status=='Running'`"},
		// Contains a backtick (raw literal): fall back to single quotes.
		{"Result[?N==`8`", "'Result[?N==`8`'"},
	}
	for _, c := range cases {
		if got := quoteExpr(c.expr); got != c.want {
			t.Errorf("quoteExpr(%q) = %s, want %s", c.expr, got, c.want)
		}
	}
}

// Both delimiters present: %q is the only unambiguous option left.
func TestQuoteExprFallsBackWhenAllDelimitersPresent(t *testing.T) {
	expr := "Result[?S=='R' && T==`1`] || \"x\""
	got := quoteExpr(expr)
	if !strings.HasPrefix(got, `"`) || !strings.Contains(got, `\`) {
		t.Fatalf("expected a %%q fallback, got %s", got)
	}
}

// The rendered error must never show two identical delimiters back to back at
// the boundary, which is what made the old output unreadable.
func TestQueryErrorBoundaryIsUnambiguous(t *testing.T) {
	msg := compileErr(t, "Result.Instances[?Status=='Running'")
	firstLine := strings.SplitN(msg, "\n", 2)[0]
	if strings.Contains(firstLine, "''") {
		t.Fatalf("doubled quote hides the expression boundary:\n%s", firstLine)
	}
	// The expression must still be reproduced verbatim.
	if !strings.Contains(firstLine, "Result.Instances[?Status=='Running'") {
		t.Fatalf("expression not shown verbatim:\n%s", firstLine)
	}
}
