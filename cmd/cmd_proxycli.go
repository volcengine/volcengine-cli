package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/volcengine/volcengine-cli/internal/proxycli"
)

type proxyCLIManager interface {
	Resolve(proxycli.Tool) (proxycli.Resolution, error)
	Upgrade([]proxycli.Tool) (proxycli.UpgradeResult, error)
}

type proxyCLIUpdateChecker interface {
	PrepareUpdateCheck(proxycli.Resolution) proxycli.UpdateCheckPlan
}

var (
	newProxyCLIManager = func() (proxyCLIManager, error) {
		return proxycli.NewManager()
	}
	ensureProxyLoginToken  = EnsureValidLoginToken
	launchProxyUpdateCheck = defaultLaunchProxyUpdateCheck
	runProxyExecutable     = defaultRunProxyExecutable
)

func init() {
	tosutilCommand := newProxyForwardCommand("tosutil", proxycli.ToolTOSUtil)
	tosutilCommand.Aliases = []string{"toscli"}
	rootCmd.AddCommand(
		newProxyForwardCommand("arkcli", proxycli.ToolArkCLI),
		tosutilCommand,
		newProxyCLIManagementCommand(),
		newProxyCLIUpdateHelperCommand(),
	)
}

func isProxyCLIForwardCommand(name string) bool {
	return name == "arkcli" || name == "tosutil" || name == "toscli"
}

func newProxyForwardCommand(alias string, tool proxycli.Tool) *cobra.Command {
	command := &cobra.Command{
		Use:                alias + " [arguments...]",
		Short:              fmt.Sprintf("Run %s through the Volcengine CLI", executableLabel(tool)),
		DisableFlagParsing: true,
		RunE: func(command *cobra.Command, args []string) error {
			manager, err := newProxyCLIManager()
			if err != nil {
				return err
			}
			return runProxyCLI(command, ctx, manager, tool, args)
		},
	}
	command.SetUsageTemplate(`Usage:
  {{.CommandPath}} [arguments...]

All arguments after {{.Name}} are forwarded unchanged to the delegated CLI.
`)
	return command
}

func executableLabel(tool proxycli.Tool) string {
	if tool == proxycli.ToolTOSUtil {
		return "tosutil"
	}
	return "arkcli"
}

func runProxyCLI(command *cobra.Command, cliContext *Context, manager proxyCLIManager, tool proxycli.Tool, args []string) error {
	if cliContext != nil && cliContext.fixedFlags != nil {
		for _, name := range []string{"profile", "region", "endpoint"} {
			if flag := cliContext.fixedFlags.GetByName(name); flag != nil && strings.TrimSpace(flag.GetValue()) != "" {
				alias := executableLabel(tool)
				return fmt.Errorf("ve --%s is not supported for delegated CLIs; pass the argument after the command so the delegated CLI parses it: ve %s --%s <value>", name, alias, name)
			}
		}
	}
	if !isProxyLocalInvocation(args) {
		if err := refreshCurrentProxyLogin(cliContext); err != nil {
			fmt.Fprintf(command.ErrOrStderr(), "Warning: failed to refresh the current Volcengine CLI session; continuing with %s: %v\n", executableLabel(tool), err)
		}
	}
	resolution, err := manager.Resolve(tool)
	if err != nil {
		return err
	}
	if resolution.Source == proxycli.SourceCDN || resolution.Source == proxycli.SourceEmbedded {
		fmt.Fprintf(command.ErrOrStderr(), "Installed %s %s from %s at %s\n", executableLabel(tool), resolution.Version, resolution.Source, resolution.Path)
	}
	var updatePlan proxycli.UpdateCheckPlan
	if checker, ok := manager.(proxyCLIUpdateChecker); ok {
		updatePlan = checker.PrepareUpdateCheck(resolution)
	}
	if updatePlan.HasNotice {
		printProxyUpdateNotice(command, updatePlan.Notice)
	}
	if updatePlan.Refresh {
		_ = launchProxyUpdateCheck(resolution.Tool, resolution.Version)
	}
	return runProxyExecutable(
		resolution.Path,
		append([]string(nil), args...),
		command.InOrStdin(),
		command.OutOrStdout(),
		command.ErrOrStderr(),
		proxyCLIEnvironment(tool, os.Environ()),
	)
}

