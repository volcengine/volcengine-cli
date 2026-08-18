package output

import (
	"encoding/json"
	"fmt"
	"io"
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
func yamlExactNumbers(data interface{}) interface{} {
	switch value := data.(type) {
	case json.Number:
		return yamlNumberScalar(value)
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
