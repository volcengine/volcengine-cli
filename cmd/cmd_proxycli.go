package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/volcengine/volcengine-cli/internal/proxycli"
)

type proxyCLIManager interface {
	Resolve(proxycli.Tool) (proxycli.Resolution, error)
	Upgrade([]proxycli.Tool) (proxycli.UpgradeResult, error)
}

var (
	newProxyCLIManager = func() (proxyCLIManager, error) {
		return proxycli.NewManager()
	}
	ensureProxyLoginToken = EnsureValidLoginToken
	runProxyExecutable    = func(path string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
		return defaultRunProxyExecutable(path, args, stdin, stdout, stderr, os.Environ())
	}
)

func init() {
	rootCmd.AddCommand(
		newProxyForwardCommand("arkcli", proxycli.ToolArkCLI),
		newProxyForwardCommand("toscli", proxycli.ToolTOSUtil),
		newProxyCLIManagementCommand(),
	)
}

func isProxyCLIForwardCommand(name string) bool {
	return name == "arkcli" || name == "toscli"
}

func proxyAlias(tool proxycli.Tool) string {
	if tool == proxycli.ToolTOSUtil {
		return "toscli"
	}
	return string(tool)
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
		if flag := cliContext.fixedFlags.GetByName("profile"); flag != nil && strings.TrimSpace(flag.GetValue()) != "" {
			alias := proxyAlias(tool)
			return fmt.Errorf("ve --profile is not supported for delegated CLIs; pass the argument after the command so the delegated CLI parses it: ve %s --profile <name>", alias)
		}
	}
	if err := refreshCurrentProxyLogin(cliContext); err != nil {
		return err
	}
	resolution, err := manager.Resolve(tool)
	if err != nil {
		return err
	}
	if resolution.Source == proxycli.SourceCDN || resolution.Source == proxycli.SourceEmbedded {
		fmt.Fprintf(command.ErrOrStderr(), "Installed %s %s from %s at %s\n", executableLabel(tool), resolution.Version, resolution.Source, resolution.Path)
	}
	return runProxyExecutable(
		resolution.Path,
		append([]string(nil), args...),
		command.InOrStdin(),
		command.OutOrStdout(),
		command.ErrOrStderr(),
	)
}

func refreshCurrentProxyLogin(cliContext *Context) error {
	profileName, _, profile, err := selectInvocationProfile(cliContext)
	if err != nil {
		return err
	}
	if profile == nil || !strings.EqualFold(strings.TrimSpace(profile.Mode), ModeConsoleLogin) {
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

func defaultRunProxyExecutable(path string, args []string, stdin io.Reader, stdout, stderr io.Writer, env []string) error {
	process := exec.Command(path, args...)
	process.Stdin = stdin
	process.Stdout = stdout
	process.Stderr = stderr
	process.Env = env
	if err := process.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code := delegatedExitCode(exitErr)
			if code < 1 {
				code = 1
			}
			return &proxyProcessExitError{code: code}
		}
		return fmt.Errorf("run delegated CLI %s: %w", path, err)
	}
	return nil
}

func newProxyCLIManagementCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "proxycli",
		Short: "Manage delegated proxy CLIs",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return command.Help()
		},
	}
	command.AddCommand(newProxyCLIUpgradeCommand())
	return command
}

func newProxyCLIUpgradeCommand() *cobra.Command {
	var yes bool
	command := &cobra.Command{
		Use:   "upgrade [arkcli|toscli]",
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
		names = append(names, proxyAlias(tool))
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
			fmt.Fprintf(command.OutOrStdout(), "%s: externally managed on PATH; skipped (%s)\n", proxyAlias(item.Tool), item.Path)
		case proxycli.StatusCurrent:
			fmt.Fprintf(command.OutOrStdout(), "%s: already current at %s (%s)\n", proxyAlias(item.Tool), item.Version, item.Path)
		default:
			fmt.Fprintf(command.OutOrStdout(), "%s: %s version %s from %s (%s)\n", proxyAlias(item.Tool), item.Status, item.Version, item.Source, item.Path)
		}
	}
}