func proxyCLIEnvironment(tool proxycli.Tool, env []string) []string {
	if tool != proxycli.ToolArkCLI {
		return env
	}

	values := map[string]string{"SOURCE_FROM": "ve"}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(key)
		}
		switch key {
		case "SOURCE_FROM":
			values[key] = value
		case "VE_CALLER_TYPE", "VE_CALLER_NAME", "VE_SKILL_NAME":
			values["ARKCLI_"+strings.TrimPrefix(key, "VE_")] = value
		}
	}

	// Replace existing target entries instead of appending duplicate keys:
	// Unix exec passes the environment directly to the child process.
	childEnv := make([]string, 0, len(env)+len(values))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(key)
		}
		if _, replaced := values[key]; !replaced {
			childEnv = append(childEnv, entry)
		}
	}
	for _, key := range []string{"SOURCE_FROM", "ARKCLI_CALLER_TYPE", "ARKCLI_CALLER_NAME", "ARKCLI_SKILL_NAME"} {
		if value, exists := values[key]; exists {
			childEnv = append(childEnv, key+"="+value)
		}
	}
	return childEnv
}

func printProxyUpdateNotice(command *cobra.Command, result proxycli.UpdateCheckResult) {
	if result.HasUpdate {
		fmt.Fprintf(command.ErrOrStderr(), "A newer %s version is available: %s (current %s). Run: ve proxycli upgrade %s\n", executableLabel(result.Tool), result.Latest, result.Current, executableLabel(result.Tool))
	}
}

func isProxyLocalInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "help", "version", "-h", "--help", "-v", "--version":
		return true
	}
	last := args[len(args)-1]
	if last == "-h" || last == "--help" {
		return true
	}
	return false
}

func refreshCurrentProxyLogin(cliContext *Context) error {
	profileName, _, profile, err := selectInvocationProfile(cliContext)
	if err != nil {
		return err
	}
	if profile == nil || !strings.EqualFold(strings.TrimSpace(profile.Mode), ModeConsoleLogin) {
		return nil
	}
	if strings.TrimSpace(profile.LoginSession) == "" {
		return nil
	}
	_, err = ensureProxyLoginToken(cliContext.config, profileName)
	return err
}

type proxyProcessExitError struct {
	code int
}

func (e *proxyProcessExitError) Error() string {
	return fmt.Sprintf("delegated CLI exited with status %d", e.code)
}

func (e *proxyProcessExitError) ExitCode() int {
	if e == nil || e.code <= 0 {
		return 1
	}
	return e.code
}

func newProxyCLIManagementCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "proxycli [command]",
		Short: "Manage delegated proxy CLIs",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return command.Help()
		},
	}
	command.SetUsageTemplate(`Usage:
  {{.UseLine}}
{{if .HasAvailableSubCommands}}
Available Commands:
{{range .Commands}}{{if .IsAvailableCommand}}  {{rpad .Name .NamePadding }} {{.Short}}
{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}
Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}
`)
	command.AddCommand(newProxyCLIUpgradeCommand())
	return command
}

func newProxyCLIUpgradeCommand() *cobra.Command {
	var yes bool
	command := &cobra.Command{
		Use:   "upgrade [arkcli|tosutil]",
		Short: "Install or upgrade ve-managed proxy CLIs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			tools := proxycli.KnownTools()
			if len(args) == 1 {
				tool, err := proxycli.ParseTool(args[0])
				if err != nil {
					return err
				}
				tools = []proxycli.Tool{tool}
			}
			if !yes {
				confirmed, err := confirmProxyCLIUpgrade(command, tools)
				if err != nil {
					return err
				}
				if !confirmed {
					fmt.Fprintln(command.OutOrStdout(), "Proxy CLI upgrade canceled.")
					return nil
				}
			}
			manager, err := newProxyCLIManager()
			if err != nil {
				return err
			}
			result, upgradeErr := manager.Upgrade(tools)
			printProxyCLIUpgradeResult(command, result)
			return upgradeErr
		},
	}
	command.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return command
}

func confirmProxyCLIUpgrade(command *cobra.Command, tools []proxycli.Tool) (bool, error) {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, executableLabel(tool))
	}
	fmt.Fprintf(command.OutOrStdout(), "Install or upgrade managed proxy CLI(s) %s? [y/N]: ", strings.Join(names, ", "))
	line, err := bufio.NewReader(command.InOrStdin()).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func printProxyCLIUpgradeResult(command *cobra.Command, result proxycli.UpgradeResult) {
	for _, item := range result.Items {
		switch item.Status {
		case proxycli.StatusExternal:
			fmt.Fprintf(command.OutOrStdout(), "%s: externally managed on PATH; skipped (%s)\n", executableLabel(item.Tool), item.Path)
		case proxycli.StatusCurrent:
			fmt.Fprintf(command.OutOrStdout(), "%s: already current at %s (%s)\n", executableLabel(item.Tool), item.Version, item.Path)
		default:
			fmt.Fprintf(command.OutOrStdout(), "%s: %s version %s from %s (%s)\n", executableLabel(item.Tool), item.Status, item.Version, item.Source, item.Path)
		}
	}
}
