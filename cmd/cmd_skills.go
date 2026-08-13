package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	skillmanager "github.com/volcengine/volcengine-cli/internal/skills"
)

type skillsManager interface {
	Install() (skillmanager.Result, error)
	Update() (skillmanager.Result, error)
	Uninstall() (skillmanager.Result, error)
}

type skillsManagerFactory func() (skillsManager, error)

func init() {
	rootCmd.AddCommand(newSkillsCommand(func() (skillsManager, error) {
		return skillmanager.NewManager()
	}))
}

func newSkillsCommand(factory skillsManagerFactory) *cobra.Command {
	command := &cobra.Command{
		Use:     "skills",
		Aliases: []string{"skill"},
		Short:   tr("Manage Volcengine Agent Skills"),
		Args:    cobra.NoArgs,
	}
	command.AddCommand(
		newSkillsActionCommand("install", tr("Install Volcengine Agent Skills"), factory, func(manager skillsManager) (skillmanager.Result, error) {
			return manager.Install()
		}),
		newSkillsActionCommand("update", tr("Update Volcengine Agent Skills"), factory, func(manager skillsManager) (skillmanager.Result, error) {
			return manager.Update()
		}),
		newSkillsActionCommand("uninstall", tr("Uninstall Volcengine Agent Skills"), factory, func(manager skillsManager) (skillmanager.Result, error) {
			return manager.Uninstall()
		}),
	)
	return command
}

func newSkillsActionCommand(
	use string,
	short string,
	factory skillsManagerFactory,
	action func(skillsManager) (skillmanager.Result, error),
) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			manager, err := factory()
			if err != nil {
				return err
			}
			result, err := action(manager)
			if err != nil {
				return err
			}
			printSkillsResult(command, use, result)
			return nil
		},
	}
}

func printSkillsResult(command *cobra.Command, action string, result skillmanager.Result) {
	for _, warning := range result.Warnings {
		fmt.Fprintf(command.ErrOrStderr(), "Warning: %s\n", warning)
	}
	if result.Source == skillmanager.SourceNPX {
		fmt.Fprintln(command.OutOrStdout(), "Installed core Skills using npx fallback")
		return
	}
	switch action {
	case "install":
		fmt.Fprintf(
			command.OutOrStdout(),
			"Installed %d Skill(s) from %s (version %s); skipped %d\n",
			len(result.Installed), result.Source, result.Version, len(result.Skipped),
		)
	case "update":
		if len(result.Installed) > 0 {
			if len(result.Updated) > 0 {
				fmt.Fprintf(
					command.OutOrStdout(),
					"Installed %d Skill(s) and updated %d Skill(s) from %s (version %s); skipped %d\n",
					len(result.Installed), len(result.Updated), result.Source, result.Version, len(result.Skipped),
				)
				return
			}
			fmt.Fprintf(
				command.OutOrStdout(),
				"Installed %d Skill(s) from %s (version %s); skipped %d\n",
				len(result.Installed), result.Source, result.Version, len(result.Skipped),
			)
			return
		}
		fmt.Fprintf(
			command.OutOrStdout(),
			"Updated %d Skill(s) from %s (version %s); skipped %d\n",
			len(result.Updated), result.Source, result.Version, len(result.Skipped),
		)
	case "uninstall":
		fmt.Fprintf(
			command.OutOrStdout(),
			"Removed %d Skill(s); skipped %d\n",
			len(result.Removed), len(result.Skipped),
		)
	}
}
