package upgrade

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
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
}
