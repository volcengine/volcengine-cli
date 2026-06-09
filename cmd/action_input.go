package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/volcengine/volcengine-cli/util"
)

func buildActionInput(flags []*Flag, apiMeta *ApiMeta, jsonBody bool) (interface{}, bool, error) {
	hasBody := false
	hasFlat := false
	var bodyVal string
	flat := make(map[string]string)

	for _, f := range flags {
		if f.Name == "body" {
			hasBody = true
			bodyVal = f.value
			continue
		}
		hasFlat = true
		flat[f.Name] = f.value
	}

	if hasBody && hasFlat {
		return nil, false, fmt.Errorf("--body cannot be used together with flattened parameters")
	}

	if hasBody {
		parsed, err := parseJSONBody(bodyVal)
		if err != nil {
			return nil, false, err
		}
		return parsed, true, nil
	}

	if jsonBody {
		nested, err := expandFlatToJSON(flat, apiMeta)
		if err != nil {
			return nil, false, err
		}
		return nested, false, nil
	}

	// Non-JSON (query/form) APIs: keep the existing dotted-key behavior; the
	// server re-expands dot-notation, so values are not nested here.
	input := make(map[string]interface{})
	for name, val := range flat {
		if isStringParam(apiMeta, name) {
			input[name] = val
		} else if a, success := util.ParseToJsonArrayOrObject(strings.TrimSpace(val)); success {
			input[name] = a
		} else {
			input[name] = val
		}
	}
	return input, false, nil
}

func parseJSONBody(body string) (interface{}, error) {
	m := make(map[string]interface{})
	if err := json.Unmarshal([]byte(body), &m); err == nil {
		return &m, nil
	}

	var a []interface{}
	if err := json.Unmarshal([]byte(body), &a); err == nil {
		return &a, nil
	}

	return nil, fmt.Errorf("json format error")
}
