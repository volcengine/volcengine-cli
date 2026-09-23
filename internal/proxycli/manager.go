package proxycli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/volcengine/volcengine-cli/upgrade"
)

var errPATHScript = errors.New("PATH proxy is a script/wrapper")

type Source string

const (
	SourcePath     Source = "path"
	SourceManaged  Source = "managed"
	SourceCatalog  Source = "catalog"
	SourceCDN      Source = "cdn"
	SourceEmbedded Source = "embedded-fallback"
)

type Status string

const (
	StatusExternal  Status = "externally-managed"
	StatusInstalled Status = "installed"
	StatusUpdated   Status = "updated"
	StatusRestored  Status = "restored"
	StatusCurrent   Status = "current"
)

type InstalledTool struct {
	Version        string `json:"version"`
	ArtifactSHA256 string `json:"artifactSha256"`
	BinarySHA256   string `json:"binarySha256"`
	Path           string `json:"path"`
	Source         Source `json:"source"`
	UpdatedAt      string `json:"updatedAt"`
}

type State struct {
	Tools map[Tool]*InstalledTool `json:"tools"`
}

type Resolution struct {
	Tool        Tool
	Path        string
	Version     string
	Source      Source
	ArgsPrefix  []string
	Environment map[string]string
	TargetPath  string
}

type UpgradeItem struct {
	Tool    Tool
	Status  Status
	Version string
	Path    string
	Source  Source
}

type UpgradeResult struct {
	Items []UpgradeItem
}

type Manager struct {
	HomeDir          string
	ConfigDir        string
	BinDir           string
	ManifestURL      string
	FallbackManifest []byte
	HTTPClient       *http.Client
	CheckHTTPClient  *http.Client
	LookPath         func(string) (string, error)
	GOOS             string
	GOARCH           string
	Now              func() time.Time
	Catalog          *Snapshot

	allowHTTPForTests bool
}

type releaseCandidate struct {
	manifest    Manifest
	manifestURL string
	source      Source
}

func NewManager() (*Manager, error) {
	store, err := NewCatalogStore()
	if err != nil {
		return nil, err
	}
	return NewManagerWithCatalog(store.Load())
}

func NewManagerWithCatalog(snapshot *Snapshot) (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home: %w", err)
	}
	configDir := filepath.Join(home, ".volcengine", "proxy-cli")
	binDir := strings.TrimSpace(os.Getenv(EnvProxyPath))
	if binDir == "" {
		binDir = defaultBinDir(home)
	} else if !filepath.IsAbs(binDir) {
		binDir, err = filepath.Abs(binDir)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", EnvProxyPath, err)
		}
	}
	return &Manager{
		HomeDir:          home,
		Catalog:          snapshot,
		ConfigDir:        configDir,
		BinDir:           filepath.Clean(binDir),
		ManifestURL:      DefaultManifestURL,
		FallbackManifest: append([]byte(nil), embeddedFallbackManifest...),
		HTTPClient:       newDownloadHTTPClient(),
		LookPath:         exec.LookPath,
		GOOS:             runtime.GOOS,
		GOARCH:           runtime.GOARCH,
	}, nil
}

// Resolve applies the fixed lookup order: the user's PATH first, followed by
// the ve-managed proxy CLI directory. A missing managed binary is installed
// lazily from the independent proxy CLI manifest.
func (m *Manager) Resolve(tool Tool) (Resolution, error) {
	if _, err := m.toolSpec(tool); err != nil {
		return Resolution{}, err
	}
	if path, external, err := m.externalPath(tool); err != nil {
		return Resolution{}, err
	} else if external {
		return Resolution{Tool: tool, Path: path, TargetPath: path, Source: SourcePath}, nil
	}

	lock, err := acquireFileLock(m.toolLockPath(tool), 20*time.Minute)
	if err != nil {
		return Resolution{}, err
	}
	defer lock.release()
	removeFilesWithPrefix(m.BinDir, executableTempPrefix(m.managedPath(tool)))

	state, err := m.readInstallState()
	if err != nil {
		return Resolution{}, err
	}
	managedPath := m.managedPath(tool)
	entry := state.Tools[tool]
	if entry != nil && samePath(entry.Path, managedPath) {
		managedPath = entry.Path
		if err := validateManagedBinary(entry, managedPath); err == nil {
			return m.launchInstalled(tool, entry, SourceManaged)
		} else if !errors.Is(err, os.ErrNotExist) {
			return Resolution{}, err
		}
	}
	if entry == nil || !samePath(entry.Path, managedPath) {
		if statErr := validateReplaceableManagedPath(managedPath); statErr != nil {
			return Resolution{}, statErr
		}
		if !m.legacyManagedEntry(tool, entry) {
			entry = nil
		}
	}

	installed, _, err := m.installLatest(tool, entry)
	if err != nil {
		return Resolution{}, err
	}
	if err := m.recordInstallation(tool, installed); err != nil {
		return Resolution{}, err
	}
	return m.launchInstalled(tool, installed, installed.Source)
}

