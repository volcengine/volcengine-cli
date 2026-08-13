package skills

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var testSkillNames = []string{
	"volcengine-cli",
	"volcengine-find-skills",
	"volcengine-knowledge-search",
	"volcengine-troubleshooting",
}

type testRelease struct {
	manifest []byte
	bundle   []byte
}

func TestInstallCreatesOnlyDefaultTargetsWhenOtherAgentsAreAbsent(t *testing.T) {
	home := tempDir(t)
	release := makeTestRelease(t, "1.0.0", "first")
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()

	manager := testManager(home, server.URL+"/latest/manifest.json")
	result, err := manager.Install()
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if result.Source != SourceCDN {
		t.Fatalf("source = %q, want %q", result.Source, SourceCDN)
	}

	for _, name := range testSkillNames {
		canonical := filepath.Join(home, ".agents", "skills", name)
		assertFileContains(t, filepath.Join(canonical, "SKILL.md"), "first")
		assertTargetMatchesCanonical(t, filepath.Join(home, ".claude", "skills", name), canonical)
		for _, target := range []string{
			filepath.Join(home, ".openclaw", "skills", name),
			filepath.Join(home, ".hermes", "skills", name),
			filepath.Join(home, ".trae", "skills", name),
		} {
			if _, err := os.Lstat(target); !os.IsNotExist(err) {
				t.Fatalf("unexpected target %s: %v", target, err)
			}
		}
	}

	state := readTestState(t, filepath.Join(home, ".volcengine", "skills", StateFileName))
	if state.SchemaVersion != StateSchemaVersion || len(state.Skills) != len(testSkillNames) {
		t.Fatalf("unexpected state: %#v", state)
	}
}

func TestInstallCreatesTargetsForDetectedAgents(t *testing.T) {
	home := tempDir(t)
	for _, directory := range []string{".openclaw", ".hermes", ".trae"} {
		if err := os.MkdirAll(filepath.Join(home, directory), 0700); err != nil {
			t.Fatal(err)
		}
	}
	release := makeTestRelease(t, "1.0.0", "detected")
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()

	manager := testManager(home, server.URL+"/latest/manifest.json")
	if _, err := manager.Install(); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	name := testSkillNames[0]
	canonical := filepath.Join(home, ".agents", "skills", name)
	for _, target := range []string{
		filepath.Join(home, ".openclaw", "skills", name),
		filepath.Join(home, ".hermes", "skills", name),
		filepath.Join(home, ".trae", "skills", name),
	} {
		assertTargetMatchesCanonical(t, target, canonical)
	}
}

