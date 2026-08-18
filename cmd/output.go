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
		return apiOutputPlan{}, fmt.Errorf("invalid --query %q: %v", expr, err)
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
	return output.Write(w, p.format, result)
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
