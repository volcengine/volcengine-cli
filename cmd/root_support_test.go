package cmd

import (
	"strings"
	"testing"

	"github.com/volcengine/volcengine-cli/asset"
	"github.com/volcengine/volcengine-cli/structset"
	"github.com/volcengine/volcengine-cli/typeset"
)

// Metadata assets may ship a service version that only exists in structset/typeset
// but not in asset. Such a version must be skipped instead of registering an empty
// service name, which would otherwise leak into command completion and MCP tools.
func TestSupportedServicesAreNamed(t *testing.T) {
	for i, svc := range rootSupport.GetAllSvc() {
		if svc == "" {
			t.Fatalf("SupportSvc[%d] is empty", i)
		}
	}
}

func TestSupportedServicesHaveActions(t *testing.T) {
	for _, svc := range rootSupport.GetAllSvc() {
		if len(rootSupport.GetAllAction(svc)) == 0 {
			t.Fatalf("service %q has no action metadata", svc)
		}
		if rootSupport.GetVersion(svc) == "" {
			t.Fatalf("service %q has no version", svc)
		}
	}
}

func TestCompatibleServiceListIsNamed(t *testing.T) {
	for i, svc := range rootSupport.GetAllSvcCompatible() {
		if svc == "" {
			t.Fatalf("GetAllSvcCompatible()[%d] is empty", i)
		}
	}
}

// META-RUNTIME-COVERAGE: every structure service version must ship matching
// metadata and types; otherwise NewRootSupport silently drops the package.
func TestStructureAssetsHaveMetadataAndTypes(t *testing.T) {
	metadata := map[string]bool{}
	for _, name := range asset.AssetNames() {
		metadata[name] = true
	}
	types := map[string]bool{}
	for _, name := range typeset.AssetNames() {
		types[name] = true
	}
	for _, name := range structset.AssetNames() {
		spaces := strings.Split(name, "/")
		if len(spaces) != 5 {
			continue
		}
		dir := spaces[2] + "/" + spaces[3]
		if want := spaces[0] + "/metadata/" + dir + "/metadata.json"; !metadata[want] {
			t.Errorf("%s has no metadata asset %s", name, want)
		}
		if want := spaces[0] + "/metatype/" + dir + "/meta_type.json"; !types[want] {
			t.Errorf("%s has no type asset %s", name, want)
		}
	}
}

func TestVersionedPackageIsRegistered(t *testing.T) {
	if !rootSupport.IsValidAction("pca20251001", "CreateRootInstance") {
		t.Fatalf("pca20251001 CreateRootInstance is not registered; services = %v", rootSupport.GetAllSvc())
	}
}
