package proxycli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func fixtureCatalog(t *testing.T, name string, binary []byte) *Snapshot {
	t.Helper()
	if name != "arkcli" {
		binary = zipWithFile(t, name, binary)
	}
	c := Catalog{SchemaVersion: 2, Revision: "fixture-1", MinInstallerProtocol: InstallerProtocolVersion, Tools: []ToolSpec{{
		ID: Tool(name), Command: name, Aliases: []string{name + "-alias"}, Description: "Offline fixture", Readiness: "ready", ReleaseVersion: "1.0.0", Executable: name, Launch: LaunchSpec{Kind: "native"}, SourceURL: "https://docs.volcengine.com/docs/83927/1176799", PathPolicy: "prefer", Skills: SkillSpec{Repository: SkillsRepository, Names: []string{}}, Platforms: map[string]ManifestAsset{
			"linux-amd64": {URL: "https://cloudcache.volccdn.com/ve/fixtures/" + name, SHA256: digestBytes(binary), Size: int64(len(binary))},
		},
	}}}
	data, _ := json.Marshal(c)
	s, err := ParseCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func catalogBytes(t *testing.T, s *Snapshot) []byte {
	t.Helper()
	data, err := json.Marshal(s.catalog)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCatalogMigrationAndInventory(t *testing.T) {
	s := EmbeddedSnapshot()
	if len(s.Catalog().Tools) != 24 {
		t.Fatal("expected 24 inventory entries")
	}
	old, err := parseManifest(embeddedFallbackManifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range LegacyTools() {
		spec, _ := s.Lookup(string(id))
		if spec.Readiness != "ready" || spec.ReleaseVersion != old.Tools[id].Version {
			t.Fatal(spec)
		}
		if len(spec.Platforms) != len(old.Tools[id].Platforms) {
			t.Fatal("platform migration lost assets")
		}
		for p, a := range old.Tools[id].Platforms {
			got := spec.Platforms[p]
			if got.URL != a.URL || got.SHA256 != a.SHA256 || got.Size != a.Size {
				t.Fatalf("changed artifact %s/%s", id, p)
			}
		}
	}
	if id, err := s.ParseTool("toscli"); err != nil || id != ToolTOSUtil {
		t.Fatal(id, err)
	}
	for _, spec := range s.Catalog().Tools {
		if spec.Readiness == "ready" && len(spec.Platforms) == 0 {
			t.Fatal(spec)
		}
	}
	copy := s.Catalog()
	copy.Tools[0].Platforms["linux-amd64"] = ManifestAsset{}
	again := s.Catalog()
	if again.Tools[0].Platforms["linux-amd64"].URL == "" {
		t.Fatal("snapshot mutated")
	}
}
func TestCatalogRejectsInvalidContracts(t *testing.T) {
	tests := map[string]func(*Catalog){
		"schema": func(c *Catalog) { c.SchemaVersion = 3 }, "protocol": func(c *Catalog) { c.MinInstallerProtocol = 9 },
		"duplicate id":      func(c *Catalog) { c.Tools = append(c.Tools, c.Tools[0]) },
		"duplicate command": func(c *Catalog) { other := c.Tools[0]; other.ID = "another"; c.Tools = append(c.Tools, other) },
		"id alias ambiguity": func(c *Catalog) {
			other := c.Tools[0]
			other.ID = "fixture-alias"
			other.Command = "other"
			other.Aliases = nil
			c.Tools = append(c.Tools, other)
		},
		"duplicate alias": func(c *Catalog) { c.Tools[0].Aliases = []string{"fixture"} },
		"self":            func(c *Catalog) { c.Tools[0].Executable = "ve" }, "traversal": func(c *Catalog) { c.Tools[0].ID = "../tool" },
		"runtime":     func(c *Catalog) { c.Tools[0].Launch.Kind = "python" },
		"no platform": func(c *Catalog) { c.Tools[0].Platforms = nil },
		"untrusted source": func(c *Catalog) {
			a := c.Tools[0].Platforms["linux-amd64"]
			a.URL = "https://evil.example/tool"
			c.Tools[0].Platforms["linux-amd64"] = a
		},
		"bad hash": func(c *Catalog) {
			a := c.Tools[0].Platforms["linux-amd64"]
			a.SHA256 = "bad"
			c.Tools[0].Platforms["linux-amd64"] = a
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			c := fixtureCatalog(t, "fixture", []byte("native")).Catalog()
			mutate(&c)
			data, _ := json.Marshal(c)
			if _, err := ParseCatalog(data); err == nil {
				t.Fatal("accepted invalid catalog")
			}
		})
	}
}
func TestCatalogRefreshKeepsPreviousAndSnapshot(t *testing.T) {
	store := &CatalogStore{Dir: t.TempDir()}
	old := store.Load()
	fixture := fixtureCatalog(t, "unseen", []byte("native"))
	path := filepath.Join(t.TempDir(), "catalog.json")
	os.WriteFile(path, catalogBytes(t, fixture), 0600)
	fresh, err := store.RefreshFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := old.Lookup("unseen"); ok {
		t.Fatal("running snapshot changed")
	}
	if _, ok := fresh.Lookup("unseen-alias"); !ok {
		t.Fatal("refresh did not add unknown CLI")
	}
	valid, _ := os.ReadFile(filepath.Join(store.Dir, CatalogFileName))
	os.WriteFile(path, []byte(`{"schema_version":99}`), 0600)
	if _, err := store.RefreshFile(path); err == nil {
		t.Fatal("accepted incompatible schema")
	}
	after, _ := os.ReadFile(filepath.Join(store.Dir, CatalogFileName))
	if !bytes.Equal(after, valid) {
		t.Fatal("failed refresh replaced cache")
	}
	if _, err := store.replace(valid); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(store.Dir, CatalogFileName), []byte("corrupt"), 0600)
	if _, ok := store.Load().Lookup("unseen"); !ok {
		t.Fatal("previous valid catalog lost")
	}
	if _, err := store.Refresh(); err == nil {
		t.Fatal("unsigned production remote refresh permitted")
	}
}
func TestCatalogPreservesLegacyEntrypoints(t *testing.T) {
	c := EmbeddedSnapshot().Catalog()
	c.Tools[0].Aliases = nil
	data, _ := json.Marshal(c)
	u, err := ParseCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := overlayCatalog(EmbeddedSnapshot(), u, false); err == nil {
		t.Fatal("removed toscli")
	}
}
func TestCatalogSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"catalog_revision":"signed"}`)
	e := signedCatalog{KeyID: "key-1", Payload: base64.StdEncoding.EncodeToString(payload), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))}
	data, _ := json.Marshal(e)
	keys := map[string]ed25519.PublicKey{"key-1": pub}
	got, err := verifyCatalogSignature(data, keys)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal(err)
	}
	if _, err := verifyCatalogSignature(data, map[string]ed25519.PublicKey{}); err == nil {
		t.Fatal("unknown key accepted")
	}
	e.Payload = base64.StdEncoding.EncodeToString([]byte("tampered"))
	data, _ = json.Marshal(e)
	if _, err := verifyCatalogSignature(data, keys); err == nil {
		t.Fatal("tampered catalog accepted")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func dynamicManager(t *testing.T, s *Snapshot, binaries map[string][]byte) *Manager {
	m := testManager(t, t.TempDir(), nil)
	m.Catalog = s
	m.GOOS = "linux"
	m.GOARCH = "amd64"
	m.allowHTTPForTests = false
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	m.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		data, ok := binaries[filepath.Base(r.URL.Path)]
		if !ok {
			return nil, fmt.Errorf("unexpected network request: %s", r.URL)
		}
		if Tool(filepath.Base(r.URL.Path)) != ToolArkCLI {
			name := filepath.Base(r.URL.Path)
			if m.GOOS == "windows" {
				name += ".exe"
			}
			data = zipWithFile(t, name, data)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data)), Header: http.Header{}}, nil
	})}
	return m
}
func TestDynamicInstallUpgradeIsolationAndFailure(t *testing.T) {
	a, b := []byte("binary-A"), []byte("binary-B")
	s, err := overlayCatalog(fixtureCatalog(t, "alpha", a), fixtureCatalog(t, "beta", b), false)
	if err != nil {
		t.Fatal(err)
	}
	m := dynamicManager(t, s, map[string][]byte{"alpha": a, "beta": b})
	for _, id := range []Tool{"alpha", "beta"} {
		r, err := m.Resolve(id)
		if err != nil || r.Source != SourceCatalog {
			t.Fatal(r, err)
		}
	}
	before, err := m.readState()
	if err != nil {
		t.Fatal(err)
	}
	newer := fixtureCatalog(t, "alpha", []byte("A-new"))
	newer.catalog.Tools[0].ReleaseVersion = "2.0.0"
	merged, err := overlayCatalog(s, newer, false)
	if err != nil {
		t.Fatal(err)
	}
	m.Catalog = merged
	if _, err := m.Upgrade([]Tool{"alpha"}); err == nil {
		t.Fatal("corrupt hash accepted")
	}
	data, _ := os.ReadFile(m.managedPath("alpha"))
	if !bytes.Equal(data, a) {
		t.Fatal("failed upgrade destroyed previous binary")
	}
	m.HTTPClient = dynamicManager(t, merged, map[string][]byte{"alpha": []byte("A-new")}).HTTPClient
	result, err := m.Upgrade([]Tool{"missing", "alpha"})
	if err == nil || len(result.Items) != 1 {
		t.Fatal("single-item failure prevented independent upgrade", result, err)
	}
	after, _ := m.readState()
	if after.Tools["alpha"].Version != "2.0.0" || !reflect.DeepEqual(before.Tools["beta"], after.Tools["beta"]) {
		t.Fatal("upgrade isolation failed")
	}
	defaults, err := m.DefaultUpgradeTools()
	if err != nil || !reflect.DeepEqual(defaults, []Tool{ToolArkCLI, ToolTOSUtil, "alpha", "beta"}) {
		t.Fatal(defaults, err)
	}
	m.GOOS = "windows"
	if _, err := m.Resolve("alpha"); err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatal(err)
	}
}
func TestDynamicConcurrentInstall(t *testing.T) {
	data := []byte("native")
	m := dynamicManager(t, fixtureCatalog(t, "parallel", data), map[string][]byte{"parallel": data})
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := m.Resolve("parallel"); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}
func TestProxyRejectsHostAndUnverifiedWrappers(t *testing.T) {
	host, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := rejectHostExecutable(host); err == nil {
		t.Fatal("host accepted")
	}
	wrapper := filepath.Join(t.TempDir(), "nest")
	os.WriteFile(wrapper, []byte("#!/bin/sh\nexec ve nest \"$@\"\n"), 0755)
	if err := rejectHostExecutable(wrapper); err == nil {
		t.Fatal("wrapper accepted")
	}
	s := fixtureCatalog(t, "collision", []byte("native"))
	s.catalog.Tools[0].PathPolicy = "managed-only"
	m := dynamicManager(t, s, map[string][]byte{"collision": []byte("native")})
	m.LookPath = func(string) (string, error) { t.Fatal("managed-only probed PATH"); return wrapper, nil }
	if _, err := m.Resolve("collision"); err != nil {
		t.Fatal(err)
	}
}
func TestArchiveValidation(t *testing.T) {
	for _, format := range []string{"zip", "tar.gz"} {
		for _, kind := range []string{"valid", "traversal", "duplicate", "link", "oversize"} {
			t.Run(format+"/"+kind, func(t *testing.T) {
				var buf bytes.Buffer
				name := "nested/tool"
				if kind == "traversal" {
					name = "../tool"
				}
				if format == "zip" {
					z := zip.NewWriter(&buf)
					h := &zip.FileHeader{Name: name, Method: zip.Store}
					if kind == "link" {
						h.SetMode(os.ModeSymlink | 0755)
					}
					f, _ := z.CreateHeader(h)
					f.Write([]byte("data"))
					if kind == "duplicate" {
						f, _ = z.Create("other/tool")
						f.Write([]byte("data"))
					}
					z.Close()
					if kind == "oversize" {
						data := buf.Bytes()
						sig := bytes.Index(data, []byte{'P', 'K', 1, 2})
						for j := 0; j < 4; j++ {
							data[sig+24+j] = 255
						}
					}
				} else {
					gz := gzip.NewWriter(&buf)
					tw := tar.NewWriter(gz)
					h := &tar.Header{Name: name, Mode: 0755, Size: 4, Typeflag: tar.TypeReg}
					if kind == "link" {
						h.Typeflag = tar.TypeSymlink
						h.Linkname = "target"
						h.Size = 0
					}
					if kind == "oversize" {
						h.Size = maxBinaryBytes + 1
					}
					tw.WriteHeader(h)
					if h.Size == 4 {
						tw.Write([]byte("data"))
					}
					if kind == "duplicate" {
						h.Name = "other/tool"
						tw.WriteHeader(h)
						tw.Write([]byte("data"))
					}
					tw.Close()
					gz.Close()
				}
				var got []byte
				var err error
				if format == "zip" {
					got, err = extractBinaryFromZip(buf.Bytes(), "tool")
				} else {
					got, err = extractBinaryFromTarGzip(buf.Bytes(), "tool")
				}
				if kind == "valid" {
					if err != nil || string(got) != "data" {
						t.Fatal(string(got), err)
					}
				} else if err == nil {
					t.Fatal("unsafe archive accepted")
				}
			})
		}
	}
}
func TestNativeAdapterRejectsScriptArtifacts(t *testing.T) {
	data := []byte("#!/bin/sh\necho bad")
	m := dynamicManager(t, fixtureCatalog(t, "script", data), map[string][]byte{"script": data})
	if _, err := m.Resolve("script"); err == nil {
		t.Fatal("native script installed")
	}
}

func TestConcurrentCatalogRefreshRetainsBothAdditions(t *testing.T) {
	store := &CatalogStore{Dir: t.TempDir()}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, name := range []string{"first", "second"} {
		data := catalogBytes(t, fixtureCatalog(t, name, []byte("native")))
		wg.Add(1)
		go func(data []byte) { defer wg.Done(); _, err := store.replace(data); errs <- err }(data)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"first", "second"} {
		if _, ok := store.Load().Lookup(name); !ok {
			t.Fatalf("concurrent refresh lost %s", name)
		}
	}
}
func TestWindowsNativeInstallPath(t *testing.T) {
	data := []byte("windows-native-fixture")
	s := fixtureCatalog(t, "windows-tool", data)
	a := s.catalog.Tools[0].Platforms["linux-amd64"]
	archive := zipWithFile(t, "windows-tool.exe", data)
	a.SHA256, a.Size = digestBytes(archive), int64(len(archive))
	s.catalog.Tools[0].Platforms = map[string]ManifestAsset{"windows-amd64": a}
	m := dynamicManager(t, s, map[string][]byte{"windows-tool": data})
	m.GOOS = "windows"
	r, err := m.Resolve("windows-tool")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(r.Path) != "windows-tool.exe" {
		t.Fatal(r.Path)
	}
	m.LookPath = func(name string) (string, error) {
		if name != "windows-tool.exe" {
			t.Fatal(name)
		}
		return "", os.ErrNotExist
	}
	if _, err := m.Resolve("windows-tool"); err != nil {
		t.Fatal(err)
	}
}
func TestInterruptedDownloadPreservesInstalledTool(t *testing.T) {
	data := []byte("old-native")
	m := dynamicManager(t, fixtureCatalog(t, "interrupted", data), map[string][]byte{"interrupted": data})
	if _, err := m.Resolve("interrupted"); err != nil {
		t.Fatal(err)
	}
	newer := fixtureCatalog(t, "interrupted", []byte("new-native"))
	newer.catalog.Tools[0].ReleaseVersion = "2.0.0"
	m.Catalog = newer
	m.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: 10, Header: http.Header{}, Body: io.NopCloser(errorReader{})}, nil
	})}
	if _, err := m.Upgrade([]Tool{"interrupted"}); err == nil {
		t.Fatal("interrupted download succeeded")
	}
	got, err := os.ReadFile(m.managedPath("interrupted"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("previous binary lost", err)
	}
	state, _ := m.readState()
	if state.Tools["interrupted"].Version != "1.0.0" {
		t.Fatal("version changed after failure")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