func TestUpdateDoesNotRecreateRemovedOptionalAgentHome(t *testing.T) {
	home := tempDir(t)
	openClawHome := filepath.Join(home, ".openclaw")
	if err := os.MkdirAll(openClawHome, 0700); err != nil {
		t.Fatal(err)
	}
	release := makeTestRelease(t, "1.0.0", "detected")
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()
	manager := testManager(home, server.URL+"/latest/manifest.json")
	if _, err := manager.Install(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(openClawHome); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Update(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(openClawHome); !os.IsNotExist(err) {
		t.Fatalf("optional agent home was recreated: %v", err)
	}
}

func TestInstallUsesLegacyOpenClawDirectoryDetection(t *testing.T) {
	home := tempDir(t)
	if err := os.MkdirAll(filepath.Join(home, ".clawdbot"), 0700); err != nil {
		t.Fatal(err)
	}
	release := makeTestRelease(t, "1.0.0", "legacy")
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()

	manager := testManager(home, server.URL+"/latest/manifest.json")
	if _, err := manager.Install(); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	assertTargetMatchesCanonical(
		t,
		filepath.Join(home, ".clawdbot", "skills", testSkillNames[0]),
		filepath.Join(home, ".agents", "skills", testSkillNames[0]),
	)
	if _, err := os.Lstat(filepath.Join(home, ".openclaw", "skills", testSkillNames[0])); !os.IsNotExist(err) {
		t.Fatalf("unexpected .openclaw target: %v", err)
	}
}

func TestInstallAdoptsMatchingSkillsSetupInstallation(t *testing.T) {
	home := tempDir(t)
	release := makeTestRelease(t, "1.0.0", "legacy-setup")
	payloads, err := extractBundle(releaseFromTest(t, release))
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range payloads {
		canonical := filepath.Join(home, ".agents", "skills", payload.name)
		if err := installDirectory(canonical, payload.files); err != nil {
			t.Fatal(err)
		}
		claude := filepath.Join(home, ".claude", "skills", payload.name)
		if err := os.MkdirAll(filepath.Dir(claude), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(canonical, claude); err != nil {
			if err := installDirectory(claude, payload.files); err != nil {
				t.Fatal(err)
			}
		}
	}
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()

	manager := testManager(home, server.URL+"/latest/manifest.json")
	result, err := manager.Update()
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if len(result.Installed) != len(testSkillNames) || len(result.Skipped) != 0 {
		t.Fatalf("result = %#v, want matching legacy installation adopted", result)
	}
	state := readTestState(t, filepath.Join(home, ".volcengine", "skills", StateFileName))
	if len(state.Skills) != len(testSkillNames) {
		t.Fatalf("managed Skills = %d, want %d", len(state.Skills), len(testSkillNames))
	}
}

func TestInstallDoesNotAdoptModifiedSkillsSetupInstallation(t *testing.T) {
	home := tempDir(t)
	release := makeTestRelease(t, "1.0.0", "official")
	writeFile(
		t,
		filepath.Join(home, ".agents", "skills", "volcengine-cli", "SKILL.md"),
		[]byte("user content\n"),
		0644,
	)
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()

	manager := testManager(home, server.URL+"/latest/manifest.json")
	result, err := manager.Install()
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if !contains(result.Skipped, "volcengine-cli") {
		t.Fatalf("skipped = %v, want modified existing Skill", result.Skipped)
	}
	assertFileContains(
		t,
		filepath.Join(home, ".agents", "skills", "volcengine-cli", "SKILL.md"),
		"user content",
	)
}

func TestInstallFallsBackToGitHubRelease(t *testing.T) {
	home := tempDir(t)
	release := makeTestRelease(t, "1.0.0", "github")

	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "cdn unavailable", http.StatusServiceUnavailable)
	}))
	defer cdn.Close()
	github := releaseServer(t, &release, http.StatusOK)
	defer github.Close()

	manager := testManager(home, cdn.URL+"/latest/manifest.json")
	manager.GitHubManifestURL = github.URL + "/latest/manifest.json"
	result, err := manager.Install()
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if result.Source != SourceGitHub {
		t.Fatalf("source = %q, want %q", result.Source, SourceGitHub)
	}
	assertFileContains(
		t,
		filepath.Join(home, ".agents", "skills", testSkillNames[0], "SKILL.md"),
		"github",
	)
}

