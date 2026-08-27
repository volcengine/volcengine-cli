// Package output formats API responses for the CLI.
package output

import (
	"bufio"
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

// bufferedWriter batches the renderers' line-at-a-time writes.
//
// text and table emit one write per rendered line straight at an unbuffered
// os.Stdout, so a 20k-record response costs tens of thousands of syscalls. It
// also keeps the Unwrap chain intact, which bufio.Writer alone would break and
// silently disable terminal-width fitting.
type bufferedWriter struct {
	*bufio.Writer
	under io.Writer
}

func (w *bufferedWriter) Unwrap() io.Writer { return w.under }

// renderWriters builds the writer chain every renderer sees. Tests use it too,
// so a change to the chain cannot quietly cut the width-detection path.
func renderWriters(w io.Writer) (*bufferedWriter, *checkedWriter) {
	checked := &checkedWriter{Writer: w}
	return &bufferedWriter{Writer: bufio.NewWriter(checked), under: checked}, checked
}

// Write formats data to w according to format, using default options.
func Write(w io.Writer, format Format, data interface{}) error {
	return WriteWithOptions(w, format, data, Options{})
}

// WriteWithOptions formats data to w according to format and opts.
func WriteWithOptions(w io.Writer, format Format, data interface{}, opts Options) error {
	if w == nil {
		return fmt.Errorf("output writer is nil")
	}
	buffered, checked := renderWriters(w)
	var err error
	switch format {
	case FormatJSON:
		err = writeJSON(buffered, data)
	case FormatTable:
		err = writeTable(buffered, data, opts, false)
	case FormatTableNum:
		err = writeTable(buffered, data, opts, true)
	case FormatText:
		err = writeText(buffered, data, opts)
	case FormatYAML:
		err = writeYAML(buffered, data)
	case FormatOff:
		return nil
	default:
		return fmt.Errorf("unsupported output format %q, supported: %s", format, supportedFormatsMessage())
	}
	// Flush even after a rendering error: the lines already produced belong on
	// stdout, and the flush is what surfaces a write failure the buffer hid.
	if flushErr := buffered.Flush(); err == nil {
		err = flushErr
	}
	if checked.err != nil {
		return checked.err
	}
	return err
}
