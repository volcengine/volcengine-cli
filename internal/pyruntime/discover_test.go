package pyruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func skipShellFakesOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake interpreters are POSIX shell scripts")
	}
}

// writeScript writes an executable shell script into dir (created if needed).
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

// localPython places a launcher for the real test interpreter in dir.
func localPython(t *testing.T, dir, name string) string {
	t.Helper()
	return writeScript(t, dir, name, "exec '"+testPython(t)+"' \"$@\"\n")
}

func fakePython(t *testing.T, dir, name, implementation, version, minor string) string {
	t.Helper()
	return writeScript(t, dir, name, `echo '{"implementation":"`+implementation+`","version":"`+version+`","major":3,"minor":`+minor+`}'`+"\n")
}

func setPath(t *testing.T, dirs ...string) {
	t.Helper()
	t.Setenv("PATH", strings.Join(dirs, string(os.PathListSeparator)))
}

func installWrapperRuntime(t *testing.T) string {
	t.Helper()
	data := wrapperArchive(t)
	server, _ := serveArchive(t, data)
	root := filepath.Join(t.TempDir(), "script-runtime")
	installer := &Installer{Root: root, Client: server.Client()}
	if _, err := installer.Install(context.Background(), artifactFor(server.URL+"/python.tar.gz", data, probeVersion(t, testPython(t)), "python/bin/python3.12")); err != nil {
		t.Fatalf("install managed runtime: %v", err)
	}
	return root
}

func TestResolveUsesLocalPythonWithoutManagedRuntime(t *testing.T) {
	skipShellFakesOnWindows(t)
	dir := filepath.Join(t.TempDir(), "bin with spaces")
	local := localPython(t, dir, "python3")
	setPath(t, dir)
	interpreter, err := Resolve(context.Background(), "", filepath.Join(t.TempDir(), "none"))
	if err != nil || interpreter.Source != SourceSystem || interpreter.Path != local {
		t.Fatalf("Resolve() = %+v, %v", interpreter, err)
	}
}

func TestResolvePrefersLocalPythonOverManagedRuntime(t *testing.T) {
	skipShellFakesOnWindows(t)
	root := installWrapperRuntime(t)
	dir := filepath.Join(t.TempDir(), "bin")
	local := localPython(t, dir, "python")
	setPath(t, dir)
	interpreter, err := Resolve(context.Background(), "", root)
	if err != nil || interpreter.Source != SourceSystem || interpreter.Path != local {
		t.Fatalf("Resolve() = %+v, %v", interpreter, err)
	}

	setPath(t, t.TempDir())
	interpreter, err = Resolve(context.Background(), "", root)
	if err != nil || interpreter.Source != SourceManaged || !isWithin(root, interpreter.Path) {
		t.Fatalf("Resolve() without local Python = %+v, %v", interpreter, err)
	}
}

func TestResolveSkipsUnusableLocalPythonAndFallsBackToManaged(t *testing.T) {
	skipShellFakesOnWindows(t)
	base := t.TempDir()
	old := filepath.Join(base, "old python")
	oldPython := fakePython(t, old, "python3", "cpython", "3.8.18", "8")
	fakePython(t, old, "python", "pypy", "3.10.14", "10")
	broken := filepath.Join(base, "broken")
	writeScript(t, broken, "python3", "exit 3\n")
	plain := filepath.Join(base, "plain")
	if err := os.MkdirAll(plain, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plain, "python3"), []byte("not executable"), 0644); err != nil {
		t.Fatal(err)
	}

	// Nothing usable and no managed runtime: the preparation error keeps
	// why each local candidate was rejected.
	setPath(t, old, broken, plain)
	_, err := Resolve(context.Background(), "", filepath.Join(base, "none"))
	if !errors.Is(err, ErrNotPrepared) || !strings.Contains(err.Error(), oldPython) || !strings.Contains(err.Error(), "3.8.18") || !strings.Contains(err.Error(), "pypy") {
		t.Fatalf("error = %v", err)
	}

	root := installWrapperRuntime(t)
	interpreter, err := Resolve(context.Background(), "", root)
	if err != nil || interpreter.Source != SourceManaged {
		t.Fatalf("fallback = %+v, %v", interpreter, err)
	}

	// The first usable candidate wins in PATH order; empty and relative PATH
	// entries are never searched.
	good := filepath.Join(base, "good")
	first := localPython(t, good, "python")
	second := filepath.Join(base, "second")
	localPython(t, second, "python3")
	setPath(t, "", ".", "relative", old, broken, plain, good, second)
	interpreter, err = Resolve(context.Background(), "", root)
	if err != nil || interpreter.Source != SourceSystem || interpreter.Path != first {
		t.Fatalf("PATH order = %+v, %v", interpreter, err)
	}
}

func TestResolveExplicitPathOverridesLocalAndManaged(t *testing.T) {
	skipShellFakesOnWindows(t)
	root := installWrapperRuntime(t)
	dir := filepath.Join(t.TempDir(), "bin")
	localPython(t, dir, "python3")
	setPath(t, dir)
	explicitDir := filepath.Join(t.TempDir(), "explicit dir")
	explicit := localPython(t, explicitDir, "py")

	interpreter, err := Resolve(context.Background(), explicit, root)
	if err != nil || interpreter.Source != SourceExplicit || interpreter.Path != explicit {
		t.Fatalf("explicit = %+v, %v", interpreter, err)
	}
	// An invalid explicit path is an error even though local and managed
	// interpreters are available.
	old := fakePython(t, explicitDir, "old", "cpython", "3.8.18", "8")
	for _, invalid := range []string{old, filepath.Join(explicitDir, "missing"), "python3"} {
		if _, err := Resolve(context.Background(), invalid, root); err == nil || errors.Is(err, ErrNotPrepared) {
			t.Fatalf("invalid explicit %q fell back: %v", invalid, err)
		}
	}
}

func TestLocalPythonCandidateNames(t *testing.T) {
	if names := candidateNames("windows"); strings.Join(names, ",") != "python.exe,python3.exe" {
		t.Fatalf("windows names = %v", names)
	}
	if names := candidateNames("darwin"); strings.Join(names, ",") != "python3,python" {
		t.Fatalf("unix names = %v", names)
	}
	if reason := skipCandidate("windows", `C:\Users\me\AppData\Local\Microsoft\WindowsApps\python.exe`); reason == "" {
		t.Fatal("Microsoft Store alias was not skipped")
	}
	if reason := skipCandidate("windows", `C:\Program Files\Python312\python.exe`); reason != "" {
		t.Fatalf("regular Windows Python skipped: %s", reason)
	}
}
