package pyruntime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// lookPathPython3 is resolved when the test binary starts, before tests
// replace PATH to control local interpreter discovery.
var lookPathPython3, lookPathPython3Err = exec.LookPath("python3")

func testPython(t *testing.T) string {
	t.Helper()
	path := os.Getenv("VOLCENGINE_TEST_PYTHON")
	if path == "" {
		if lookPathPython3Err != nil {
			t.Skip("python3 is not available; set VOLCENGINE_TEST_PYTHON")
		}
		path = lookPathPython3
	}
	absolute, _ := filepath.Abs(path)
	return absolute
}

type tarEntry struct {
	name     string
	kind     byte
	body     string
	linkname string
	mode     int64
}

func buildTarGz(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(compressed)
	for _, entry := range entries {
		mode := entry.mode
		if mode == 0 {
			mode = 0644
		}
		header := &tar.Header{Name: entry.name, Typeflag: entry.kind, Mode: mode, Linkname: entry.linkname, Size: int64(len(entry.body))}
		if entry.kind != tar.TypeReg {
			header.Size = 0
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if entry.kind == tar.TypeReg {
			if _, err := writer.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func serveArchive(t *testing.T, data []byte) (*httptest.Server, *int) {
	t.Helper()
	downloads := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads++
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)
	return server, &downloads
}

func artifactFor(url string, data []byte, version, executable string) Artifact {
	digest := sha256.Sum256(data)
	return Artifact{
		Platform: "test-platform", Implementation: "cpython", Version: version, Build: "test",
		URL: url, SHA256: hex.EncodeToString(digest[:]), Size: int64(len(data)), Format: "tar.gz", Executable: executable,
	}
}

func probeVersion(t *testing.T, python string) string {
	t.Helper()
	interpreter, err := Probe(context.Background(), python)
	if err != nil {
		t.Fatalf("Probe(%s) error = %v", python, err)
	}
	return interpreter.Version
}

func TestDefaultManifestPinsFiveVerifiedPlatforms(t *testing.T) {
	manifest, err := DefaultManifest()
	if err != nil {
		t.Fatalf("DefaultManifest() error = %v", err)
	}
	want := map[string]string{
		"darwin-arm64":  "9866e11cf2ae9caa1e671383786f248859a52dd8bfb702fb4c0dc2c65f1d36de",
		"darwin-amd64":  "d09998f85752b646873a8a6bc7eea442a0d251b1c8d2413aa8ea06ef131c3335",
		"linux-arm64":   "9467a99bacc7d90b64b02de7fa9ecfc99b72905e64e74f7ff6b460ad95cfed5a",
		"linux-amd64":   "178d3d83b6e8e99285a65d3fce9c16b87952cc5efdb4d1a0e3ff3be18ff0fc9c",
		"windows-amd64": "7a2aef48e5ee569e310b29b32681debbc5d888e92790d85331a47736c6b06aad",
	}
	if len(manifest.Runtimes) != len(want) {
		t.Fatalf("manifest has %d runtimes", len(manifest.Runtimes))
	}
	for platform, digest := range want {
		artifact, err := manifest.Select(platform, "")
		if err != nil || artifact.SHA256 != digest || artifact.Version != "3.12.15" || !strings.HasPrefix(artifact.URL, "https://github.com/astral-sh/python-build-standalone/releases/download/20261013/") {
			t.Fatalf("%s: artifact = %+v, error = %v", platform, artifact, err)
		}
	}
	// The Linux runtimes are glibc builds: musl systems such as Alpine get an
	// explicit unsupported error instead of a runtime that cannot start.
	for _, platform := range []string{"linux-arm64", "linux-amd64"} {
		if _, err := manifest.Select(platform, "glibc"); err != nil {
			t.Fatalf("%s glibc: %v", platform, err)
		}
		if _, err := manifest.Select(platform, "musl"); err == nil || !strings.Contains(err.Error(), "(musl)") || !strings.Contains(err.Error(), "VOLCENGINE_SCRIPT_PYTHON") {
			t.Fatalf("%s musl: expected an explicit unsupported error, got %v", platform, err)
		}
	}
	for _, platform := range []string{"linux-386", "linux-arm", "freebsd-amd64", "windows-arm64", "windows-386"} {
		if _, err := manifest.Select(platform, ""); err == nil || !strings.Contains(err.Error(), "VOLCENGINE_SCRIPT_PYTHON") {
			t.Fatalf("%s: expected an explicit unsupported-platform error, got %v", platform, err)
		}
	}
}

func TestParseManifestRejectsUnsafeEntries(t *testing.T) {
	valid := `{"platform":"linux-amd64","build":"20261013","implementation":"cpython","version":"3.12.1","url":"https://example.com/a.tar.gz","sha256":"` + strings.Repeat("a", 64) + `","size":1,"format":"tar.gz","executable":"python/bin/python3"}`
	cases := map[string]string{
		"http url":     strings.Replace(valid, "https://", "http://", 1),
		"short digest": strings.Replace(valid, strings.Repeat("a", 64), "abc", 1),
		"escaping exe": strings.Replace(valid, "python/bin/python3", "../python3", 1),
		"absolute exe": strings.Replace(valid, "python/bin/python3", "/usr/bin/python3", 1),
		"zip format":   strings.Replace(valid, `"format":"tar.gz"`, `"format":"zip"`, 1),
		"pypy":         strings.Replace(valid, `"cpython"`, `"pypy"`, 1),
		"unknown key":  strings.Replace(valid, `"size":1`, `"size":1,"extra":true`, 1),
	}
	if _, err := ParseManifest([]byte(`{"schema_version":1,"runtimes":[` + valid + `]}`)); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	for name, entry := range cases {
		if _, err := ParseManifest([]byte(`{"schema_version":1,"runtimes":[` + entry + `]}`)); err == nil {
			t.Fatalf("%s: manifest was accepted", name)
		}
	}
	if _, err := ParseManifest([]byte(`{"schema_version":2,"runtimes":[]}`)); err == nil {
		t.Fatal("unsupported schema version was accepted")
	}
}

func TestProbeValidatesInterpreters(t *testing.T) {
	python := testPython(t)
	if version := probeVersion(t, python); !strings.HasPrefix(version, "3.") {
		t.Fatalf("version = %s", version)
	}
	if _, err := Probe(context.Background(), "python3"); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative path error = %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	directory := t.TempDir()
	old := filepath.Join(directory, "python-old")
	if err := os.WriteFile(old, []byte("#!/bin/sh\necho '{\"implementation\":\"cpython\",\"version\":\"3.8.18\",\"major\":3,\"minor\":8}'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := Probe(context.Background(), old); err == nil || !strings.Contains(err.Error(), "3.9") {
		t.Fatalf("old interpreter error = %v", err)
	}
	pypy := filepath.Join(directory, "pypy")
	if err := os.WriteFile(pypy, []byte("#!/bin/sh\necho '{\"implementation\":\"pypy\",\"version\":\"3.10.0\",\"major\":3,\"minor\":10}'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := Probe(context.Background(), pypy); err == nil || !strings.Contains(err.Error(), "requires CPython") {
		t.Fatalf("pypy error = %v", err)
	}
	plain := filepath.Join(directory, "not-executable")
	if err := os.WriteFile(plain, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Probe(context.Background(), plain); err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("non-executable error = %v", err)
	}
}

// wrapperArchive builds a small archive whose interpreter is a shell wrapper
// around the real test Python, with the same symlink topology as
// python-build-standalone Unix artifacts.
func wrapperArchive(t *testing.T) []byte {
	t.Helper()
	python := testPython(t)
	return buildTarGz(t, []tarEntry{
		{name: "python/", kind: tar.TypeDir, mode: 0755},
		{name: "python/bin/", kind: tar.TypeDir, mode: 0755},
		{name: "python/bin/python3.12", kind: tar.TypeReg, mode: 0755, body: "#!/bin/sh\nexec '" + python + "' \"$@\"\n"},
		{name: "python/bin/python3", kind: tar.TypeSymlink, linkname: "python3.12"},
		{name: "python/share/terminfo/1/", kind: tar.TypeDir, mode: 0755},
		{name: "python/share/terminfo/a/adm1178", kind: tar.TypeReg, body: "terminfo"},
		{name: "python/share/terminfo/1/1178", kind: tar.TypeSymlink, linkname: "../a/adm1178"},
	})
}

func TestInstallerDownloadsVerifiesExtractsAndActivates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the wrapper interpreter is a POSIX shell script")
	}
	data := wrapperArchive(t)
	server, downloads := serveArchive(t, data)
	root := filepath.Join(t.TempDir(), "script-runtime")
	installer := &Installer{Root: root, Client: server.Client()}
	version := probeVersion(t, testPython(t))
	artifact := artifactFor(server.URL+"/python.tar.gz", data, version, "python/bin/python3.12")

	result, err := installer.Install(context.Background(), artifact)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if !result.Downloaded || *downloads != 1 || !isWithin(root, result.State.Python) {
		t.Fatalf("result = %+v, downloads = %d", result, *downloads)
	}
	if target, err := os.Readlink(filepath.Join(filepath.Dir(result.State.Python), "python3")); err != nil || target != "python3.12" {
		t.Fatalf("python3 link = %q, %v", target, err)
	}
	if data, err := os.ReadFile(filepath.Join(filepath.Dir(result.State.Python), "..", "share", "terminfo", "1", "1178")); err != nil || string(data) != "terminfo" {
		t.Fatalf("relative ../ link = %q, %v", data, err)
	}

	setPath(t, t.TempDir())
	interpreter, err := Resolve(context.Background(), "", root)
	if err != nil || interpreter.Source != SourceManaged || interpreter.Path != result.State.Python {
		t.Fatalf("Resolve() = %+v, %v", interpreter, err)
	}

	again, err := installer.Install(context.Background(), artifact)
	if err != nil || again.Downloaded || *downloads != 1 {
		t.Fatalf("reinstall = %+v, %v, downloads = %d", again, err, *downloads)
	}
	leftovers, _ := filepath.Glob(filepath.Join(root, ".*"))
	if len(leftovers) != 0 {
		t.Fatalf("temporary files left behind: %v", leftovers)
	}
}

func TestInstallerRejectsTamperedOrUnsafeArchives(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixtures require a POSIX host")
	}
	good := wrapperArchive(t)
	version := probeVersion(t, testPython(t))
	unsafe := map[string][]tarEntry{
		"absolute path":     {{name: "/tmp/evil", kind: tar.TypeReg, body: "x"}},
		"parent path":       {{name: "python/../../evil", kind: tar.TypeReg, body: "x"}},
		"absolute link":     {{name: "python/link", kind: tar.TypeSymlink, linkname: "/etc/passwd"}},
		"escaping link":     {{name: "python/link", kind: tar.TypeSymlink, linkname: "../../outside"}},
		"hard link":         {{name: "python/hard", kind: tar.TypeLink, linkname: "python/bin/python3.12"}},
		"device":            {{name: "python/dev", kind: tar.TypeChar}},
		"write through dir": {{name: "python/dir", kind: tar.TypeSymlink, linkname: "."}, {name: "python/dir/file", kind: tar.TypeReg, body: "x"}},
		// Each link is lexically inside the root, but following "here" first
		// makes "escape" resolve one level above it.
		"chained escape": {
			{name: "here", kind: tar.TypeSymlink, linkname: "."},
			{name: "escape", kind: tar.TypeSymlink, linkname: "here/../outside"},
		},
	}
	for name, entries := range unsafe {
		data := buildTarGz(t, entries)
		server, _ := serveArchive(t, data)
		root := filepath.Join(t.TempDir(), "script-runtime")
		installer := &Installer{Root: root, Client: server.Client()}
		if _, err := installer.Install(context.Background(), artifactFor(server.URL+"/a.tar.gz", data, version, "python/bin/python3.12")); err == nil {
			t.Fatalf("%s: archive was installed", name)
		}
		if state, _ := ReadState(root); state != nil {
			t.Fatalf("%s: runtime was activated", name)
		}
		if _, err := os.Lstat(filepath.Join(filepath.Dir(root), "outside")); err == nil {
			t.Fatalf("%s: wrote outside the runtime directory", name)
		}
	}

	server, _ := serveArchive(t, good)
	installer := &Installer{Root: filepath.Join(t.TempDir(), "rt"), Client: server.Client()}
	tampered := artifactFor(server.URL+"/a.tar.gz", good, version, "python/bin/python3.12")
	tampered.SHA256 = strings.Repeat("0", 64)
	if _, err := installer.Install(context.Background(), tampered); err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("digest mismatch error = %v", err)
	}
	resized := artifactFor(server.URL+"/a.tar.gz", good, version, "python/bin/python3.12")
	resized.Size++
	if _, err := installer.Install(context.Background(), resized); err == nil || !strings.Contains(err.Error(), "bytes") {
		t.Fatalf("size mismatch error = %v", err)
	}
	wrongVersion := artifactFor(server.URL+"/a.tar.gz", good, "3.99.0", "python/bin/python3.12")
	if _, err := installer.Install(context.Background(), wrongVersion); err == nil || !strings.Contains(err.Error(), "pins 3.99.0") {
		t.Fatalf("version mismatch error = %v", err)
	}
	if state, _ := ReadState(installer.Root); state != nil {
		t.Fatal("a rejected artifact was activated")
	}
}

func TestInstallerRefusesPlainHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("plain HTTP server must not be contacted")
	}))
	defer server.Close()
	installer := NewInstaller(t.TempDir())
	artifact := artifactFor(server.URL+"/a.tar.gz", []byte("x"), "3.12.1", "python/bin/python3")
	if _, err := installer.Install(context.Background(), artifact); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("plain HTTP error = %v", err)
	}
	if _, err := installer.LoadManifest(context.Background(), server.URL+"/manifest.json", ""); err == nil {
		t.Fatal("plain HTTP manifest was accepted")
	}

	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, server.URL+"/a.tar.gz", http.StatusFound)
	}))
	defer redirect.Close()
	client := redirect.Client()
	client.CheckRedirect = httpsOnlyRedirect
	installer = &Installer{Root: t.TempDir(), Client: client}
	if _, err := installer.Install(context.Background(), artifactFor(redirect.URL+"/a.tar.gz", []byte("x"), "3.12.1", "python/bin/python3")); err == nil || !strings.Contains(err.Error(), "non-HTTPS redirect") {
		t.Fatalf("redirect error = %v", err)
	}
}

func TestLoadManifestFromFileVerifiesDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, defaultManifest, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(defaultManifest)
	installer := NewInstaller(t.TempDir())
	if _, err := installer.LoadManifest(context.Background(), path, hex.EncodeToString(digest[:])); err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if _, err := installer.LoadManifest(context.Background(), path, strings.Repeat("1", 64)); err == nil {
		t.Fatal("manifest digest mismatch was accepted")
	}
	if _, err := installer.LoadManifest(context.Background(), "ftp://example.com/manifest.json", ""); err == nil {
		t.Fatal("unsupported manifest scheme was accepted")
	}
}

func TestResolvePrefersExplicitAndReportsUnprepared(t *testing.T) {
	root := filepath.Join(t.TempDir(), "script-runtime")
	setPath(t, t.TempDir())
	if _, err := Resolve(context.Background(), "", root); err != ErrNotPrepared {
		t.Fatalf("unprepared error = %v", err)
	}
	python := testPython(t)
	interpreter, err := Resolve(context.Background(), python, root)
	if err != nil || interpreter.Source != SourceExplicit || interpreter.Path != python {
		t.Fatalf("explicit = %+v, %v", interpreter, err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(root, stateFile), State{Python: python, Version: "3.12.15"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(context.Background(), "", root); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("state outside root error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, stateFile), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(context.Background(), "", root); err == nil || !strings.Contains(err.Error(), "corrupted") {
		t.Fatalf("corrupted state error = %v", err)
	}
}

// TestInstallerRealPythonBuildStandaloneArchive installs a real pinned
// python-build-standalone archive. Point VOLCENGINE_TEST_RUNTIME_ARCHIVE at a
// downloaded copy of this platform's manifest artifact; it is served over a
// local TLS server so the production HTTPS-only path is exercised. Set
// VOLCENGINE_TEST_RUNTIME_DOWNLOAD=1 instead to download from the manifest URL.
func TestInstallerRealPythonBuildStandaloneArchive(t *testing.T) {
	archivePath := os.Getenv("VOLCENGINE_TEST_RUNTIME_ARCHIVE")
	download := os.Getenv("VOLCENGINE_TEST_RUNTIME_DOWNLOAD") == "1"
	if archivePath == "" && !download {
		t.Skip("set VOLCENGINE_TEST_RUNTIME_ARCHIVE or VOLCENGINE_TEST_RUNTIME_DOWNLOAD=1")
	}
	manifest, err := DefaultManifest()
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := manifest.Select(CurrentPlatform(), CurrentLibc())
	if err != nil {
		t.Fatal(err)
	}
	installer := NewInstaller(filepath.Join(t.TempDir(), "script-runtime"))
	if !download {
		data, err := os.ReadFile(archivePath)
		if err != nil {
			t.Fatal(err)
		}
		server, _ := serveArchive(t, data)
		artifact.URL = server.URL + "/" + filepath.Base(archivePath)
		installer.Client = server.Client()
	}
	result, err := installer.Install(context.Background(), artifact)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	setPath(t, t.TempDir())
	interpreter, err := Resolve(context.Background(), "", installer.Root)
	if err != nil || interpreter.Version != artifact.Version {
		t.Fatalf("Resolve() = %+v, %v", interpreter, err)
	}
	// The stdlib modules the worker and typical scripts need must load from
	// the extracted tree, through the archive's own symlink layout.
	output, err := exec.Command(filepath.Join(filepath.Dir(result.State.Python), "python3"), "-I", "-S", "-c",
		"import asyncio, json, ssl, sqlite3, decimal, zlib, sys; print(sys.prefix)").CombinedOutput()
	prefix, _ := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(result.State.Python)))
	if err != nil || strings.TrimSpace(string(output)) != prefix {
		t.Fatalf("stdlib check output = %q, error = %v", output, err)
	}
}

// Every manifest field that becomes part of the install path must be
// validated, and the activation directory must be a direct child of Root
// before anything is removed or renamed.
func TestInstallerRejectsPathFieldsThatEscapeRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the wrapper interpreter is a POSIX shell script")
	}
	data := wrapperArchive(t)
	server, downloads := serveArchive(t, data)
	version := probeVersion(t, testPython(t))
	parent := t.TempDir()
	root := filepath.Join(parent, "runtime")
	sentinel := filepath.Join(parent, "outside-test-platform-keep")
	if err := os.MkdirAll(sentinel, 0700); err != nil {
		t.Fatal(err)
	}
	installer := &Installer{Root: root, Client: server.Client()}
	unsafe := map[string]func(*Artifact){
		"unix parent build":     func(a *Artifact) { a.Build = "../../../outside" },
		"unix absolute build":   func(a *Artifact) { a.Build = "/tmp/outside" },
		"windows parent build":  func(a *Artifact) { a.Build = `..\..\outside` },
		"windows volume build":  func(a *Artifact) { a.Build = `C:\outside` },
		"windows drive build":   func(a *Artifact) { a.Build = "C:outside" },
		"unc build":             func(a *Artifact) { a.Build = `\\server\share` },
		"dot build":             func(a *Artifact) { a.Build = ".." },
		"nul build":             func(a *Artifact) { a.Build = "a\x00b" },
		"colon platform":        func(a *Artifact) { a.Platform = "c:linux-amd64" },
		"dot platform":          func(a *Artifact) { a.Platform = "../linux" },
		"windows sep platform":  func(a *Artifact) { a.Platform = `linux\amd64` },
		"uppercase escape plat": func(a *Artifact) { a.Platform = "linux-amd64/.." },
	}
	for name, mutate := range unsafe {
		artifact := artifactFor(server.URL+"/python.tar.gz", data, version, "python/bin/python3.12")
		mutate(&artifact)
		if _, err := installer.Install(context.Background(), artifact); err == nil {
			t.Fatalf("%s: artifact with unsafe path fields was installed", name)
		}
		if _, err := ParseManifest([]byte(`{"schema_version":1,"runtimes":[` + mustJSON(t, artifact) + `]}`)); err == nil {
			t.Fatalf("%s: manifest with unsafe path fields was accepted", name)
		}
	}
	if *downloads != 0 {
		t.Fatalf("unsafe artifacts were downloaded %d times", *downloads)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("a directory outside the root was touched: %v", err)
	}
	entries, _ := filepath.Glob(filepath.Join(parent, "*"))
	for _, entry := range entries {
		if entry != root && entry != sentinel {
			t.Fatalf("unexpected entry next to the root: %s", entry)
		}
	}

	// Verified date builds and dotted build labels stay valid.
	for _, build := range []string{"20261013", "20261013.1", "test_build-2"} {
		artifact := artifactFor(server.URL+"/python.tar.gz", data, version, "python/bin/python3.12")
		artifact.Build = build
		result, err := installer.Install(context.Background(), artifact)
		if err != nil || filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(result.State.Python)))) != root {
			t.Fatalf("build %q: result = %+v, error = %v", build, result, err)
		}
	}
}

func mustJSON(t *testing.T, value interface{}) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDetectLibc(t *testing.T) {
	dir := t.TempDir()
	loaders := filepath.Join(dir, "ld-musl-*.so.1")
	if got := detectLibc("linux", loaders); got != "glibc" {
		t.Fatalf("linux without a musl loader = %q, want glibc", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "ld-musl-aarch64.so.1"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := detectLibc("linux", loaders); got != "musl" {
		t.Fatalf("linux with a musl loader = %q, want musl", got)
	}
	if got := detectLibc("darwin", loaders); got != "" {
		t.Fatalf("darwin = %q, want no libc", got)
	}
}