// Upgrade installs or upgrades ve-managed proxy CLIs. Binaries found first on
// PATH are deliberately left to their original package manager.
func (m *Manager) Upgrade(tools []Tool) (UpgradeResult, error) {
	if len(tools) == 0 {
		var err error
		tools, err = m.DefaultUpgradeTools()
		if err != nil {
			return UpgradeResult{}, err
		}
	}
	tools = uniqueTools(tools)
	result := UpgradeResult{Items: make([]UpgradeItem, 0, len(tools))}
	managed := make([]Tool, 0, len(tools))
	var installErrors []string
	for _, tool := range tools {
		if _, err := m.toolSpec(tool); err != nil {
			installErrors = append(installErrors, fmt.Sprintf("%s: %v", tool, err))
			continue
		}
		path, external, err := m.externalPath(tool)
		if err != nil {
			installErrors = append(installErrors, fmt.Sprintf("%s: %v", tool, err))
			continue
		}
		if external {
			result.Items = append(result.Items, UpgradeItem{Tool: tool, Status: StatusExternal, Path: path, Source: SourcePath})
			continue
		}
		managed = append(managed, tool)
	}
	if len(managed) == 0 {
		if len(installErrors) > 0 {
			return result, fmt.Errorf("proxy CLI upgrade failed (%s)", strings.Join(installErrors, "; "))
		}
		return result, nil
	}

	for _, tool := range managed {
		installed, status, installErr := m.upgradeTool(tool)
		if installErr != nil {
			installErrors = append(installErrors, fmt.Sprintf("%s: %v", tool, installErr))
			continue
		}
		result.Items = append(result.Items, UpgradeItem{
			Tool: tool, Status: status, Version: installed.Version, Path: installed.Path, Source: installed.Source,
		})
	}
	if len(installErrors) > 0 {
		return result, fmt.Errorf("proxy CLI upgrade failed (%s)", strings.Join(installErrors, "; "))
	}
	return result, nil
}

// Only calls for the same executable wait on downloads. The bounded wait
// accommodates the manifest, artifact and fallback download timeouts.
func (m *Manager) upgradeTool(tool Tool) (*InstalledTool, Status, error) {
	lock, err := acquireFileLock(m.toolLockPath(tool), 20*time.Minute)
	if err != nil {
		return nil, "", err
	}
	defer lock.release()
	removeFilesWithPrefix(m.BinDir, executableTempPrefix(m.managedPath(tool)))
	state, err := m.readInstallState()
	if err != nil {
		return nil, "", err
	}
	path := m.managedPath(tool)
	entry := state.Tools[tool]
	if entry == nil || !samePath(entry.Path, path) {
		if err := validateReplaceableManagedPath(path); err != nil {
			return nil, "", err
		}
		if !m.legacyManagedEntry(tool, entry) {
			entry = nil
		}
	}
	installed, status, err := m.installLatest(tool, entry)
	if err != nil {
		return nil, "", err
	}
	if err := m.recordInstallation(tool, installed); err != nil {
		return nil, "", err
	}
	return installed, status, nil
}

// Hold the shared state lock only for local state I/O, never network I/O.
// Binary temporary files are scoped to their executable and protected by its
// tool lock; the state lock protects legacy temporary files and state files.
func (m *Manager) readInstallState() (State, error) {
	lock, err := acquireFileLock(m.lockPath(), 30*time.Second)
	if err != nil {
		return State{}, err
	}
	defer lock.release()
	m.cleanupStaleInstallTemps()
	return m.readState()
}

func (m *Manager) recordInstallation(tool Tool, installed *InstalledTool) error {
	lock, err := acquireFileLock(m.lockPath(), 30*time.Second)
	if err != nil {
		return err
	}
	defer lock.release()
	// Reload after the download so another tool's completed install survives.
	state, err := m.readState()
	if err != nil {
		return err
	}
	state.Tools[tool] = installed
	return m.writeState(state)
}

