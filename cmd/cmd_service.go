package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	generateServiceCommands()
}

func generateServiceCommands() {
	usageTemplate := serviceUsageTemplate()
	for svc, actionMeta := range rootSupport.SupportAction {
		apiMetas := rootSupport.SupportTypes[svc]
		svc := svc
		validActions := rootSupport.GetAllAction(svc)
		svcCmd := &cobra.Command{
			Use:                svc,
			Short:              formatServiceShort(svc),
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				return runServiceCmd(cmd, svc, validActions, args)
			},
		}

		svcCmd.SetUsageTemplate(usageTemplate)
		svcCmd.ValidArgs = validActions

		actionCmds := generateActionCmd(svc, actionMeta, apiMetas)
		for i := 0; i < len(actionCmds); i++ {
			svcCmd.AddCommand(actionCmds[i])
		}

		svcCmd.Flags().BoolP("help", "h", false, "")

		rootCmd.AddCommand(svcCmd)

		for _, v := range compatible_support_cmd {
			if strings.ReplaceAll(v, "_", "") == svc {
				//copy a non ptr value from svcCmd for compatible svc cmd with _
				compatibleCmd := *svcCmd
				compatibleCmd.Use = v
				compatibleCmd.Hidden = true
				rootCmd.AddCommand(&compatibleCmd)
			}
		}
	}
}

// runServiceCmd handles invocation of a service command. Because the command
// uses DisableFlagParsing, cobra only reaches here when no valid action
// subcommand matched. We resolve the intended action from the raw args and
// surface a clear "unsupported action" error instead of cobra's flag-parsing
// error, even when fixed flags such as ---region are present.
func runServiceCmd(cmd *cobra.Command, svc string, validActions []string, args []string) error {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return cmd.Help()
		}
	}
	positional, err := parseInvocationArgs(args)
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		return cmd.Help()
	}
	action := positional[0]
	known := false
	for _, va := range validActions {
		if va == action {
			known = true
			break
		}
	}
	return dispatchServiceAction(ctx, svc, action, known)
}

func serviceUsageTemplate() string {
	return tr("Usage:") + `{{if .Runnable}}
  {{.CommandPath}} [action]{{end}} [params] {{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

` + tr("Available Actions:") + `
  ` + tr("Action") + `                  ` + tr("Description") + `
  ------                  -----------{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

` + tr(`Use "{{.CommandPath}} [action] --help" for more information about an action.`) + `{{end}}

` + tr("Fixed Flags:") + `
` + localizedFixedFlagsHelp() + `
`
}
