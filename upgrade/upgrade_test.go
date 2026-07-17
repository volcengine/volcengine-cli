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
		CurrentVersion: "10.0.0",
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

	os.Setenv(EnvDownloadBaseURL, srv.URL)
	defer os.Unsetenv(EnvDownloadBaseURL)
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

func TestDoUpgrade_NPMBlocksWithoutForce(t *testing.T) {
	// 强制识别为 npm，不依赖真实目录布局
	orig := detectInstallFunc
	defer func() { detectInstallFunc = orig }()
	detectInstallFunc = func(path string) InstallInfo {
		return npmInfo(path)
	}

	// 若错误地进入下载流程，此处请求会被标记
	downloadHit := false
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		downloadHit = true
		http.Error(w, "no", 500)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	oldEnv := os.Getenv(EnvDownloadBaseURL)
	os.Setenv(EnvDownloadBaseURL, srv.URL)
	defer os.Setenv(EnvDownloadBaseURL, oldEnv)
	SetHTTPClient(srv.Client())
	defer SetHTTPClient(&http.Client{Timeout: DefaultHTTPTimeout})

	var stdout bytes.Buffer
	err := DoUpgrade(Options{
		CurrentVersion: "1.0.0",
		TargetVersion:  "9.9.9",
		Yes:            true,
		Stdout:         &stdout,
		ExecPath:       filepath.Join(t.TempDir(), "ve"),
	})
	// 仅打印指引并成功返回，避免 root 再向 stderr 重复输出
	if err != nil {
		t.Fatalf("expected nil error for npm guidance, got %v", err)
	}
	if downloadHit {
		t.Fatal("npm path must not download")
	}
	if !strings.Contains(stdout.String(), "npm install -g @volcengine/cli@9.9.9") {
		t.Fatalf("stdout should pin version: %s", stdout.String())
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
	if !strings.Contains(err.Error(), "--version") {
		t.Fatalf("err: %v", err)
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

func TestDoUpgrade_HomebrewForceInPlace(t *testing.T) {
	// Homebrew + --force 应跳过 brew，走 CDN 原地替换
	origDetect := detectInstallFunc
	defer func() { detectInstallFunc = origDetect }()
	detectInstallFunc = func(path string) InstallInfo {
		return homebrewInfo(path)
	}

	payload := []byte("forced-homebrew-binary")
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
	version := "9.9.8"
	archive := ArchiveName(version, runtime.GOOS, runtime.GOARCH)
	checksumName := ChecksumName(version)
	sumsBody := fmt.Sprintf("%s  %s\n", hexSum, archive)

	mux := http.NewServeMux()
	mux.HandleFunc("/v"+version+"/"+archive, func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipBytes)
	})
	mux.HandleFunc("/v"+version+"/"+checksumName, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, sumsBody)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	os.Setenv(EnvDownloadBaseURL, srv.URL)
	defer os.Unsetenv(EnvDownloadBaseURL)
	SetHTTPClient(srv.Client())
	defer SetHTTPClient(&http.Client{Timeout: DefaultHTTPTimeout})

	brewCalled := false
	origExec := execCommand
	defer func() { execCommand = origExec }()
	execCommand = func(name string, arg ...string) *exec.Cmd {
		if name == "brew" {
			brewCalled = true
		}
		if runtime.GOOS == "windows" {
			return exec.Command("cmd", "/C", "echo "+version)
		}
		return exec.Command("echo", version)
	}

	dir := t.TempDir()
	execPath := filepath.Join(dir, binName)
	if err := ioutil.WriteFile(execPath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err = DoUpgrade(Options{
		CurrentVersion: "1.0.0",
		TargetVersion:  version,
		Yes:            true,
		Force:          true,
		Stdout:         &stdout,
		Stderr:         &stderr,
		ExecPath:       execPath,
	})
	if err != nil {
		t.Fatalf("DoUpgrade: %v\n%s", err, stdout.String())
	}
	if brewCalled {
		t.Fatal("brew must not run with --force")
	}
	if !strings.Contains(stderr.String(), "forcing in-place") {
		t.Fatalf("stderr: %s", stderr.String())
	}
	got, err := ioutil.ReadFile(execPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("content: %q", got)
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