func TestInstallFallsBackToNpxAfterRemoteSourcesFail(t *testing.T) {
	home := tempDir(t)
	unavailable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer unavailable.Close()

	manager := testManager(home, unavailable.URL+"/cdn/manifest.json")
	manager.GitHubManifestURL = unavailable.URL + "/github/manifest.json"
	var command string
	var args []string
	manager.RunCommand = func(name string, values ...string) error {
		command = name
		args = append([]string(nil), values...)
		return nil
	}

	result, err := manager.Install()
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if result.Source != SourceNPX {
		t.Fatalf("source = %q, want %q", result.Source, SourceNPX)
	}
	if command != "npx" {
		t.Fatalf("command = %q, want npx", command)
	}
	wantArgs := []string{
		"-y", "skills", "add",
		"https://github.com/volcengine/volcengine-skills/tree/main/skills/core",
		"--global", "--yes", "--copy", "--full-depth", "--skill", "*",
	}
	if strings.Join(args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
	if _, err := os.Stat(filepath.Join(home, ".volcengine", "skills", StateFileName)); !os.IsNotExist(err) {
		t.Fatalf("npx fallback wrote ve state: %v", err)
	}
}

func TestUpdateWithoutStateFallsBackToNpx(t *testing.T) {
	home := tempDir(t)
	manager := testManager(home, "http://127.0.0.1:1/cdn/manifest.json")
	manager.GitHubManifestURL = "http://127.0.0.1:1/github/manifest.json"
	calls := 0
	manager.RunCommand = func(name string, values ...string) error {
		calls++
		return nil
	}

	result, err := manager.Update()
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if result.Source != SourceNPX || calls != 1 {
		t.Fatalf("result = %#v, calls = %d", result, calls)
	}
}

func TestUpdateWithManagedStateDoesNotFallBackToNpx(t *testing.T) {
	home := tempDir(t)
	release := makeTestRelease(t, "1.0.0", "first")
	server := releaseServer(t, &release, http.StatusOK)
	manager := testManager(home, server.URL+"/latest/manifest.json")
	if _, err := manager.Install(); err != nil {
		server.Close()
		t.Fatalf("Install() error = %v", err)
	}
	server.Close()

	manager.CDNManifestURL = "http://127.0.0.1:1/cdn/manifest.json"
	manager.GitHubManifestURL = "http://127.0.0.1:1/github/manifest.json"
	calls := 0
	manager.RunCommand = func(name string, values ...string) error {
		calls++
		return nil
	}

	_, err := manager.Update()
	if err == nil || !strings.Contains(err.Error(), "resolve Skill release failed") {
		t.Fatalf("Update() error = %v, want remote resolution error", err)
	}
	if calls != 0 {
		t.Fatalf("npx calls = %d, want 0", calls)
	}
	state := readTestState(t, filepath.Join(home, ".volcengine", "skills", StateFileName))
	if state.LastResolvedVersion != "1.0.0" {
		t.Fatalf("npx fallback changed ve state: %#v", state)
	}
}

func TestInstallWithManagedStateDoesNotFallBackToNpx(t *testing.T) {
	home := tempDir(t)
	release := makeTestRelease(t, "1.0.0", "first")
	server := releaseServer(t, &release, http.StatusOK)
	manager := testManager(home, server.URL+"/latest/manifest.json")
	if _, err := manager.Install(); err != nil {
		server.Close()
		t.Fatalf("Install() error = %v", err)
	}
	server.Close()

	manager.CDNManifestURL = "http://127.0.0.1:1/cdn/manifest.json"
	manager.GitHubManifestURL = "http://127.0.0.1:1/github/manifest.json"
	calls := 0
	manager.RunCommand = func(name string, values ...string) error {
		calls++
		return nil
	}

	if _, err := manager.Install(); err == nil || !strings.Contains(err.Error(), "resolve Skill release failed") {
		t.Fatalf("Install() error = %v, want remote resolution error", err)
	}
	if calls != 0 {
		t.Fatalf("npx calls = %d, want 0", calls)
	}
}

func TestInstallReportsAllSourcesWhenNpxFails(t *testing.T) {
	home := tempDir(t)
	manager := testManager(home, "http://127.0.0.1:1/cdn/manifest.json")
	manager.GitHubManifestURL = "http://127.0.0.1:1/github/manifest.json"
	manager.RunCommand = func(name string, values ...string) error {
		return fmt.Errorf("npx unavailable")
	}

	_, err := manager.Install()
	if err == nil {
		t.Fatal("Install() error = nil, want all sources failure")
	}
	for _, marker := range []string{"cdn:", "github:", "npx:", "npx unavailable"} {
		if !strings.Contains(err.Error(), marker) {
			t.Fatalf("Install() error = %q, want %q", err, marker)
		}
	}
}

func TestInstallInvalidStateDoesNotRunNpx(t *testing.T) {
	home := tempDir(t)
	statePath := filepath.Join(home, ".volcengine", "skills", StateFileName)
	writeFile(t, statePath, []byte("not json\n"), 0600)
	manager := testManager(home, "http://127.0.0.1:1/cdn/manifest.json")
	manager.GitHubManifestURL = "http://127.0.0.1:1/github/manifest.json"
	calls := 0
	manager.RunCommand = func(name string, values ...string) error {
		calls++
		return nil
	}

	if _, err := manager.Install(); err == nil || !strings.Contains(err.Error(), "parse Skill install state") {
		t.Fatalf("Install() error = %v, want state parse error", err)
	}
	if calls != 0 {
		t.Fatalf("npx calls = %d, want 0", calls)
	}
}

func TestInstallDoesNotUpgradeManagedCopyTarget(t *testing.T) {
	home := tempDir(t)
	release := makeTestRelease(t, "1.0.0", "first")
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()
	manager := testManager(home, server.URL+"/latest/manifest.json")
	if _, err := manager.Install(); err != nil {
		t.Fatal(err)
	}

	name := "volcengine-cli"
	canonical := filepath.Join(home, ".agents", "skills", name)
	claudeTarget := filepath.Join(home, ".claude", "skills", name)
	if err := os.Remove(claudeTarget); err != nil {
		t.Fatal(err)
	}
	content, err := ioutil.ReadFile(filepath.Join(canonical, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := installDirectory(claudeTarget, map[string][]byte{"SKILL.md": content}); err != nil {
		t.Fatal(err)
	}
	state := readTestState(t, filepath.Join(home, ".volcengine", "skills", StateFileName))
	state.Skills[name].Targets["claude-code"] = &InstalledTarget{
		Mode: "copy", Path: claudeTarget, ContentSHA256: state.Skills[name].ContentSHA256,
	}
	writeTestState(t, manager, state)

	release = makeTestRelease(t, "1.1.0", "second")
	if _, err := manager.Install(); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, filepath.Join(canonical, "SKILL.md"), "first")
	assertFileContains(t, filepath.Join(claudeTarget, "SKILL.md"), "first")
}

func TestInstallDoesNotDowngradeMissingManagedSkillFromFallback(t *testing.T) {
	home := tempDir(t)
	release := makeTestRelease(t, "2.0.0", "newer")
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()
	manager := testManager(home, server.URL+"/latest/manifest.json")
	if _, err := manager.Install(); err != nil {
		t.Fatal(err)
	}

	skillName := "volcengine-cli"
	canonical := filepath.Join(home, ".agents", "skills", skillName)
	if err := os.RemoveAll(canonical); err != nil {
		t.Fatal(err)
	}
	release = makeTestRelease(t, "1.0.0", "older-fallback")

	result, err := manager.Install()
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if !contains(result.Skipped, skillName) {
		t.Fatalf("skipped = %v, want %s", result.Skipped, skillName)
	}
	if _, err := os.Stat(canonical); !os.IsNotExist(err) {
		t.Fatalf("older release restored missing Skill: %v", err)
	}
	state := readTestState(t, filepath.Join(home, ".volcengine", "skills", StateFileName))
	if state.Skills[skillName].Version != "2.0.0" {
		t.Fatalf("version = %q, want 2.0.0", state.Skills[skillName].Version)
	}
}

func TestUpdateReplacesLocalChangesWithLatestRelease(t *testing.T) {
	home := tempDir(t)
	release := makeTestRelease(t, "1.0.0", "first")
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()
	manager := testManager(home, server.URL+"/latest/manifest.json")
	if _, err := manager.Install(); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	modifiedName := "volcengine-cli"
	modifiedPath := filepath.Join(home, ".agents", "skills", modifiedName, "SKILL.md")
	modifiedTarget := filepath.Join(home, ".claude", "skills", modifiedName)
	if err := os.Remove(modifiedTarget); err != nil {
		t.Fatal(err)
	}
	state := readTestState(t, filepath.Join(home, ".volcengine", "skills", StateFileName))
	if err := installDirectory(modifiedTarget, map[string][]byte{
		"SKILL.md": []byte("user target change\n"),
	}); err != nil {
		t.Fatal(err)
	}
	state.Skills[modifiedName].Targets["claude-code"] = &InstalledTarget{
		Mode:          "copy",
		Path:          modifiedTarget,
		ContentSHA256: state.Skills[modifiedName].ContentSHA256,
	}
	writeTestState(t, manager, state)
	if err := ioutil.WriteFile(modifiedPath, []byte("user change\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".agents", "skills", modifiedName, "extra.txt"), []byte("extra\n"), 0644)
	release = makeTestRelease(t, "1.1.0", "second")

	result, err := manager.Update()
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !contains(result.Updated, modifiedName) {
		t.Fatalf("updated = %v, want %s", result.Updated, modifiedName)
	}
	assertFileContains(t, modifiedPath, "second")
	assertFileContains(t, filepath.Join(modifiedTarget, "SKILL.md"), "second")
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", modifiedName, "extra.txt")); !os.IsNotExist(err) {
		t.Fatalf("extra file survived update: %v", err)
	}
	assertFileContains(
		t,
		filepath.Join(home, ".agents", "skills", "volcengine-find-skills", "SKILL.md"),
		"second",
	)

	state = readTestState(t, filepath.Join(home, ".volcengine", "skills", StateFileName))
	if state.Skills[modifiedName].Version != "1.1.0" {
		t.Fatalf("modified skill version = %q", state.Skills[modifiedName].Version)
	}
	if state.Skills["volcengine-find-skills"].Version != "1.1.0" {
		t.Fatalf("updated skill version = %q", state.Skills["volcengine-find-skills"].Version)
	}
}

func TestUpdateRestoresMissingManagedSkillAtCurrentRelease(t *testing.T) {
	home := tempDir(t)
	release := makeTestRelease(t, "1.0.0", "first")
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()
	manager := testManager(home, server.URL+"/latest/manifest.json")
	if _, err := manager.Install(); err != nil {
		t.Fatal(err)
	}

	name := "volcengine-cli"
	canonical := filepath.Join(home, ".agents", "skills", name)
	if err := os.RemoveAll(canonical); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Update()
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !contains(result.Updated, name) {
		t.Fatalf("updated = %v, want restored %s", result.Updated, name)
	}
	assertFileContains(t, filepath.Join(canonical, "SKILL.md"), "first")
	assertTargetMatchesCanonical(t, filepath.Join(home, ".claude", "skills", name), canonical)
}

func TestUpdateRestoresModifiedManagedSkillAtCurrentRelease(t *testing.T) {
	home := tempDir(t)
	release := makeTestRelease(t, "1.0.0", "official")
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()
	manager := testManager(home, server.URL+"/latest/manifest.json")
	if _, err := manager.Install(); err != nil {
		t.Fatal(err)
	}

	name := "volcengine-cli"
	canonical := filepath.Join(home, ".agents", "skills", name)
	writeFile(t, filepath.Join(canonical, "SKILL.md"), []byte("user change\n"), 0644)
	writeFile(t, filepath.Join(canonical, "extra.txt"), []byte("extra\n"), 0644)

	result, err := manager.Update()
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !contains(result.Updated, name) {
		t.Fatalf("updated = %v, want restored %s", result.Updated, name)
	}
	assertFileContains(t, filepath.Join(canonical, "SKILL.md"), "official")
	if _, err := os.Stat(filepath.Join(canonical, "extra.txt")); !os.IsNotExist(err) {
		t.Fatalf("extra file survived update: %v", err)
	}
}

func TestUpdateRestoresManagedTargetWhoseTypeChanged(t *testing.T) {
	home := tempDir(t)
	release := makeTestRelease(t, "1.0.0", "official")
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()
	manager := testManager(home, server.URL+"/latest/manifest.json")
	if _, err := manager.Install(); err != nil {
		t.Fatal(err)
	}

	name := "volcengine-cli"
	canonical := filepath.Join(home, ".agents", "skills", name)
	target := filepath.Join(home, ".claude", "skills", name)
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(target, "SKILL.md"), []byte("user replacement\n"), 0644)

	if _, err := manager.Update(); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	assertTargetMatchesCanonical(t, target, canonical)
	assertFileContains(t, filepath.Join(target, "SKILL.md"), "official")
}

func TestUpdateRestoresManagedPathsReplacedWithFiles(t *testing.T) {
	home := tempDir(t)
	release := makeTestRelease(t, "1.0.0", "official")
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()
	manager := testManager(home, server.URL+"/latest/manifest.json")
	if _, err := manager.Install(); err != nil {
		t.Fatal(err)
	}

	name := "volcengine-cli"
	canonical := filepath.Join(home, ".agents", "skills", name)
	target := filepath.Join(home, ".claude", "skills", name)
	if err := os.RemoveAll(canonical); err != nil {
		t.Fatal(err)
	}
	writeFile(t, canonical, []byte("user replacement\n"), 0644)
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	writeFile(t, target, []byte("user replacement\n"), 0644)

	if _, err := manager.Update(); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	assertFileContains(t, filepath.Join(canonical, "SKILL.md"), "official")
	assertTargetMatchesCanonical(t, target, canonical)
}

func TestUpdateInstallsSkillAddedToManifest(t *testing.T) {
	home := tempDir(t)
	release := makeTestReleaseWithNames(t, "1.0.0", "first", testSkillNames)
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()
	manager := testManager(home, server.URL+"/latest/manifest.json")
	if _, err := manager.Install(); err != nil {
		t.Fatal(err)
	}

	newSkill := "volcengine-new-core"
	release = makeTestReleaseWithNames(
		t,
		"1.1.0",
		"second",
		append(append([]string{}, testSkillNames...), newSkill),
	)
	result, err := manager.Update()
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !contains(result.Installed, newSkill) {
		t.Fatalf("installed = %v, want %s", result.Installed, newSkill)
	}
	assertFileContains(t, filepath.Join(home, ".agents", "skills", newSkill, "SKILL.md"), "second")
}

func TestUpdateWithoutStateRunsInstall(t *testing.T) {
	home := tempDir(t)
	release := makeTestRelease(t, "1.0.0", "installed-by-update")
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()

	manager := testManager(home, server.URL+"/latest/manifest.json")
	result, err := manager.Update()
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if len(result.Installed) != len(testSkillNames) {
		t.Fatalf("installed = %v, want all core Skills", result.Installed)
	}
	assertFileContains(
		t,
		filepath.Join(home, ".agents", "skills", "volcengine-cli", "SKILL.md"),
		"installed-by-update",
	)
}

func TestUpdateRejectsSameVersionWithDifferentContent(t *testing.T) {
	home := tempDir(t)
	release := makeTestRelease(t, "1.0.0", "first")
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()
	manager := testManager(home, server.URL+"/latest/manifest.json")
	if _, err := manager.Install(); err != nil {
		t.Fatal(err)
	}
	release = makeTestRelease(t, "1.0.0", "republished")
	if _, err := manager.Update(); err == nil || !strings.Contains(err.Error(), "same version") {
		t.Fatalf("Update() error = %v, want same-version error", err)
	}
}

func TestUninstallOnlyRemovesUnmodifiedManagedSkills(t *testing.T) {
	home := tempDir(t)
	release := makeTestRelease(t, "1.0.0", "first")
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()
	manager := testManager(home, server.URL+"/latest/manifest.json")
	if _, err := manager.Install(); err != nil {
		t.Fatal(err)
	}

	modifiedName := "volcengine-cli"
	modifiedPath := filepath.Join(home, ".agents", "skills", modifiedName, "SKILL.md")
	if err := ioutil.WriteFile(modifiedPath, []byte("user change\n"), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Uninstall()
	if err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if !contains(result.Skipped, modifiedName) {
		t.Fatalf("skipped = %v", result.Skipped)
	}
	assertFileContains(t, modifiedPath, "user change")
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "volcengine-find-skills")); !os.IsNotExist(err) {
		t.Fatalf("unmodified skill still exists: %v", err)
	}
	state := readTestState(t, filepath.Join(home, ".volcengine", "skills", StateFileName))
	if len(state.Skills) != 1 || state.Skills[modifiedName] == nil {
		t.Fatalf("unexpected remaining state: %#v", state)
	}
}

func TestUninstallRemovesManagedLinkWhenCanonicalSkillIsMissing(t *testing.T) {
	home := tempDir(t)
	release := makeTestRelease(t, "1.0.0", "first")
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()
	manager := testManager(home, server.URL+"/latest/manifest.json")
	if _, err := manager.Install(); err != nil {
		t.Fatal(err)
	}

	skillName := "volcengine-cli"
	canonical := filepath.Join(home, ".agents", "skills", skillName)
	target := filepath.Join(home, ".claude", "skills", skillName)
	if err := os.RemoveAll(canonical); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target); err != nil {
		t.Fatalf("managed link is missing before uninstall: %v", err)
	}

	if _, err := manager.Uninstall(); err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("dangling managed link still exists: %v", err)
	}
}

