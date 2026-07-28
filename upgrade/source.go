package upgrade

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

const (
	defaultCDNBaseURL    = "https://cloudcache.volccdn.com/ve"
	defaultGitHubAPIBase = "https://api.github.com"
	defaultGitHubOwner   = "volcengine"
	defaultGitHubRepo    = "volcengine-cli"
	officialReleasesURL  = "https://github.com/volcengine/volcengine-cli/releases"
)

// EnvDownloadBaseURL overrides the CDN base URL (same as npm install.js).
const EnvDownloadBaseURL = "VOLCENGINE_CLI_DOWNLOAD_BASE_URL"

// VersionManifest is the official CDN version listing.
type VersionManifest struct {
	Latest         string `json:"latest"`
	MinSupported   string `json:"min_supported"`
	SecurityUpdate bool   `json:"security_update"`
	Channels       struct {
		CDNBase string `json:"cdn_base"`
	} `json:"channels"`
}

// AssetSource describes where to download a given version's archive and checksums.
type AssetSource struct {
	Version      string
	ArchiveName  string
	ArchiveURL   string
	ChecksumURL  string
	ChecksumName string
}

// normalizeBaseURL strips trailing slashes.
func normalizeBaseURL(url string) string {
	return strings.TrimRight(strings.TrimSpace(url), "/")
}

// CDNBaseURL returns the configured CDN base.
func CDNBaseURL() string {
	if v := strings.TrimSpace(os.Getenv(EnvDownloadBaseURL)); v != "" {
		return normalizeBaseURL(v)
	}
	return defaultCDNBaseURL
}

// ResolveLatestVersion finds the latest release version (upgrade path, full timeout).
// Order: CDN version_manifest.json → CDN latest text → GitHub Releases API.
func ResolveLatestVersion() (string, error) {
	return resolveLatestVersion(httpClient)
}

// ResolveLatestVersionQuick is for background checks: short timeout only, no long fallback.
func ResolveLatestVersionQuick() (string, error) {
	return resolveLatestVersion(checkHTTPClient)
}

func resolveLatestVersion(client *http.Client) (string, error) {
	if client == nil {
		client = httpClient
	}
	if v, err := fetchLatestFromCDNManifest(client); err == nil && v != "" {
		return NormalizeVersion(v), nil
	}
	if v, err := fetchLatestFromCDNLatestFile(client); err == nil && v != "" {
		return NormalizeVersion(v), nil
	}
	v, err := fetchLatestFromGitHub(client)
	if err != nil {
		return "", fmt.Errorf("failed to resolve latest version from CDN and GitHub: %v", err)
	}
	return NormalizeVersion(v), nil
}

// ResolveAssetSource builds download URLs for a target version.
// Prefers CDN layout; archive/checksum paths match npm install.js.
// When the CDN probe fails, falls back to GitHub Releases. If both fail,
// returns an error that includes CDN probe status and the GitHub failure
// (instead of pretending resolve succeeded and deferring a vague download error).
func ResolveAssetSource(version string) (*AssetSource, error) {
	version = NormalizeVersion(version)
	if err := ValidateVersion(version); err != nil {
		return nil, err
	}
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	if goos == "darwin" && (goarch == "386" || goarch == "arm") {
		return nil, fmt.Errorf("unsupported platform: %s/%s", goos, goarch)
	}
	if goos == "windows" && goarch == "arm" {
		return nil, fmt.Errorf("unsupported platform: %s/%s", goos, goarch)
	}

	archive := ArchiveName(version, goos, goarch)
	checksum := ChecksumName(version)
	base := CDNBaseURL()

	src := &AssetSource{
		Version:      version,
		ArchiveName:  archive,
		ArchiveURL:   fmt.Sprintf("%s/v%s/%s", base, version, archive),
		ChecksumName: checksum,
		ChecksumURL:  fmt.Sprintf("%s/v%s/%s", base, version, checksum),
	}

	// Both files are required for a safe install. Fall back to GitHub when the
	// archive or its checksum is absent from the CDN.
	probeClient := &http.Client{Timeout: 10 * time.Second}
	archiveOK := urlOK(probeClient, src.ArchiveURL)
	checksumOK := urlOK(probeClient, src.ChecksumURL)
	if archiveOK && checksumOK {
		return src, nil
	}

	gh, err := resolveAssetSourceFromGitHub(version, archive, checksum)
	if err == nil {
		return gh, nil
	}

	cdnDetail := cdnProbeDetail(src.ArchiveURL, src.ChecksumURL, archiveOK, checksumOK)
	return nil, fmt.Errorf(
		"could not resolve download assets for v%s: %s; GitHub fallback failed: %v; see %s",
		version, cdnDetail, err, officialReleasesURL,
	)
}

