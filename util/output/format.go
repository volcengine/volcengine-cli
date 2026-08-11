// Package output formats API responses for the CLI (AWS-inspired).
package output

import (
	"fmt"
	"io"
	"strings"
)

// Format is a supported --output value.
type Format string

const (
	FormatJSON       Format = "json"
	FormatTable      Format = "table"
	FormatText       Format = "text"
	FormatYAML       Format = "yaml"
	FormatYAMLStream Format = "yaml-stream"
	FormatOff        Format = "off"
)

// ParseFormat normalizes and validates an --output value.
// Empty string defaults to json.
func ParseFormat(s string) (Format, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return FormatJSON, nil
	}
	switch Format(s) {
	case FormatJSON, FormatTable, FormatText, FormatYAML, FormatYAMLStream, FormatOff:
		return Format(s), nil
	default:
		return "", fmt.Errorf("unsupported output format %q, supported: %s", s, supportedFormatsMessage())
	}
}

func supportedFormatsMessage() string {
	return "json, table, text, yaml, yaml-stream, off"
}

// Write formats data to w according to format.
func Write(w io.Writer, format Format, data interface{}) error {
	if w == nil {
		return fmt.Errorf("output writer is nil")
	}
	switch format {
	case FormatJSON:
		return writeJSON(w, data)
	case FormatTable:
		return writeTable(w, data)
	case FormatText:
		return writeText(w, data)
	case FormatYAML:
		return writeYAML(w, data)
	case FormatYAMLStream:
		return writeYAMLStream(w, data)
	case FormatOff:
		return nil
	default:
		return fmt.Errorf("unsupported output format %q, supported: %s", format, supportedFormatsMessage())
	}
}
