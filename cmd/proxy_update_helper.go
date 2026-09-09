package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/volcengine/volcengine-cli/internal/proxycli"
)

const (
	internalProxyUpdateCommand = "_proxycli-update-check"
	proxyUpdateHelperEnv       = "VOLCENGINE_CLI_PROXY_UPDATE_HELPER"
)

var (
	proxyExecutablePath            = os.Executable
	startDetachedProxyUpdateWorker = defaultStartDetachedProxyUpdateWorker
	refreshProxyUpdateCheck        = func(tool proxycli.Tool, current string) error {
		manager, err := proxycli.NewManager()
		if err != nil {
			return err
		}
		return manager.RefreshUpdateCheck(tool, current)
	}
)

func defaultLaunchProxyUpdateCheck(tool proxycli.Tool, current string) error {
	executable, err := proxyExecutablePath()
	if err != nil {
		return err
	}
	return startDetachedProxyUpdateWorker(
		executable,
		[]string{internalProxyUpdateCommand, "worker", string(tool), current},
		append(os.Environ(), proxyUpdateHelperEnv+"=1"),
	)
}

func newProxyCLIUpdateHelperCommand() *cobra.Command {
	return &cobra.Command{
		Use:    internalProxyUpdateCommand + " worker <tool> <current-version>",
		Hidden: true,
		Args:   cobra.ExactArgs(3),
		RunE: func(command *cobra.Command, args []string) error {
			if os.Getenv(proxyUpdateHelperEnv) != "1" {
				return fmt.Errorf("%s is an internal command", internalProxyUpdateCommand)
			}
			tool, err := proxycli.ParseTool(args[1])
			if err != nil {
				return err
			}
			if args[0] != "worker" {
				return fmt.Errorf("invalid proxy CLI update helper mode %q", args[0])
			}
			return refreshProxyUpdateCheck(tool, args[2])
		},
	}
}
