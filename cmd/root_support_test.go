package cmd

import "testing"

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
