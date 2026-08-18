package output

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v2"
)

func writeYAML(w io.Writer, data interface{}) error {
	b, err := yaml.Marshal(data)
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
	if err := enc.Encode(data); err != nil {
		_ = enc.Close()
		return fmt.Errorf("yaml-stream encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("yaml-stream close: %w", err)
	}
	return nil
}
