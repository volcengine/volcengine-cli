package proxycli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeInventoryAndThreeFieldAssets(t *testing.T) {
	ready, disabled := 0, 0
	for _, tool := range EmbeddedSnapshot().Catalog().Tools {
		if tool.Readiness != "ready" {
			disabled++
			if len(tool.Gaps) == 0 || len(tool.Platforms) != 0 {
				t.Fatal("skipped tool is not explicitly documented", tool.ID)
			}
			continue
		}
		ready++
		if tool.Executable != tool.Command || tool.Launch.Kind != "native" {
			t.Fatal("non-native or non-product executable", tool.ID)
		}
		for platform, asset := range tool.Platforms {
			b, _ := json.Marshal(asset)
			var fields map[string]interface{}
			json.Unmarshal(b, &fields)
			if len(fields) != 3 || fields["url"] == nil || fields["sha256"] == nil || fields["size"] == nil {
				t.Fatal(fields)
			}
			if tool.ID != ToolArkCLI && !strings.HasSuffix(asset.URL, ".zip") {
				t.Fatal(tool.ID, platform, "not a ZIP")
			}
		}
	}
	if ready != 16 || disabled != 8 {
		t.Fatalf("ready=%d disabled=%d", ready, disabled)
	}
}

func TestNativeInstallFlattensZIPAndDoesNotInstallResources(t *testing.T) {
	payload := []byte("native-payload")
	snapshot := fixtureCatalog(t, "single", payload)
	archive := zipWithFiles(t, map[string][]byte{"upstream/bin/single": payload, "upstream/templates/config.txt": []byte("must not be installed")})
	asset := snapshot.catalog.Tools[0].Platforms["linux-amd64"]
	asset.Size = int64(len(archive))
	asset.SHA256 = digestBytes(archive)
	snapshot.catalog.Tools[0].Platforms["linux-amd64"] = asset
	m := dynamicManager(t, snapshot, nil)
	// Use the actual download/install path; no archive flags appear in the manifest.
	m.HTTPClient = artifactClient(archive)
	result, err := m.Resolve("single")
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != filepath.Join(m.BinDir, "single") {
		t.Fatal(result.Path)
	}
	entries, err := os.ReadDir(m.BinDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "single" {
		t.Fatal("resources escaped the single-file install", entries)
	}
}

func TestOldPackageCatalogCannotRestoreSkippedRuntimeProducts(t *testing.T) {
	c := EmbeddedSnapshot().Catalog()
	c.MinInstallerProtocol = 2
	b, _ := json.Marshal(c)
	if _, err := ParseCatalog(b); err == nil {
		t.Fatal("old package protocol accepted")
	}
	c.MinInstallerProtocol = InstallerProtocolVersion
	for i := range c.Tools {
		if c.Tools[i].ID == "emr-cli" {
			c.Tools[i].Readiness = "ready"
		}
	}
	b, _ = json.Marshal(c)
	if _, err := ParseCatalog(b); err == nil {
		t.Fatal("runtime product was enabled without a native executable")
	}
}

func zipWithFiles(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, data := range files {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func artifactClient(data []byte) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data)), Header: http.Header{}}, nil
	})}
}

