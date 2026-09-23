package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/volcengine/volcengine-cli/internal/proxycli"
	"github.com/volcengine/volcengine-cli/upgrade"
)

// This initializer is a dependency of argument preprocessing. Ordinary startup
// only reads a bounded local catalog and never refreshes it from the network.
var processProxyCatalog = loadProxyCatalog()

func loadProxyCatalog() *proxycli.Snapshot {
	store, err := proxycli.NewCatalogStore()
	if err != nil {
		return proxycli.EmbeddedSnapshot()
	}
	return store.Load()
}

// Names needed before command init functions run. Runtime registration also
// checks the actual Cobra tree. A regression test keeps both sets aligned.
func reservedProxyCommand(name string) bool {
	switch name {
	case "ve", "help", "version", "enable-color", "disable-color", "completion", "__complete", "__completeNoDesc", "configure", "docs", "login", "logout", "proxycli", "mcp", "sso", "skills", "skill", "upgrade", internalProxyUpdateCommand, upgrade.InternalUpgradeHelperCommand, upgrade.InternalUpgradeCleanupCommand:
		return true
	}
	return rootSupport.IsValidSvc(strings.ReplaceAll(name, "_", ""))
}
func proxyDirectNames(spec proxycli.ToolSpec) []string {
	names := []string{}
	for _, name := range append([]string{spec.Command}, spec.Aliases...) {
		if nativeProxyNameAllowed(spec, name) {
			names = append(names, name)
		}
	}
	return names
}
func registerProxyCommands(root *cobra.Command, snapshot *proxycli.Snapshot) {
	occupied := map[string]bool{}
	for _, c := range root.Commands() {
		occupied[c.Name()] = true
		for _, a := range c.Aliases {
			occupied[a] = true
		}
	}
	for _, spec := range snapshot.Catalog().Tools {
		if spec.Readiness != "ready" {
			continue
		}
		var names []string
		for _, name := range proxyDirectNames(spec) {
			if sharedProxyName(spec, name) {
				continue
			} // registered by the OpenAPI service command
			if !occupied[name] {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			continue
		}
		c := newProxyForwardCommand(names[0], spec.ID)
		c.Aliases = names[1:]
		c.Annotations = map[string]string{"proxycli": string(spec.ID)}
		c.Short = spec.Description
		if spec.Readiness != "ready" {
			c.Hidden = true
		}
		root.AddCommand(c)
		for _, n := range names {
			occupied[n] = true
		}
	}
}
func proxyToolCompletion(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	var names []string
	for _, t := range processProxyCatalog.Catalog().Tools {
		if strings.HasPrefix(string(t.ID), toComplete) {
			names = append(names, string(t.ID)+"\t"+t.Readiness+": "+t.Description)
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
func addProxyCatalogCommands(parent *cobra.Command) {
	var output string
	list := &cobra.Command{Use: "list", Short: tr("List the local proxy CLI catalog"), Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		if output == "json" {
			return writeProxyJSON(c, struct {
				Source  string           `json:"source"`
				Catalog proxycli.Catalog `json:"catalog"`
			}{processProxyCatalog.Source(), processProxyCatalog.Catalog()})
		}
		if output != "table" {
			return fmt.Errorf("unsupported output %q; use table or json", output)
		}
		w := tabwriter.NewWriter(c.OutOrStdout(), 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tCOMMAND\tREADINESS\tVERSION\tSOURCE")
		for _, t := range processProxyCatalog.Catalog().Tools {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", t.ID, t.Command, t.Readiness, t.ReleaseVersion, processProxyCatalog.Source())
		}
		return w.Flush()
	}}
	list.Flags().StringVar(&output, "output", "table", "Output format: table or json")
	info := &cobra.Command{Use: "info <tool-id>", Short: tr("Show proxy CLI requirements and sources"), Args: cobra.ExactArgs(1), ValidArgsFunction: proxyToolCompletion, RunE: func(c *cobra.Command, args []string) error {
		t, ok := processProxyCatalog.Lookup(args[0])
		if !ok {
			_, err := processProxyCatalog.ParseTool(args[0])
			return err
		}
		conflicts := []string{}
		for _, n := range append([]string{t.Command}, t.Aliases...) {
			if !nativeProxyNameAllowed(t, n) {
				conflicts = append(conflicts, n)
			}
		}
		return writeProxyJSON(c, struct {
			Tool      proxycli.ToolSpec `json:"tool"`
			Source    string            `json:"catalog_source"`
			Conflicts []string          `json:"root_command_conflicts"`
		}{t, processProxyCatalog.Source(), conflicts})
	}}
	var file string
	refresh := &cobra.Command{Use: "refresh", Short: tr("Validate and refresh the proxy CLI catalog"), Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		store, err := proxycli.NewCatalogStore()
		if err != nil {
			return err
		}
		var snapshot *proxycli.Snapshot
		if file != "" {
			snapshot, err = store.RefreshFile(file)
		} else {
			snapshot, err = store.Refresh()
		}
		if err != nil {
			return fmt.Errorf("catalog refresh failed (previous catalog retained): %w", err)
		}
		fmt.Fprintf(c.OutOrStdout(), "Catalog %s saved; new commands are available on the next invocation.\n", snapshot.Catalog().Revision)
		return nil
	}}
	refresh.Flags().StringVar(&file, "file", "", "Import a reviewed local v2 catalog (official artifact URLs only)")
	install := &cobra.Command{Use: "install <tool-id>", Short: tr("Install a proxy CLI on demand"), Args: cobra.ExactArgs(1), ValidArgsFunction: proxyToolCompletion, RunE: func(c *cobra.Command, args []string) error {
		tool, err := processProxyCatalog.ParseTool(args[0])
		if err != nil {
			return err
		}
		if _, err := processProxyCatalog.Ready(tool); err != nil {
			return err
		}
		m, err := newProxyCLIManager()
		if err != nil {
			return err
		}
		r, err := m.Resolve(tool)
		if err != nil {
			return err
		}
		fmt.Fprintf(c.OutOrStdout(), "%s %s (%s): %s\n", tool, r.Version, r.Source, r.Path)
		return nil
	}}
	run := &cobra.Command{Use: "run <tool-id> -- [arguments...]", Short: tr("Run a proxy CLI by stable id"), DisableFlagParsing: true, ValidArgsFunction: proxyToolCompletion, RunE: func(c *cobra.Command, args []string) error {
		if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
			return c.Help()
		}
		if len(args) < 2 || args[1] != "--" {
			return fmt.Errorf("usage: ve proxycli run <tool-id> -- [arguments...]")
		}
		tool, err := processProxyCatalog.ParseTool(args[0])
		if err != nil {
			return err
		}
		if _, err := processProxyCatalog.Ready(tool); err != nil {
			return err
		}
		m, err := newProxyCLIManager()
		if err != nil {
			return err
		}
		return runProxyCLI(c, ctx, m, tool, args[2:])
	}}
	validate := &cobra.Command{Use: "validate <catalog.json>", Short: tr("Validate a catalog before publication"), Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		snapshot, err := proxycli.ReadCatalogFile(args[0])
		if err != nil {
			return err
		}
		for _, t := range snapshot.Catalog().Tools {
			if t.Readiness != "ready" {
				continue
			}
			for _, name := range append([]string{t.Command}, t.Aliases...) {
				if !nativeProxyNameAllowed(t, name) {
					return fmt.Errorf("%s command %q conflicts with a native command; choose another name before publishing", t.ID, name)
				}
			}
		}
		fmt.Fprintf(c.OutOrStdout(), "Catalog %s is structurally valid; release artifacts and product identity still require smoke verification.\n", snapshot.Catalog().Revision)
		return nil
	}}

	parent.AddCommand(list, info, refresh, install, run, validate)
}
func writeProxyJSON(c *cobra.Command, value interface{}) error {
	e := json.NewEncoder(c.OutOrStdout())
	e.SetIndent("", "  ")
	return e.Encode(value)
}

// Classify local metadata/help without probing tools or loading host credentials.
func localOnlyInvocation(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "help", "version", "completion", "__complete", "__completeNoDesc", "--help", "-h", "--version", "-v":
		return true
	case "proxycli":
		if len(args) < 2 || args[1] != "run" {
			return true
		}
		for i, a := range args {
			if a == "--" {
				return isProxyLocalInvocation(args[i+1:])
			}
		}
	}
	if isProxyCLIInvocation(args[0], args[1:]) {
		return isProxyLocalInvocation(args[1:])
	}
	return false
}
