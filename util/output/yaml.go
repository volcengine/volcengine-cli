package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"

	"gopkg.in/yaml.v3"
)

func writeYAML(w io.Writer, data interface{}) error {
	node, err := prepareYAML(data)
	if err != nil {
		return fmt.Errorf("yaml encode: %w", err)
	}
	var encoded bytes.Buffer
	encoder := yaml.NewEncoder(&encoded)
	// yaml.v2 used two-space indentation. Keep the established CLI output
	// stable while using yaml.v3 nodes for exact numeric tags and literals.
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		return fmt.Errorf("yaml encode: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("yaml encode: %w", err)
	}
	if _, err = w.Write(encoded.Bytes()); err != nil {
		return fmt.Errorf("yaml write: %w", err)
	}
	return nil
}

// prepareYAML converts data to explicit yaml.v3 nodes:
//   - json.Number becomes an !!int or !!float scalar whose value is the exact
//     original JSON token; it is never parsed through float64
//   - exact integer float64 values become integer scalars when they fit
//   - maps become mapping nodes with sorted keys so object key order is
//     stable; list order is unchanged
//   - typed nil maps/slices stay nil (YAML null), distinct from {} and []
func prepareYAML(data interface{}) (*yaml.Node, error) {
	switch value := data.(type) {
	case json.Number:
		return yamlNumberScalar(value), nil
	case float64:
		return yamlEncodeNode(yamlFloatScalar(value))
	case map[string]interface{}:
		if value == nil {
			return yamlNullNode(), nil
		}
		return yamlSortedMapping(value)
	case []interface{}:
		if value == nil {
			return yamlNullNode(), nil
		}
		node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		node.Content = make([]*yaml.Node, 0, len(value))
		for _, item := range value {
			child, err := prepareYAML(item)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, child)
		}
		return node, nil
	default:
		return yamlEncodeNode(data)
	}
}

func yamlSortedMapping(value map[string]interface{}) (*yaml.Node, error) {
	keys := sortedMapKeys(value)
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	node.Content = make([]*yaml.Node, 0, 2*len(keys))
	for _, key := range keys {
		child, err := prepareYAML(value[key])
		if err != nil {
			return nil, err
		}
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			child,
		)
	}
	return node, nil
}

func yamlEncodeNode(value interface{}) (*yaml.Node, error) {
	if value == nil {
		return yamlNullNode(), nil
	}
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		return nil, err
	}
	return &node, nil
}

func yamlNullNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
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

func yamlNumberScalar(number json.Number) *yaml.Node {
	raw := number.String()
	tag := "!!int"
	if strings.ContainsAny(raw, ".eE") {
		tag = "!!float"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: raw}
}
