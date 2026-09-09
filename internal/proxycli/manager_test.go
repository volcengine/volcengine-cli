package proxycli

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestResolvePrefersExternalPath(t *testing.T) {
	home := t.TempDir()
	externalDir := t.TempDir()
	external := filepath.Join(externalDir, executableName(ToolArkCLI, runtime.GOOS))
	writeExecutable(t, external, []byte("external"))

	m := testManager(t, home, nil)
	m.LookPath = func(name string) (string, error) {
		if name != executableName(ToolArkCLI, runtime.GOOS) {
			t.Fatalf("LookPath(%q)", name)
		}
		return external, nil
	}

	got, err := m.Resolve(ToolArkCLI)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Path != external || got.Source != SourcePath {
		t.Fatalf("Resolve() = %+v, want PATH binary %s", got, external)
	}
	if _, err := os.Stat(m.statePath()); !os.IsNotExist(err) {
		t.Fatalf("PATH resolution must not create managed state, stat err = %v", err)
	}
}

func TestResolveReinstallsUntrackedManagedBinary(t *testing.T) {
	home := t.TempDir()
	want := []byte("official-binary")
	server, manifestURL := releaseServer(t, ToolArkCLI, want)
	defer server.Close()
	m := testManager(t, home, server.Client())
	m.ManifestURL = manifestURL
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	writeExecutable(t, m.managedPath(ToolArkCLI), []byte("orphaned-binary"))

	resolved, err := m.Resolve(ToolArkCLI)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	got, err := os.ReadFile(resolved.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("managed binary = %q, want %q", got, want)
	}
}

func TestResolveReplacesUntrackedManagedSymlink(t *testing.T) {
	home := t.TempDir()
	want := []byte("official-binary")
	server, manifestURL := releaseServer(t, ToolArkCLI, want)
	defer server.Close()
	m := testManager(t, home, server.Client())
	m.ManifestURL = manifestURL
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	target := filepath.Join(home, "user-binary")
	writeExecutable(t, target, []byte("user-binary"))
	path := m.managedPath(ToolArkCLI)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}

	resolved, err := m.Resolve(ToolArkCLI)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	info, err := os.Lstat(resolved.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("managed path is still a symlink: %s", resolved.Path)
	}
	got, err := os.ReadFile(resolved.Path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("managed binary = %q, err=%v", got, err)
	}
	userBinary, err := os.ReadFile(target)
	if err != nil || string(userBinary) != "user-binary" {
		t.Fatalf("symlink target was modified: %q, err=%v", userBinary, err)
	}
}

