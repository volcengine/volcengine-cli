package upgrade

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// forceStandaloneDetect 固定识别为 standalone，避免环境变量或路径启发式干扰 DoUpgrade 测试。
func forceStandaloneDetect(t *testing.T) {
	t.Helper()
	orig := detectInstallFunc
	detectInstallFunc = func(p string) InstallInfo {
		return standaloneInfo(p, DetectedByDefault)
	}
	t.Cleanup(func() { detectInstallFunc = orig })
}

// forceNPMDetect 固定识别为 npm，不依赖真实目录布局。
func forceNPMDetect(t *testing.T) {
	t.Helper()
	orig := detectInstallFunc
	detectInstallFunc = func(p string) InstallInfo {
		return npmInfo(p)
	}
	t.Cleanup(func() { detectInstallFunc = orig })
}

func platformOKCmd() *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/C", "echo OUT& echo ERR 1>&2")
	}
	return exec.Command("sh", "-c", "echo OUT; echo ERR >&2")
}

func platformFailCmd() *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/C", "echo OUT& echo ERR 1>&2& exit /b 1")
	}
	return exec.Command("sh", "-c", "echo OUT; echo ERR >&2; exit 1")
}

// stubExecRecording records argv and returns platform OK/fail commands that
// write distinct markers to stdout/stderr so writer wiring can be asserted.
func stubExecRecording(t *testing.T, calls *[][]string, ok bool) {
	t.Helper()
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = func(name string, arg ...string) *exec.Cmd {
		*calls = append(*calls, append([]string{name}, arg...))
		if ok {
			return platformOKCmd()
		}
		return platformFailCmd()
	}
}

// blockUpgradeDownloads fails any HTTP download if the npm path incorrectly
// falls through to standalone asset resolution.
func blockUpgradeDownloads(t *testing.T) (downloadHit *bool) {
	t.Helper()
	hit := false
	downloadHit = &hit
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		hit = true
		http.Error(w, "no", 500)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	oldEnv := os.Getenv(EnvDownloadBaseURL)
	os.Setenv(EnvDownloadBaseURL, srv.URL)
	t.Cleanup(func() { os.Setenv(EnvDownloadBaseURL, oldEnv) })
	SetHTTPClient(srv.Client())
	t.Cleanup(func() { SetHTTPClient(&http.Client{Timeout: DefaultHTTPTimeout}) })
	return downloadHit
}

func TestDoUpgrade_InstallsTargetVersion(t *testing.T) {
	forceStandaloneDetect(t)
	configDir := t.TempDir()
	origConfigDirFunc := ConfigDirFunc
	ConfigDirFunc = func() (string, error) { return configDir, nil }
	defer func() { ConfigDirFunc = origConfigDirFunc }()

	// Fake binary content and zip
	payload := []byte("fake-ve-binary-v2")
	binName := BinaryName()
	zipBuf := &bytes.Buffer{}
	zw := zip.NewWriter(zipBuf)
	w, err := zw.Create(binName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zipBytes := zipBuf.Bytes()
	sum := sha256.Sum256(zipBytes)
	hexSum := hex.EncodeToString(sum[:])

	version := "9.9.9"
	archive := ArchiveName(version, runtime.GOOS, runtime.GOARCH)
	checksumName := ChecksumName(version)
	sumsBody := fmt.Sprintf("%s  %s\n", hexSum, archive)

	mux := http.NewServeMux()
	mux.HandleFunc("/version_manifest.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"latest":"%s"}`, version)
	})
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, version)
	})
	mux.HandleFunc("/v"+version+"/"+archive, func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipBytes)
	})
	mux.HandleFunc("/v"+version+"/"+checksumName, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, sumsBody)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Point CDN base to test server via env
	oldEnv := os.Getenv(EnvDownloadBaseURL)
	os.Setenv(EnvDownloadBaseURL, srv.URL)
	defer os.Setenv(EnvDownloadBaseURL, oldEnv)

	SetHTTPClient(srv.Client())
	SetCheckHTTPClient(srv.Client())
	defer func() {
		SetHTTPClient(&http.Client{Timeout: DefaultHTTPTimeout})
		SetCheckHTTPClient(&http.Client{Timeout: CheckHTTPTimeout})
	}()

	dir := t.TempDir()
	execPath := filepath.Join(dir, binName)
	if err := ioutil.WriteFile(execPath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}

	origExec := execCommand
	defer func() { execCommand = origExec }()
	execCommand = func(name string, arg ...string) *exec.Cmd {
		if runtime.GOOS == "windows" {
			return exec.Command("cmd", "/C", "echo "+version)
		}
		return exec.Command("echo", version)
	}

	var stdout bytes.Buffer
	err = DoUpgrade(Options{
		CurrentVersion: "1.0.0",
		TargetVersion:  version,
		Yes:            true,
		Stdout:         &stdout,
		ExecPath:       execPath,
	})
	if err != nil {
		t.Fatalf("DoUpgrade: %v\n%s", err, stdout.String())
	}
	got, err := ioutil.ReadFile(execPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("installed content mismatch: %q", got)
	}
	if !strings.Contains(stdout.String(), "Successfully upgraded") {
		t.Fatalf("stdout: %s", stdout.String())
	}
}

