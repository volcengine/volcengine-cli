package cmd

import (
	"github.com/spf13/cobra"
)

var (
	profileFlags Profile
)

func init() {
	configureCmd := newConfigureRootCmd()

	configureCmd.AddCommand(newConfigureGetCmd())
	configureCmd.AddCommand(newConfigureListCmd())
	configureCmd.AddCommand(newConfigureDeleteCmd())
	configureCmd.AddCommand(newConfigureSetCmd())

	rootCmd.AddCommand(configureCmd)
}

func newConfigureRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "configure",
		Args: cobra.MatchAll(cobra.OnlyValidArgs),
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Usage()
		},
	}

	cmd.SetUsageTemplate(configureUsageTemplate())

	return cmd
}

func newConfigureGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "get",
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName := cmd.Flag("profile").Value.String()
			return getConfigProfile(profileName)
		},
		Short: "show target profile's information",
		Long: `Description:
  show target profile's information
  if no profile name specified, show default profile`,
		DisableFlagsInUseLine: true,
	}

	cmd.SetUsageTemplate(configureActionUsageTemplate())

	cmd.Flags().StringVar(&profileFlags.Name, "profile", "", "target profile name")
	cmd.Flags().BoolP("help", "h", false, "")

	return cmd
}

func newConfigureSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "set",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setConfigProfile(&profileFlags)
		},
		Short: "add new profile, or modify target profile",
		Long: `Description:
  add new profile, or modify target profile:
      1. if profile not exist, add new;
      2. if profile exist, modify target field`,
		DisableFlagsInUseLine: true,
	}

	cmd.SetUsageTemplate(configureActionUsageTemplate())

	cmd.Flags().StringVar(&profileFlags.Name, "profile", "", "target profile name")
	cmd.Flags().StringVar(&profileFlags.AccessKey, "access-key", "", "your access key(AK)")
	cmd.Flags().StringVar(&profileFlags.SecretKey, "secret-key", "", "your secret key(SK)")
	cmd.Flags().StringVar(&profileFlags.Region, "region", "", "your region")
	cmd.Flags().StringVar(&profileFlags.Endpoint, "endpoint", "", "endpoint bind with region")
	cmd.Flags().StringVar(&profileFlags.SessionToken, "session-token", "", "your session token")

	profileFlags.DisableSSL = cmd.Flags().Bool("disable-ssl", true, "disable ssl")
	cmd.Flags().BoolP("help", "h", false, "")

	cmd.MarkFlagRequired("profile")
	cmd.MarkFlagRequired("region")

	return cmd
}

func newConfigureListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "list",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listConfigProfiles()
		},
		Short: "list all profiles",
		Long: `Description:
  list all profiles`,
		DisableFlagsInUseLine: true,
	}

	cmd.SetUsageTemplate(configureActionUsageTemplate())

	cmd.Flags().BoolP("help", "h", false, "")

	return cmd
}

func newConfigureDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "delete",
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName := cmd.Flag("profile").Value.String()
			return deleteConfigProfile(profileName)
		},
		Short: "delete target profile",
		Long: `Description:
  delete target profile`,
		DisableFlagsInUseLine: true,
	}

	cmd.SetUsageTemplate(configureActionUsageTemplate())

	cmd.Flags().StringVar(&profileFlags.Name, "profile", "", "target profile name")
	cmd.Flags().BoolP("help", "h", false, "")

	cmd.MarkFlagRequired("profile")

	return cmd
}

func configureUsageTemplate() string {
	return `Usage:{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
}

func configureActionUsageTemplate() string {
	return `Usage:{{if .Runnable}}
  {{.UseLine}} [params]{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
}
