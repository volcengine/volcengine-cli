package cmd

import (
	"github.com/spf13/cobra"
	"github.com/volcengine/volcengine-cli/internal/proxycli"
)

// Only this audited product shares its official native command with an
// existing OpenAPI service. Arbitrary catalog entries cannot shadow services.
func sharedProxyName(spec proxycli.ToolSpec, name string) bool {
	return spec.ID == "vefaas-cli" && spec.Command == "vefaas" && name == "vefaas"
}
func sharedNativeTool(name string) (proxycli.Tool, bool) {
	if name != "vefaas" {
		return "", false
	}
	spec, err := processProxyCatalog.Ready("vefaas-cli")
	return spec.ID, err == nil && sharedProxyName(spec, name)
}
func sharedOpenAPIInvocation(name string, args []string) bool {
	if len(args) == 0 {
		return false
	}
	if rootSupport.IsValidAction(name, args[0]) {
		return true
	}
	// Preserve explicit --force calls for future PascalCase OpenAPI actions.
	action := args[0]
	if action == "" || action[0] < 'A' || action[0] > 'Z' {
		return false
	}
	for _, arg := range args[1:] {
		if arg == "--" {
			break
		}
		if arg == "--force" || arg == "---force" {
			return true
		}
	}
	return false
}
func isProxyCLIInvocation(name string, args []string) bool {
	if _, shared := sharedNativeTool(name); shared && sharedOpenAPIInvocation(name, args) {
		return false
	}
	return isProxyCLIForwardCommand(name)
}
func nativeProxyNameAllowed(spec proxycli.ToolSpec, name string) bool {
	return !reservedProxyCommand(name) || sharedProxyName(spec, name)
}

// prepareSharedProxyRouting prevents Cobra from interpreting native arguments
// as OpenAPI subcommands. The first argument after the shared service is the
// sole routing decision; later tokens belong to the selected CLI unchanged.
// Keep the service tree intact for help/completion and restore it after execution.
func prepareSharedProxyRouting(root *cobra.Command, args []string) func() {
	if len(args) == 0 {
		return func() {}
	}
	if _, shared := sharedNativeTool(args[0]); !shared || sharedOpenAPIInvocation(args[0], args[1:]) {
		return func() {}
	}
	for _, service := range root.Commands() {
		if service.Name() != args[0] {
			continue
		}
		actions := append([]*cobra.Command(nil), service.Commands()...)
		service.RemoveCommand(actions...)
		return func() { service.AddCommand(actions...) }
	}
	return func() {}
}
