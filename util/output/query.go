package output

import (
	"encoding/json"
	"math"
	"strconv"
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
//
// go-jmespath does not treat encoding/json.Number as a number. Comparisons
// such as N == `8` return false, N != `8` return true, and [?N == `8`]
// returns an empty list — without an error. Evaluate on a copy whose exact
// integers/floats are native numbers, then put json.Number leaves back when
// the original tree yields the same shape so large integers and long decimals
// stay exact.
func (q *Query) Search(data interface{}) (interface{}, error) {
	if q == nil || q.compiled == nil {
		return data, nil
	}
	if !containsJSONNumber(data) {
		return q.compiled.Search(data)
	}
	result, err := q.compiled.Search(queryCompatibleData(data))
	if err != nil {
		return nil, err
	}
	return restoreExactNumbersFromData(data, result), nil
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

func containsJSONNumber(data interface{}) bool {
	switch value := data.(type) {
	case json.Number:
		return true
	case map[string]interface{}:
		for _, item := range value {
			if containsJSONNumber(item) {
				return true
			}
		}
	case []interface{}:
		for _, item := range value {
			if containsJSONNumber(item) {
				return true
			}
		}
	}
	return false
}

// restoreExactNumbersFromData puts original json.Number digits back onto
// computed floats that still match that number. It never zips two query trees
// by index, so sort/filter cannot pair the wrong digits.
func restoreExactNumbersFromData(data, computed interface{}) interface{} {
	return rewriteExactNumbers(computed, uniqueNumbersByFloat(data))
}

func uniqueNumbersByFloat(data interface{}) map[float64]json.Number {
	catalog := make(map[float64]json.Number)
	ambiguous := make(map[float64]struct{})
	var walk func(interface{})
	walk = func(value interface{}) {
		switch typed := value.(type) {
		case json.Number:
			parsed, err := strconv.ParseFloat(typed.String(), 64)
			if err != nil {
				return
			}
			if _, taken := ambiguous[parsed]; taken {
				return
			}
			if existing, ok := catalog[parsed]; ok && existing.String() != typed.String() {
				delete(catalog, parsed)
				ambiguous[parsed] = struct{}{}
				return
			}
			catalog[parsed] = typed
		case map[string]interface{}:
			for _, item := range typed {
				walk(item)
			}
		case []interface{}:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(data)
	return catalog
}

func rewriteExactNumbers(computed interface{}, catalog map[float64]json.Number) interface{} {
	switch computedValue := computed.(type) {
	case float64:
		if number, ok := catalog[computedValue]; ok && shouldRestoreNumber(number, computedValue) {
			return number
		}
		return computedValue
	case map[string]interface{}:
		restored := make(map[string]interface{}, len(computedValue))
		for key, item := range computedValue {
			restored[key] = rewriteExactNumbers(item, catalog)
		}
		return restored
	case []interface{}:
		restored := make([]interface{}, len(computedValue))
		for i, item := range computedValue {
			restored[i] = rewriteExactNumbers(item, catalog)
		}
		return restored
	default:
		return computed
	}
}

// queryCompatibleNumber turns a JSON number into float64 so go-jmespath can
// compare it. Integers larger than 2^53 become inexact IEEE values; identity
// projections restore the original json.Number when the float still matches.
func shouldRestoreNumber(number json.Number, computed float64) bool {
	_, frac := math.Modf(computed)
	if frac != 0 {
		return true
	}
	raw := number.String()
	if strings.ContainsAny(raw, ".eE") {
		return false
	}
	if integer, err := strconv.ParseInt(raw, 10, 64); err == nil {
		const maxExact = int64(1 << 53)
		return integer > maxExact || integer < -maxExact
	}
	if _, err := strconv.ParseUint(raw, 10, 64); err == nil {
		return true
	}
	return false
}

func queryCompatibleNumber(value json.Number) interface{} {
	if number, err := strconv.ParseFloat(value.String(), 64); err == nil {
		return number
	}
	return value
}


