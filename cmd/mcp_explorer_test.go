package cmd

import "testing"

func TestMCPListServicesReturnsEveryService(t *testing.T) {
	results := listMCPServices(rootSupport)
	if len(results) != len(rootSupport.GetAllSvc()) {
		t.Fatalf("service count = %d, want %d", len(results), len(rootSupport.GetAllSvc()))
	}
	for i := 1; i < len(results); i++ {
		if results[i-1].Service >= results[i].Service {
			t.Fatalf("services are not strictly sorted: %q then %q", results[i-1].Service, results[i].Service)
		}
	}
}

func TestMCPListAPIsReturnsEveryAPIForService(t *testing.T) {
	results, err := listMCPAPIs(rootSupport, "ecs")
	if err != nil {
		t.Fatalf("listMCPAPIs() error = %v", err)
	}
	if len(results) != len(rootSupport.GetAllAction("ecs")) {
		t.Fatalf("API count = %d, want %d", len(results), len(rootSupport.GetAllAction("ecs")))
	}
	for i := 1; i < len(results); i++ {
		if results[i-1].API >= results[i].API {
			t.Fatalf("APIs are not strictly sorted: %q then %q", results[i-1].API, results[i].API)
		}
	}
}

func TestMCPAPIParametersAreStableAndStructured(t *testing.T) {
	result, err := getMCPAPIParameters(rootSupport, "sts", "GetCallerIdentity")
	if err != nil {
		t.Fatalf("getMCPAPIParameters() error = %v", err)
	}
	if result.Service != "sts" || result.API != "GetCallerIdentity" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
