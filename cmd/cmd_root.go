package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

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

	rootCmd.SetHelpCommand(&cobra.Command{
		Hidden: true,
	})

	rootCmd.Flags().BoolP("help", "h", false, "")

	rootCmd.Flags().BoolP("version", "v", false, tr("Show CLI version"))

	rootCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		showVersion, _ := cmd.Flags().GetBool("version")
		if showVersion {
			fmt.Fprintln(cmd.OutOrStdout(), clientVersion)
			os.Exit(0)
		}
		return nil
	}

	// todo enable color?
	rootCmd.SetUsageTemplate(rootUsageTemplate())

	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: tr("Show CLI version"),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), clientVersion)
		},
	}, &cobra.Command{
		Use: "enable-color",
		Run: func(cmd *cobra.Command, args []string) {
			config.EnableColor = true
			WriteConfigToFile(config)
		},
		Hidden: true,
	}, &cobra.Command{
		Use: "disable-color",
		Run: func(cmd *cobra.Command, args []string) {
			config.EnableColor = false
			WriteConfigToFile(config)
		},
		Hidden: true,
	})
}

func Execute() {
	if processLanguageResolution.err != nil {
		fmt.Fprintln(os.Stderr, processLanguageResolution.err)
		os.Exit(1)
	}
	setCurrentLanguage(processLanguageResolution.language)
	rootCmd.SetArgs(processLanguageResolution.args)
	initRootCmd()
	localizeHelpFlags(rootCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
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
  {{.CommandPath}} [service]{{end}} [action] [params] {{if .HasExample}}

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

` + tr("Fixed Flags:") + `
  ---profile string    ` + tr("Use a configured profile only for this invocation.") + `
  ---region string     ` + tr("Override the region only for this invocation.") + `
  ---endpoint string   ` + tr("Override the endpoint only for this invocation.") + `
  ---lang string       ` + tr("Set the display language for this invocation (EN or ZH).") + `

` + tr("Examples:") + `
  ve sts GetCallerIdentity ---profile default ---region cn-beijing
  ve sts GetCallerIdentity ---region cn-beijing ---endpoint sts.volcengineapi.com

` + tr(`Use "{{.CommandPath}} [service] --help" for more information about a service.`) + `{{end}}
`
}
