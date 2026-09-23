package proxycli

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Additional audited, first-party distribution prefixes. Artifacts still need
// their catalog's SHA256 and size; this is not an arbitrary URL/plugin option.
//
//go:embed trusted_sources.json
var trustedSourcesJSON []byte

var trustedArtifactPrefixes = loadTrustedArtifactPrefixes()

func loadTrustedArtifactPrefixes() []string {
	var prefixes []string
	if err := json.Unmarshal(trustedSourcesJSON, &prefixes); err != nil {
		panic(err)
	}
	return prefixes
}
func trustedArtifactURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") || u.Fragment != "" {
		return false
	}
	if !unambiguousArtifactPath(u.Path) {
		return false
	}
	if strings.EqualFold(u.Hostname(), proxyCDNHost) {
		return true
	}
	for _, prefix := range trustedArtifactPrefixes {
		p, err := url.Parse(prefix)
		if err == nil && strings.EqualFold(u.Hostname(), p.Hostname()) && ((strings.HasSuffix(p.Path, "/") && strings.HasPrefix(u.Path, p.Path)) || u.Path == p.Path) {
			return true
		}
	}
	return false
}

// url.Parse decodes one escape layer but does not clean dot segments. Check
// further layers too: a CDN/origin may decode them again. Encoded npm scope
// separators remain valid; only dot segments, backslashes and malformed or excessively
// nested escaping are rejected. The bound prevents quadratic decoding work.
func unambiguousArtifactPath(path string) bool {
	for depth := 0; depth < 8; depth++ {
		if strings.Contains(path, `\`) {
			return false
		}
		for _, part := range strings.Split(path, "/") {
			if part == "." || part == ".." {
				return false
			}
		}
		decoded, err := url.PathUnescape(path)
		if err != nil {
			return false
		}
		if decoded == path {
			return true
		}
		path = decoded
	}
	return false
}

func trustedArtifactRedirect(original, next *url.URL) bool {
	if next.Scheme != "https" || next.User != nil || (next.Port() != "" && next.Port() != "443") || !unambiguousArtifactPath(next.Path) {
		return false
	}
	if trustedArtifactURL(next.String()) {
		return true
	}
	// GitHub's release download redirects to a short-lived signed asset URL.
	return original.Hostname() == "github.com" && trustedArtifactURL(original.String()) && next.Hostname() == "release-assets.githubusercontent.com"
}
func requireTrustedArtifact(raw string) error {
	if !trustedArtifactURL(raw) {
		return fmt.Errorf("proxy CLI artifact URL is not an audited official distribution source: %q", raw)
	}
	return nil
}
