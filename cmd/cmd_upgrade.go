package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"github.com/spf13/cobra"
	"github.com/volcengine/volcengine-cli/upgrade"
	"github.com/volcengine/volcengine-cli/util"
)

func init() {
	// Align upgrade package config dir with CLI config (~/.volcengine/).
	upgrade.ConfigDirFunc = func() (string, error) {
		return util.GetConfigFileDir()
	}
	rootCmd.AddCommand(newUpgradeCmd())
}

func newUpgradeCmd() *cobra.Command {
	var (
		yes           bool
		targetVersion string
	)

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade the Volcengine CLI to the latest or a specified version",
		Long: `Upgrade the Volcengine CLI binary in place.

By default, checks the latest release (CDN version manifest, then GitHub)
and installs it after confirmation. Use --yes to skip the prompt, or
--version to install a specific release (upgrade or downgrade).

Downloads prefer https://cloudcache.volccdn.com/ve and verify SHA256
checksums before replacing the current binary.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return upgrade.DoUpgrade(upgrade.Options{
				CurrentVersion: clientVersion,
				TargetVersion:  targetVersion,
				Yes:            yes,
				Stdout:         cmd.OutOrStdout(),
				Stderr:         cmd.ErrOrStderr(),
				Stdin:          cmd.InOrStdin(),
			})
		},
	}

	cmd.SetUsageTemplate(`Usage:
  {{.CommandPath}} [flags]
{{if .HasAvailableLocalFlags}}
Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}
`)

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&targetVersion, "version", "", "Install a specific version (e.g. 1.0.49)")
	return cmd
}
