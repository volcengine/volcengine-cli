package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/volcengine/volcengine-cli/util"
	"github.com/volcengine/volcengine-cli/util/output"
)

// apiOutputWriter is the destination for non-color API response rendering.
// Tests may replace it; production uses os.Stdout.
var apiOutputWriter io.Writer = os.Stdout

type apiOutputPlan struct {
	format output.Format
	query  *output.Query
}

func resolveOutputFormat(c *Context) (output.Format, error) {
	if c == nil || c.fixedFlags == nil {
		return output.FormatJSON, nil
	}
	f := c.fixedFlags.GetByName("output")
	if f == nil {
		return output.FormatJSON, nil
	}
	return output.ParseFormat(f.GetValue())
}

func resolveQuery(c *Context) string {
	if c == nil || c.fixedFlags == nil {
		return ""
	}
	f := c.fixedFlags.GetByName("query")
	if f == nil {
		return ""
	}
	return strings.TrimSpace(f.GetValue())
}

func resolveAPIOutputPlan(c *Context) (apiOutputPlan, error) {
	format, err := resolveOutputFormat(c)
	if err != nil {
		return apiOutputPlan{}, err
	}
	expr := resolveQuery(c)
	query, err := output.CompileQuery(expr)
	if err != nil {
		// output.QueryError already renders the expression, a caret at the
		// failure and a hint; wrapping it again would repeat the prefix.
		return apiOutputPlan{}, err
	}
	return apiOutputPlan{format: format, query: query}, nil
}

// renderAPIOutput applies optional JMESPath --query then formats with --output.
//
// Pipeline: data → [--query] → [--output] → writer.
// --output off skips query evaluation and writes nothing.
// Colored JSON uses the same writer and error handling as other formats.
func renderAPIOutput(c *Context, cfg *Configure, data interface{}) error {
	plan, err := resolveAPIOutputPlan(c)
	if err != nil {
		return err
	}
	return plan.render(cfg, data)
}

func (p apiOutputPlan) render(cfg *Configure, data interface{}) error {
	// off: request already ran; skip query evaluation and stdout.
	if p.format == output.FormatOff {
		return nil
	}

	result := data
	if p.query != nil {
		var err error
		result, err = p.query.Search(data)
		if err != nil {
			return fmt.Errorf("--query evaluation failed: %w", err)
		}
	}

	w := apiOutputWriter
	if w == nil {
		w = os.Stdout
	}

	if p.format == output.FormatJSON && shouldColorJSON(cfg, w) {
		content, err := output.EncodeJSON(result)
		if err != nil {
			return err
		}
		content = util.ColorizeJSON(content)
		n, writeErr := w.Write(content)
		if n < len(content) && writeErr == nil {
			return io.ErrShortWrite
		}
		return writeErr
	}
	return output.WriteWithOptions(w, p.format, result, output.Options{
		// Column order follows what the user wrote in --query's multiselect
		// hash; ignored unless it matches the rendered keys exactly.
		Columns: p.query.Columns(),
		// 0 lets the renderer probe the real terminal; width fitting is skipped
		// automatically when stdout is not a terminal.
		TerminalWidth: 0,
		// Reuse the same gate as colored JSON so `enableColor` / NO_COLOR /
		// piped output behave consistently across formats.
		Color: shouldColorTable(cfg, w),
	})
}

// shouldColorTable reports whether table styling should be emitted. Tables are
// colored only on a real terminal: piping into grep/awk must stay plain, unlike
// JSON where tests inject a buffer intentionally.
func shouldColorTable(cfg *Configure, w io.Writer) bool {
	if cfg == nil || !cfg.EnableColor {
		return false
	}
	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func shouldColorJSON(cfg *Configure, w io.Writer) bool {
	if cfg == nil || !cfg.EnableColor {
		return false
	}
	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" {
		return false
	}
	if w != os.Stdout {
		return true
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func renderSuccessfulAPIOutput(plan apiOutputPlan, cfg *Configure, data interface{}) error {
	if err := plan.render(cfg, data); err != nil {
		return fmt.Errorf("API call succeeded but response output failed: %w", err)
	}
	return nil
}
