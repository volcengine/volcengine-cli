package output

import (
	"strings"

	"github.com/jmespath/go-jmespath"
)

// Query is a validated JMESPath expression.
type Query struct {
	compiled *jmespath.JMESPath
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
		return nil, err
	}
	return &Query{compiled: compiled}, nil
}

// Search evaluates a previously validated query.
func (q *Query) Search(data interface{}) (interface{}, error) {
	if q == nil || q.compiled == nil {
		return data, nil
	}
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
