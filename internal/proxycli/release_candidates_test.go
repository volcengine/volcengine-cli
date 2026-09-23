package proxycli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
)

func TestCachedLegacyCatalogKeepsCDNUpgrades(t *testing.T) {
	for _, tool := range LegacyTools() {
		t.Run(string(tool), func(t *testing.T) {
			oldBinary, newBinary := []byte("native-old"), []byte("native-new")
			snapshot := fixtureCatalog(t, string(tool), oldBinary)
			snapshot.catalog.Tools[0].ReleaseVersion = "9.0.0"
			m := dynamicManager(t, snapshot, nil)
			m.ManifestURL = DefaultManifestURL
			packageBinary := func(body []byte) []byte {
				if tool == ToolArkCLI {
					return body
				}
				return zipWithFile(t, string(tool), body)
			}
			oldArtifact, newArtifact := packageBinary(oldBinary), packageBinary(newBinary)
			localURL := snapshot.catalog.Tools[0].Platforms["linux-amd64"].URL
			newURL := "https://cloudcache.volccdn.com/ve/fixtures/new-" + string(tool)
			var online, corrupt bool
			m.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				var body []byte
				switch r.URL.String() {
				case DefaultManifestURL:
					if !online {
						return nil, fmt.Errorf("CDN unavailable")
					}
					body, _ = json.Marshal(Manifest{Tools: map[Tool]ManifestTool{tool: {Version: "9.0.1", Platforms: map[string]ManifestAsset{"linux-amd64": {URL: newURL, SHA256: digestBytes(newArtifact), Size: int64(len(newArtifact))}}}}})
				case localURL:
					body = oldArtifact
				case newURL:
					body = newArtifact
					if corrupt {
						body = []byte("bad download")
					}
				default:
					return nil, fmt.Errorf("unexpected URL %s", r.URL)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Header: http.Header{}}, nil
			})}
			// A reviewed local catalog remains a useful fallback when the CDN is down.
			installed, err := m.Resolve(tool)
			if err != nil || installed.Version != "9.0.0" {
				t.Fatalf("local fallback: %+v, %v", installed, err)
			}
			online, corrupt = true, true
			if _, err := m.Upgrade([]Tool{tool}); err == nil {
				t.Fatal("bad newer CDN artifact must fail upgrade, not report cached version as current")
			}
			body, _ := os.ReadFile(installed.Path)
			if !bytes.Equal(body, oldBinary) {
				t.Fatal("failed upgrade replaced installed binary")
			}
			corrupt = false
			result, err := m.Upgrade([]Tool{tool})
			if err != nil || len(result.Items) != 1 || result.Items[0].Version != "9.0.1" {
				t.Fatalf("CDN upgrade: %+v, %v", result, err)
			}
			// A subsequently unavailable CDN must not roll an installed release back.
			online = false
			result, err = m.Upgrade([]Tool{tool})
			if err != nil || len(result.Items) != 1 || result.Items[0].Version != "9.0.1" {
				t.Fatalf("preserve installed version: %+v, %v", result, err)
			}
		})
	}
}

func TestCatalogRejectsCollidingExecutablePaths(t *testing.T) {
	for _, tt := range []struct{ name, platform, first, second string }{
		{"linux exact", "linux-amd64", "shared", "shared"},
		{"windows extension and case", "windows-amd64", "Shared", "shared.EXE"},
		{"darwin case", "darwin-arm64", "Shared", "shared"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, b := fixtureCatalog(t, "alpha", []byte("a")), fixtureCatalog(t, "beta", []byte("b"))
			for i, s := range []*Snapshot{a, b} {
				asset := s.catalog.Tools[0].Platforms["linux-amd64"]
				s.catalog.Tools[0].Platforms = map[string]ManifestAsset{tt.platform: asset}
				s.catalog.Tools[0].Executable = []string{tt.first, tt.second}[i]
			}
			if _, err := overlayCatalog(a, b, false); err == nil {
				t.Fatal("accepted distinct tools that overwrite the same executable")
			}
		})
	}
}
