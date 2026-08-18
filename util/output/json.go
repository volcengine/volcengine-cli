package output

import (
	"encoding/json"
	"fmt"
	"io"
)

func writeJSON(w io.Writer, data interface{}) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "    ")
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("json encode: %w", err)
	}
	return nil
}