func TestTrackedManagedSymlinkPointsToUpgradeRecovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks may require elevated privileges on Windows")
	}
	home := t.TempDir()
	official := []byte("official-binary")
	server, manifestURL := releaseServer(t, ToolArkCLI, official)
	defer server.Close()
	m := testManager(t, home, server.Client())
	m.ManifestURL = manifestURL
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	target := filepath.Join(home, "user-binary")
	writeExecutable(t, target, []byte("user-binary"))
	path := m.managedPath(ToolArkCLI)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := m.writeState(State{Tools: map[Tool]*InstalledTool{
		ToolArkCLI: {
			Version: "9.8.7", ArtifactSHA256: digest(official), BinarySHA256: digest(official),
			Path: path, Source: SourceCDN,
		},
	}}); err != nil {
		t.Fatal(err)
	}

	_, err := m.Resolve(ToolArkCLI)
	if err == nil || !strings.Contains(err.Error(), "run 've proxycli upgrade' to restore") {
		t.Fatalf("Resolve() error = %v, want upgrade recovery guidance", err)
	}

	result, err := m.Upgrade([]Tool{ToolArkCLI})
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Status != StatusRestored {
		t.Fatalf("Upgrade() = %+v, want restored", result)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("managed path mode = %s, want regular file", info.Mode())
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, official) {
		t.Fatalf("managed binary = %q, err=%v", got, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "user-binary" {
		t.Fatalf("symlink target = %q, err=%v", got, err)
	}
}

func TestResolveDoesNotDeleteDirectoryAtManagedBinaryPath(t *testing.T) {
	m := testManager(t, t.TempDir(), nil)
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	path := m.managedPath(ToolArkCLI)
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	_, err := m.Resolve(ToolArkCLI)
	if err == nil || !strings.Contains(err.Error(), "move it aside") {
		t.Fatalf("Resolve() error = %v", err)
	}
	if info, statErr := os.Stat(path); statErr != nil || !info.IsDir() {
		t.Fatalf("managed directory was removed: info=%v err=%v", info, statErr)
	}
}

func TestProxyPathUsesIndependentInstallState(t *testing.T) {
	home := t.TempDir()
	binary := []byte("official-binary")
	server, manifestURL := releaseServer(t, ToolArkCLI, binary)
	defer server.Close()
	newManager := func(binDir string) *Manager {
		m := testManager(t, home, server.Client())
		m.BinDir = binDir
		m.ManifestURL = manifestURL
		m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
		return m
	}
	dirA := filepath.Join(home, "bin-a")
	dirB := filepath.Join(home, "bin-b")
	mA := newManager(dirA)
	mB := newManager(dirB)
	if mA.statePath() == mB.statePath() {
		t.Fatalf("different proxy paths share state %q", mA.statePath())
	}
	if _, err := mA.Resolve(ToolArkCLI); err != nil {
		t.Fatal(err)
	}
	if _, err := mB.Resolve(ToolArkCLI); err != nil {
		t.Fatal(err)
	}
	if _, err := newManager(dirA).Resolve(ToolArkCLI); err != nil {
		t.Fatalf("switching back to the first proxy path: %v", err)
	}
}

func TestCorruptStateIsTreatedAsNotInstalled(t *testing.T) {
	home := t.TempDir()
	binary := []byte("official-binary")
	server, manifestURL := releaseServer(t, ToolArkCLI, binary)
	defer server.Close()
	m := testManager(t, home, server.Client())
	m.ManifestURL = manifestURL
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	if err := os.MkdirAll(filepath.Dir(m.statePath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.statePath(), []byte(`{"broken":`), 0600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, m.managedPath(ToolArkCLI), []byte("stale-binary"))

	resolved, err := m.Resolve(ToolArkCLI)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Version != "9.8.7" {
		t.Fatalf("resolved version = %q", resolved.Version)
	}
}

func TestResolveCleansInterruptedInstallTemporaryFiles(t *testing.T) {
	home := t.TempDir()
	m := testManager(t, home, nil)
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	binary := []byte("managed-binary")
	path := m.managedPath(ToolArkCLI)
	writeExecutable(t, path, binary)
	if err := m.writeState(State{Tools: map[Tool]*InstalledTool{
		ToolArkCLI: {
			Version: "1.0.0", ArtifactSHA256: digest(binary), BinarySHA256: digest(binary),
			Path: path, Source: SourceEmbedded,
		},
	}}); err != nil {
		t.Fatal(err)
	}
	binTemp := filepath.Join(m.BinDir, ".proxy-cli-install-stale")
	stateTemp := filepath.Join(m.ConfigDir, m.stateTempPrefix()+"stale.tmp")
	if err := os.WriteFile(binTemp, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateTemp, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Resolve(ToolArkCLI); err != nil {
		t.Fatal(err)
	}
	for _, temp := range []string{binTemp, stateTemp} {
		if _, err := os.Stat(temp); !os.IsNotExist(err) {
			t.Fatalf("stale temporary file was not removed: %s", temp)
		}
	}
}

func TestDefaultCleanupDoesNotRemoveCustomPathTemporaryFile(t *testing.T) {
	home := t.TempDir()
	defaultManager := testManager(t, home, nil)
	customManager := testManager(t, home, nil)
	customManager.BinDir = filepath.Join(home, "custom-bin")
	if defaultManager.stateTempPrefix() == customManager.stateTempPrefix() || strings.HasPrefix(customManager.stateTempPrefix(), defaultManager.stateTempPrefix()) {
		t.Fatalf("temporary prefixes overlap: default=%q custom=%q", defaultManager.stateTempPrefix(), customManager.stateTempPrefix())
	}
	if err := os.MkdirAll(defaultManager.ConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	customTemp := filepath.Join(defaultManager.ConfigDir, customManager.stateTempPrefix()+"123")
	if err := os.WriteFile(customTemp, []byte("in-flight"), 0600); err != nil {
		t.Fatal(err)
	}
	defaultManager.cleanupStaleInstallTemps()
	if _, err := os.Stat(customTemp); err != nil {
		t.Fatalf("default cleanup removed custom temporary file: %v", err)
	}
}

func TestResolveInstallsMissingToolAndRecordsState(t *testing.T) {
	home := t.TempDir()
	binary := []byte("arkcli-test-binary")
	server, manifestURL := releaseServer(t, ToolArkCLI, binary)
	defer server.Close()

	m := testManager(t, home, server.Client())
	m.ManifestURL = manifestURL
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }

	got, err := m.Resolve(ToolArkCLI)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Source != SourceCDN || got.Version != "9.8.7" {
		t.Fatalf("Resolve() = %+v", got)
	}
	installed, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if !bytes.Equal(installed, binary) {
		t.Fatalf("installed binary = %q", installed)
	}

	state := readTestState(t, m.statePath())
	entry := state.Tools[ToolArkCLI]
	if entry == nil || entry.Version != "9.8.7" || entry.Path != got.Path {
		t.Fatalf("state entry = %+v", entry)
	}
	if entry.ArtifactSHA256 != digest(binary) || entry.BinarySHA256 != digest(binary) {
		t.Fatalf("state hashes = %+v", entry)
	}
	stateData, err := os.ReadFile(m.statePath())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stateData, []byte("schemaVersion")) {
		t.Fatalf("state should evolve additively without schemaVersion: %s", stateData)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(m.statePath())
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0600 {
			t.Fatalf("state mode = %o, want 600", got)
		}
	}
}

func TestEmbeddedFallbackManifestIsValid(t *testing.T) {
	manifest, err := parseManifest(embeddedFallbackManifest)
	if err != nil {
		t.Fatalf("embedded fallback manifest: %v", err)
	}
	for _, tool := range KnownTools() {
		if _, ok := manifest.Tools[tool]; !ok {
			t.Fatalf("embedded fallback manifest is missing %s", tool)
		}
	}
	if bytes.Contains(embeddedFallbackManifest, []byte("github.com")) {
		t.Fatal("embedded fallback must use the proxy CLI CDN only")
	}
}

func TestManifestSupportsAdditiveFieldsWithoutSchemaVersion(t *testing.T) {
	data := []byte(`{
		"futureTopLevel": true,
		"tools": {
			"arkcli": {
				"version": "1.2.3",
				"futureToolField": "ignored",
				"platforms": {
					"darwin-arm64": {
						"url": "https://example.com/arkcli",
						"sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						"futureAssetField": 1
					}
				}
			},
			"future-tool": {
				"version": "9.9.9",
				"platforms": {
					"darwin-arm64": {"url": "https://example.com/future", "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
				}
			}
		}
	}`)
	manifest, err := parseManifest(data)
	if err != nil {
		t.Fatalf("parseManifest() error = %v", err)
	}
	if manifest.Tools[ToolArkCLI].Version != "1.2.3" {
		t.Fatalf("manifest = %+v", manifest)
	}
}

func TestStateIgnoresUnknownToolWithoutDiscardingKnownEntries(t *testing.T) {
	m := testManager(t, t.TempDir(), nil)
	path := m.managedPath(ToolArkCLI)
	data := []byte(fmt.Sprintf(`{"tools":{"arkcli":{"version":"1.0.0","artifactSha256":"%s","binarySha256":"%s","path":%q,"source":"cdn","updatedAt":""},"future-tool":{"future":true}}}`,
		digest([]byte("artifact")), digest([]byte("binary")), path))
	if err := os.MkdirAll(m.ConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.statePath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	state, err := m.readState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Tools[ToolArkCLI] == nil {
		t.Fatalf("known state entry was discarded: %+v", state.Tools)
	}
}

func TestResolveUsesEmbeddedManifestWhenRemoteFails(t *testing.T) {
	home := t.TempDir()
	binary := []byte("fallback-binary")
	server, _ := releaseServer(t, ToolArkCLI, binary)
	defer server.Close()
	fallbackData := manifestFor(t, ToolArkCLI, "1.2.3", server.URL+"/artifact", binary)

	m := testManager(t, home, server.Client())
	m.ManifestURL = server.URL + "/missing-manifest"
	m.FallbackManifest = fallbackData
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	m.allowHTTPForTests = true

	got, err := m.Resolve(ToolArkCLI)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Source != SourceEmbedded || got.Version != "1.2.3" {
		t.Fatalf("Resolve() = %+v", got)
	}
}

func TestResolveUsesEmbeddedManifestWhenCDNArtifactFails(t *testing.T) {
	home := t.TempDir()
	binary := []byte("fallback-binary")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			_, _ = w.Write(manifestFor(t, ToolArkCLI, "1.2.4", server.URL+"/missing-artifact", binary))
		case "/fallback-artifact":
			_, _ = w.Write(binary)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	m := testManager(t, home, server.Client())
	m.ManifestURL = server.URL + "/manifest.json"
	m.FallbackManifest = manifestFor(t, ToolArkCLI, "1.2.3", server.URL+"/fallback-artifact", binary)
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }

	got, err := m.Resolve(ToolArkCLI)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Source != SourceEmbedded || got.Version != "1.2.3" {
		t.Fatalf("Resolve() = %+v", got)
	}
}

func TestResolveRejectsChecksumMismatch(t *testing.T) {
	home := t.TempDir()
	binary := []byte("real-binary")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			_, _ = w.Write(manifestFor(t, ToolArkCLI, "9.8.7", server.URL+"/artifact", []byte("fake-binary")))
		case "/artifact":
			_, _ = w.Write(binary)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	m := testManager(t, home, server.Client())
	m.ManifestURL = server.URL + "/manifest.json"
	m.FallbackManifest = manifestFor(t, ToolArkCLI, "9.8.7", server.URL+"/artifact", []byte("fake-binary"))
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	m.allowHTTPForTests = true

	_, err := m.Resolve(ToolArkCLI)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "sha256") {
		t.Fatalf("Resolve() error = %v, want checksum failure", err)
	}
	if _, statErr := os.Stat(m.managedPath(ToolArkCLI)); !os.IsNotExist(statErr) {
		t.Fatalf("checksum failure published a binary, stat err = %v", statErr)
	}
}

func TestResolveExtractsTosutilZip(t *testing.T) {
	home := t.TempDir()
	binary := []byte("tosutil-test-binary")
	archive := zipWithFile(t, "nested/tosutil", binary)
	server, manifestURL := releaseServer(t, ToolTOSUtil, archive)
	defer server.Close()

	m := testManager(t, home, server.Client())
	m.ManifestURL = manifestURL
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }

	got, err := m.Resolve(ToolTOSUtil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	installed, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(installed, binary) {
		t.Fatalf("installed binary = %q", installed)
	}
	entry := readTestState(t, m.statePath()).Tools[ToolTOSUtil]
	if entry.ArtifactSHA256 != digest(archive) || entry.BinarySHA256 != digest(binary) {
		t.Fatalf("state hashes = %+v", entry)
	}
}

func TestResolveRejectsModifiedManagedBinary(t *testing.T) {
	home := t.TempDir()
	m := testManager(t, home, nil)
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	path := m.managedPath(ToolArkCLI)
	writeExecutable(t, path, []byte("modified"))
	state := State{
		Tools: map[Tool]*InstalledTool{
			ToolArkCLI: {
				Version:        "1.0.0",
				ArtifactSHA256: digest([]byte("original")),
				BinarySHA256:   digest([]byte("original")),
				Path:           path,
				Source:         SourceEmbedded,
			},
		},
	}
	if err := m.writeState(state); err != nil {
		t.Fatal(err)
	}

	_, err := m.Resolve(ToolArkCLI)
	if err == nil || !strings.Contains(err.Error(), "local changes") {
		t.Fatalf("Resolve() error = %v, want local changes", err)
	}
}

func TestResolveDoesNotReturnMissingBinaryFromOlderFallback(t *testing.T) {
	home := t.TempDir()
	m := testManager(t, home, nil)
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	path := m.managedPath(ToolArkCLI)
	state := State{
		Tools: map[Tool]*InstalledTool{
			ToolArkCLI: {
				Version:        "9.9.9",
				ArtifactSHA256: digest([]byte("old-artifact")),
				BinarySHA256:   digest([]byte("old-binary")),
				Path:           path,
				Source:         SourceEmbedded,
			},
		},
	}
	if err := m.writeState(state); err != nil {
		t.Fatal(err)
	}
	m.FallbackManifest = manifestFor(t, ToolArkCLI, "1.0.0", "http://127.0.0.1:1/artifact", []byte("fallback"))

	_, err := m.Resolve(ToolArkCLI)
	if err == nil || !strings.Contains(err.Error(), "older than unusable installed version") {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestParseManifestRejectsTrailingData(t *testing.T) {
	manifest := manifestFor(t, ToolArkCLI, "1.0.0", "https://example.com/arkcli", []byte("binary"))
	manifest = append(manifest, []byte(" trailing")...)
	if _, err := parseManifest(manifest); err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("parseManifest() error = %v", err)
	}
}

func TestProxyAssetURLRejectsNonCDNHost(t *testing.T) {
	_, err := resolveAssetURL(DefaultManifestURL, "https://github.com/volcengine/ark-cli/releases/download/v1/arkcli", false)
	if err == nil || !strings.Contains(err.Error(), proxyCDNHost) {
		t.Fatalf("resolveAssetURL() error = %v", err)
	}
}

func TestUpgradeSkipsExternalAndUpdatesManagedTools(t *testing.T) {
	home := t.TempDir()
	binary := []byte("new-tosutil")
	archive := zipWithFile(t, "tosutil", binary)
	server, manifestURL := releaseServer(t, ToolTOSUtil, archive)
	defer server.Close()
	externalArk := filepath.Join(t.TempDir(), executableName(ToolArkCLI, runtime.GOOS))
	writeExecutable(t, externalArk, []byte("external"))

	m := testManager(t, home, server.Client())
	m.ManifestURL = manifestURL
	m.LookPath = func(name string) (string, error) {
		if name == executableName(ToolArkCLI, runtime.GOOS) {
			return externalArk, nil
		}
		return "", os.ErrNotExist
	}

	result, err := m.Upgrade([]Tool{ToolArkCLI, ToolTOSUtil})
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if len(result.Items) != 2 || result.Items[0].Status != StatusExternal || result.Items[1].Status != StatusInstalled {
		t.Fatalf("Upgrade() = %+v", result)
	}
}

func TestUpgradeReportsFailedNewerArtifactInsteadOfHidingBehindFallback(t *testing.T) {
	home := t.TempDir()
	currentBinary := []byte("current-binary")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest.json" {
			_, _ = w.Write(manifestFor(t, ToolArkCLI, "2.0.0", server.URL+"/missing", []byte("new-binary")))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	m := testManager(t, home, server.Client())
	m.ManifestURL = server.URL + "/manifest.json"
	m.FallbackManifest = manifestFor(t, ToolArkCLI, "1.0.0", server.URL+"/unused", currentBinary)
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	path := m.managedPath(ToolArkCLI)
	writeExecutable(t, path, currentBinary)
	if err := m.writeState(State{
		Tools: map[Tool]*InstalledTool{
			ToolArkCLI: {
				Version:        "1.0.0",
				ArtifactSHA256: digest(currentBinary),
				BinarySHA256:   digest(currentBinary),
				Path:           path,
				Source:         SourceEmbedded,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := m.Upgrade([]Tool{ToolArkCLI})
	if err == nil || !strings.Contains(err.Error(), "failed candidate") {
		t.Fatalf("Upgrade() error = %v", err)
	}
}

func TestUpgradeRejectsMutableReleaseAtSameVersion(t *testing.T) {
	home := t.TempDir()
	installed := []byte("immutable-build")
	rebuilt := []byte("changed-build")
	server, manifestURL := releaseServer(t, ToolArkCLI, rebuilt)
	defer server.Close()
	m := testManager(t, home, server.Client())
	m.ManifestURL = manifestURL
	m.FallbackManifest = manifestFor(t, ToolArkCLI, "9.8.7", server.URL+"/artifact", installed)
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	path := m.managedPath(ToolArkCLI)
	writeExecutable(t, path, installed)
	if err := m.writeState(State{Tools: map[Tool]*InstalledTool{
		ToolArkCLI: {
			Version: "9.8.7", ArtifactSHA256: digest(installed), BinarySHA256: digest(installed),
			Path: path, Source: SourceEmbedded,
		},
	}}); err != nil {
		t.Fatal(err)
	}

	_, err := m.Upgrade([]Tool{ToolArkCLI})
	if err == nil || !strings.Contains(err.Error(), "changed sha256 for the same version") {
		t.Fatalf("Upgrade() error = %v", err)
	}
}

func TestUpgradeReportsRestoredForModifiedManagedBinary(t *testing.T) {
	home := t.TempDir()
	official := []byte("official-binary")
	server, manifestURL := releaseServer(t, ToolArkCLI, official)
	defer server.Close()
	m := testManager(t, home, server.Client())
	m.ManifestURL = manifestURL
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	path := m.managedPath(ToolArkCLI)
	writeExecutable(t, path, []byte("modified-binary"))
	if err := m.writeState(State{Tools: map[Tool]*InstalledTool{
		ToolArkCLI: {
			Version: "9.8.7", ArtifactSHA256: digest(official), BinarySHA256: digest(official),
			Path: path, Source: SourceCDN,
		},
	}}); err != nil {
		t.Fatal(err)
	}
	result, err := m.Upgrade([]Tool{ToolArkCLI})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].Status != StatusRestored {
		t.Fatalf("Upgrade() = %+v", result)
	}
}

func TestUpgradeKeepsHealthyCurrentWhenNewerLacksPlatform(t *testing.T) {
	home := t.TempDir()
	currentBinary := []byte("current-binary")
	newer := Manifest{Tools: map[Tool]ManifestTool{
		ToolArkCLI: {
			Version: "2.0.0",
			Platforms: map[string]ManifestAsset{
				"plan9-mips": {URL: "https://example.com/arkcli", SHA256: digest([]byte("new"))},
			},
		},
	}}
	manifestData, err := json.Marshal(newer)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(manifestData)
	}))
	defer server.Close()
	m := testManager(t, home, server.Client())
	m.ManifestURL = server.URL
	m.FallbackManifest = manifestFor(t, ToolArkCLI, "1.0.0", "https://example.com/current", currentBinary)
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	path := m.managedPath(ToolArkCLI)
	writeExecutable(t, path, currentBinary)
	if err := m.writeState(State{Tools: map[Tool]*InstalledTool{
		ToolArkCLI: {
			Version: "1.0.0", ArtifactSHA256: digest(currentBinary), BinarySHA256: digest(currentBinary),
			Path: path, Source: SourceEmbedded,
		},
	}}); err != nil {
		t.Fatal(err)
	}

	result, err := m.Upgrade([]Tool{ToolArkCLI})
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Status != StatusCurrent {
		t.Fatalf("Upgrade() = %+v", result)
	}
}

func TestNewManagerAllowsSlowArtifactDownload(t *testing.T) {
	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if m.HTTPClient.Timeout < 4*time.Minute {
		t.Fatalf("HTTP client timeout = %v, want at least 4m", m.HTTPClient.Timeout)
	}
}

func TestConcurrentResolvePreservesBothStateEntries(t *testing.T) {
	home := t.TempDir()
	arkBinary := []byte("ark-binary")
	tosBinary := []byte("tos-binary")
	tosArchive := zipWithFile(t, "tosutil", tosBinary)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			manifest := Manifest{
				Tools: map[Tool]ManifestTool{
					ToolArkCLI: {
						Version: "1.0.0",
						Platforms: map[string]ManifestAsset{
							platformKey(runtime.GOOS, runtime.GOARCH): {URL: server.URL + "/ark", SHA256: digest(arkBinary), Size: int64(len(arkBinary))},
						},
					},
					ToolTOSUtil: {
						Version: "1.0.0",
						Platforms: map[string]ManifestAsset{
							platformKey(runtime.GOOS, runtime.GOARCH): {URL: server.URL + "/tos", SHA256: digest(tosArchive), Size: int64(len(tosArchive))},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(manifest)
		case "/ark":
			_, _ = w.Write(arkBinary)
		case "/tos":
			_, _ = w.Write(tosArchive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	m := testManager(t, home, server.Client())
	m.ManifestURL = server.URL + "/manifest.json"
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	var wait sync.WaitGroup
	errorsByTool := make(chan error, 2)
	for _, tool := range []Tool{ToolArkCLI, ToolTOSUtil} {
		tool := tool
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := m.Resolve(tool)
			errorsByTool <- err
		}()
	}
	wait.Wait()
	close(errorsByTool)
	for err := range errorsByTool {
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
	}
	state := readTestState(t, m.statePath())
	if state.Tools[ToolArkCLI] == nil || state.Tools[ToolTOSUtil] == nil {
		t.Fatalf("concurrent state lost an entry: %+v", state.Tools)
	}
}

func testManager(t *testing.T, home string, client *http.Client) *Manager {
	t.Helper()
	if client == nil {
		client = &http.Client{}
	}
	return &Manager{
		HomeDir:           home,
		ConfigDir:         filepath.Join(home, ".volcengine", "proxy-cli"),
		BinDir:            filepath.Join(home, ".volcengine", "proxy-cli", "bin"),
		ManifestURL:       "http://127.0.0.1:1/manifest.json",
		FallbackManifest:  []byte(`{"tools":{}}`),
		HTTPClient:        client,
		GOOS:              runtime.GOOS,
		GOARCH:            runtime.GOARCH,
		allowHTTPForTests: true,
	}
}

func releaseServer(t *testing.T, tool Tool, artifact []byte) (*httptest.Server, string) {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			_, _ = w.Write(manifestFor(t, tool, "9.8.7", server.URL+"/artifact", artifact))
		case "/artifact":
			_, _ = w.Write(artifact)
		default:
			http.NotFound(w, r)
		}
	}))
	return server, server.URL + "/manifest.json"
}

func manifestFor(t *testing.T, tool Tool, version, artifactURL string, artifact []byte) []byte {
	t.Helper()
	manifest := Manifest{
		Tools: map[Tool]ManifestTool{
			tool: {
				Version: version,
				Platforms: map[string]ManifestAsset{
					platformKey(runtime.GOOS, runtime.GOARCH): {
						URL: artifactURL, SHA256: digest(artifact), Size: int64(len(artifact)),
					},
				},
			},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func zipWithFile(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeExecutable(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0755); err != nil {
		t.Fatal(err)
	}
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readTestState(t *testing.T, path string) State {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}
