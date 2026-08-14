package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	skillmanager "github.com/volcengine/volcengine-cli/internal/skills"
)

type fakeSkillsManager struct {
	installResult   skillmanager.Result
	installErr      error
	updateResult    skillmanager.Result
	updateErr       error
	uninstallResult skillmanager.Result
	uninstallErr    error
	calls           []string
}

func (f *fakeSkillsManager) Install() (skillmanager.Result, error) {
	f.calls = append(f.calls, "install")
	return f.installResult, f.installErr
}

func (f *fakeSkillsManager) Update() (skillmanager.Result, error) {
	f.calls = append(f.calls, "update")
	return f.updateResult, f.updateErr
}

func (f *fakeSkillsManager) Uninstall() (skillmanager.Result, error) {
	f.calls = append(f.calls, "uninstall")
	return f.uninstallResult, f.uninstallErr
}

func TestSkillsSubcommandsInvokeManager(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		manager  *fakeSkillsManager
		wantCall string
		wantText string
	}{
		{
			name: "install",
			args: []string{"install"},
			manager: &fakeSkillsManager{installResult: skillmanager.Result{
				Source: "cdn", Version: "1.2.3", Installed: []string{"volcengine-cli"},
			}},
			wantCall: "install",
			wantText: "Installed 1 Skill(s) from cdn (version 1.2.3)",
		},
		{
			name: "update",
			args: []string{"update"},
			manager: &fakeSkillsManager{updateResult: skillmanager.Result{
				Source: "github", Version: "1.2.4", Updated: []string{"volcengine-cli"},
			}},
			wantCall: "update",
			wantText: "Updated 1 Skill(s) from github (version 1.2.4)",
		},
		{
			name: "update with newly added Skill",
			args: []string{"update"},
			manager: &fakeSkillsManager{updateResult: skillmanager.Result{
				Source:    "cdn",
				Version:   "1.3.0",
				Installed: []string{"volcengine-new-core"},
				Updated:   []string{"volcengine-cli"},
			}},
			wantCall: "update",
			wantText: "Installed 1 Skill(s) and updated 1 Skill(s) from cdn (version 1.3.0)",
		},
		{
			name: "uninstall",
			args: []string{"uninstall"},
			manager: &fakeSkillsManager{uninstallResult: skillmanager.Result{
				Removed: []string{"volcengine-cli"}, Skipped: []string{"volcengine-troubleshooting"},
				Warnings: []string{"local changes"},
			}},
			wantCall: "uninstall",
			wantText: "Removed 1 Skill(s); skipped 1",
		},
		{
			name: "npx fallback",
			args: []string{"install"},
			manager: &fakeSkillsManager{installResult: skillmanager.Result{
				Source: skillmanager.SourceNPX,
			}},
			wantCall: "install",
			wantText: "Installed core Skills using npx fallback",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := newSkillsCommand(func() (skillsManager, error) { return test.manager, nil })
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetErr(&output)
			command.SetArgs(test.args)
			if err := command.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if len(test.manager.calls) != 1 || test.manager.calls[0] != test.wantCall {
				t.Fatalf("calls = %v, want %s", test.manager.calls, test.wantCall)
			}
			if !strings.Contains(output.String(), test.wantText) {
				t.Fatalf("output = %q, want %q", output.String(), test.wantText)
			}
		})
	}
}

func TestSkillsCommandPropagatesManagerError(t *testing.T) {
	manager := &fakeSkillsManager{installErr: errors.New("download failed")}
	command := newSkillsCommand(func() (skillsManager, error) { return manager, nil })
	command.SetArgs([]string{"install"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "download failed") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestSkillsCommandRejectsUnexpectedArguments(t *testing.T) {
	manager := &fakeSkillsManager{}
	command := newSkillsCommand(func() (skillsManager, error) { return manager, nil })
	command.SetArgs([]string{"install", "extra"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected argument validation error")
	}
}

func TestSkillsCommandRejectsUnknownSubcommand(t *testing.T) {
	for _, name := range []string{"skills", "skill"} {
		t.Run(name, func(t *testing.T) {
			root := &cobra.Command{Use: "ve"}
			root.AddCommand(newSkillsCommand(func() (skillsManager, error) { return &fakeSkillsManager{}, nil }))
			root.SetArgs([]string{name, "unknown"})
			if err := root.Execute(); err == nil {
				t.Fatal("expected unknown subcommand error")
			}
		})
	}
}

func TestSkillsCommandSupportsSingularAlias(t *testing.T) {
	command := newSkillsCommand(func() (skillsManager, error) { return &fakeSkillsManager{}, nil })
	if len(command.Aliases) != 1 || command.Aliases[0] != "skill" {
		t.Fatalf("aliases = %v, want [skill]", command.Aliases)
	}
}
