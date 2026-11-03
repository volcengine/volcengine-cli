package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/volcengine/volcengine-cli/internal/pyruntime"
	"github.com/volcengine/volcengine-cli/internal/scriptrun"
)

// scriptRunEnabledEnvironment must be true for ve script run. Requiring an
// environment opt-in rather than a flag keeps an agent that is only allowed
// to run "ve ..." commands from turning that permission into arbitrary local
// code execution.
const scriptRunEnabledEnvironment = "VOLCENGINE_SCRIPT_RUN_ENABLED"

func init() {
	rootCmd.AddCommand(newScriptCommand())
}

func newScriptCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "script",
		Short: tr("Run multi-API Python scripts and manage their Python runtime"),
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newScriptRunCommand(), newScriptRuntimeCommand())
	return command
}

type scriptRunCommandOptions struct {
	File               string
	Args               string
	ArgsFile           string
	Profile            string
	Region             string
	Python             string
	MaxCalls           int
	MaxConcurrency     int
	TimeoutSeconds     int
	CallTimeoutSeconds int
	Yes                bool
}

func newScriptRunCommand() *cobra.Command {
	options := scriptRunCommandOptions{}
	defaults := scriptrun.DefaultLimits()
	command := &cobra.Command{
		Use:   "run",
		Short: tr("Run a trusted local Python script that orchestrates API calls"),
		Long: tr(`Run a Python script with the same contract as the MCP run_script tool: await call_api(service, api, parameters=None, region=None), the args global and the result global.
The script runs as trusted local code with your OS privileges; it is not a sandbox. Set VOLCENGINE_SCRIPT_RUN_ENABLED=true to enable this command.
The JSON report, including the call journal, is written to stdout.`),
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			enabled, err := environmentBool(scriptRunEnabledEnvironment, false)
			if err != nil {
				return err
			}
			if !enabled {
				return errors.New(tr("ve script run executes trusted local Python code with your OS privileges and is not a sandbox; set VOLCENGINE_SCRIPT_RUN_ENABLED=true to enable it"))
			}
			code, err := readScriptSource(options.File, scriptrun.DefaultLimits().MaxCodeBytes)
			if err != nil {
				return err
			}
			args := json.RawMessage("null")
			if options.Args != "" && options.ArgsFile != "" {
				return errors.New(tr("--args and --args-file cannot be used together"))
			}
			if options.Args != "" {
				if !json.Valid([]byte(options.Args)) {
					return errors.New(tr("--args must be valid JSON"))
				}
				args = json.RawMessage(options.Args)
			}
			// --args-file avoids the per-argument size limit of the OS, as
			// for args passed by the hosted sandbox launcher.
			if options.ArgsFile != "" {
				data, err := os.ReadFile(options.ArgsFile)
				if err != nil {
					return err
				}
				if !json.Valid(data) {
					return errors.New(tr("--args-file must contain valid JSON"))
				}
				args = json.RawMessage(data)
			}
			limits, err := scriptLimitsFromArguments(map[string]interface{}{
				"max_calls": options.MaxCalls, "max_concurrency": options.MaxConcurrency,
				"timeout_seconds": options.TimeoutSeconds, "call_timeout_seconds": options.CallTimeoutSeconds,
			})
			if err != nil {
				return err
			}
			invoker, err := newReloadingVolcengineMCPInvoker(runtimeConfig(), options.Profile, options.Region)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			interpreter, err := resolveScriptPython(ctx, options.Python)
			if err != nil {
				return err
			}
			runner := &localScriptRunner{invoker: invoker, interpreter: interpreter}
			report, err := runner.RunScript(ctx, scriptrun.Request{Code: code, Args: args, Limits: limits, Approved: options.Yes})
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(command.OutOrStdout())
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(report); err != nil {
				return err
			}
			if report.Status != scriptrun.RunStatusSucceeded {
				return fmt.Errorf(tr("script run ended with status %s"), report.Status)
			}
			return nil
		},
	}
	command.Flags().StringVar(&options.File, "file", "", tr("Python script file, or - to read standard input"))
	command.Flags().StringVar(&options.Args, "args", "", tr("JSON value exposed to the script as args"))
	command.Flags().StringVar(&options.ArgsFile, "args-file", "", tr("File holding the JSON value exposed to the script as args"))
	command.Flags().StringVar(&options.Profile, "profile", "", tr("Use a configured profile"))
	command.Flags().StringVar(&options.Region, "region", "", tr("Set the fallback region"))
	command.Flags().StringVar(&options.Python, "python", "", tr("Absolute path of the CPython 3.9+ interpreter for run_script"))
	command.Flags().IntVar(&options.MaxCalls, "max-calls", defaults.MaxCalls, tr("Maximum API calls for the run"))
	command.Flags().IntVar(&options.MaxConcurrency, "max-concurrency", defaults.MaxConcurrency, tr("Maximum API calls in flight at once"))
	command.Flags().IntVar(&options.TimeoutSeconds, "timeout-seconds", int(defaults.Timeout.Seconds()), tr("Wall-clock limit for the whole run"))
	command.Flags().IntVar(&options.CallTimeoutSeconds, "call-timeout-seconds", int(defaults.CallTimeout.Seconds()), tr("Limit for each API call"))
	command.Flags().BoolVar(&options.Yes, "yes", false, tr("Approve the whole script: calls that safety policy confirm rules protect are sent (deny rules still apply)"))
	_ = command.MarkFlagRequired("file")
	return command
}