func TestDoUpgrade_ChecksumMismatch(t *testing.T) {
	forceStandaloneDetect(t)
	payload := []byte("data")
	binName := BinaryName()
	zipBuf := &bytes.Buffer{}
	zw := zip.NewWriter(zipBuf)
	w, _ := zw.Create(binName)
	w.Write(payload)
	zw.Close()
	zipBytes := zipBuf.Bytes()

	version := "8.8.8"
	archive := ArchiveName(version, runtime.GOOS, runtime.GOARCH)
	checksumName := ChecksumName(version)
	// wrong hash
	sumsBody := fmt.Sprintf("%s  %s\n", strings.Repeat("0", 64), archive)

	mux := http.NewServeMux()
	mux.HandleFunc("/v"+version+"/"+archive, func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipBytes)
	})
	mux.HandleFunc("/v"+version+"/"+checksumName, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, sumsBody)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	oldDownloadBase := os.Getenv(EnvDownloadBaseURL)
	os.Setenv(EnvDownloadBaseURL, srv.URL)
	defer os.Setenv(EnvDownloadBaseURL, oldDownloadBase)
	SetHTTPClient(srv.Client())
	defer SetHTTPClient(&http.Client{Timeout: DefaultHTTPTimeout})

	dir := t.TempDir()
	execPath := filepath.Join(dir, binName)
	ioutil.WriteFile(execPath, []byte("old"), 0755)

	err := DoUpgrade(Options{
		CurrentVersion: "1.0.0",
		TargetVersion:  version,
		Yes:            true,
		Stdout:         ioutil.Discard,
		ExecPath:       execPath,
	})
	if err == nil {
		t.Fatal("expected checksum error")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err: %v", err)
	}
	got, _ := ioutil.ReadFile(execPath)
	if string(got) != "old" {
		t.Fatal("binary should be unchanged")
	}
}