func (m *Manager) installLatest(tool Tool, current *InstalledTool) (*InstalledTool, Status, error) {
	candidates, resolutionErrors := m.toolReleaseCandidates(tool)
	var failures []string
	failedNewerCandidate := false
	failures = append(failures, resolutionErrors...)
	for _, candidate := range candidates {
		release, ok := candidate.manifest.Tools[tool]
		if !ok {
			failures = append(failures, fmt.Sprintf("%s manifest: no entry for %s", candidate.source, tool))
			continue
		}
		if err := upgrade.ValidateVersion(release.Version); err != nil {
			failures = append(failures, fmt.Sprintf("%s manifest: invalid %s version: %v", candidate.source, tool, err))
			continue
		}
		isNewer := current != nil && upgrade.IsNewer(current.Version, release.Version)
		asset, ok := release.Platforms[platformKey(m.GOOS, m.GOARCH)]
		if !ok {
			failures = append(failures, fmt.Sprintf("%s manifest: %s does not support %s/%s", candidate.source, tool, m.GOOS, m.GOARCH))
			continue
		}
		restoring := false
		if current != nil {
			relocating := !samePath(current.Path, m.managedPath(tool))
			switch {
			case upgrade.SameVersion(current.Version, release.Version):
				if !relocating && !strings.EqualFold(current.ArtifactSHA256, asset.SHA256) {
					return nil, "", fmt.Errorf("%s manifest: %s %s changed sha256 for the same version", candidate.source, tool, release.Version)
				}
				if err := validateManagedBinary(current, current.Path); err == nil && !relocating {
					if failedNewerCandidate {
						failures = append(failures, fmt.Sprintf("%s manifest: %s %s is not newer than the failed candidate", candidate.source, tool, release.Version))
						continue
					}
					return current, StatusCurrent, nil
				}
				restoring = true
			case !upgrade.IsNewer(current.Version, release.Version):
				// Never replace a newer locally managed release with an older
				// embedded fallback.
				if err := validateManagedBinary(current, current.Path); err == nil && !relocating {
					if failedNewerCandidate {
						failures = append(failures, fmt.Sprintf("%s manifest: %s %s is older than the failed candidate", candidate.source, tool, release.Version))
						continue
					}
					return current, StatusCurrent, nil
				}
				failures = append(failures, fmt.Sprintf("%s manifest: resolved %s %s is older than unusable installed version %s", candidate.source, tool, release.Version, current.Version))
				continue
			}
		}

		installed, err := m.installCandidate(tool, release.Version, asset, candidate)
		if err != nil {
			if isNewer {
				failedNewerCandidate = true
			}
			failures = append(failures, fmt.Sprintf("%s: %v", candidate.source, err))
			continue
		}
		status := StatusInstalled
		if current != nil {
			if restoring {
				status = StatusRestored
			} else {
				status = StatusUpdated
			}
		}
		return installed, status, nil
	}
	return nil, "", fmt.Errorf("install %s failed (%s)", tool, strings.Join(failures, "; "))
}

func (m *Manager) releaseCandidates() ([]releaseCandidate, []string) {
	var candidates []releaseCandidate
	var failures []string
	remoteData, err := m.download(m.ManifestURL, maxManifestBytes)
	if err != nil {
		failures = append(failures, "cdn manifest: "+err.Error())
	} else if manifest, parseErr := parseManifest(remoteData); parseErr != nil {
		failures = append(failures, "cdn manifest: "+parseErr.Error())
	} else {
		candidates = append(candidates, releaseCandidate{manifest: manifest, manifestURL: m.ManifestURL, source: SourceCDN})
	}
	fallback, err := parseManifest(m.FallbackManifest)
	if err != nil {
		failures = append(failures, "embedded manifest: "+err.Error())
	} else {
		fallbackBase := "https://embedded.invalid/manifest.json"
		if m.allowHTTPForTests {
			fallbackBase = "http://embedded.invalid/manifest.json"
		}
		candidates = append(candidates, releaseCandidate{manifest: fallback, manifestURL: fallbackBase, source: SourceEmbedded})
	}
	return candidates, failures
}