func readScriptSource(file string, limit int) (string, error) {
	var reader io.Reader
	if file == "-" {
		reader = os.Stdin
	} else {
		handle, err := os.Open(file)
		if err != nil {
			return "", err
		}
		defer handle.Close()
		reader = handle
	}
	data, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return "", err
	}
	if len(data) > limit {
		return "", fmt.Errorf(tr("the script exceeds %d bytes"), limit)
	}
	return string(data), nil
}

func newScriptRuntimeCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "runtime",
		Short: tr("Install and inspect the Python runtime used by run_script"),
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newScriptRuntimeInstallCommand(), newScriptRuntimeStatusCommand())
	return command
}

func newScriptRuntimeInstallCommand() *cobra.Command {
	var manifestSource, manifestSHA256 string
	command := &cobra.Command{
		Use:   "install",
		Short: tr("Download, verify and activate the pinned CPython runtime for this platform"),
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			configDir, err := configFileDirFunc()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			installer := pyruntime.NewInstaller(pyruntime.Root(configDir))
			manifest, err := loadScriptRuntimeManifest(ctx, installer, manifestSource, manifestSHA256)
			if err != nil {
				return err
			}
			artifact, err := manifest.Select(pyruntime.CurrentPlatform())
			if err != nil {
				return err
			}
			fmt.Fprintf(command.ErrOrStderr(), tr("Installing CPython %s for %s (%d bytes, SHA-256 %s) from %s\n"), artifact.Version, artifact.Platform, artifact.Size, artifact.SHA256, artifact.URL)
			result, err := installer.Install(ctx, artifact)
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(command.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		},
	}
	command.Flags().StringVar(&manifestSource, "manifest", "", tr("Runtime manifest HTTPS URL or local file (default: the manifest built into ve)"))
	command.Flags().StringVar(&manifestSHA256, "manifest-sha256", "", tr("Expected SHA-256 of the runtime manifest"))
	return command
}

func loadScriptRuntimeManifest(ctx context.Context, installer *pyruntime.Installer, source, digest string) (*pyruntime.Manifest, error) {
	if source == "" {
		if digest != "" {
			return nil, errors.New(tr("--manifest-sha256 requires --manifest"))
		}
		return pyruntime.DefaultManifest()
	}
	return installer.LoadManifest(ctx, source, digest)
}

func newScriptRuntimeStatusCommand() *cobra.Command {
	var python string
	command := &cobra.Command{
		Use:   "status",
		Short: tr("Show the Python runtime that run_script would use"),
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			status := map[string]interface{}{"platform": pyruntime.CurrentPlatform()}
			if manifest, err := pyruntime.DefaultManifest(); err == nil {
				if artifact, err := manifest.Select(pyruntime.CurrentPlatform()); err == nil {
					status["managed_available"] = artifact.Version
				} else {
					status["managed_available"] = false
				}
			}
			interpreter, err := resolveScriptPython(command.Context(), python)
			if err != nil {
				status["ready"] = false
				status["error"] = err.Error()
			} else {
				status["ready"] = true
				status["runtime"] = interpreter
			}
			encoder := json.NewEncoder(command.OutOrStdout())
			encoder.SetIndent("", "  ")
			if encodeErr := encoder.Encode(status); encodeErr != nil {
				return encodeErr
			}
			return err
		},
	}
	command.Flags().StringVar(&python, "python", "", tr("Absolute path of the CPython 3.9+ interpreter for run_script"))
	return command
}