func TestDownloadRejectsHTTPSRedirectDowngrade(t *testing.T) {
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("unexpected"))
	}))
	defer httpServer.Close()
	httpsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, httpServer.URL, http.StatusFound)
	}))
	defer httpsServer.Close()

	manager := &Manager{HTTPClient: httpsServer.Client()}
	if _, err := manager.download(httpsServer.URL, 1024); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("download() error = %v, want HTTPS downgrade rejection", err)
	}
}

func TestDownloadRejectsPlainHTTP(t *testing.T) {
	manager := &Manager{HTTPClient: http.DefaultClient}
	if _, err := manager.download("http://example.com/manifest.json", 1024); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("download() error = %v, want HTTPS requirement", err)
	}
}

func TestContentDigestMatchesReleaseContract(t *testing.T) {
	digest, err := digestDirectory(filepath.Join("testdata", "content-digest"))
	if err != nil {
		t.Fatal(err)
	}
	const expected = "b08649421766bbc8bd9f11f5245bb9f4440e6a5cd526bf7772d58dbdd9b5df74"
	if digest != expected {
		t.Fatalf("content digest = %s, want %s", digest, expected)
	}
}

func TestUninstallRejectsStateSkillPathTraversal(t *testing.T) {
	home := tempDir(t)
	manager := testManager(home, "http://127.0.0.1:1/manifest.json")
	victim := filepath.Join(home, "victim")
	if err := os.MkdirAll(victim, 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(filepath.Join(victim, "keep.txt"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	digest, err := digestDirectory(victim)
	if err != nil {
		t.Fatal(err)
	}
	state := State{
		SchemaVersion: StateSchemaVersion,
		Skills: map[string]*InstalledSkill{
			"../../../victim": {
				Version:       "1.0.0",
				BundleSHA256:  strings.Repeat("0", 64),
				ContentSHA256: digest,
				Targets:       map[string]*InstalledTarget{},
			},
		},
	}
	writeTestState(t, manager, state)

	if _, err := manager.Uninstall(); err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("Uninstall() error = %v, want invalid state rejection", err)
	}
	assertFileContains(t, filepath.Join(victim, "keep.txt"), "keep")
}

func TestResolveBundleURLRejectsHTTPSDowngrade(t *testing.T) {
	if _, err := resolveBundleURL(
		"https://cloudcache.volccdn.com/ve/skills/latest/manifest.json",
		"http://example.com/volcengine-skill-bundle.zip",
	); err == nil {
		t.Fatal("resolveBundleURL() accepted an HTTP bundle for an HTTPS manifest")
	}
}

func TestParseManifestEnforcesFiftyMiBBundleLimit(t *testing.T) {
	release := makeTestRelease(t, "1.0.0", "limit")
	release.manifest = setManifestBundleSize(t, release.manifest, 50*1024*1024)
	if _, err := parseManifest(release.manifest); err != nil {
		t.Fatalf("parseManifest() rejected 50 MiB bundle: %v", err)
	}

	release.manifest = setManifestBundleSize(t, release.manifest, 50*1024*1024+1)
	if _, err := parseManifest(release.manifest); err == nil {
		t.Fatal("parseManifest() accepted bundle larger than 50 MiB")
	}
}

func TestInstallRejectsUnsafeBundlePath(t *testing.T) {
	home := tempDir(t)
	release := makeTestRelease(t, "1.0.0", "safe")
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	entry, err := archive.Create("../escaped")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("bad"))
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	release.bundle = buffer.Bytes()
	release.manifest = replaceBundleMetadata(t, release.manifest, release.bundle)
	server := releaseServer(t, &release, http.StatusOK)
	defer server.Close()

	manager := testManager(home, server.URL+"/latest/manifest.json")
	if _, err := manager.Install(); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("Install() error = %v, want unsafe archive error", err)
	}
	if _, err := os.Stat(filepath.Join(home, "escaped")); !os.IsNotExist(err) {
		t.Fatalf("archive escaped destination: %v", err)
	}
}

