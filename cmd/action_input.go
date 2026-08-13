package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/volcengine/volcengine-cli/util"
)

// errBodyRequiresJSON is returned when --body is used with a non-JSON Content-Type.
// Users can re-enable --body via --header Content-Type=application/json.
const errBodyRequiresJSON = "--body requires Content-Type application/json (use --header Content-Type=application/json or flattened --Param values)"

func buildActionInput(flags []*Flag, apiMeta *ApiMeta, jsonBody bool) (interface{}, bool, error) {
	hasBody := false
	hasFlat := false
	var bodyVal string
	flat := make(map[string]string)

	for _, f := range flags {
		if f == nil {
			continue
		}
		if f.Name == "body" {
			hasBody = true
			bodyVal = f.value
			continue
		}
		if isSkipBodyDynamicFlag(f.Name) {
			// Reserved CLI double-dash controls (e.g. --header).
			continue
		}
		hasFlat = true
		flat[f.Name] = f.value
	}

	if hasBody && hasFlat {
		return nil, false, fmt.Errorf("--body cannot be used together with flattened parameters")
	}

	if hasBody {
		// --body is the JSON request-body path. Non-JSON (query/form) actions must use
		// flattened --Param values; accepting --body there used to type-assert away the
		// payload and silently call the SDK with an empty map.
		if !jsonBody {
			return nil, false, fmt.Errorf("%s", errBodyRequiresJSON)
		}
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
	decoder := json.NewDecoder(bytes.NewBufferString(body))
	decoder.UseNumber()

	var parsed interface{}
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("json format error")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("json format error")
	}

	switch value := parsed.(type) {
	case map[string]interface{}:
		return &value, nil
	case []interface{}:
		return &value, nil
	default:
		return nil, fmt.Errorf("json format error")
	}
}