func (m *Manager) installCandidate(tool Tool, version string, asset ManifestAsset, candidate releaseCandidate) (*InstalledTool, error) {
	assetURL, err := resolveAssetURL(candidate.manifestURL, asset.URL, m.allowHTTPForTests)
	if err != nil {
		return nil, err
	}
	artifact, err := m.download(assetURL, maxArtifactBytes)
	if err != nil {
		return nil, err
	}
	if asset.Size > 0 && int64(len(artifact)) != asset.Size {
		return nil, fmt.Errorf("artifact size mismatch: expected %d, got %d", asset.Size, len(artifact))
	}
	artifactDigest := digestBytes(artifact)
	if artifactDigest != strings.ToLower(asset.SHA256) {
		return nil, fmt.Errorf("artifact sha256 mismatch: expected %s, got %s", asset.SHA256, artifactDigest)
	}

	spec, err := m.toolSpec(tool)
	if err != nil {
		return nil, err
	}
	binary := artifact
	if tool != ToolArkCLI {
		binary, err = extractBinaryFromZip(artifact, archiveEntrypoint(spec, m.GOOS))
		if err != nil {
			return nil, err
		}
	}
	if len(binary) >= 2 && string(binary[:2]) == "#!" {
		return nil, fmt.Errorf("native artifact is a script; this installer requires a standalone binary")
	}
	if len(binary) == 0 || int64(len(binary)) > maxBinaryBytes {
		return nil, fmt.Errorf("invalid extracted %s binary size %d", tool, len(binary))
	}
	path := m.managedPath(tool)
	if err := publishExecutable(path, binary, m.GOOS); err != nil {
		return nil, err
	}
	return &InstalledTool{
		Version:        upgrade.NormalizeVersion(version),
		ArtifactSHA256: artifactDigest,
		BinarySHA256:   digestBytes(binary),
		Path:           path,
		Source:         candidate.source,
		UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (m *Manager) externalPath(tool Tool) (string, bool, error) {
	lookup := m.LookPath
	if lookup == nil {
		lookup = exec.LookPath
	}
	spec, err := m.toolSpec(tool)
	if err != nil {
		return "", false, err
	}
	if spec.PathPolicy == "managed-only" || spec.Launch.Kind != "native" {
		return "", false, nil
	}
	path, err := lookup(toolExecutableName(spec, m.GOOS))
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false, err
	}
	if err := rejectHostExecutable(abs); err != nil {
		if errors.Is(err, errPATHScript) {
			return "", false, nil
		}
		return "", false, err
	}
	if sameFilePath(abs, m.managedPath(tool)) {
		return "", false, nil
	}
	return abs, true, nil
}

func (m *Manager) managedPath(tool Tool) string {
	spec, err := m.toolSpec(tool)
	if err != nil {
		return ""
	}
	return filepath.Join(m.BinDir, toolExecutableName(spec, m.GOOS))
}

func (m *Manager) statePath() string {
	if samePath(m.BinDir, defaultBinDir(m.HomeDir)) {
		return filepath.Join(m.ConfigDir, StateFileName)
	}
	absolute, err := filepath.Abs(m.BinDir)
	if err != nil {
		absolute = filepath.Clean(m.BinDir)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(absolute)))
	return filepath.Join(m.ConfigDir, fmt.Sprintf("install-state-%x.json", sum[:8]))
}

func (m *Manager) toolLockPath(tool Tool) string {
	return filepath.Join(m.ConfigDir, uniqueTempPrefix("install", m.managedPath(tool))+"lock")
}

func (m *Manager) lockPath() string {
	return m.statePath() + ".lock"
}