func testManager(home, manifestURL string) *Manager {
	return &Manager{
		HomeDir:           home,
		ConfigDir:         filepath.Join(home, ".volcengine", "skills"),
		CDNManifestURL:    manifestURL,
		GitHubManifestURL: "http://127.0.0.1:1",
		HTTPClient:        http.DefaultClient,
		allowHTTPForTests: true,
		ClaudeConfigDir:   filepath.Join(home, ".claude"),
		HermesHome:        filepath.Join(home, ".hermes"),
	}
}

func makeTestRelease(t *testing.T, version, marker string) testRelease {
	return makeTestReleaseWithNames(t, version, marker, testSkillNames)
}

func makeTestReleaseWithNames(t *testing.T, version, marker string, names []string) testRelease {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	skills := make([]ManifestSkill, 0, len(names))
	for _, name := range names {
		content := []byte(fmt.Sprintf("---\nname: %s\ndescription: test\n---\n%s\n", name, marker))
		entry, err := archive.Create(name + "/SKILL.md")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
		skills = append(skills, ManifestSkill{Name: name, SHA256: digestFiles(map[string][]byte{"SKILL.md": content})})
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	bundle := buffer.Bytes()
	manifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Version:       version,
		Bundle: ManifestBundle{
			File:   BundleFileName,
			SHA256: digestBytes(bundle),
			Size:   int64(len(bundle)),
			URL:    "/bundle.zip",
		},
		Skills: skills,
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return testRelease{manifest: data, bundle: bundle}
}

func releaseServer(t *testing.T, release *testRelease, status int) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			http.Error(w, "unavailable", status)
			return
		}
		switch r.URL.Path {
		case "/latest/manifest.json":
			var manifest Manifest
			if err := json.Unmarshal(release.manifest, &manifest); err != nil {
				t.Fatal(err)
			}
			manifest.Bundle.URL = server.URL + "/bundle.zip"
			_ = json.NewEncoder(w).Encode(manifest)
		case "/bundle.zip":
			_, _ = w.Write(release.bundle)
		default:
			http.NotFound(w, r)
		}
	}))
	return server
}

