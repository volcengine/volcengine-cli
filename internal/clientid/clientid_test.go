package clientid

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestLoadOrCreatePersistsRandomIdentity(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "client")
	id, err := LoadOrCreate(directory)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 4 || parsed.Variant() != uuid.RFC4122 {
		t.Fatalf("ID %q is not a UUIDv4", id)
	}
	content, err := os.ReadFile(filepath.Join(directory, "client_id"))
	if err != nil || string(content) != id+"\n" {
		t.Fatalf("persisted ID = %q, err = %v", content, err)
	}
	for i := 0; i < 10; i++ {
		again, err := LoadOrCreate(directory)
		if err != nil || again != id {
			t.Fatalf("reused ID = %q, err = %v; want %q", again, err, id)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "client_id" {
		t.Fatalf("unexpected store files: %v, err = %v", entries, err)
	}
	if runtime.GOOS != "windows" {
		for path, mode := range map[string]os.FileMode{directory: 0700, filepath.Join(directory, "client_id"): 0600} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != mode {
				t.Fatalf("permissions on %s: %v, err = %v; want %o", path, info, err, mode)
			}
		}
	}
}

func TestDifferentClientsGetDifferentIDs(t *testing.T) {
	root := t.TempDir()
	seen := make(map[string]bool)
	for i := 0; i < 32; i++ {
		id, err := LoadOrCreate(filepath.Join(root, fmt.Sprint(i)))
		if err != nil {
			t.Fatal(err)
		}
		if seen[id] {
			t.Fatalf("distinct clients shared ID %q", id)
		}
		seen[id] = true
	}
}

func TestConcurrentFirstUseSharesOneID(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "client")
	const callers = 32
	ids := make([]string, callers)
	errors := make([]error, callers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ids[i], errors[i] = LoadOrCreate(directory)
		}(i)
	}
	close(start)
	wg.Wait()
	want, err := LoadOrCreate(directory)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		if errors[i] != nil || id != want {
			t.Fatalf("caller %d: ID = %q, err = %v; want %q", i, id, errors[i], want)
		}
	}
}

func TestConcurrentProcessesSharePersistedID(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "client")
	const callers = 12
	commands := make([]*exec.Cmd, callers)
	inputs := make([]io.WriteCloser, callers)
	outputs := make([]bytes.Buffer, callers)
	for i := range commands {
		command := exec.Command(os.Args[0], "-test.run=^TestLoadOrCreateProcess$", "--", directory)
		command.Env = append(os.Environ(), "VOLCENGINE_CLIENT_ID_TEST_PROCESS=1")
		command.Stdout = &outputs[i]
		command.Stderr = &outputs[i]
		input, err := command.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = command.Process.Kill() })
		commands[i], inputs[i] = command, input
	}
	for _, input := range inputs {
		_, _ = input.Write([]byte{1})
		_ = input.Close()
	}
	for i, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatalf("process %d: %v\n%s", i, err, &outputs[i])
		}
	}
	want, err := LoadOrCreate(directory)
	if err != nil {
		t.Fatal(err)
	}
	for i := range outputs {
		if got := strings.TrimSpace(outputs[i].String()); got != want {
			t.Fatalf("process %d used %q, persisted ID is %q", i, got, want)
		}
	}
}

func TestLoadOrCreateProcess(t *testing.T) {
	if os.Getenv("VOLCENGINE_CLIENT_ID_TEST_PROCESS") != "1" {
		return
	}
	if _, err := io.ReadFull(os.Stdin, make([]byte, 1)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	id, err := LoadOrCreate(os.Args[len(os.Args)-1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Println(id)
	os.Exit(0)
}

func TestInvalidIdentityIsNotReplaced(t *testing.T) {
	for name, value := range map[string]string{
		"empty": "", "text": "not-an-id", "oversized": strings.Repeat("x", 256),
		"nil UUID": "00000000-0000-0000-0000-000000000000",
		"UUIDv1":   "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		"variant":  "d284587e-0c76-4a60-00b9-f2d85de7d836",
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "client_id")
			if err := os.WriteFile(path, []byte(value), 0600); err != nil {
				t.Fatal(err)
			}
			if id, err := LoadOrCreate(directory); err == nil || id != "" {
				t.Fatalf("invalid file produced ID %q, err = %v", id, err)
			}
			content, err := os.ReadFile(path)
			if err != nil || string(content) != value {
				t.Fatalf("existing file was changed: %q, err = %v", content, err)
			}
		})
	}
}

func TestIdentityPathMustBeRegularFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "client_id")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(directory); err == nil {
		t.Fatal("accepted directory as ID")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(directory, "missing"), path); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if _, err := LoadOrCreate(directory); err == nil {
		t.Fatal("accepted symlink as ID")
	}
}

func TestReadOnlyExistingIdentityCanBeReused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions")
	}
	directory := t.TempDir()
	want, err := LoadOrCreate(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0700) })
	got, err := LoadOrCreate(directory)
	if err != nil || got != want {
		t.Fatalf("read-only reuse: ID = %q, err = %v", got, err)
	}
}

func TestUnwritableStorageDoesNotReturnEphemeralID(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("Unix non-root permissions")
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0700) })
	if id, err := LoadOrCreate(directory); err == nil || id != "" {
		t.Fatalf("unwritable store returned ID = %q, err = %v", id, err)
	}
}
