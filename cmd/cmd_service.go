package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	generateServiceCommands()
}

func generateServiceCommands() {
	for svc, actionMeta := range rootSupport.SupportAction {
		apiMetas := rootSupport.SupportTypes[svc]
		svcCmd := &cobra.Command{
			Use: svc,
			Run: func(cmd *cobra.Command, args []string) {
				cmd.Help()
			},
			Args: cobra.MatchAll(cobra.OnlyValidArgs),
		}

		svcCmd.SetUsageTemplate(serviceUsageTemplate())
		svcCmd.ValidArgs = rootSupport.GetAllAction(svc)

		actionCmds := generateActionCmd(actionMeta, apiMetas)
		for i := 0; i < len(actionCmds); i++ {
			svcCmd.AddCommand(actionCmds[i])
		}

		svcCmd.Flags().BoolP("help", "h", false, "")

		rootCmd.AddCommand(svcCmd)

		for _, v := range compatible_support_cmd {
			if strings.ReplaceAll(v, "_", "") == svc {
				compatibleCmd := *svcCmd
				compatibleCmd.Use = v
				compatibleCmd.Hidden = true
				rootCmd.AddCommand(&compatibleCmd)
			}
		}
	}
}

func serviceUsageTemplate() string {
	return `Usage:{{if .Runnable}}
  {{.CommandPath}} [action]{{end}} [params] {{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Actions:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Use "{{.CommandPath}} [action] --help" for more information about a action.{{end}}
`
}