func releaseFromTest(t *testing.T, value testRelease) release {
	t.Helper()
	manifest, err := parseManifest(value.manifest)
	if err != nil {
		t.Fatal(err)
	}
	return release{manifest: manifest, bundle: value.bundle, source: SourceCDN}
}

func replaceBundleMetadata(t *testing.T, manifestData, bundle []byte) []byte {
	t.Helper()
	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Bundle.SHA256 = digestBytes(bundle)
	manifest.Bundle.Size = int64(len(bundle))
	result, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func setManifestBundleSize(t *testing.T, manifestData []byte, size int64) []byte {
	t.Helper()
	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Bundle.Size = size
	result, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := ioutil.TempDir("", "ve-skills-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func writeFile(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(path, content, mode); err != nil {
		t.Fatal(err)
	}
}

func assertFileContains(t *testing.T, path, expected string) {
	t.Helper()
	content, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(content), expected) {
		t.Fatalf("%s = %q, want content %q", path, content, expected)
	}
}

func assertTargetMatchesCanonical(t *testing.T, target, canonical string) {
	t.Helper()
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("Lstat(%s): %v", target, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(target)
		if err != nil {
			t.Fatal(err)
		}
		want, err := filepath.EvalSymlinks(canonical)
		if err != nil {
			t.Fatal(err)
		}
		if resolved != want {
			t.Fatalf("symlink %s -> %s, want %s", target, resolved, want)
		}
		return
	}
	assertFileContains(t, filepath.Join(target, "SKILL.md"), "description: test")
}

func readTestState(t *testing.T, path string) State {
	t.Helper()
	data, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func writeTestState(t *testing.T, manager *Manager, state State) {
	t.Helper()
	if err := manager.writeState(state); err != nil {
		t.Fatal(err)
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return "ve.exe"
	}
	return "ve"
}
