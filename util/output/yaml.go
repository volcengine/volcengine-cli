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
	b, err := yaml.Marshal(prepareYAML(data))
	if err != nil {
		return fmt.Errorf("yaml encode: %w", err)
	}
	if _, err = w.Write(b); err != nil {
		return fmt.Errorf("yaml write: %w", err)
	}
	return nil
}

// prepareYAML converts data for yaml.v2:
//   - json.Number / exact integer float64 become int64/uint64 when they fit
//   - non-integer json.Number becomes float64 when that float's shortest
//     decimal form matches the original digits (so 0.1 / 1.5 stay YAML
//     numbers); otherwise the exact digit string, so yaml.v2 cannot silently
//     round or emit scientific notation such as 2.106494982e+09
//   - maps become yaml.MapSlice with sorted keys so object key order is
//     stable; list order is unchanged
//   - typed nil maps/slices stay nil (YAML null), distinct from {} and []
func prepareYAML(data interface{}) interface{} {
	switch value := data.(type) {
	case json.Number:
		return yamlNumberScalar(value)
	case float64:
		return yamlFloatScalar(value)
	case map[string]interface{}:
		if value == nil {
			return nil
		}
		return yamlSortedMapping(value)
	case []interface{}:
		if value == nil {
			return nil
		}
		normalized := make([]interface{}, len(value))
		for index, item := range value {
			normalized[index] = prepareYAML(item)
		}
		return normalized
	default:
		return data
	}
}

func yamlSortedMapping(value map[string]interface{}) yaml.MapSlice {
	keys := sortedMapKeys(value)
	items := make(yaml.MapSlice, len(keys))
	for i, key := range keys {
		items[i] = yaml.MapItem{Key: key, Value: prepareYAML(value[key])}
	}
	return items
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
	// Non-integer: emit a real YAML number when float64 has the same shortest
	// decimal representation. Otherwise keep the raw JSON number string to avoid
	// silently rounding digits. Production responses decode numbers as
	// json.Number (WithForceJsonNumberDecode), so this branch is the common path
	// for decimals such as 0.1 / 1.5 and must not quote them.
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		if strconv.FormatFloat(f, 'g', -1, 64) == raw {
			return f
		}
	}
	return raw
}