func (m *Manager) readState() (State, error) {
	empty := State{Tools: map[Tool]*InstalledTool{}}
	data, err := os.ReadFile(m.statePath())
	if os.IsNotExist(err) {
		return empty, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read proxy CLI install state: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var state State
	if err := decoder.Decode(&state); err != nil {
		return empty, nil
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return empty, nil
	}
	if state.Tools == nil {
		return empty, nil
	}
	for tool, entry := range state.Tools {
		if _, err := m.toolSpec(tool); err != nil {
			continue
		}
		if entry == nil {
			return empty, nil
		}
		if err := upgrade.ValidateVersion(entry.Version); err != nil || !validSHA256(entry.ArtifactSHA256) || !validSHA256(entry.BinarySHA256) || !filepath.IsAbs(entry.Path) {
			return empty, nil
		}
		switch entry.Source {
		case SourceCDN, SourceEmbedded, SourceCatalog:
		default:
			return empty, nil
		}
	}
	return state, nil
}

func (m *Manager) writeState(state State) error {
	if state.Tools == nil {
		state.Tools = map[Tool]*InstalledTool{}
	}
	if err := os.MkdirAll(m.ConfigDir, 0700); err != nil {
		return fmt.Errorf("create proxy CLI state directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(m.ConfigDir, m.stateTempPrefix()+"*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := replaceFile(temporaryPath, m.statePath()); err != nil {
		return fmt.Errorf("replace proxy CLI install state: %w", err)
	}
	return nil
}

func (m *Manager) download(rawURL string, limit int64) ([]byte, error) {
	return m.downloadWithClient(rawURL, limit, m.HTTPClient)
}

func (m *Manager) downloadWithClient(rawURL string, limit int64, client *http.Client) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "https" && !(m.allowHTTPForTests && parsed.Scheme == "http")) {
		return nil, fmt.Errorf("proxy CLI download URL must use https: %q", rawURL)
	}
	if !m.allowHTTPForTests {
		if err := requireTrustedArtifact(rawURL); err != nil {
			return nil, err
		}
	}
	if client == nil {
		client = newDownloadHTTPClient()
	}
	copyClient := *client
	previousRedirect := client.CheckRedirect
	copyClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > 0 && via[0].URL.Scheme == "https" && request.URL.Scheme != "https" {
			return fmt.Errorf("refusing to redirect an https download to %s", request.URL.Scheme)
		}
		if !m.allowHTTPForTests && !trustedArtifactRedirect(parsed, request.URL) {
			return fmt.Errorf("refusing proxy CLI redirect to unaudited source %s", request.URL.Hostname())
		}
		if previousRedirect != nil {
			return previousRedirect(request, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
	response, err := copyClient.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("HTTP %d for %s", response.StatusCode, rawURL)
	}
	if response.ContentLength > limit {
		return nil, fmt.Errorf("download exceeds %d bytes", limit)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download exceeds %d bytes", limit)
	}
	return data, nil
}

func newDownloadHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 4 * time.Minute,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
	}
}

func extractBinaryFromZip(data []byte, binaryName string) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open proxy CLI archive: %w", err)
	}
	var binary []byte
	var total uint64
	if len(reader.File) > 10000 {
		return nil, fmt.Errorf("too many archive members")
	}
	for _, file := range reader.File {
		if err := validateArchiveMember(file.Name); err != nil {
			return nil, err
		}
		if file.Mode()&os.ModeSymlink != 0 || (!file.FileInfo().IsDir() && !file.Mode().IsRegular()) {
			return nil, fmt.Errorf("archive contains non-regular file: %s", file.Name)
		}
		if file.UncompressedSize64 > uint64(maxBinaryBytes) || total > uint64(maxBinaryBytes)-file.UncompressedSize64 {
			return nil, fmt.Errorf("archive exceeds extracted size limit")
		}
		total += file.UncompressedSize64
		if file.FileInfo().IsDir() || filepath.Base(file.Name) != binaryName {
			continue
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("proxy CLI archive binary is a symlink: %s", file.Name)
		}
		if binary != nil {
			return nil, fmt.Errorf("proxy CLI archive contains multiple %q binaries", binaryName)
		}
		if file.UncompressedSize64 == 0 || file.UncompressedSize64 > uint64(maxBinaryBytes) {
			return nil, fmt.Errorf("invalid proxy CLI archive binary size %d", file.UncompressedSize64)
		}
		stream, err := file.Open()
		if err != nil {
			return nil, err
		}
		binary, err = io.ReadAll(io.LimitReader(stream, maxBinaryBytes+1))
		closeErr := stream.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if int64(len(binary)) != int64(file.UncompressedSize64) {
			return nil, fmt.Errorf("proxy CLI archive binary size mismatch")
		}
	}
	if binary == nil {
		return nil, fmt.Errorf("binary %q not found in proxy CLI archive", binaryName)
	}
	return binary, nil
}

func publishExecutable(path string, data []byte, goos string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), executableTempPrefix(path)+"*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0755); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := replaceFile(temporaryPath, path); err != nil {
		return fmt.Errorf("publish proxy CLI binary: %w", err)
	}
	return os.Chmod(path, 0755)
}

