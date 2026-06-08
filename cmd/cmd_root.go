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

	rootCmd.Flags().BoolP("version", "v", false, "Show CLI version")

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
		Short: "Show CLI version",
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
	initRootCmd()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func rootUsageTemplate() string {
	return `Usage:{{if .Runnable}}
  {{.CommandPath}} [service]{{end}} [action] [params] {{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:
  Service                 Description
  -------                 -----------{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:
  Service                 Description
  -------                 -----------{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

Fixed Flags:
  ---profile string    Use a configured profile only for this invocation.
  ---region string     Override the region only for this invocation.
  ---endpoint string   Override the endpoint only for this invocation.

Examples:
  ve sts GetCallerIdentity ---profile default ---region cn-beijing
  ve sts GetCallerIdentity ---region cn-beijing ---endpoint sts.volcengineapi.com

Use "{{.CommandPath}} [service] --help" for more information about a service.{{end}}
`
}
