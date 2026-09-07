package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/volcengine/volcengine-cli/internal/docs"
	clioutput "github.com/volcengine/volcengine-cli/util/output"
)

func init() {
	rootCmd.AddCommand(newDocsCommand(docs.New(docsUserAgent(os.LookupEnv))))
}

// docsUserAgent intentionally delegates to the exact formatter used by normal
// signed service calls, including skill-invoker metadata for agent callers.
func docsUserAgent(getenv envGetter) string {
	return clientUserAgent(getenv)
}

func newDocsCommand(module docs.Module) *cobra.Command {
	command := &cobra.Command{
		Use:   "docs",
		Short: tr("Search and fetch Volcengine documentation"),
	}
	command.SetUsageTemplate(docsUsageTemplate())

	// These system flags are intentionally accepted but ignored. Documentation
	// requests are public, unsigned, and independent of profiles and regions.
	command.PersistentFlags().String("profile", "", tr("Ignored for public documentation requests"))
	command.PersistentFlags().String("region", "", tr("Ignored for public documentation requests"))
	command.PersistentFlags().String("endpoint", "", tr("Ignored for public documentation requests"))
	command.PersistentFlags().String("lang", "", tr("Set the display language for this invocation (EN or ZH)."))
	for _, name := range []string{"profile", "region", "endpoint", "lang"} {
		_ = command.PersistentFlags().MarkHidden(name)
	}
	command.PersistentPreRunE = func(command *cobra.Command, _ []string) error {
		value, err := command.Flags().GetString("lang")
		if err != nil || strings.TrimSpace(value) == "" {
			return err
		}
		language, _ := normalizeLanguage(value)
		setCurrentLanguage(language)
		return nil
	}

	command.AddCommand(newDocsSearchCommand(module), newDocsFetchCommand(module))
	return command
}

func docsUsageTemplate() string {
	return tr("Usage:") + `
  {{.UseLine}}{{if .HasAvailableSubCommands}}

` + tr("Available Commands:") + `
{{range .Commands}}{{if .IsAvailableCommand}}  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

` + tr("Flags:") + `
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

` + tr(`Use "{{.CommandPath}} [command] --help" for more information about a command.`) + `{{end}}
`
}

func newDocsSearchCommand(module docs.Module) *cobra.Command {
	var (
		limit         int
		serviceCodes  []string
		snippetLength int
		outputFormat  string
		query         string
	)
	command := &cobra.Command{
		Use:   "search <text>",
		Short: tr("Search Volcengine documentation"),
		Args:  docsSingleArgument("search text"),
		RunE: func(command *cobra.Command, args []string) error {
			if module == nil {
				return fmt.Errorf("documentation module is not configured")
			}
			plan, err := newDocsOutputPlan(command.OutOrStdout(), outputFormat, query, docsSearchFormats)
			if err != nil {
				return err
			}
			result, err := module.Search(command.Context(), docs.SearchOptions{
				Query:         args[0],
				Limit:         limit,
				ServiceCodes:  serviceCodes,
				SnippetLength: snippetLength,
			})
			if err != nil {
				return err
			}
			return plan.render(result, searchTextOutput(result))
		},
	}
	command.Flags().IntVar(&limit, "limit", docs.DefaultSearchLimit, tr("Maximum number of documents to return (positive integer)"))
	command.Flags().StringArrayVar(&serviceCodes, "service-code", nil, tr("Filter by service code; repeat for multiple codes"))
	command.Flags().IntVar(&snippetLength, "snippet-length", docs.DefaultSnippetLength, tr("Maximum Unicode characters per snippet (positive integer)"))
	command.Flags().StringVar(&outputFormat, "output", "json", tr("Output format: json, text, yaml, or table"))
	command.Flags().StringVar(&query, "query", "", tr("JMESPath expression applied before formatting"))
	_ = command.Flags().MarkHidden("query")
	return command
}