func validateManagedBinary(entry *InstalledTool, path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect managed proxy CLI %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("managed proxy CLI is not a regular file: %s; run 've proxycli upgrade' to restore the official binary", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("managed proxy CLI is not a regular file: %s; move it aside and retry 've proxycli upgrade'", path)
	}
	digest, err := digestFile(path)
	if err != nil {
		return err
	}
	if digest != strings.ToLower(entry.BinarySHA256) {
		return fmt.Errorf("managed proxy CLI has local changes: %s; run 've proxycli upgrade' to restore the official binary", path)
	}
	return nil
}

func executableTempPrefix(path string) string {
	return uniqueTempPrefix("binary", path)
}

func (m *Manager) stateTempPrefix() string {
	return uniqueTempPrefix("install-state", m.statePath())
}

func (m *Manager) cleanupStaleInstallTemps() {
	removeFilesWithPrefix(m.BinDir, ".proxy-cli-install-")
	removeFilesWithPrefix(m.ConfigDir, m.stateTempPrefix())
}

func removeFilesWithPrefix(directory, prefix string) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		_ = os.Remove(filepath.Join(directory, entry.Name()))
	}
}

func digestFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validateReplaceableManagedPath(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	return fmt.Errorf("proxy CLI path cannot be replaced automatically: %s (mode %s); move it aside and retry 've proxycli upgrade'", path, info.Mode())
}

func uniqueTempPrefix(kind, target string) string {
	target = filepath.Clean(target)
	// The lock and temporary-file namespace must agree for case aliases of
	// the same executable on the usual macOS and Windows filesystems. On
	// case-sensitive volumes this only adds harmless serialization.
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		target = strings.ToLower(target)
	}
	sum := sha256.Sum256([]byte(target))
	return fmt.Sprintf(".proxycli-%s-%x-", kind, sum[:8])
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return filepath.Clean(left) == filepath.Clean(right)
	}
	return filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func uniqueTools(tools []Tool) []Tool {
	seen := map[Tool]bool{}
	result := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		if seen[tool] {
			continue
		}
		seen[tool] = true
		result = append(result, tool)
	}
	return result
}

func (m *Manager) toolSpec(tool Tool) (ToolSpec, error) {
	snapshot := m.Catalog
	if snapshot == nil {
		snapshot = EmbeddedSnapshot()
	}
	return snapshot.Ready(tool)
}
func toolExecutableName(t ToolSpec, goos string) string {
	if goos == "windows" && !strings.HasSuffix(strings.ToLower(t.Executable), ".exe") {
		return t.Executable + ".exe"
	}
	return t.Executable
}

