// Package output formats API responses for the CLI.
package output

import (
	"fmt"
	"io"
	"strings"
)

// Format is a supported --output value.
type Format string

const (
	FormatJSON  Format = "json"
	FormatTable Format = "table"
	FormatText  Format = "text"
	FormatYAML  Format = "yaml"
	FormatOff   Format = "off"
)

// ParseFormat normalizes and validates an --output value.
// Empty string defaults to json.
func ParseFormat(s string) (Format, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return FormatJSON, nil
	}
	switch Format(s) {
	case FormatJSON, FormatTable, FormatText, FormatYAML, FormatOff:
		return Format(s), nil
	default:
		return "", fmt.Errorf("unsupported output format %q, supported: %s", s, supportedFormatsMessage())
	}
}

func supportedFormatsMessage() string {
	return "json, table, text, yaml, off"
}

type checkedWriter struct {
	io.Writer
	err error
}

func (w *checkedWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if n < len(p) && err == nil {
		err = io.ErrShortWrite
	}
	if err != nil && w.err == nil {
		w.err = err
	}
	return n, err
}

// Write formats data to w according to format.
func Write(w io.Writer, format Format, data interface{}) error {
	if w == nil {
		return fmt.Errorf("output writer is nil")
	}
	writer := &checkedWriter{Writer: w}
	var err error
	switch format {
	case FormatJSON:
		err = writeJSON(writer, data)
	case FormatTable:
		err = writeTable(writer, data)
	case FormatText:
		err = writeText(writer, data)
	case FormatYAML:
		err = writeYAML(writer, data)
	case FormatOff:
		return nil
	default:
		return fmt.Errorf("unsupported output format %q, supported: %s", format, supportedFormatsMessage())
	}
	if writer.err != nil {
		return writer.err
	}
	return err
}