// cdnProbeDetail describes which CDN assets failed the existence probe.
func cdnProbeDetail(archiveURL, checksumURL string, archiveOK, checksumOK bool) string {
	switch {
	case !archiveOK && !checksumOK:
		return fmt.Sprintf("CDN archive and checksum unavailable (%s, %s)", archiveURL, checksumURL)
	case !archiveOK:
		return fmt.Sprintf("CDN archive unavailable (%s)", archiveURL)
	default:
		return fmt.Sprintf("CDN checksum unavailable (%s)", checksumURL)
	}
}

func fetchLatestFromCDNManifest(client *http.Client) (string, error) {
	url := CDNBaseURL() + "/version_manifest.json"
	body, err := FetchURLBytes(client, url, 64*1024)
	if err != nil {
		return "", err
	}
	var m VersionManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return "", err
	}
	if strings.TrimSpace(m.Latest) == "" {
		return "", fmt.Errorf("empty latest in version_manifest.json")
	}
	return m.Latest, nil
}

func fetchLatestFromCDNLatestFile(client *http.Client) (string, error) {
	url := CDNBaseURL() + "/latest"
	body, err := FetchURLBytes(client, url, 64)
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(body))
	if v == "" {
		return "", fmt.Errorf("empty latest file")
	}
	// reject HTML error pages
	if strings.Contains(v, "<") {
		return "", fmt.Errorf("invalid latest file content")
	}
	return v, nil
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func fetchLatestFromGitHub(client *http.Client) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", defaultGitHubAPIBase, defaultGitHubOwner, defaultGitHubRepo)
	body, err := FetchURLBytes(client, url, 256*1024)
	if err != nil {
		return "", err
	}
	var rel githubRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", err
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return "", fmt.Errorf("empty tag_name from GitHub")
	}
	return rel.TagName, nil
}

func resolveAssetSourceFromGitHub(version, archive, checksum string) (*AssetSource, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/tags/v%s",
		defaultGitHubAPIBase, defaultGitHubOwner, defaultGitHubRepo, version)
	// also try without forcing v prefix on tag
	body, err := FetchURLBytes(httpClient, url, 512*1024)
	if err != nil {
		url2 := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s",
			defaultGitHubAPIBase, defaultGitHubOwner, defaultGitHubRepo, version)
		body, err = FetchURLBytes(httpClient, url2, 512*1024)
		if err != nil {
			return nil, err
		}
	}
	var rel githubRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, err
	}
	src := &AssetSource{
		Version:      version,
		ArchiveName:  archive,
		ChecksumName: checksum,
	}
	for _, a := range rel.Assets {
		switch a.Name {
		case archive:
			src.ArchiveURL = a.BrowserDownloadURL
		case checksum:
			src.ChecksumURL = a.BrowserDownloadURL
		}
	}
	if src.ArchiveURL == "" {
		return nil, fmt.Errorf("archive %s not found in GitHub release v%s; see %s",
			archive, version, officialReleasesURL)
	}
	if src.ChecksumURL == "" {
		return nil, fmt.Errorf("checksum file %s not found in GitHub release v%s", checksum, version)
	}
	return src, nil
}

func urlOK(client *http.Client, url string) bool {
	if client == nil {
		client = httpClient
	}
	// Prefer HEAD so we never pull multi-MB archives just to probe existence.
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		// Defensive: rare non-nil resp+err (e.g. redirect edge cases).
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		// Some endpoints reject HEAD; fall back to a ranged GET of 1 byte.
		req, err = http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return false
		}
		req.Header.Set("Range", "bytes=0-0")
		resp, err = client.Do(req)
		if err != nil {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			return false
		}
	}
	defer resp.Body.Close()
	// 200 OK or 206 Partial Content both mean the object exists.
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent
}

// OfficialReleasesURL is used in user-facing error messages.
func OfficialReleasesURL() string {
	return officialReleasesURL
}