// ZIP entrypoints use the product executable name, except the historical TOS
// ZIP which contains "tosutil" even on Windows.
func archiveEntrypoint(spec ToolSpec, goos string) string {
	if spec.ID == ToolTOSUtil {
		return "tosutil"
	}
	return toolExecutableName(spec, goos)
}
func (m *Manager) launchInstalled(tool Tool, entry *InstalledTool, source Source) (Resolution, error) {
	return Resolution{Tool: tool, Path: entry.Path, TargetPath: entry.Path, Version: entry.Version, Source: source}, nil
}
func (m *Manager) toolReleaseCandidates(tool Tool) ([]releaseCandidate, []string) {
	candidates, failures := m.releaseCandidates()
	// A local catalog provides the offline release information for dynamically
	// registered products; downloads still use the same native installer.
	if m.Catalog != nil {
		if t, err := m.toolSpec(tool); err == nil {
			local := releaseCandidate{manifest: Manifest{Tools: map[Tool]ManifestTool{tool: {Version: t.ReleaseVersion, Platforms: t.Platforms}}}, manifestURL: DefaultManifestURL, source: SourceCatalog}
			if tool == ToolArkCLI || tool == ToolTOSUtil {
				embedded, _ := EmbeddedSnapshot().Lookup(string(tool))
				left, _ := json.Marshal(t.Platforms)
				right, _ := json.Marshal(embedded.Platforms)
				if t.ReleaseVersion != embedded.ReleaseVersion || !bytes.Equal(left, right) {
					// A reviewed cached catalog must not pin legacy tools: the
					// production manifest remains authoritative for upgrades.
					return append(candidates[:remoteCandidateCount(candidates)], local), failures
				}
			}
			// The catalog is an offline fallback for a known product. CDN stays
			// first, matching the legacy tosutil download path.
			if tool != ToolArkCLI && tool != ToolTOSUtil {
				return append(candidates[:remoteCandidateCount(candidates)], local), failures
			}
		}
	}
	return candidates, failures
}
func remoteCandidateCount(candidates []releaseCandidate) int {
	if len(candidates) > 0 && candidates[0].source == SourceCDN {
		return 1
	}
	return 0
}
func (m *Manager) DefaultUpgradeTools() ([]Tool, error) {
	state, err := m.readState()
	if err != nil {
		return nil, err
	}
	result := LegacyTools()
	var extra []string
	for tool := range state.Tools {
		if _, err := m.toolSpec(tool); err == nil && tool != ToolArkCLI && tool != ToolTOSUtil {
			extra = append(extra, string(tool))
		}
	}
	sort.Strings(extra)
	for _, tool := range extra {
		result = append(result, Tool(tool))
	}
	return result, nil
}
func sameFilePath(a, b string) bool {
	if samePath(a, b) {
		return true
	}
	ai, ae := os.Stat(a)
	bi, be := os.Stat(b)
	return ae == nil && be == nil && os.SameFile(ai, bi)
}
func rejectHostExecutable(path string) error {
	host, err := os.Executable()
	if err == nil && sameFilePath(host, path) {
		return fmt.Errorf("refusing recursive proxy executable %s (ve itself)", path)
	}
	if err == nil {
		hostInfo, he := os.Stat(host)
		childInfo, ce := os.Stat(path)
		if he == nil && ce == nil && hostInfo.Size() == childInfo.Size() {
			hostDigest, he := digestFile(host)
			childDigest, ce := digestFile(path)
			if he == nil && ce == nil && hostDigest == childDigest {
				return fmt.Errorf("refusing recursive proxy executable %s (copy of ve)", path)
			}
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head, _ := io.ReadAll(io.LimitReader(f, 4096))
	if bytes.HasPrefix(head, []byte("#!")) {
		return fmt.Errorf("%w with unverified identity: %s", errPATHScript, path)
	}
	return nil
}
func validateArchiveMember(name string) error {
	if name == "" || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return fmt.Errorf("unsafe archive path %q", name)
	}
	for _, part := range strings.Split(strings.TrimSuffix(name, "/"), "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("unsafe archive path %q", name)
		}
	}
	return nil
}
func extractBinaryFromTarGzip(data []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	// Bound the full decompressed stream (including headers and padding).
	limited := &io.LimitedReader{R: gz, N: maxBinaryBytes + (10 << 20)}
	reader := tar.NewReader(limited)
	var binary []byte
	var total int64
	for count := 0; ; count++ {
		h, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if count >= 10000 {
			return nil, fmt.Errorf("too many archive members")
		}
		if err := validateArchiveMember(h.Name); err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeDir && h.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("archive contains non-regular file: %s", h.Name)
		}
		if h.Size < 0 || h.Size > maxBinaryBytes || total > maxBinaryBytes-h.Size {
			return nil, fmt.Errorf("archive exceeds extracted size limit")
		}
		total += h.Size
		if h.Typeflag != tar.TypeReg || filepath.Base(h.Name) != name {
			continue
		}
		if binary != nil {
			return nil, fmt.Errorf("archive contains multiple %q binaries", name)
		}
		if h.Size == 0 {
			return nil, fmt.Errorf("empty archive binary")
		}
		binary, err = io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
	}
	if _, err := io.Copy(io.Discard, limited); err != nil {
		return nil, err
	}
	if limited.N <= 0 {
		return nil, fmt.Errorf("archive exceeds decompression limit")
	}
	if binary == nil {
		return nil, fmt.Errorf("binary %q not found in proxy CLI archive", name)
	}
	return binary, nil
}

// Previous development builds used bin/tools/<id>/... . Relocation may
// change the archive format, but must still preserve version ordering.
func (m *Manager) legacyManagedEntry(tool Tool, entry *InstalledTool) bool {
	if entry == nil {
		return false
	}
	formerName := string(tool)
	if m.GOOS == "windows" {
		formerName += ".exe"
	}
	if samePath(entry.Path, filepath.Join(m.BinDir, formerName)) {
		return true
	}
	root := filepath.Join(m.BinDir, "tools", string(tool))
	relative, err := filepath.Rel(root, entry.Path)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
