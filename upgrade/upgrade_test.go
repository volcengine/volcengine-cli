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

func TestDoUpgrade_InstallsTargetVersion(t *testing.T) {
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
	var stdout bytes.Buffer
	err := DoUpgrade(Options{
		CurrentVersion: "1.2.3",
		TargetVersion:  "1.2.3",
		Yes:            true,
		Stdout:         &stdout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "already using") {
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
