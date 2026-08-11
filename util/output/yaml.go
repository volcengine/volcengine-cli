package output

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v2"
)

func writeYAML(w io.Writer, data interface{}) error {
	b, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("yaml encode: %v", err)
	}
	_, err = w.Write(b)
	return err
}

// writeYAMLStream encodes with a streaming YAML encoder (single document today).
// Encoder is always closed so the stream is fully flushed.
func writeYAMLStream(w io.Writer, data interface{}) error {
	enc := yaml.NewEncoder(w)
	if err := enc.Encode(data); err != nil {
		_ = enc.Close()
		return fmt.Errorf("yaml-stream encode: %v", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("yaml-stream close: %v", err)
	}
	return nil
}
