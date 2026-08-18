package output

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/jmespath/go-jmespath"
)

const maxExactJSONInteger = int64(1 << 53)

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
	result, err := q.compiled.Search(data)
	if err == nil && result != nil {
		return result, nil
	}
	compatibleResult, compatibleErr := q.compiled.Search(queryCompatibleData(data))
	if compatibleErr == nil {
		return compatibleResult, nil
	}
	if err != nil {
		return nil, compatibleErr
	}
	return result, nil
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

func queryCompatibleData(data interface{}) interface{} {
	switch value := data.(type) {
	case json.Number:
		return queryCompatibleNumber(value)
	case map[string]interface{}:
		normalized := make(map[string]interface{}, len(value))
		for key, item := range value {
			normalized[key] = queryCompatibleData(item)
		}
		return normalized
	case []interface{}:
		normalized := make([]interface{}, len(value))
		for index, item := range value {
			normalized[index] = queryCompatibleData(item)
		}
		return normalized
	default:
		return data
	}
}

func queryCompatibleNumber(value json.Number) interface{} {
	raw := value.String()
	if !strings.ContainsAny(raw, ".eE") {
		integer, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return value
		}
		if integer >= -maxExactJSONInteger && integer <= maxExactJSONInteger {
			return float64(integer)
		}
		return value
	}
	if number, err := strconv.ParseFloat(raw, 64); err == nil {
		return number
	}
	return value
}
