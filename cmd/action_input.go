package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/volcengine/volcengine-cli/util"
)

func buildActionInput(flags []*Flag, apiMeta *ApiMeta) (interface{}, bool, error) {
	input := make(map[string]interface{})
	hasBody := false
	hasFlat := false

	for _, f := range flags {
		if f.Name == "body" {
			hasBody = true
			input[f.Name] = f.value
			continue
		}

		hasFlat = true
		if isStringParam(apiMeta, f.Name) {
			input[f.Name] = f.value
		} else if a, success := util.ParseToJsonArrayOrObject(strings.TrimSpace(f.value)); success {
			input[f.Name] = a
		} else {
			input[f.Name] = f.value
		}
	}

	if hasBody && hasFlat {
		return nil, false, fmt.Errorf("--body cannot be used together with flattened parameters")
	}

	if hasBody {
		body, ok := input["body"].(string)
		if !ok {
			return nil, false, fmt.Errorf("--body must be a JSON string")
		}
		parsed, err := parseJSONBody(body)
		if err != nil {
			return nil, false, err
		}
		return parsed, true, nil
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
