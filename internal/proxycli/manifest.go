package proxycli

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/volcengine/volcengine-cli/upgrade"
)

const (
	DefaultManifestURL = "https://cloudcache.volccdn.com/ve/proxycli/manifest.json"
	proxyCDNHost       = "cloudcache.volccdn.com"
	EnvProxyPath       = "VOLCENGINE_CLI_PROXY_PATH"
	StateFileName      = "install-state.json"

	maxManifestBytes = int64(1 << 20)
	maxArtifactBytes = int64(512 << 20)
	maxBinaryBytes   = int64(256 << 20)
)

//go:embed fallback_manifest.json
var embeddedFallbackManifest []byte

type Tool string

const (
	ToolArkCLI  Tool = "arkcli"
	ToolTOSUtil Tool = "tosutil"
)

// LegacyTools is the historical default install set, not catalog enumeration.
func LegacyTools() []Tool {
	return []Tool{ToolArkCLI, ToolTOSUtil}
}

func ParseTool(value string) (Tool, error) { return EmbeddedSnapshot().ParseTool(value) }

// executableName is retained for v1 compatibility tests; installers resolve
// names and formats from their per-invocation ToolSpec snapshot.
func executableName(tool Tool, goos string) string {
	spec, err := EmbeddedSnapshot().Ready(tool)
	if err != nil {
		return ""
	}
	return toolExecutableName(spec, goos)
}

func platformKey(goos, goarch string) string {
	return goos + "-" + goarch
}

type Manifest struct {
	Tools map[Tool]ManifestTool `json:"tools"`
}

type ManifestTool struct {
	Version   string                   `json:"version"`
	Platforms map[string]ManifestAsset `json:"platforms"`
}

// ManifestAsset is deliberately identical for every product. Installation
// uses the built-in native ZIP convention (Ark retains its legacy raw format).
type ManifestAsset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func parseManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse proxy CLI manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Manifest{}, fmt.Errorf("parse proxy CLI manifest: trailing data")
	}
	if len(manifest.Tools) == 0 {
		return Manifest{}, fmt.Errorf("proxy CLI manifest contains no tools")
	}
	for tool, release := range manifest.Tools {
		if _, err := EmbeddedSnapshot().Ready(tool); err != nil {
			continue
		}
		if err := upgrade.ValidateVersion(release.Version); err != nil {
			return Manifest{}, fmt.Errorf("invalid %s version: %w", tool, err)
		}
		if len(release.Platforms) == 0 {
			return Manifest{}, fmt.Errorf("proxy CLI manifest has no platforms for %s", tool)
		}
		for platform, asset := range release.Platforms {
			if !validPlatformKey(platform) {
				return Manifest{}, fmt.Errorf("invalid proxy CLI platform %q for %s", platform, tool)
			}
			if strings.TrimSpace(asset.URL) == "" || !validSHA256(asset.SHA256) || asset.Size < 0 || asset.Size > maxArtifactBytes {
				return Manifest{}, fmt.Errorf("invalid proxy CLI asset for %s/%s", tool, platform)
			}
		}
	}
	return manifest, nil
}

func validPlatformKey(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range strings.ToLower(value) {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}

func resolveAssetURL(manifestURL, value string, allowHTTP bool) (string, error) {
	base, err := url.Parse(manifestURL)
	if err != nil {
		return "", err
	}
	reference, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	resolved := base.ResolveReference(reference)
	if resolved.Scheme != "https" && !(allowHTTP && resolved.Scheme == "http") {
		return "", fmt.Errorf("proxy CLI download URL must use https: %q", resolved.String())
	}
	if base.Scheme == "https" && resolved.Scheme != "https" {
		return "", fmt.Errorf("refusing to redirect an https manifest to %s", resolved.Scheme)
	}
	if !allowHTTP {
		if err := requireTrustedArtifact(resolved.String()); err != nil {
			return "", err
		}
	}
	return resolved.String(), nil
}

func defaultBinDir(home string) string {
	return filepath.Join(home, ".volcengine", "proxy-cli", "bin")
}