func TestNativeRelocationChangesArchiveWithoutDowngrading(t *testing.T) {
	for _, oldVersion := range []string{"1.0.0", "2.0.0"} {
		t.Run(oldVersion, func(t *testing.T) {
			body := []byte("old-native")
			m := dynamicManager(t, fixtureCatalog(t, "move", body), map[string][]byte{"move": body})
			oldPath := filepath.Join(m.BinDir, "tools", "move", "move")
			if err := os.MkdirAll(filepath.Dir(oldPath), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(oldPath, body, 0700); err != nil {
				t.Fatal(err)
			}
			entry := &InstalledTool{Version: oldVersion, ArtifactSHA256: digestBytes(body), BinarySHA256: digestBytes(body), Path: oldPath, Source: SourceCatalog}
			if err := m.writeState(State{Tools: map[Tool]*InstalledTool{"move": entry}}); err != nil {
				t.Fatal(err)
			}
			result, err := m.Resolve("move")
			if oldVersion == "2.0.0" {
				if err == nil {
					t.Fatal("relocation downgraded installed version")
				}
			} else {
				if err != nil || result.Path != filepath.Join(m.BinDir, "move") {
					t.Fatal(result, err)
				}
			}
			if data, err := os.ReadFile(oldPath); err != nil || string(data) != string(body) {
				t.Fatal("old file changed", err)
			}
		})
	}
}

func TestCatalogRejectsExtraPlatformConfiguration(t *testing.T) {
	raw := catalogBytes(t, fixtureCatalog(t, "minimal", []byte("native")))
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	tool := data["tools"].([]interface{})[0].(map[string]interface{})
	asset := tool["platforms"].(map[string]interface{})["linux-amd64"].(map[string]interface{})
	asset["entrypoint_path"] = "bin/tool"
	raw, _ = json.Marshal(data)
	if _, err := ParseCatalog(raw); err == nil {
		t.Fatal("extra platform field accepted")
	}
}

func TestAuditedSourcePolicies(t *testing.T) {
	for _, spec := range EmbeddedSnapshot().Catalog().Tools {
		for _, asset := range spec.Platforms {
			if !trustedArtifactURL(asset.URL) {
				t.Fatal(asset.URL)
			}
		}
	}
	for _, raw := range []string{"https://registry.npmjs.org/unrelated/-/unrelated-1.0.0.tgz", "https://github.com/unknown/ve-storage-uni-cli/releases/download/v1/a", "http://cloudcache.volccdn.com/tool", "https://cloudcache.volccdn.com.evil.test/tool", "https://user:secret@cloudcache.volccdn.com/tool", "https://cloudcache.volccdn.com:444/tool"} {
		if trustedArtifactURL(raw) {
			t.Fatal("untrusted source accepted", raw)
		}
	}
	github, _ := url.Parse("https://github.com/volcengine/ve-storage-uni-cli/releases/download/v1.0.2/a.tar.gz")
	asset, _ := url.Parse("https://release-assets.githubusercontent.com/signed")
	cdn, _ := url.Parse(DefaultManifestURL)
	if !trustedArtifactRedirect(github, asset) || trustedArtifactRedirect(cdn, asset) {
		t.Fatal("GitHub redirect scope changed")
	}
}

func TestNativeCDNManifestUsesMinimalPlatformRecord(t *testing.T) {
	for _, version := range []string{"2.0.0", "../../invalid"} {
		t.Run(version, func(t *testing.T) {
			oldBody, newBody := []byte("old-native"), []byte("new-native")
			snapshot := fixtureCatalog(t, "cdn-tool", oldBody)
			m := dynamicManager(t, snapshot, map[string][]byte{"cdn-tool": oldBody})
			m.ManifestURL = DefaultManifestURL
			fallback := m.HTTPClient.Transport
			archive := zipWithFile(t, "cdn-tool", newBody)
			remote := Manifest{Tools: map[Tool]ManifestTool{"cdn-tool": {Version: version, Platforms: map[string]ManifestAsset{"linux-amd64": {URL: "https://cloudcache.volccdn.com/ve/fixtures/cdn-tool-new.zip", SHA256: digestBytes(archive), Size: int64(len(archive))}}}}}
			manifest, _ := json.Marshal(remote)
			m.HTTPClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				var data []byte
				switch req.URL.String() {
				case DefaultManifestURL:
					data = manifest
				case "https://cloudcache.volccdn.com/ve/fixtures/cdn-tool-new.zip":
					data = archive
				default:
					return fallback.RoundTrip(req)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data)), Header: http.Header{}}, nil
			})
			result, err := m.Resolve("cdn-tool")
			if err != nil {
				t.Fatal(err)
			}
			if version == "2.0.0" {
				if result.Version != version || result.Source != SourceCDN {
					t.Fatal(result)
				}
			} else {
				if result.Version != "1.0.0" || result.Source != SourceCatalog {
					t.Fatal("invalid remote version was installed", result)
				}
			}
			if result.Path != filepath.Join(m.BinDir, "cdn-tool") {
				t.Fatal(result.Path)
			}
		})
	}
}

func TestReadyProductRequiresOfficialWebsiteSource(t *testing.T) {
	c := fixtureCatalog(t, "source-review", []byte("native")).Catalog()
	c.Tools[0].SourceURL = "https://github.com/volcengine/example-cli"
	raw, _ := json.Marshal(c)
	if _, err := ParseCatalog(raw); err == nil {
		t.Fatal("GitHub-only source accepted as official website evidence")
	}
}

func TestNativePATHWrapperFallsBackToManagedZIP(t *testing.T) {
	body := []byte("native-body")
	m := dynamicManager(t, fixtureCatalog(t, "wrapper-tool", body), map[string][]byte{"wrapper-tool": body})
	wrapper := filepath.Join(t.TempDir(), "wrapper-tool")
	if err := os.WriteFile(wrapper, []byte("#!/usr/bin/env node\nthrow new Error('must not execute');\n"), 0700); err != nil {
		t.Fatal(err)
	}
	m.LookPath = func(string) (string, error) { return wrapper, nil }
	got, err := m.Resolve("wrapper-tool")
	if err != nil || got.Path != filepath.Join(m.BinDir, "wrapper-tool") || got.Source != SourceCatalog {
		t.Fatal(got, err)
	}
}
