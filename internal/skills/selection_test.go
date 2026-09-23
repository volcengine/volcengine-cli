package skills

import (
	"net/http"
	"reflect"
	"testing"
)

func TestSelectedSkillsInstallUpdateUninstallIsolation(t *testing.T) {
	names := []string{"product-a", "product-b"}
	rel := makeTestReleaseWithNames(t, "1.0.0", "old", names)
	server := releaseServer(t, &rel, http.StatusOK)
	defer server.Close()
	m := testManager(t.TempDir(), server.URL+"/latest/manifest.json")
	if _, err := m.Install(); err != nil {
		t.Fatal(err)
	}
	before, _ := m.readState()
	if err := m.SelectNames([]string{"product-a"}); err != nil {
		t.Fatal(err)
	}
	rel = makeTestReleaseWithNames(t, "2.0.0", "new", names)
	r, err := m.Update()
	if err != nil || !reflect.DeepEqual(r.Updated, []string{"product-a"}) {
		t.Fatal(r, err)
	}
	after, _ := m.readState()
	if !reflect.DeepEqual(before.Skills["product-b"], after.Skills["product-b"]) {
		t.Fatal("unselected Skill changed")
	}
	r, err = m.Uninstall()
	if err != nil || !reflect.DeepEqual(r.Removed, []string{"product-a"}) {
		t.Fatal(r, err)
	}
	state, _ := m.readState()
	if len(state.Skills) != 1 || state.Skills["product-b"] == nil {
		t.Fatal("unselected Skill removed")
	}
}
func TestSelectedSkillsMissingNeverFallsBackToCore(t *testing.T) {
	rel := makeTestReleaseWithNames(t, "1.0.0", "data", []string{"core"})
	server := releaseServer(t, &rel, http.StatusOK)
	defer server.Close()
	m := testManager(t.TempDir(), server.URL+"/latest/manifest.json")
	if err := m.SelectNames(nil); err == nil {
		t.Fatal("explicit empty selection accepted")
	}
	if err := m.SelectNames([]string{"missing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Install(); err == nil {
		t.Fatal("absent mapping silently installed core")
	}
	m.CDNManifestURL = "http://127.0.0.1:1"
	m.RunCommand = func(string, ...string) error { t.Fatal("selection used wildcard npx fallback"); return nil }
	if _, err := m.Install(); err == nil {
		t.Fatal("unavailable release accepted")
	}
}
