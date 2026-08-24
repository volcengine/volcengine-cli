package cmd

import (
	"fmt"

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
		Short: tr("Log in to Volcengine Console"),
		Long: tr(`Authenticate with Volcengine Console and cache temporary STS credentials locally.

Login uses the OAuth 2.0 Device Authorization Grant: the CLI prints a
verification URL and a user code, then polls for the token until you finish
authorizing on any device.

Use --no-browser to skip opening the default browser.`),
		RunE: func(cmd *cobra.Command, args []string) error {
			warnDeprecatedRemoteFlag(cmd)
			return login.Login()
		},
	}

	cmd.SetUsageTemplate(loginUsageTemplate())

	// Register flags.
	cmd.Flags().StringVarP(&login.Profile, "profile", "p", "default", tr("Configuration profile name"))
	cmd.Flags().StringVarP(&login.Region, "region", "r", "", tr("Region (prompts when omitted; empty input defaults to cn-beijing)"))
	cmd.Flags().Bool("remote", false, tr("Deprecated: cross-device login is now the only mode"))
	cmd.Flags().BoolVar(&login.NoBrowser, "no-browser", false, tr("Do not automatically open the browser during login"))
	cmd.Flags().StringVar(&login.EndpointURL, "endpoint-url", "https://signin.volcengine.com", tr("Override signin service endpoint URL"))

	// --remote shipped in released versions and is kept as a hidden no-op so
	// existing scripts neither hit "unknown flag" nor exit non-zero. Nothing
	// reads its value. pflag's MarkDeprecated is deliberately avoided: it
	// prefixes the notice with an untranslated "Flag --remote has been
	// deprecated," which would render half-English under ---lang ZH.
	_ = cmd.Flags().MarkHidden("remote")

	return cmd
}

// warnDeprecatedRemoteFlag notifies scripts that still pass --remote. It writes
// to stderr so that callers parsing stdout are unaffected.
func warnDeprecatedRemoteFlag(cmd *cobra.Command) {
	if !cmd.Flags().Changed("remote") {
		return
	}
	fmt.Fprintln(cmd.ErrOrStderr(), tr("Warning: --remote is deprecated and ignored; 've login' always uses cross-device device code authorization."))
}

func newLogoutCmd() *cobra.Command {
	logout := &ConsoleLogout{}

	cmd := &cobra.Command{
		Use:   "logout",
		Short: tr("Log out of Volcengine Console and clear cached credentials"),
		Long: tr(`Remove locally cached login credentials for the specified profile or all profiles.

This is a purely local operation - no network requests are made to any server.
It deletes the cached STS token files from disk and clears the login_session
from the CLI configuration.

Modes:
  - Default: Logs out the specified profile (or "default" if not specified)
  - --all:   Logs out all configured console-login profiles with active sessions`),
		Example: tr(`  # Log out the default profile
  ve logout

  # Log out a specific profile
  ve logout --profile my-profile

  # Log out all profiles and clear all cached login credentials
  ve logout --all`),
		RunE: func(cmd *cobra.Command, args []string) error {
			return logout.Logout()
		},
	}

	cmd.SetUsageTemplate(loginUsageTemplate())

	// Register flags.
	cmd.Flags().StringVarP(&logout.Profile, "profile", "p", "default", tr("Configuration profile name"))
	cmd.Flags().BoolVar(&logout.All, "all", false, tr("Log out all profiles and remove all cached login credentials"))

	return cmd
}

func loginUsageTemplate() string {
	return tr("Usage:") + `{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

` + tr("Aliases:") + `
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

` + tr("Examples:") + `
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

` + tr("Available Commands:") + `{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

` + tr("Flags:") + `
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

` + tr("Global Flags:") + `
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

` + tr("Additional help topics:") + `{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

` + tr(`Use "{{.CommandPath}} [command] --help" for more information about a command.`) + `{{end}}

` + tr("System Flags:") + `
  --lang string     ` + tr("Set the display language for this invocation (EN or ZH).") + `
`
}