func newDocsFetchCommand(module docs.Module) *cobra.Command {
	var (
		startIndex   int
		maxLength    int
		outputFormat string
		query        string
	)
	command := &cobra.Command{
		Use:   "fetch <url>",
		Short: tr("Fetch a Volcengine documentation article"),
		Args:  docsSingleArgument("document URL"),
		RunE: func(command *cobra.Command, args []string) error {
			if module == nil {
				return fmt.Errorf("documentation module is not configured")
			}
			plan, err := newDocsOutputPlan(command.OutOrStdout(), outputFormat, query, docsFetchFormats)
			if err != nil {
				return err
			}
			result, err := module.Fetch(command.Context(), docs.FetchOptions{
				URL:        args[0],
				StartIndex: startIndex,
				MaxLength:  maxLength,
			})
			if err != nil {
				return err
			}
			return plan.render(result, fetchTextOutput(result))
		},
	}
	command.Flags().IntVar(&startIndex, "start-index", 0, tr("Zero-based Unicode character index at which to start"))
	command.Flags().IntVar(&maxLength, "max-length", docs.DefaultFetchLength, tr("Maximum Unicode characters to return (positive integer)"))
	command.Flags().StringVar(&outputFormat, "output", "json", tr("Output format: json, text, or yaml"))
	command.Flags().StringVar(&query, "query", "", tr("JMESPath expression applied before formatting"))
	_ = command.Flags().MarkHidden("query")
	return command
}

func docsSingleArgument(name string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("%s is required", name)
		}
		if len(args) > 1 {
			return fmt.Errorf("expected one %s, received %d arguments", name, len(args))
		}
		return nil
	}
}

var (
	docsSearchFormats = map[clioutput.Format]struct{}{
		clioutput.FormatJSON: {}, clioutput.FormatText: {}, clioutput.FormatYAML: {}, clioutput.FormatTable: {},
	}
	docsFetchFormats = map[clioutput.Format]struct{}{
		clioutput.FormatJSON: {}, clioutput.FormatText: {}, clioutput.FormatYAML: {},
	}
)

type docsOutputPlan struct {
	writer io.Writer
	format clioutput.Format
	query  *clioutput.Query
}

func newDocsOutputPlan(writer io.Writer, rawFormat, queryExpression string, allowed map[clioutput.Format]struct{}) (docsOutputPlan, error) {
	format, err := clioutput.ParseFormat(rawFormat)
	if err != nil {
		return docsOutputPlan{}, err
	}
	if _, ok := allowed[format]; !ok {
		return docsOutputPlan{}, fmt.Errorf("unsupported output format %q for this command", format)
	}
	query, err := clioutput.CompileQuery(queryExpression)
	if err != nil {
		return docsOutputPlan{}, err
	}
	return docsOutputPlan{writer: writer, format: format, query: query}, nil
}

func (p docsOutputPlan) render(result interface{}, textResult string) error {
	if p.format == clioutput.FormatText && p.query == nil {
		_, err := io.WriteString(p.writer, textResult)
		return err
	}

	data, err := docsStructuredData(result)
	if err != nil {
		return err
	}
	if p.query != nil {
		data, err = p.query.Search(data)
		if err != nil {
			return err
		}
	}
	return clioutput.WriteWithOptions(p.writer, p.format, data, clioutput.Options{Columns: p.query.Columns()})
}

func docsStructuredData(value interface{}) (interface{}, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode documentation output: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.UseNumber()
	var data interface{}
	if err := decoder.Decode(&data); err != nil {
		return nil, fmt.Errorf("prepare documentation output: %w", err)
	}
	return data, nil
}

func searchTextOutput(result docs.SearchResult) string {
	if len(result.Documents) == 0 {
		return tr("No documents found.") + "\n"
	}
	var output strings.Builder
	for index, document := range result.Documents {
		if index > 0 {
			output.WriteString("\n---\n\n")
		}
		title := document.Title
		if strings.TrimSpace(title) == "" {
			title = tr("Untitled")
		}
		fmt.Fprintf(&output, "## %d. %s\n%s\nServiceCodes: %s\n\n%s\n", index+1, title, document.URL, strings.Join(document.ServiceCodes, ", "), document.Snippet)
	}
	return output.String()
}

func fetchTextOutput(result docs.FetchResult) string {
	var output strings.Builder
	if strings.TrimSpace(result.Title) != "" {
		fmt.Fprintf(&output, "# %s\n", result.Title)
	}
	output.WriteString(result.URL)
	output.WriteString("\n\n")
	if !result.HasContent {
		output.WriteString(tr("This URL has no specific article content."))
		output.WriteByte('\n')
		return output.String()
	}
	output.WriteString(result.Content)
	if result.HasMore && result.NextStartIndex != nil {
		fmt.Fprintf(&output, tr("\n\n[Continue] %d characters remain. Run fetch again with --start-index %d."), result.RemainingCharacters, *result.NextStartIndex)
	}
	output.WriteByte('\n')
	return output.String()
}
