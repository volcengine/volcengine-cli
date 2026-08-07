package cmd

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildAssetPropagatesBindataFailureBeforeGofmt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("build_asset.sh requires bash")
	}
	for _, command := range []string{"bash", "git"} {
		if _, err := exec.LookPath(command); err != nil {
			t.Skipf("%s is not available: %v", command, err)
		}
	}

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}
	repoRoot := filepath.Dir(filepath.Dir(currentFile))
	script, err := ioutil.ReadFile(filepath.Join(repoRoot, "build_asset.sh"))
	if err != nil {
		t.Fatalf("read build_asset.sh: %v", err)
	}

	testRoot := tempDirForTest(t)
	defer cleanupDirForTest(testRoot)()
	workRepo := filepath.Join(testRoot, "work")
	metadataRepo := filepath.Join(testRoot, "metadata")
	fakeBin := filepath.Join(testRoot, "bin")
	for _, dir := range []string{
		workRepo,
		metadataRepo,
		fakeBin,
		filepath.Join(workRepo, "asset", "paramdescriptions"),
		filepath.Join(metadataRepo, "metadata"),
		filepath.Join(metadataRepo, "metatype"),
		filepath.Join(metadataRepo, "structure"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}

	writeTestFile(t, filepath.Join(workRepo, "build_asset.sh"), script, 0755)
	writeTestFile(t, filepath.Join(workRepo, "asset", "paramdescriptions", "params.json"), []byte("{}\n"), 0644)
	writeTestFile(t, filepath.Join(metadataRepo, "metadata", ".keep"), nil, 0644)
	writeTestFile(t, filepath.Join(metadataRepo, "metatype", ".keep"), nil, 0644)
	writeTestFile(t, filepath.Join(metadataRepo, "structure", ".keep"), nil, 0644)

	initTestGitRepo(t, workRepo)
	initTestGitRepo(t, metadataRepo)

	bindataMarker := filepath.Join(testRoot, "go-bindata-called")
	gofmtMarker := filepath.Join(testRoot, "gofmt-called")
	writeTestFile(t, filepath.Join(fakeBin, "go"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	writeTestFile(t, filepath.Join(fakeBin, "go-bindata"), []byte("#!/bin/sh\ntouch \"$BINDATA_MARKER\"\nexit 23\n"), 0755)
	writeTestFile(t, filepath.Join(fakeBin, "gofmt"), []byte("#!/bin/sh\ntouch \"$GOFMT_MARKER\"\nexit 0\n"), 0755)

	command := exec.Command("bash", filepath.Join(workRepo, "build_asset.sh"), metadataRepo, "--target", "param")
	command.Dir = workRepo
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_ALLOW_PROTOCOL=file",
		"BINDATA_MARKER="+bindataMarker,
		"GOFMT_MARKER="+gofmtMarker,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("build_asset.sh succeeded after go-bindata failure:\n%s", output)
	}
	if !strings.Contains(string(output), "paramdescriptions bindata failed") {
		t.Fatalf("missing bindata failure diagnostic:\n%s", output)
	}
	if _, err := os.Stat(bindataMarker); err != nil {
		t.Fatalf("go-bindata was not invoked: %v", err)
	}
	if _, err := os.Stat(gofmtMarker); !os.IsNotExist(err) {
		t.Fatalf("gofmt ran after go-bindata failure: %v", err)
	}

	status := runTestCommand(t, workRepo, "git", "status", "--short")
	if strings.TrimSpace(status) != "" {
		t.Fatalf("build_asset.sh left repository changes after failure:\n%s", status)
	}
}

func initTestGitRepo(t *testing.T, dir string) {
	t.Helper()
	runTestCommand(t, dir, "git", "init", "-q")
	runTestCommand(t, dir, "git", "config", "user.email", "test@example.com")
	runTestCommand(t, dir, "git", "config", "user.name", "test")
	runTestCommand(t, dir, "git", "add", ".")
	runTestCommand(t, dir, "git", "commit", "-qm", "init")
}

func runTestCommand(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func writeTestFile(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := ioutil.WriteFile(path, content, mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
