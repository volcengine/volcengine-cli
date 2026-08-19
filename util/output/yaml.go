package output

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"

	"gopkg.in/yaml.v2"
)

func writeYAML(w io.Writer, data interface{}) error {
	b, err := yaml.Marshal(yamlExactNumbers(data))
	if err != nil {
		return fmt.Errorf("yaml encode: %w", err)
	}
	if _, err = w.Write(b); err != nil {
		return fmt.Errorf("yaml write: %w", err)
	}
	return nil
}

// writeYAMLStream encodes with a streaming YAML encoder (single document today).
// Encoder is always closed so the stream is fully flushed.
func writeYAMLStream(w io.Writer, data interface{}) error {
	enc := yaml.NewEncoder(w)
	if err := enc.Encode(yamlExactNumbers(data)); err != nil {
		_ = enc.Close()
		return fmt.Errorf("yaml-stream encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("yaml-stream close: %w", err)
	}
	return nil
}

// yamlExactNumbers replaces json.Number with int64/uint64 when they fit, and
// otherwise keeps the exact digit string so yaml.v2 cannot round via float64.
// Exact integer float64 values (typical JMESPath projections) become int64 so
// yaml.v2 does not emit scientific notation such as 2.106494982e+09.
func yamlExactNumbers(data interface{}) interface{} {
	switch value := data.(type) {
	case json.Number:
		return yamlNumberScalar(value)
	case float64:
		return yamlFloatScalar(value)
	case map[string]interface{}:
		normalized := make(map[string]interface{}, len(value))
		for key, item := range value {
			normalized[key] = yamlExactNumbers(item)
		}
		return normalized
	case []interface{}:
		normalized := make([]interface{}, len(value))
		for index, item := range value {
			normalized[index] = yamlExactNumbers(item)
		}
		return normalized
	default:
		return data
	}
}

func yamlFloatScalar(value float64) interface{} {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	integer := int64(value)
	if float64(integer) == value {
		return integer
	}
	if value >= 0 {
		unsigned := uint64(value)
		if float64(unsigned) == value {
			return unsigned
		}
	}
	return value
}

func yamlNumberScalar(number json.Number) interface{} {
	raw := number.String()
	if integer, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return integer
	}
	if unsigned, err := strconv.ParseUint(raw, 10, 64); err == nil {
		return unsigned
	}
	return raw
}
