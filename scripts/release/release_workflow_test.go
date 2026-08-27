package release

import (
	"io/ioutil"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoRootForTest returns the repository root relative to this test file
// (scripts/release/ is two directories below the repo root).
func repoRootForTest(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(currentFile)))
}

func readReleaseWorkflow(t *testing.T) string {
	t.Helper()
	workflowPath := filepath.Join(repoRootForTest(t), ".github", "workflows", "release.yml")
	data, err := ioutil.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	// Normalize newlines so Windows CRLF checkouts match LF-oriented assertions.
	workflow := strings.ReplaceAll(string(data), "\r\n", "\n")
	return strings.ReplaceAll(workflow, "\r", "\n")
}

func TestReleasePublishesStablePointersAfterNPM(t *testing.T) {
	workflow := readReleaseWorkflow(t)

	if strings.Contains(workflow, "\nconcurrency:\n") {
		t.Fatal("the whole release workflow must not be serialized because GitHub cancels older pending runs")
	}

	steps := []string{
		"- name: Extract version",
		"- name: Upload release assets to TOS",
		"- name: Verify public TOS download",
		"- name: Test npm package",
		"- name: Publish npm package",
		"- name: Publish version manifest to TOS root",
	}
	lastIndex := -1
	for _, step := range steps {
		if count := strings.Count(workflow, step); count != 1 {
			t.Fatalf("release workflow must contain exactly one %q step, got %d", step, count)
		}
		index := strings.Index(workflow, step)
		if index <= lastIndex {
			t.Fatalf("release workflow step %q is out of order", step)
		}
		lastIndex = index
	}

	versionStart := strings.Index(workflow, "- name: Extract version")
	npmPublishStart := strings.Index(workflow, "- name: Publish npm package")
	stablePublishStart := strings.Index(workflow, "- name: Publish version manifest to TOS root")

	versionStep := workflow[versionStart:strings.Index(workflow, "- name: Run GoReleaser")]
	for _, want := range []string{
		`npm_tag="latest"`,
		`if [[ "$version" == *-* ]]; then`,
		`npm_tag="next"`,
		`echo "npm_tag=$npm_tag" >> "$GITHUB_OUTPUT"`,
	} {
		if !strings.Contains(versionStep, want) {
			t.Fatalf("version step must route prereleases off latest, missing %q", want)
		}
	}

	npmPublishStep := workflow[npmPublishStart:stablePublishStart]
	for _, want := range []string{
		`NPM_TAG: ${{ steps.version.outputs.npm_tag }}`,
		`package_spec="@volcengine/cli@${VERSION}"`,
		`npm_version_or_empty "$package_spec"`,
		`npm publish --access public --tag "$NPM_TAG"`,
		`npm dist-tag add "$package_spec" "$NPM_TAG"`,
		`return 1`,
	} {
		if !strings.Contains(npmPublishStep, want) {
			t.Fatalf("npm package publication step missing guard %q", want)
		}
	}
	if strings.Contains(workflow, "--tag staging") {
		t.Fatal("publication must target the real channel instead of parking on a staging dist-tag")
	}
	if strings.Contains(npmPublishStep, "npm dist-tag rm") {
		t.Fatal("publication must not delete dist-tags because automation tokens are denied DELETE")
	}

	stableStep := workflow[stablePublishStart:]
	if !strings.Contains(stableStep, "if: ${{ steps.version.outputs.npm_tag == 'latest' }}") {
		t.Fatal("stable pointer publication must be limited to stable releases")
	}
	if !strings.Contains(stableStep, "VERSION: ${{ steps.version.outputs.version }}") {
		t.Fatal("stable manifest must publish the version built by this run")
	}
	manifestUpload := `s3 cp version_manifest.json "${root_dest}version_manifest.json"`
	latestUpload := `s3 cp latest "${root_dest}latest"`
	manifestIndex := strings.Index(stableStep, manifestUpload)
	latestIndex := strings.Index(stableStep, latestUpload)
	if manifestIndex < 0 || latestIndex < 0 {
		t.Fatalf("stable pointer step must upload both manifest and latest")
	}
	if latestIndex <= manifestIndex {
		t.Fatal("latest must be uploaded after version_manifest.json")
	}
}

// The channel move now happens inside the single release job, so a leftover
// promote job would publish the same version twice.
func TestReleaseHasNoPromoteJob(t *testing.T) {
	workflow := readReleaseWorkflow(t)

	if strings.Contains(workflow, "\n  promote:\n") {
		t.Fatal("release workflow must not keep a separate promote job")
	}
	for _, unwanted := range []string{
		"steps.promote.outputs",
		"needs.release.outputs",
		"release_version_guard.py",
	} {
		if strings.Contains(workflow, unwanted) {
			t.Fatalf("release workflow still references removed promotion machinery %q", unwanted)
		}
	}
}