func TestDoUpgrade_AlreadyLatest(t *testing.T) {
	forceStandaloneDetect(t)
	dir := t.TempDir()
	execPath := filepath.Join(dir, BinaryName())
	if err := ioutil.WriteFile(execPath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err := DoUpgrade(Options{
		CurrentVersion: "1.2.3",
		TargetVersion:  "1.2.3",
		Yes:            true,
		Stdout:         &stdout,
		ExecPath:       execPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "already using") {
		t.Fatal(stdout.String())
	}
}

func TestDoUpgrade_NPMDelegatesToNpm(t *testing.T) {
	forceNPMDetect(t)
	downloadHit := blockUpgradeDownloads(t)

	var calls [][]string
	stubExecRecording(t, &calls, true)

	var stdout, stderr bytes.Buffer
	err := DoUpgrade(Options{
		CurrentVersion: "1.0.0",
		TargetVersion:  "9.9.9",
		Yes:            true,
		Stdout:         &stdout,
		Stderr:         &stderr,
		ExecPath:       filepath.Join(t.TempDir(), "ve"),
	})
	if err != nil {
		t.Fatalf("expected nil error for npm delegate, got %v", err)
	}
	if *downloadHit {
		t.Fatal("npm path must not download")
	}
	if len(calls) != 1 {
		t.Fatalf("calls: %#v", calls)
	}
	if calls[0][0] != "npm" || calls[0][1] != "install" || calls[0][2] != "-g" ||
		calls[0][3] != "@volcengine/cli@9.9.9" {
		t.Fatalf("unexpected npm call: %#v", calls[0])
	}
	out := stdout.String()
	if !strings.Contains(out, "npm install -g @volcengine/cli@9.9.9") {
		t.Fatalf("stdout should show pinned npm command: %s", out)
	}
	if !strings.Contains(out, "delegating to npm") {
		t.Fatalf("stdout should mention delegating: %s", out)
	}
	if !strings.Contains(out, "npm upgrade complete") {
		t.Fatalf("stdout should report success: %s", out)
	}
	if !strings.Contains(out, "OUT") {
		t.Fatalf("npm child stdout should be wired: %s", out)
	}
	if !strings.Contains(stderr.String(), "ERR") {
		t.Fatalf("npm child stderr should be wired: %s", stderr.String())
	}
	if strings.Contains(out, "--force") {
		t.Fatalf("npm path must not mention --force:\n%s", out)
	}
}

func TestDoUpgrade_NPMExecFailurePrintsManualCmd(t *testing.T) {
	forceNPMDetect(t)
	downloadHit := blockUpgradeDownloads(t)

	var calls [][]string
	stubExecRecording(t, &calls, false)

	var stdout, stderr bytes.Buffer
	err := DoUpgrade(Options{
		CurrentVersion: "1.0.0",
		TargetVersion:  "9.9.9",
		Yes:            true,
		Stdout:         &stdout,
		Stderr:         &stderr,
		ExecPath:       filepath.Join(t.TempDir(), "ve"),
	})
	if err == nil {
		t.Fatal("expected error when npm exec fails")
	}
	if *downloadHit {
		t.Fatal("npm path must not download on failure")
	}
	if len(calls) != 1 {
		t.Fatalf("expected one npm attempt, got %#v", calls)
	}
	out := stdout.String()
	if !strings.Contains(out, "delegating to npm") {
		t.Fatalf("stdout should show progress before failure:\n%s", out)
	}
	if !strings.Contains(out, "npm install -g @volcengine/cli@9.9.9") {
		t.Fatalf("stdout should show the delegated command:\n%s", out)
	}
	if !strings.Contains(stderr.String(), "ERR") {
		t.Fatalf("npm child stderr should be wired on failure: %s", stderr.String())
	}
	msg := err.Error()
	if !strings.Contains(msg, "npm upgrade failed") {
		t.Fatalf("error should mention npm upgrade failed: %v", err)
	}
	if !strings.Contains(msg, "npm install -g @volcengine/cli@9.9.9") {
		t.Fatalf("error should include manual install command: %v", err)
	}
	if !strings.Contains(msg, "To upgrade manually") {
		t.Fatalf("error should guide manual upgrade: %v", err)
	}
}

func TestDoUpgrade_NPMUnpinnedUsesLatestSpec(t *testing.T) {
	forceNPMDetect(t)
	downloadHit := blockUpgradeDownloads(t)

	var calls [][]string
	stubExecRecording(t, &calls, true)

	var stdout bytes.Buffer
	err := DoUpgrade(Options{
		CurrentVersion: "1.0.0",
		Yes:            true,
		Stdout:         &stdout,
		ExecPath:       filepath.Join(t.TempDir(), "ve"),
	})
	if err != nil {
		t.Fatalf("DoUpgrade: %v\n%s", err, stdout.String())
	}
	if *downloadHit {
		t.Fatal("unpinned npm path must not download")
	}
	if len(calls) != 1 {
		t.Fatalf("calls: %#v", calls)
	}
	if calls[0][0] != "npm" || calls[0][3] != "@volcengine/cli@latest" {
		t.Fatalf("expected @latest package spec, got %#v", calls[0])
	}
	if !strings.Contains(stdout.String(), "npm install -g @volcengine/cli@latest") {
		t.Fatalf("stdout: %s", stdout.String())
	}
}

func TestDoUpgrade_NPMSameVersionPinStillDelegates(t *testing.T) {
	// Same pin is allowed by rejectOlderTarget; npm reinstall is intentional
	// (unlike standalone "already using" short-circuit).
	forceNPMDetect(t)
	var calls [][]string
	stubExecRecording(t, &calls, true)

	var stdout bytes.Buffer
	err := DoUpgrade(Options{
		CurrentVersion: "1.0.0",
		TargetVersion:  "1.0.0",
		Yes:            true,
		Stdout:         &stdout,
		ExecPath:       filepath.Join(t.TempDir(), "ve"),
	})
	if err != nil {
		t.Fatalf("DoUpgrade: %v\n%s", err, stdout.String())
	}
	if len(calls) != 1 || calls[0][3] != "@volcengine/cli@1.0.0" {
		t.Fatalf("same-version pin should still delegate to npm: %#v", calls)
	}
}

func TestDoUpgrade_NPMRejectsIncomparablePin(t *testing.T) {
	forceNPMDetect(t)
	var calls [][]string
	stubExecRecording(t, &calls, true)

	err := DoUpgrade(Options{
		CurrentVersion: "1.0.0",
		TargetVersion:  "dev-build",
		Yes:            true,
		Stdout:         ioutil.Discard,
		ExecPath:       filepath.Join(t.TempDir(), "ve"),
	})
	if err == nil {
		t.Fatal("expected error for incomparable npm pin")
	}
	msg := err.Error()
	if !strings.Contains(msg, "must be newer") {
		t.Fatalf("expected not-newer wording, got: %v", err)
	}
	if strings.Contains(msg, "older version") {
		t.Fatalf("incomparable target must not claim older: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("npm must not run for incomparable pin: %#v", calls)
	}
}

func TestDoUpgrade_HomebrewRejectsVersionPin(t *testing.T) {
	origDetect := detectInstallFunc
	origExec := execCommand
	defer func() {
		detectInstallFunc = origDetect
		execCommand = origExec
	}()
	detectInstallFunc = func(path string) InstallInfo {
		return homebrewInfo(path)
	}
	brewCalled := false
	execCommand = func(name string, arg ...string) *exec.Cmd {
		if name == "brew" {
			brewCalled = true
		}
		if runtime.GOOS == "windows" {
			return exec.Command("cmd", "/C", "echo ok")
		}
		return exec.Command("echo", "ok")
	}
	err := DoUpgrade(Options{
		CurrentVersion: "1.0.0",
		TargetVersion:  "1.0.40",
		Yes:            true,
		Stdout:         ioutil.Discard,
		ExecPath:       filepath.Join(t.TempDir(), "ve"),
	})
	if err == nil {
		t.Fatal("expected error for brew + --version")
	}
	msg := err.Error()
	if !strings.Contains(msg, "--version") {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(msg, "1.0.40") {
		t.Fatalf("error should include pinned version: %v", err)
	}
	if !strings.Contains(msg, "brew") {
		t.Fatalf("error should direct users to brew: %v", err)
	}
	if strings.Contains(msg, "--force") {
		t.Fatalf("error must not suggest --force: %v", err)
	}
	if brewCalled {
		t.Fatal("brew must not run when --version is rejected")
	}
}

func TestDoUpgrade_HomebrewDelegatesToBrew(t *testing.T) {
	origDetect := detectInstallFunc
	origExec := execCommand
	defer func() {
		detectInstallFunc = origDetect
		execCommand = origExec
	}()
	detectInstallFunc = func(path string) InstallInfo {
		return homebrewInfo(path)
	}

	var calls [][]string
	execCommand = func(name string, arg ...string) *exec.Cmd {
		calls = append(calls, append([]string{name}, arg...))
		if runtime.GOOS == "windows" {
			return exec.Command("cmd", "/C", "echo ok")
		}
		return exec.Command("echo", "ok")
	}

	var stdout bytes.Buffer
	err := DoUpgrade(Options{
		CurrentVersion: "1.0.0",
		Yes:            true,
		Stdout:         &stdout,
		ExecPath:       filepath.Join(t.TempDir(), "ve"),
	})
	if err != nil {
		t.Fatalf("DoUpgrade: %v\n%s", err, stdout.String())
	}
	if len(calls) != 2 {
		t.Fatalf("calls: %#v", calls)
	}
	if calls[0][0] != "brew" || calls[0][1] != "update" {
		t.Fatalf("first call: %#v", calls[0])
	}
	if calls[1][0] != "brew" || calls[1][1] != "upgrade" || calls[1][2] != HomebrewFormula {
		t.Fatalf("second call: %#v", calls[1])
	}
	if !strings.Contains(stdout.String(), "Homebrew upgrade complete") {
		t.Fatal(stdout.String())
	}
}

func TestRefreshVersionCacheAfterInstall_PinInvalidatesOnResolveFail(t *testing.T) {
	dir := t.TempDir()
	orig := ConfigDirFunc
	ConfigDirFunc = func() (string, error) { return dir, nil }
	defer func() { ConfigDirFunc = orig }()

	// Poison cache as if a bad pin wrote latest=installed.
	if err := SaveCheckCache("1.0.40", "1.0.40"); err != nil {
		t.Fatal(err)
	}
	// Point CDN to a closed server so quick resolve fails.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", 500)
	}))
	// Close immediately to force connection errors from quick client.
	srv.Close()

	oldEnv := os.Getenv(EnvDownloadBaseURL)
	os.Setenv(EnvDownloadBaseURL, srv.URL)
	defer os.Setenv(EnvDownloadBaseURL, oldEnv)
	SetCheckHTTPClient(&http.Client{Timeout: 50 * time.Millisecond})
	defer SetCheckHTTPClient(&http.Client{Timeout: CheckHTTPTimeout})

	refreshVersionCacheAfterInstall(true, "1.0.40")
	if _, ok := LoadCheckCache(); ok {
		t.Fatal("expected cache invalidated when pin install cannot re-resolve latest")
	}
}
