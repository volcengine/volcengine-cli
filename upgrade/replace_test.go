package upgrade

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestReplaceBinary(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "ve")
	if runtime.GOOS == "windows" {
		current += ".exe"
	}
	newPath := filepath.Join(dir, "ve.new")
	if err := ioutil.WriteFile(current, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(newPath, []byte("new-payload"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceBinary(newPath, current); err != nil {
		t.Fatal(err)
	}
	got, err := ioutil.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-payload" {
		t.Fatalf("got %q", got)
	}
	// Windows .old should be cleaned up
	if _, err := os.Stat(current + ".old"); !os.IsNotExist(err) {
		t.Fatalf("expected .old removed, err=%v", err)
	}
}

func TestReplaceBinary_WindowsCopyFailKeepsOldWhenRollbackWorks(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only rollback path")
	}
	dir := t.TempDir()
	current := filepath.Join(dir, "ve.exe")
	// Missing new file → copy fails after rename to .old; rollback should restore.
	if err := ioutil.WriteFile(current, []byte("old-bin"), 0755); err != nil {
		t.Fatal(err)
	}
	missingNew := filepath.Join(dir, "missing-new.exe")
	err := ReplaceBinary(missingNew, current)
	if err == nil {
		t.Fatal("expected error")
	}
	got, readErr := ioutil.ReadFile(current)
	if readErr != nil {
		t.Fatalf("expected current restored, read err: %v; replace err: %v", readErr, err)
	}
	if string(got) != "old-bin" {
		t.Fatalf("got %q", got)
	}
}

func TestReplaceBinaryWithBackup_SelfCheckFail(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "ve")
	if runtime.GOOS == "windows" {
		current += ".exe"
	}
	newPath := filepath.Join(dir, "ve.new")
	if err := ioutil.WriteFile(current, []byte("old-bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(newPath, []byte("new-bin"), 0755); err != nil {
		t.Fatal(err)
	}

	// Force self-check to fail by making exec.Command return a fake failure path:
	// write a non-executable "binary" that can't produce version.
	orig := execCommand
	defer func() { execCommand = orig }()
	execCommand = func(name string, arg ...string) *exec.Cmd {
		// Run a command that exits 0 but wrong output
		if runtime.GOOS == "windows" {
			return exec.Command("cmd", "/C", "echo wrong-version")
		}
		return exec.Command("echo", "wrong-version")
	}

	err := ReplaceBinaryWithBackup(newPath, current, "9.9.9")
	if err == nil {
		t.Fatal("expected self-check failure")
	}
	got, readErr := ioutil.ReadFile(current)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "old-bin" {
		t.Fatalf("expected rollback to old-bin, got %q", got)
	}
}

func TestSelfCheckVersion_ExactMatchOnly(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()
	execCommand = func(name string, arg ...string) *exec.Cmd {
		if runtime.GOOS == "windows" {
			return exec.Command("cmd", "/C", "echo 1.0.40")
		}
		return exec.Command("echo", "1.0.40")
	}
	// Prefix match must NOT pass: "1.0.4" is not "1.0.40".
	if err := SelfCheckVersion("dummy", "1.0.4"); err == nil {
		t.Fatal("expected mismatch for prefix version")
	}
	if err := SelfCheckVersion("dummy", "1.0.40"); err != nil {
		t.Fatal(err)
	}
}

