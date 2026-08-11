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
// Colored JSON keeps the historical util.ShowJson(stdout) path when enableColor
// is on and the destination is the process stdout (not an injected test writer).
func renderAPIOutput(c *Context, cfg *Configure, data interface{}) error {
	plan, err := resolveAPIOutputPlan(c)
	if err != nil {
		return err
	}
	return plan.render(cfg, data)
}

func (p apiOutputPlan) render(cfg *Configure, data interface{}) error {
	result := data
	if p.query != nil {
		var err error
		result, err = p.query.Search(data)
		if err != nil {
			return fmt.Errorf("--query evaluation failed: %v", err)
		}
	}

	w := apiOutputWriter
	if w == nil {
		w = os.Stdout
	}

	if p.format == output.FormatJSON && cfg != nil && cfg.EnableColor && w == os.Stdout {
		util.ShowJson(result, true)
		return nil
	}
	return output.Write(w, p.format, result)
}
