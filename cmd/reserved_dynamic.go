package cmd

// reservedDynamicFlag describes a double-dash flag that is owned by the CLI
// (not an OpenAPI request parameter).
//
// Keep this map as the single source of truth for:
//   - multi-value parsing (FlagSet.AddByName)
//   - skipping body/query assembly (buildActionInput / buildForceInput)
//   - upgrade root positional skipping (document names in upgrade.rootValueFlags)
//   - help text for reserved double-dash controls
//
// When adding a reserved dynamic flag, update upgrade/check.go rootValueFlags
// (upgrade package cannot import cmd without cycles) and user docs.
type reservedDynamicFlag struct {
	// Multi allows the flag to appear more than once (values accumulate).
	Multi bool
	// SkipBody excludes the flag from OpenAPI request body/query construction.
	SkipBody bool
}

// reservedDynamicFlags is the canonical registry of double-dash CLI controls.
var reservedDynamicFlags = map[string]reservedDynamicFlag{
	// --header Name=Value: custom HTTP headers (repeatable).
	"header": {Multi: true, SkipBody: true},
	// --body: full JSON body for application/json style calls.
	"body": {Multi: false, SkipBody: true},
}

// isMultiValueDynamicFlag reports whether the reserved dynamic flag may repeat.
func isMultiValueDynamicFlag(name string) bool {
	spec, ok := reservedDynamicFlags[name]
	return ok && spec.Multi
}

// isSkipBodyDynamicFlag reports whether the flag must not enter API params.
func isSkipBodyDynamicFlag(name string) bool {
	spec, ok := reservedDynamicFlags[name]
	return ok && spec.SkipBody
}
