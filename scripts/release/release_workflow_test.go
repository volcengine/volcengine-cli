package release

import (
	"io/ioutil"
	"os/exec"
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

func TestReleasePublishesStablePointersAfterNPM(t *testing.T) {
	repoRoot := repoRootForTest(t)
	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "release.yml")
	data, err := ioutil.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	workflow := string(data)

	if strings.Contains(workflow, "\nconcurrency:\n") {
		t.Fatal("the whole release workflow must not be serialized because GitHub cancels older pending runs")
	}
	promoteJobStart := strings.Index(workflow, "\n  promote:\n")
	if promoteJobStart < 0 {
		t.Fatal("release workflow missing promote job")
	}
	releaseJob := workflow[:promoteJobStart]
	promoteJob := workflow[promoteJobStart:]
	for _, want := range []string{
		"needs: release",
		"concurrency:\n      group: release-channel-${{ contains(needs.release.outputs.version, '-') && 'next' || 'latest' }}",
		"cancel-in-progress: false",
	} {
		if !strings.Contains(promoteJob, want) {
			t.Fatalf("promote job missing serialization guard %q", want)
		}
	}
	if strings.Contains(releaseJob, "group: release-channel") {
		t.Fatal("immutable release assets must not be subject to channel-promotion concurrency")
	}

	steps := []string{
		"- name: Upload release assets to TOS",
		"- name: Verify public TOS download",
		"- name: Test npm package",
		"- name: Publish npm package",
		"- name: Promote npm channel",
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

	npmPublishStart := strings.Index(workflow, "- name: Publish npm package")
	promoteStart := strings.Index(workflow, "- name: Promote npm channel")
	stablePublishStart := strings.Index(workflow, "- name: Publish version manifest to TOS root")
	npmPublishStep := workflow[npmPublishStart:promoteStart]
	for _, want := range []string{
		`package_spec="@volcengine/cli@${VERSION}"`,
		`npm_version_or_empty "$package_spec"`,
		`npm publish --access public --tag "$staging_tag"`,
		`trap 'status=$?; cleanup_staging_tag || true; exit "$status"' EXIT`,
		`return 1`,
	} {
		if !strings.Contains(npmPublishStep, want) {
			t.Fatalf("npm package publication step missing guard %q", want)
		}
	}
	if strings.Contains(npmPublishStep, "npm dist-tag add") {
		t.Fatal("immutable npm publication must not move latest/next")
	}

	promoteStep := workflow[promoteStart:stablePublishStart]
	for _, want := range []string{
		"id: promote",
		`npm view "@volcengine/cli" versions --json`,
		`--select-channel "$npm_tag"`,
		`package_spec="@volcengine/cli@${promote_version}"`,
		`published_version="$(npm_version_or_empty "$package_spec")"`,
		`current_npm_version="$(npm_version_or_empty "@volcengine/cli@${npm_tag}")"`,
		`release_version_guard.py`,
		`echo "advance_channel=${advance_channel}" >> "$GITHUB_OUTPUT"`,
		`echo "promote_version=${promote_version}" >> "$GITHUB_OUTPUT"`,
		`npm dist-tag add "$package_spec" "$npm_tag"`,
		`exit 1`,
	} {
		if !strings.Contains(promoteStep, want) {
			t.Fatalf("npm channel promotion step missing guard %q", want)
		}
	}

	stableStep := workflow[lastIndex:]
	stableCondition := "if: ${{ !contains(steps.promote.outputs.promote_version, '-') && steps.promote.outputs.advance_channel == 'true' }}"
	if !strings.Contains(stableStep, stableCondition) {
		t.Fatal("stable pointer publication must require a stable version and the monotonic version guard")
	}
	if !strings.Contains(stableStep, "VERSION: ${{ steps.promote.outputs.promote_version }}") {
		t.Fatal("stable manifest must publish the version selected by the channel promotion guard")
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

func TestReleaseVersionGuard(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skipf("python3 is not available: %v", err)
	}
	script := filepath.Join(repoRootForTest(t), "scripts", "release_version_guard.py")

	tests := []struct {
		name      string
		candidate string
		current   []string
		want      string
		wantError bool
	}{
		{name: "first stable release", candidate: "1.0.50", want: "true"},
		{name: "same stable release recovery", candidate: "1.0.50", current: []string{"npm:latest=1.0.50", "tos:stable=1.0.50"}, want: "true"},
		{name: "new stable release", candidate: "1.0.51", current: []string{"npm:latest=1.0.50", "tos:stable=1.0.50"}, want: "true"},
		{name: "older than npm stable", candidate: "1.0.50", current: []string{"npm:latest=1.0.51"}, want: "false"},
		{name: "older than TOS stable", candidate: "1.0.50", current: []string{"tos:stable=1.0.51"}, want: "false"},
		{name: "older prerelease", candidate: "1.0.51-rc.1", current: []string{"npm:next=1.0.51-rc.2"}, want: "false"},
		{name: "stable after prerelease", candidate: "1.0.51", current: []string{"npm:latest=1.0.51-rc.2"}, want: "true"},
		{name: "numeric prerelease precedence", candidate: "1.0.51-rc.9", current: []string{"npm:next=1.0.51-rc.10"}, want: "false"},
		{name: "invalid current fails closed", candidate: "1.0.51", current: []string{"npm:latest=invalid"}, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{script, "--candidate", tt.candidate}
			for _, current := range tt.current {
				args = append(args, "--current", current)
			}
			output, err := exec.Command("python3", args...).CombinedOutput()
			if tt.wantError {
				if err == nil {
					t.Fatalf("guard succeeded, want error:\n%s", output)
				}
				return
			}
			if err != nil {
				t.Fatalf("guard failed: %v\n%s", err, output)
			}
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			if got := lines[len(lines)-1]; got != tt.want {
				t.Fatalf("guard result = %q, want %q\n%s", got, tt.want, output)
			}
		})
	}
}

func TestReleaseVersionGuardSelectsHighestChannelVersion(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skipf("python3 is not available: %v", err)
	}
	script := filepath.Join(repoRootForTest(t), "scripts", "release_version_guard.py")

	tests := []struct {
		name     string
		channel  string
		versions string
		want     string
	}{
		{
			name:     "latest ignores prereleases and input order",
			channel:  "latest",
			versions: `["1.0.52-rc.1", "1.0.50", "1.0.52", "1.0.51"]`,
			want:     "1.0.52",
		},
		{
			name:     "next selects highest prerelease",
			channel:  "next",
			versions: `["1.0.52-rc.9", "1.0.51", "1.0.52-rc.10", "1.0.52-beta.1"]`,
			want:     "1.0.52-rc.10",
		},
		{
			name:     "stable outranks same version prerelease",
			channel:  "latest",
			versions: `["1.0.52-rc.2", "1.0.52"]`,
			want:     "1.0.52",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := exec.Command(
				"python3",
				script,
				"--select-channel", tt.channel,
				"--versions-json", tt.versions,
			).CombinedOutput()
			if err != nil {
				t.Fatalf("select channel failed: %v\n%s", err, output)
			}
			if got := strings.TrimSpace(string(output)); got != tt.want {
				t.Fatalf("selected version = %q, want %q", got, tt.want)
			}
		})
	}
}
