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
	FormatJSON     Format = "json"
	FormatTable    Format = "table"
	FormatTableNum Format = "table-num"
	FormatText     Format = "text"
	FormatYAML     Format = "yaml"
	FormatOff      Format = "off"
)

// ParseFormat normalizes and validates an --output value.
// Empty string defaults to json.
func ParseFormat(s string) (Format, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return FormatJSON, nil
	}
	switch Format(s) {
	case FormatJSON, FormatTable, FormatTableNum, FormatText, FormatYAML, FormatOff:
		return Format(s), nil
	default:
		return "", fmt.Errorf("unsupported output format %q, supported: %s", s, supportedFormatsMessage())
	}
}

func supportedFormatsMessage() string {
	return "json, table, table-num, text, yaml, off"
}

// Options carries rendering hints that do not change the data itself.
type Options struct {
	// Columns is the preferred column order for table/text. It is applied only
	// when it exactly matches the keys present in the data; otherwise renderers
	// keep alphabetical order. See column_order.go.
	Columns []string

	// TerminalWidth caps table width. 0 means "detect from stdout"; a negative
	// value disables fitting entirely (useful for tests and for piped output
	// that should keep full-width columns).
	TerminalWidth int

	// Color enables ANSI styling for table headers and cells. Styling never
	// affects column widths; see style.go.
	Color bool
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

// Unwrap exposes the wrapped writer so terminal-width detection can still reach
// the underlying *os.File. Without this, wrapping stdout here would make every
// width probe fail and silently disable column fitting.
func (w *checkedWriter) Unwrap() io.Writer { return w.Writer }

// Write formats data to w according to format, using default options.
func Write(w io.Writer, format Format, data interface{}) error {
	return WriteWithOptions(w, format, data, Options{})
}

// WriteWithOptions formats data to w according to format and opts.
func WriteWithOptions(w io.Writer, format Format, data interface{}, opts Options) error {
	if w == nil {
		return fmt.Errorf("output writer is nil")
	}
	writer := &checkedWriter{Writer: w}
	var err error
	switch format {
	case FormatJSON:
		err = writeJSON(writer, data)
	case FormatTable:
		err = writeTable(writer, data, opts, false)
	case FormatTableNum:
		err = writeTable(writer, data, opts, true)
	case FormatText:
		err = writeText(writer, data, opts)
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
