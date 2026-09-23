package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/volcengine/volcengine-cli/internal/proxycli"
)

func useFixtureCatalog(t *testing.T) *proxycli.Snapshot {
	t.Helper()
	c := processProxyCatalog.Catalog()
	spec := c.Tools[0]
	spec.ID = "fixture"
	spec.Command = "fixture"
	spec.Aliases = []string{"fixture-alias"}
	spec.Executable = "fixture"
	c.Tools = append(c.Tools, spec)
	raw, _ := json.Marshal(c)
	snapshot, err := proxycli.ParseCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	old := processProxyCatalog
	processProxyCatalog = snapshot
	t.Cleanup(func() { processProxyCatalog = old })
	return snapshot
}
func TestDynamicArgumentOwnership(t *testing.T) {
	useFixtureCatalog(t)
	child := []string{"--profile", "child", "--region", "r", "--endpoint", "e", "--lang", "not-a-host-language", "--version", "--help", "--", "", "中文", "space value"}
	for _, prefix := range [][]string{{"fixture"}, {"fixture-alias"}, {"proxycli", "run", "fixture", "--"}} {
		args := append([]string{"--lang", "ZH"}, prefix...)
		args = append(args, child...)
		r, err := resolveSystemFlags(args)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(r.args, append(append([]string{}, prefix...), child...)) || !reflect.DeepEqual(r.fixedFlags, map[string]string{"lang": "ZH"}) {
			t.Fatalf("argv changed: %+v", r)
		}
	}
}
func TestDynamicRegistrarRespectsNativeNames(t *testing.T) {
	s := useFixtureCatalog(t)
	c := s.Catalog()
	c.Tools[len(c.Tools)-1].Command = "ecs"
	c.Tools[len(c.Tools)-1].Aliases = []string{"configure", "safe-alias"}
	raw, _ := json.Marshal(c)
	s, err := proxycli.ParseCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	processProxyCatalog = s
	if isProxyCLIForwardCommand("ecs") || isProxyCLIForwardCommand("configure") {
		t.Fatal("native name treated as proxy")
	}
	root := &cobra.Command{Use: "ve"}
	native := &cobra.Command{Use: "ecs", Run: func(*cobra.Command, []string) {}}
	root.AddCommand(native)
	registerProxyCommands(root, s)
	got, _, _ := root.Find([]string{"ecs"})
	if got != native {
		t.Fatal("native command overwritten")
	}
	got, _, err = root.Find([]string{"safe-alias"})
	if err != nil || got == root {
		t.Fatal("non-conflicting alias lost")
	}
}
func TestEarlyReservedNamesCoverNativeTree(t *testing.T) {
	for _, c := range rootCmd.Commands() {
		if _, ok := c.Annotations["proxycli"]; ok {
			continue
		}
		for _, name := range append([]string{c.Name()}, c.Aliases...) {
			if !reservedProxyCommand(name) {
				t.Errorf("reserve native root name %q before preprocessing", name)
			}
		}
	}
}
func TestCatalogCommandsAreLocalAndUnreadyCannotRun(t *testing.T) {
	useFixtureCatalog(t)
	c := processProxyCatalog.Catalog()
	for i := range c.Tools {
		if c.Tools[i].ID == "tls-cli" {
			c.Tools[i].Readiness = "discovered"
			c.Tools[i].Gaps = []string{"unready test fixture"}
		}
	}
	data, _ := json.Marshal(c)
	var err error
	processProxyCatalog, err = proxycli.ParseCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	old := newProxyCLIManager
	t.Cleanup(func() { newProxyCLIManager = old })
	newProxyCLIManager = func() (proxyCLIManager, error) { t.Fatal("local command created installer"); return nil, nil }
	for _, args := range [][]string{{"list", "--output", "json"}, {"info", "toscli"}, {"run", "tls-cli", "--", "--help"}, {"install", "tls-cli"}} {
		c := newProxyCLIManagementCommand()
		var out bytes.Buffer
		c.SetOut(&out)
		c.SetErr(&out)
		c.SetArgs(args)
		err := c.Execute()
		if args[0] == "run" || args[0] == "install" {
			if err == nil || !strings.Contains(err.Error(), "discovered") {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

func TestSkippedProductsHaveNoEmbeddedForwardCommand(t *testing.T) {
	snapshot := proxycli.EmbeddedSnapshot()
	old := processProxyCatalog
	processProxyCatalog = snapshot
	t.Cleanup(func() { processProxyCatalog = old })
	root := &cobra.Command{Use: "ve"}
	registerProxyCommands(root, snapshot)
	for _, spec := range snapshot.Catalog().Tools {
		if spec.Readiness == "ready" {
			continue
		}
		for _, cmd := range root.Commands() {
			if cmd.Annotations["proxycli"] == string(spec.ID) {
				t.Fatal("skipped product registered", spec.ID)
			}
		}
		for _, name := range append([]string{spec.Command}, spec.Aliases...) {
			if isProxyCLIForwardCommand(name) {
				t.Fatal("skipped product owns forwarded argv", name)
			}
		}
	}
}

func TestInactiveNativeCommandConflictDoesNotBlockCatalogValidation(t *testing.T) {
	data, _ := json.Marshal(proxycli.EmbeddedSnapshot().Catalog())
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	command := newProxyCLIManagementCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"validate", path})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "structurally valid") {
		t.Fatal(output.String())
	}
}
