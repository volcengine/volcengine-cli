package proxycli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Keep one artifact request suspended until the test explicitly releases it.
// Unrelated tools must complete while that request is still in flight.
func TestSlowInstallDoesNotBlockOtherTool(t *testing.T) {
	for _, tc := range []struct {
		name              string
		cached, upgrading bool
	}{
		{name: "resolve/cold"}, {name: "resolve/cached", cached: true},
		{name: "upgrade/cold", upgrading: true}, {name: "upgrade/cached", cached: true, upgrading: true},
	} {
		name := tc.name
		cached := tc.cached
		t.Run(name, func(t *testing.T) {
			started := make(chan struct{})
			unblock := make(chan struct{})
			var once sync.Once
			ark := []byte("ark-native")
			tos := []byte("tos-native")
			zip := zipWithFile(t, "tosutil", tos)
			manifest := Manifest{Tools: map[Tool]ManifestTool{
				ToolArkCLI:  {Version: "1.0.0", Platforms: map[string]ManifestAsset{platformKey(runtime.GOOS, runtime.GOARCH): {URL: "/ark", SHA256: digest(ark), Size: int64(len(ark))}}},
				ToolTOSUtil: {Version: "1.0.0", Platforms: map[string]ManifestAsset{platformKey(runtime.GOOS, runtime.GOARCH): {URL: "/tos", SHA256: digest(zip), Size: int64(len(zip))}}},
			}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/manifest":
					_ = json.NewEncoder(w).Encode(manifest)
				case "/ark":
					close(started)
					<-unblock
					_, _ = w.Write(ark)
				case "/tos":
					_, _ = w.Write(zip)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			// Close the blocked request before server.Close even on assertion failure.
			defer once.Do(func() { close(unblock) })
			m := testManager(t, t.TempDir(), server.Client())
			m.ManifestURL = server.URL + "/manifest"
			m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
			if cached {
				if _, err := m.Resolve(ToolTOSUtil); err != nil {
					t.Fatal(err)
				}
			}
			arkDone := make(chan error, 1)
			go func() {
				var err error
				if tc.upgrading {
					_, err = m.Upgrade([]Tool{ToolArkCLI})
				} else {
					_, err = m.Resolve(ToolArkCLI)
				}
				arkDone <- err
			}()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("first install did not reach artifact")
			}
			tosDone := make(chan error, 1)
			go func() { _, err := m.Resolve(ToolTOSUtil); tosDone <- err }()
			select {
			case err := <-tosDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("unrelated tool blocked on slow artifact download")
			}
			once.Do(func() { close(unblock) })
			if err := <-arkDone; err != nil {
				t.Fatal(err)
			}
			state := readTestState(t, m.statePath())
			if state.Tools[ToolArkCLI] == nil || state.Tools[ToolTOSUtil] == nil {
				t.Fatalf("lost concurrently installed state: %+v", state.Tools)
			}
		})
	}
}

func TestConcurrentSameToolDownloadsOnlyOnce(t *testing.T) {
	var downloads int32
	started := make(chan struct{})
	unblock := make(chan struct{})
	var once sync.Once
	artifact := []byte("ark-native")
	manifest := manifestFor(t, ToolArkCLI, "1.0.0", "/artifact", artifact)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest" {
			_, _ = w.Write(manifest)
			return
		}
		if atomic.AddInt32(&downloads, 1) == 1 {
			close(started)
		}
		<-unblock
		_, _ = w.Write(artifact)
	}))
	defer server.Close()
	defer once.Do(func() { close(unblock) })
	m := testManager(t, t.TempDir(), server.Client())
	m.ManifestURL = server.URL + "/manifest"
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	done := make(chan error, 3)
	go func() { _, err := m.Resolve(ToolArkCLI); done <- err }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("install did not start")
	}
	go func() { _, err := m.Resolve(ToolArkCLI); done <- err }()
	go func() { _, err := m.Upgrade([]Tool{ToolArkCLI}); done <- err }()
	// Neither resolver nor upgrader may bypass the in-flight installation.
	select {
	case err := <-done:
		t.Fatalf("same-tool operation completed before installation: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	once.Do(func() { close(unblock) })
	for i := 0; i < 3; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("same-tool resolve did not complete")
		}
	}
	if atomic.LoadInt32(&downloads) != 1 {
		t.Fatalf("downloaded %d times", atomic.LoadInt32(&downloads))
	}
}

func TestInstallCleanupPreservesOtherToolTemporaryFiles(t *testing.T) {
	m := testManager(t, t.TempDir(), nil)
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	binary := []byte("managed")
	path := m.managedPath(ToolTOSUtil)
	writeExecutable(t, path, binary)
	if err := m.writeState(State{Tools: map[Tool]*InstalledTool{ToolTOSUtil: {Version: "1.0.0", ArtifactSHA256: digest(binary), BinarySHA256: digest(binary), Path: path, Source: SourceCDN}}}); err != nil {
		t.Fatal(err)
	}
	ownTemp := filepath.Join(m.BinDir, executableTempPrefix(path)+"interrupted")
	otherTemp := filepath.Join(m.BinDir, executableTempPrefix(m.managedPath(ToolArkCLI))+"active")
	for _, path := range []string{ownTemp, otherTemp} {
		if err := os.WriteFile(path, []byte("in-flight"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Resolve(ToolTOSUtil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ownTemp); !os.IsNotExist(err) {
		t.Fatalf("own interrupted temp survived: %v", err)
	}
	if _, err := os.Stat(otherTemp); err != nil {
		t.Fatalf("other install temp removed: %v", err)
	}
}

func TestInstallLockAndTemporaryPrefixesNormalizePathCase(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("case-insensitive installation paths apply to macOS and Windows")
	}
	home := t.TempDir()
	upper := testManager(t, home, nil)
	lower := testManager(t, home, nil)
	upper.BinDir = filepath.Join(home, "CustomBin")
	lower.BinDir = filepath.Join(home, "custombin")
	if upper.toolLockPath(ToolArkCLI) != lower.toolLockPath(ToolArkCLI) {
		t.Fatal("path case aliases do not share the installation lock")
	}
	if executableTempPrefix(upper.managedPath(ToolArkCLI)) != executableTempPrefix(lower.managedPath(ToolArkCLI)) {
		t.Fatal("path case aliases do not share the temporary-file prefix")
	}
}