func TestSelfCheckVersion_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep-based hang test is unix-oriented")
	}
	origTimeout := selfCheckTimeout
	selfCheckTimeout = 200 * time.Millisecond
	defer func() { selfCheckTimeout = origTimeout }()

	orig := execCommand
	defer func() { execCommand = orig }()
	execCommand = func(name string, arg ...string) *exec.Cmd {
		return exec.Command("sleep", "5")
	}
	err := SelfCheckVersion("dummy", "1.0.0")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestRollbackBinary_OverwriteWithoutUnlinkGap(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "ve")
	backup := filepath.Join(dir, "ve.bak")
	if err := ioutil.WriteFile(current, []byte("broken"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(backup, []byte("good"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := RollbackBinary(backup, current); err != nil {
		t.Fatal(err)
	}
	got, err := ioutil.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "good" {
		t.Fatalf("got %q, want good", got)
	}
}

func TestReplaceBinaryWithBackup_Success(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "ve")
	if runtime.GOOS == "windows" {
		current += ".exe"
	}
	newPath := filepath.Join(dir, "ve.new")
	if err := ioutil.WriteFile(current, []byte("old-bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(newPath, []byte("new-bin"), 0755); err != nil {
		t.Fatal(err)
	}

	orig := execCommand
	defer func() { execCommand = orig }()
	execCommand = func(name string, arg ...string) *exec.Cmd {
		if runtime.GOOS == "windows" {
			return exec.Command("cmd", "/C", "echo 1.2.3")
		}
		return exec.Command("echo", "1.2.3")
	}

	if err := ReplaceBinaryWithBackup(newPath, current, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	got, err := ioutil.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-bin" {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(current + ".bak"); !os.IsNotExist(err) {
		t.Fatal("expected backup removed after success")
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".ve.backup-*.bak"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary backups left after success: %v", matches)
	}
}

func TestReplaceBinaryWithBackupRejectsConcurrentUpgrade(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "ve")
	if runtime.GOOS == "windows" {
		current += ".exe"
	}
	newPath := filepath.Join(dir, "ve.new")
	if err := ioutil.WriteFile(current, []byte("old-bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(newPath, []byte("new-bin"), 0755); err != nil {
		t.Fatal(err)
	}

	lock, err := acquireUpgradeLock(current)
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}
	defer lock.release()

	err = ReplaceBinaryWithBackup(newPath, current, "2.0.0")
	if err == nil || !strings.Contains(err.Error(), "another upgrade is already in progress") {
		t.Fatalf("expected concurrent-upgrade error, got %v", err)
	}
	got, readErr := ioutil.ReadFile(current)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "old-bin" {
		t.Fatalf("current binary changed while lock held: %q", got)
	}
}

func TestReplaceBinary_UnixCopyFailCleansTemp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix temp path")
	}
	dir := t.TempDir()
	current := filepath.Join(dir, "ve")
	if err := ioutil.WriteFile(current, []byte("old-bin"), 0755); err != nil {
		t.Fatal(err)
	}
	missingNew := filepath.Join(dir, "missing-new")
	err := ReplaceBinary(missingNew, current)
	if err == nil {
		t.Fatal("expected error when new binary is missing")
	}
	got, readErr := ioutil.ReadFile(current)
	if readErr != nil {
		t.Fatalf("expected current binary intact: %v", readErr)
	}
	if string(got) != "old-bin" {
		t.Fatalf("got %q, want old-bin", got)
	}
	entries, listErr := os.ReadDir(dir)
	if listErr != nil {
		t.Fatal(listErr)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".ve.upgrade-") && strings.HasSuffix(name, ".tmp") {
			t.Fatalf("temp file not cleaned up after failure: %s", name)
		}
	}
}

func TestReplaceBinary_UnixNoTempLeftOnSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix temp path")
	}
	dir := t.TempDir()
	current := filepath.Join(dir, "ve")
	newPath := filepath.Join(dir, "ve.new")
	if err := ioutil.WriteFile(current, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(newPath, []byte("new-payload"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceBinary(newPath, current); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".ve.upgrade-") && strings.HasSuffix(name, ".tmp") {
			t.Fatalf("temp file left after success: %s", name)
		}
	}
}

func TestCreateTempUpgradePatternUniqueness(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix temp naming used by ReplaceBinary")
	}
	dir := t.TempDir()
	f1, err := os.CreateTemp(dir, ".ve.upgrade-*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	defer f1.Close()
	f2, err := os.CreateTemp(dir, ".ve.upgrade-*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()
	if f1.Name() == f2.Name() {
		t.Fatal("expected unique CreateTemp names")
	}
	// Both should live under the target directory (not os.TempDir).
	if filepath.Dir(f1.Name()) != dir || filepath.Dir(f2.Name()) != dir {
		t.Fatalf("temps not in target dir: %q %q (dir=%q)", f1.Name(), f2.Name(), dir)
	}
}

func TestCopyFile_SyncsBeforeReturn(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := ioutil.WriteFile(src, []byte("payload-sync"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := ioutil.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "payload-sync" {
		t.Fatalf("got %q", got)
	}
}
