package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/volcengine/volcengine-cli/upgrade"
	"github.com/volcengine/volcengine-cli/util"
)

var (
	runWindowsUpgradeHelper  = upgrade.RunWindowsUpgradeHelper
	runWindowsUpgradeCleanup = upgrade.RunWindowsUpgradeCleanup
)

func init() {
	// Align upgrade package config dir with CLI config (~/.volcengine/).
	upgrade.ConfigDirFunc = func() (string, error) {
		return util.GetConfigFileDir()
	}
	rootCmd.AddCommand(newUpgradeCmd(), newUpgradeHelperCmd(), newUpgradeCleanupCmd())
}

func newUpgradeHelperCmd() *cobra.Command {
	var opts upgrade.WindowsUpgradeHelperOptions
	cmd := &cobra.Command{
		Use:          upgrade.InternalUpgradeHelperCommand,
		Hidden:       true,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if os.Getenv(upgrade.InternalUpgradeEnvironment) != "1" {
				return fmt.Errorf("%s is an internal command", upgrade.InternalUpgradeHelperCommand)
			}
			if opts.ParentPID <= 0 || strings.TrimSpace(opts.NewBinaryPath) == "" ||
				strings.TrimSpace(opts.TargetPath) == "" || strings.TrimSpace(opts.WorkDir) == "" ||
				strings.TrimSpace(opts.ExpectedVersion) == "" {
				return fmt.Errorf("invalid internal upgrade helper arguments")
			}
			opts.Stdout = cmd.OutOrStdout()
			opts.Stderr = cmd.ErrOrStderr()
			return runWindowsUpgradeHelper(opts)
		},
	}
	cmd.Flags().IntVar(&opts.ParentPID, "parent-pid", 0, "internal parent process id")
	cmd.Flags().StringVar(&opts.NewBinaryPath, "new-binary", "", "internal replacement binary")
	cmd.Flags().StringVar(&opts.TargetPath, "target", "", "internal target executable")
	cmd.Flags().StringVar(&opts.WorkDir, "work-dir", "", "internal temporary directory")
	cmd.Flags().StringVar(&opts.CurrentVersion, "current-version", "", "internal current version")
	cmd.Flags().StringVar(&opts.ExpectedVersion, "expected-version", "", "internal expected version")
	cmd.Flags().BoolVar(&opts.ExplicitTarget, "explicit-target", false, "internal explicit-version marker")
	cmd.Flags().BoolVar(&opts.SkipSelfCheck, "skip-self-check", false, "internal test option")
	return cmd
}

func newUpgradeCleanupCmd() *cobra.Command {
	var parentPID int
	var workDir string
	cmd := &cobra.Command{
		Use:          upgrade.InternalUpgradeCleanupCommand,
		Hidden:       true,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if os.Getenv(upgrade.InternalUpgradeEnvironment) != "1" {
				return fmt.Errorf("%s is an internal command", upgrade.InternalUpgradeCleanupCommand)
			}
			if parentPID <= 0 || strings.TrimSpace(workDir) == "" {
				return fmt.Errorf("invalid internal upgrade cleanup arguments")
			}
			return runWindowsUpgradeCleanup(parentPID, workDir)
		},
	}
	cmd.Flags().IntVar(&parentPID, "parent-pid", 0, "internal parent process id")
	cmd.Flags().StringVar(&workDir, "work-dir", "", "internal temporary directory")
	return cmd
}

func newUpgradeCmd() *cobra.Command {
	var (
		yes           bool
		targetVersion string
	)

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade the Volcengine CLI to the latest or a specified version",
		Long: `Upgrade the Volcengine CLI.

Install-source behavior:
  - Homebrew (macOS/Linux): delegates to "brew update" and "brew upgrade volcengine-cli"
    (requires network; --version is not supported for Homebrew installs)
  - npm (@volcengine/cli): prints "npm install -g @volcengine/cli@..." guidance (no in-place replace)
  - standalone (Release/source): downloads from CDN/GitHub and replaces the current binary

For standalone installs, checks the latest release (CDN version manifest, then GitHub)
and installs it after confirmation. The default path never downgrades. Use
--yes to skip the prompt, or --version to install a specific release
(including an explicit downgrade).

Standalone downloads prefer https://cloudcache.volccdn.com/ve and verify SHA256
checksums before replacing the current binary. On Windows, a temporary helper
finishes replacement after the running process exits.`,
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
