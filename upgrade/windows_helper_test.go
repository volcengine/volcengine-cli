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
)

func TestRunWindowsUpgradeHelperWaitsBeforeReplacing(t *testing.T) {
	configDir := t.TempDir()
	originalConfigDir := ConfigDirFunc
	ConfigDirFunc = func() (string, error) { return configDir, nil }
	defer func() { ConfigDirFunc = originalConfigDir }()

	originalWait := waitForParentProcess
	originalReplace := replaceBinaryWithBackup
	originalCleanup := startWindowsUpgradeCleanup
	defer func() {
		waitForParentProcess = originalWait
		replaceBinaryWithBackup = originalReplace
		startWindowsUpgradeCleanup = originalCleanup
	}()

	waited := false
	waitForParentProcess = func(pid int) error {
		if pid != 1234 {
			t.Fatalf("parent pid = %d, want 1234", pid)
		}
		waited = true
		return nil
	}
	replaceBinaryWithBackup = func(newPath, currentPath, expectedVersion string) error {
		if !waited {
			t.Fatal("replacement started before the parent process exited")
		}
		if newPath != "new.exe" || currentPath != "ve.exe" || expectedVersion != "2.0.0" {
			t.Fatalf("unexpected replacement arguments: %q %q %q", newPath, currentPath, expectedVersion)
		}
		return nil
	}
	cleanupStarted := false
	startWindowsUpgradeCleanup = func(targetPath, workDir string, parentPID int) error {
		cleanupStarted = true
		if targetPath != "ve.exe" || workDir != "work" || parentPID <= 0 {
			t.Fatalf("unexpected cleanup arguments: %q %q %d", targetPath, workDir, parentPID)
		}
		return nil
	}

	var stdout bytes.Buffer
	err := RunWindowsUpgradeHelper(WindowsUpgradeHelperOptions{
		ParentPID:       1234,
		NewBinaryPath:   "new.exe",
		TargetPath:      "ve.exe",
		WorkDir:         "work",
		CurrentVersion:  "1.0.0",
		ExpectedVersion: "2.0.0",
		Stdout:          &stdout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cleanupStarted {
		t.Fatal("expected cleanup process to be scheduled")
	}
	if !strings.Contains(stdout.String(), "Successfully upgraded") {
		t.Fatalf("missing helper success output: %q", stdout.String())
	}
}

func TestLaunchWindowsUpgradeHelperCopiesExecutable(t *testing.T) {
	originalStart := startWindowsUpgradeCommand
	defer func() { startWindowsUpgradeCommand = originalStart }()

	workDir := t.TempDir()
	currentPath := filepath.Join(t.TempDir(), "ve.exe")
	newPath := filepath.Join(workDir, "ve.new.exe")
	if err := ioutil.WriteFile(currentPath, []byte("helper-binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(newPath, []byte("new-binary"), 0755); err != nil {
		t.Fatal(err)
	}

	var started *exec.Cmd
	startWindowsUpgradeCommand = func(cmd *exec.Cmd) error {
		started = cmd
		return nil
	}
	var stdout, stderr bytes.Buffer
	err := LaunchWindowsUpgradeHelper(WindowsUpgradeLaunchOptions{
		CurrentExecutable: currentPath,
		NewBinaryPath:     newPath,
		TargetPath:        currentPath,
		WorkDir:           workDir,
		CurrentVersion:    "1.0.0",
		ExpectedVersion:   "2.0.0",
		Stdout:            &stdout,
		Stderr:            &stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	if started == nil || len(started.Args) < 2 || started.Args[1] != InternalUpgradeHelperCommand {
		t.Fatalf("helper command was not started correctly: %#v", started)
	}
	helperPayload, err := ioutil.ReadFile(started.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(helperPayload) != "helper-binary" {
		t.Fatalf("helper copy = %q", helperPayload)
	}
}

func TestRunWindowsUpgradeCleanupRemovesWorkDir(t *testing.T) {
	workDir, err := ioutil.TempDir("", "ve-upgrade-cleanup-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workDir) })
	if err := ioutil.WriteFile(filepath.Join(workDir, "helper.exe"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := RunWindowsUpgradeCleanup(0, workDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Fatalf("work directory was not removed: %v", err)
	}
}

func TestShouldLaunchWindowsUpgradeHelper(t *testing.T) {
	if !shouldLaunchWindowsUpgradeHelper("windows", "") {
		t.Fatal("running Windows executable should use the helper")
	}
	if shouldLaunchWindowsUpgradeHelper("windows", "test-target.exe") {
		t.Fatal("an explicit test target must be replaced directly")
	}
	if shouldLaunchWindowsUpgradeHelper("linux", "") {
		t.Fatal("non-Windows upgrades must stay in-process")
	}
}

func TestDoUpgradeDelegatesRunningWindowsExecutableToHelper(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows helper integration")
	}
	forceStandaloneDetect(t)

	version := "2.0.0"
	binName := BinaryName()
	zipBuffer := &bytes.Buffer{}
	zipWriter := zip.NewWriter(zipBuffer)
	binaryWriter, err := zipWriter.Create(binName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := binaryWriter.Write([]byte("new-binary")); err != nil {
		t.Fatal(err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	archiveBody := zipBuffer.Bytes()
	archiveName := ArchiveName(version, runtime.GOOS, runtime.GOARCH)
	checksumName := ChecksumName(version)
	sum := sha256.Sum256(archiveBody)
	checksumBody := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), archiveName)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v" + version + "/" + archiveName:
			_, _ = w.Write(archiveBody)
		case "/v" + version + "/" + checksumName:
			_, _ = fmt.Fprint(w, checksumBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldBase := os.Getenv(EnvDownloadBaseURL)
	os.Setenv(EnvDownloadBaseURL, server.URL)
	defer os.Setenv(EnvDownloadBaseURL, oldBase)
	SetHTTPClient(server.Client())
	defer SetHTTPClient(&http.Client{Timeout: DefaultHTTPTimeout})

	originalLaunch := launchWindowsUpgradeHelper
	defer func() { launchWindowsUpgradeHelper = originalLaunch }()
	var captured WindowsUpgradeLaunchOptions
	launchWindowsUpgradeHelper = func(opts WindowsUpgradeLaunchOptions) error {
		captured = opts
		return nil
	}
	t.Cleanup(func() {
		if captured.WorkDir != "" {
			_ = os.RemoveAll(captured.WorkDir)
		}
	})

	var stdout bytes.Buffer
	err = DoUpgrade(Options{
		CurrentVersion: "1.0.0",
		TargetVersion:  version,
		Yes:            true,
		Stdout:         &stdout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if captured.WorkDir == "" || captured.NewBinaryPath == "" || captured.TargetPath == "" {
		t.Fatalf("helper was not given the prepared replacement: %+v", captured)
	}
	if _, err := os.Stat(captured.NewBinaryPath); err != nil {
		t.Fatalf("prepared replacement was cleaned before helper start: %v", err)
	}
	if strings.Contains(stdout.String(), "Successfully upgraded") {
		t.Fatalf("parent printed success before helper replacement: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "after this process exits") {
		t.Fatalf("missing deferred-install message: %q", stdout.String())
	}
}
