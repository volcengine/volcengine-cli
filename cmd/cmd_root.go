package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/volcengine/volcengine-cli/upgrade"
)

// errVersionFlagShown is returned after printing CLI version for -v/--version.
// It stops cobra from continuing into Run/Usage without os.Exit, so upgrade
// notices in runMain's defer still run.
var errVersionFlagShown = errors.New("version flag shown")

var rootCmd = &cobra.Command{
	Use: "ve",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Usage()
		return nil
	},
	ValidArgs:     rootSupport.GetAllSvcCompatible(),
	SilenceErrors: true,
	SilenceUsage:  true,
}

func initRootCmd() {
	// 关闭 cobra 默认 help 子命令：root 列表按「Service」展示，默认 help 会混进服务名。
	// 常规帮助入口仍是 -h/--help（及各 service/action 上的 --help）。
	rootCmd.SetHelpCommand(&cobra.Command{
		Hidden: true,
	})

	rootCmd.Flags().BoolP("help", "h", false, "")

	rootCmd.Flags().BoolP("version", "v", false, tr("Show CLI version"))
	registerRootSystemFlags()

	rootCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		showVersion, _ := cmd.Flags().GetBool("version")
		if showVersion {
			fmt.Fprintln(cmd.OutOrStdout(), clientVersion)
			return errVersionFlagShown
		}
		return nil
	}

	// todo enable color?
	rootCmd.SetUsageTemplate(rootUsageTemplate())

	// 显式注册 help 子命令（覆盖上面关掉的默认实现）：
	// runMain 会先走 tryExecuteGenericInvoke，只把 rootCmd 已注册子命令交给 cobra；
	// 若不注册 help，`ve help` 会被当成未知 service 误进 ---force 路径。
	// 同时提供 `ve help` / `ve help <cmd>`，输出走自定义 Usage 模板。
	rootCmd.AddCommand(&cobra.Command{
		Use:   "help [command]",
		Short: "Help about any command",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return rootCmd.Usage()
			}
			target, _, err := rootCmd.Find(args)
			if err != nil {
				_ = rootCmd.Usage()
				return err
			}
			return target.Usage()
		},
	}, &cobra.Command{
		Use:   "version",
		Short: tr("Show CLI version"),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), clientVersion)
		},
	}, newColorCommand("enable-color", true), newColorCommand("disable-color", false))
}

func newColorCommand(use string, enabled bool) *cobra.Command {
	return &cobra.Command{
		Use:    use,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configForWrite()
			if err != nil {
				return err
			}
			cfg.EnableColor = enabled
			if err := WriteConfigToFile(cfg); err != nil {
				return err
			}
			setRuntimeConfig(cfg)
			return nil
		},
	}
}

func Execute() {
	exitCode := runMain()
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

// runMain executes the CLI and returns a process exit code.
// Completed background version checks may print to stderr without delaying command exit.
// Language-parse failures return 1 (not os.Exit) so the upgrade-notice defer still runs.
func runMain() int {
	// Prefer language-stripped args for the upgrade skip decision so
	// `ve ---lang ZH upgrade` is recognized as the upgrade command. Fall back
	// to os.Args when ---lang itself is malformed (args may be nil).
	checkArgs := os.Args[1:]
	if processLanguageResolution.err == nil && processLanguageResolution.args != nil {
		checkArgs = processLanguageResolution.args
	}
	// Start the non-blocking check before early error returns so a ready cache
	// notice can still print via defer when ---lang is malformed.
	asyncCheck := upgrade.StartBackgroundCheck(clientVersion, checkArgs)
	defer upgrade.MaybePrintUpgradeNotice(os.Stderr, clientVersion, asyncCheck)

	if processLanguageResolution.err != nil {
		fmt.Fprintln(os.Stderr, processLanguageResolution.err)
		return 1
	}
	setCurrentLanguage(processLanguageResolution.language)
	if err := applyResolvedSystemFlags(ctx, processLanguageResolution.fixedFlags); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rootCmd.SetArgs(processLanguageResolution.args)
	initRootCmd()
	localizeHelpFlags(rootCmd)

	// cobra 只为 metadata 中的 service 注册了子命令；未知 service 需在此前置拦截并走 ---force 路径。
	// 必须使用 processLanguageResolution.args（已剥离 ---lang），与 rootCmd.SetArgs 一致；
	// 若仍传 os.Args[1:]，parser 会把 ---lang 当成不受支持的固定参数拒绝。
	if err := tryExecuteGenericInvoke(processLanguageResolution.args); err == nil {
		return 0
	} else if !errors.Is(err, errNotGenericInvoke) {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if err := rootCmd.Execute(); err != nil {
		// -v/--version already printed the version; treat as success so defer
		// can still emit a non-blocking upgrade notice on stderr.
		if errors.Is(err, errVersionFlagShown) {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func registerRootSystemFlags() {
	// Only preprocessable system flags are registered on root (completion/help).
	// force/version/method are action-scoped and stay off the root cobra flag set
	// so root `ve --version` remains the CLI binary version switch.
	flags := rootCmd.Flags()
	for _, d := range systemFlagDefs {
		if !d.preprocess || !d.public {
			continue
		}
		if flags.Lookup(d.name) != nil {
			continue
		}
		switch d.name {
		case "profile":
			flags.String(d.name, "", tr("Use a configured profile only for this invocation."))
		case "region":
			flags.String(d.name, "", tr("Override the region only for this invocation."))
		case "endpoint":
			flags.String(d.name, "", tr("Override the endpoint only for this invocation."))
		case "lang":
			flags.String(d.name, "", tr("Set the display language for this invocation (EN or ZH)."))
		default:
			flags.String(d.name, "", "")
		}
	}
}

func localizeHelpFlags(command *cobra.Command) {
	localizeHelpFlag(command)
	localCommandNames := map[string]struct{}{
		"completion": {},
		"configure":  {},
		"login":      {},
		"logout":     {},
		"sso":        {},
		"skills":     {},
		"version":    {},
	}
	for _, child := range command.Commands() {
		if _, ok := localCommandNames[child.Name()]; ok {
			localizeHelpFlagTree(child)
		}
	}
}

func localizeHelpFlagTree(command *cobra.Command) {
	localizeHelpFlag(command)
	for _, child := range command.Commands() {
		localizeHelpFlagTree(child)
	}
}

func localizeHelpFlag(command *cobra.Command) {
	command.InitDefaultHelpFlag()
	if helpFlag := command.Flags().Lookup("help"); helpFlag != nil {
		helpFlag.Usage = trf("Show help for %s", command.Name())
	}
}

func rootUsageTemplate() string {
	return tr("Usage:") + `{{if .Runnable}}
  {{.CommandPath}} [service]{{end}} [action] [params] [system flags] {{if .HasExample}}

` + tr("Examples:") + `
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

` + tr("Available Commands:") + `
  ` + tr("Service") + `                 ` + tr("Description") + `
  -------                 -----------{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

` + tr("Additional Commands:") + `
  ` + tr("Service") + `                 ` + tr("Description") + `
  -------                 -----------{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

` + tr("Flags:") + `
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

` + tr("System Flags:") + `
` + localizedSystemFlagsHelp() + `

` + tr("Examples:") + `
  ve sts GetCallerIdentity --profile default --region cn-beijing
  ve sts GetCallerIdentity --region cn-beijing --endpoint sts.volcengineapi.com
  ve upgrade
  ve upgrade --yes
  ve upgrade --version 1.0.49
` + tr(`Use "{{.CommandPath}} [service] --help" for more information about a service.`) + `{{end}}
`
}
