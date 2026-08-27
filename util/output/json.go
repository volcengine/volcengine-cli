package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func writeJSON(w io.Writer, data interface{}) error {
	content, err := EncodeJSON(data)
	if err != nil {
		return err
	}
	n, err := w.Write(content)
	if n < len(content) && err == nil {
		return io.ErrShortWrite
	}
	return err
}

// EncodeJSON returns indented JSON with HTML escaping disabled.
func EncodeJSON(data interface{}) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "    ")
	if err := encoder.Encode(data); err != nil {
		return nil, fmt.Errorf("json encode: %w", err)
	}
	return buf.Bytes(), nil
}
