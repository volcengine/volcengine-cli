package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	generateServiceCommands()
}

func generateServiceCommands() {
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

		svcCmd.SetUsageTemplate(serviceUsageTemplate())
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
	var first string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			first = a
			break
		}
	}
	if first == "" {
		return cmd.Help()
	}
	for _, va := range validActions {
		if va == first {
			return nil
		}
	}
	return fmt.Errorf("%q is not a supported action of %q", first, svc)
}

func serviceUsageTemplate() string {
	return `Usage:{{if .Runnable}}
  {{.CommandPath}} [action]{{end}} [params] {{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Actions:
  Action                  Description
  ------                  -----------{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Use "{{.CommandPath}} [action] --help" for more information about a action.{{end}}

Fixed Flags:
  ---profile string    Use a configured profile only for this invocation.
  ---region string     Override the region only for this invocation.
  ---endpoint string   Override the endpoint only for this invocation.
  ---debug bool        Print CLI debug logs for this invocation.
  ---debug-log-file string
                       Append CLI debug logs to the specified file.
`
}
