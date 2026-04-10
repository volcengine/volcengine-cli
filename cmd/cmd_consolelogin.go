package cmd

import (
	"github.com/spf13/cobra"
)

func init() {
	loginCmd := newLoginCmd()
	logoutCmd := newLogoutCmd()
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(logoutCmd)
}

func newLoginCmd() *cobra.Command {
	login := &ConsoleLogin{}

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to Volcengine Console via browser",
		Long: `Authenticate with Volcengine Console using OAuth 2.0 + PKCE.
Opens a browser for authentication and caches temporary STS credentials locally.

Supports two modes:
  - Local (default): Opens browser on the same device
  - Remote (--remote): For headless environments, displays URL and accepts code input`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// If region not specified, try to load from config.
			if login.Region == "" {
				if cfg := LoadConfig(); cfg != nil && cfg.Profiles != nil {
					if p, ok := cfg.Profiles[login.Profile]; ok && p != nil && p.Region != "" {
						login.Region = p.Region
					}
				}
			}
			return login.Login()
		},
	}

	cmd.SetUsageTemplate(loginUsageTemplate())

	// Register flags.
	cmd.Flags().StringVarP(&login.Profile, "profile", "p", "default", "Configuration profile name")
	cmd.Flags().StringVarP(&login.Region, "region", "r", "", "Region (defaults to profile config value)")
	cmd.Flags().BoolVar(&login.Remote, "remote", false, "Enable cross-device (remote) login mode")
	cmd.Flags().StringVar(&login.EndpointURL, "endpoint-url", "https://signin.volcengine.com", "Override signin service endpoint URL")

	return cmd
}

func newLogoutCmd() *cobra.Command {
	logout := &ConsoleLogout{}

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Log out of Volcengine Console and clear cached credentials",
		Long: `Remove locally cached login credentials for the specified profile or all profiles.

This is a purely local operation — no network requests are made to any server.
It deletes the cached STS token files from disk and clears the login_session
from the CLI configuration.

Modes:
  - Default: Logs out the specified profile (or "default" if not specified)
  - --all:   Scans the cache directory and removes all cached login credentials`,
		Example: `  # Log out current/default profile
  ve logout

  # Log out a specific profile
  ve logout --profile my-profile

  # Log out all profiles and clear all cached login credentials
  ve logout --all`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return logout.Logout()
		},
	}

	cmd.SetUsageTemplate(loginUsageTemplate())

	// Register flags.
	cmd.Flags().StringVarP(&logout.Profile, "profile", "p", "default", "Configuration profile name")
	cmd.Flags().BoolVar(&logout.All, "all", false, "Log out all profiles and remove all cached login credentials")

	return cmd
}

func loginUsageTemplate() string {
	return `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

Available Commands:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
}
