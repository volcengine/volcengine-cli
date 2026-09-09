package proxycli

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/volcengine/volcengine-cli/upgrade"
)

type Source string

const (
	SourcePath     Source = "path"
	SourceManaged  Source = "managed"
	SourceCDN      Source = "cdn"
	SourceEmbedded Source = "embedded-fallback"
)

type Status string

const (
	StatusExternal  Status = "externally-managed"
	StatusInstalled Status = "installed"
	StatusUpdated   Status = "updated"
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
	SchemaVersion int                     `json:"schemaVersion"`
	Tools         map[Tool]*InstalledTool `json:"tools"`
}

type Resolution struct {
	Tool    Tool
	Path    string
	Version string
	Source  Source
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
	LookPath         func(string) (string, error)
	GOOS             string
	GOARCH           string

	allowHTTPForTests bool
}

type releaseCandidate struct {
	manifest    Manifest
	manifestURL string
	source      Source
}

func NewManager() (*Manager, error) {
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
		ConfigDir:        configDir,
		BinDir:           filepath.Clean(binDir),
		ManifestURL:      DefaultManifestURL,
		FallbackManifest: append([]byte(nil), embeddedFallbackManifest...),
		HTTPClient:       &http.Client{Timeout: 30 * time.Second},
		LookPath:         exec.LookPath,
		GOOS:             runtime.GOOS,
		GOARCH:           runtime.GOARCH,
	}, nil
}

// Resolve applies the fixed lookup order: the user's PATH first, followed by
// the ve-managed proxy CLI directory. A missing managed binary is installed
// lazily from the independent proxy CLI manifest.
func (m *Manager) Resolve(tool Tool) (Resolution, error) {
	if _, err := specFor(tool); err != nil {
		return Resolution{}, err
	}
	if path, external, err := m.externalPath(tool); err != nil {
		return Resolution{}, err
	} else if external {
		return Resolution{Tool: tool, Path: path, Source: SourcePath}, nil
	}

	lock, err := acquireFileLock(m.lockPath(), 30*time.Second)
	if err != nil {
		return Resolution{}, err
	}
	defer lock.release()

	state, err := m.readState()
	if err != nil {
		return Resolution{}, err
	}
	managedPath := m.managedPath(tool)
	entry := state.Tools[tool]
	if entry != nil && samePath(entry.Path, managedPath) {
		if err := validateManagedBinary(entry, managedPath); err == nil {
			return Resolution{Tool: tool, Path: managedPath, Version: entry.Version, Source: SourceManaged}, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return Resolution{}, err
		}
	}
	if entry == nil || !samePath(entry.Path, managedPath) {
		if exists, statErr := regularFileExists(managedPath); statErr != nil {
			return Resolution{}, statErr
		} else if exists {
			return Resolution{}, fmt.Errorf("proxy CLI binary is not managed by ve: %s", managedPath)
		}
		entry = nil
	}

	installed, status, err := m.installLatest(tool, entry)
	if err != nil {
		return Resolution{}, err
	}
	_ = status
	state.Tools[tool] = installed
	if err := m.writeState(state); err != nil {
		return Resolution{}, err
	}
	return Resolution{Tool: tool, Path: installed.Path, Version: installed.Version, Source: installed.Source}, nil
}

// Upgrade installs or upgrades ve-managed proxy CLIs. Binaries found first on
// PATH are deliberately left to their original package manager.
func (m *Manager) Upgrade(tools []Tool) (UpgradeResult, error) {
	if len(tools) == 0 {
		tools = KnownTools()
	}
	tools = uniqueTools(tools)
	result := UpgradeResult{Items: make([]UpgradeItem, 0, len(tools))}
	managed := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		if _, err := specFor(tool); err != nil {
			return result, err
		}
		path, external, err := m.externalPath(tool)
		if err != nil {
			return result, err
		}
		if external {
			result.Items = append(result.Items, UpgradeItem{Tool: tool, Status: StatusExternal, Path: path, Source: SourcePath})
			continue
		}
		managed = append(managed, tool)
	}
	if len(managed) == 0 {
		return result, nil
	}

	lock, err := acquireFileLock(m.lockPath(), 30*time.Second)
	if err != nil {
		return result, err
	}
	defer lock.release()
	state, err := m.readState()
	if err != nil {
		return result, err
	}

	var installErrors []string
	for _, tool := range managed {
		path := m.managedPath(tool)
		entry := state.Tools[tool]
		if entry == nil || !samePath(entry.Path, path) {
			if exists, statErr := regularFileExists(path); statErr != nil {
				installErrors = append(installErrors, fmt.Sprintf("%s: %v", tool, statErr))
				continue
			} else if exists {
				installErrors = append(installErrors, fmt.Sprintf("%s: binary is not managed by ve: %s", tool, path))
				continue
			}
			entry = nil
		}
		installed, status, installErr := m.installLatest(tool, entry)
		if installErr != nil {
			installErrors = append(installErrors, fmt.Sprintf("%s: %v", tool, installErr))
			continue
		}
		state.Tools[tool] = installed
		result.Items = append(result.Items, UpgradeItem{
			Tool: tool, Status: status, Version: installed.Version, Path: installed.Path, Source: installed.Source,
		})
	}
	if err := m.writeState(state); err != nil {
		return result, err
	}
	if len(installErrors) > 0 {
		return result, fmt.Errorf("proxy CLI upgrade failed (%s)", strings.Join(installErrors, "; "))
	}
	return result, nil
}

func (m *Manager) installLatest(tool Tool, current *InstalledTool) (*InstalledTool, Status, error) {
	candidates, resolutionErrors := m.releaseCandidates()
	var failures []string
	failedNewerCandidate := false
	failures = append(failures, resolutionErrors...)
	for _, candidate := range candidates {
		release, ok := candidate.manifest.Tools[tool]
		if !ok {
			failures = append(failures, fmt.Sprintf("%s manifest: no entry for %s", candidate.source, tool))
			continue
		}
		isNewer := current != nil && upgrade.IsNewer(current.Version, release.Version)
		asset, ok := release.Platforms[platformKey(m.GOOS, m.GOARCH)]
		if !ok {
			if isNewer {
				failedNewerCandidate = true
			}
			failures = append(failures, fmt.Sprintf("%s manifest: %s does not support %s/%s", candidate.source, tool, m.GOOS, m.GOARCH))
			continue
		}
		if current != nil {
			switch {
			case upgrade.SameVersion(current.Version, release.Version):
				if !strings.EqualFold(current.ArtifactSHA256, asset.SHA256) {
					return nil, "", fmt.Errorf("%s manifest: %s %s changed sha256 for the same version", candidate.source, tool, release.Version)
				}
				if err := validateManagedBinary(current, current.Path); err == nil {
					if failedNewerCandidate {
						failures = append(failures, fmt.Sprintf("%s manifest: %s %s is not newer than the failed candidate", candidate.source, tool, release.Version))
						continue
					}
					return current, StatusCurrent, nil
				}
			case !upgrade.IsNewer(current.Version, release.Version):
				// Never replace a newer locally managed release with an older
				// embedded fallback.
				if err := validateManagedBinary(current, current.Path); err == nil {
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
			status = StatusUpdated
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

	spec, _ := specFor(tool)
	binary := artifact
	if spec.format == archiveZip {
		binary, err = extractBinaryFromZip(artifact, archiveBinaryName(tool, m.GOOS))
		if err != nil {
			return nil, err
		}
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
	path, err := lookup(executableName(tool, m.GOOS))
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
	if samePath(abs, m.managedPath(tool)) {
		return "", false, nil
	}
	return abs, true, nil
}

func (m *Manager) managedPath(tool Tool) string {
	return filepath.Join(m.BinDir, executableName(tool, m.GOOS))
}

func (m *Manager) statePath() string {
	return filepath.Join(m.ConfigDir, StateFileName)
}

func (m *Manager) lockPath() string {
	return filepath.Join(m.ConfigDir, ".install.lock")
}

func (m *Manager) readState() (State, error) {
	state := State{SchemaVersion: StateSchemaVersion, Tools: map[Tool]*InstalledTool{}}
	data, err := os.ReadFile(m.statePath())
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read proxy CLI install state: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("parse proxy CLI install state: %w", err)
	}
	if state.SchemaVersion != StateSchemaVersion || state.Tools == nil {
		return State{}, fmt.Errorf("unsupported proxy CLI install state schema %d", state.SchemaVersion)
	}
	for tool, entry := range state.Tools {
		if _, err := specFor(tool); err != nil || entry == nil {
			return State{}, fmt.Errorf("invalid proxy CLI install state entry %q", tool)
		}
		if err := upgrade.ValidateVersion(entry.Version); err != nil || !validSHA256(entry.ArtifactSHA256) || !validSHA256(entry.BinarySHA256) || !filepath.IsAbs(entry.Path) {
			return State{}, fmt.Errorf("invalid proxy CLI install state metadata for %q", tool)
		}
		switch entry.Source {
		case SourceCDN, SourceEmbedded:
		default:
			return State{}, fmt.Errorf("invalid proxy CLI install source %q for %q", entry.Source, tool)
		}
	}
	return state, nil
}

func (m *Manager) writeState(state State) error {
	if state.Tools == nil {
		state.Tools = map[Tool]*InstalledTool{}
	}
	state.SchemaVersion = StateSchemaVersion
	if err := os.MkdirAll(m.ConfigDir, 0700); err != nil {
		return fmt.Errorf("create proxy CLI state directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(m.ConfigDir, ".install-state-*.tmp")
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
	if runtime.GOOS == "windows" {
		_ = os.Remove(m.statePath())
	}
	if err := os.Rename(temporaryPath, m.statePath()); err != nil {
		return fmt.Errorf("replace proxy CLI install state: %w", err)
	}
	return nil
}

func (m *Manager) download(rawURL string, limit int64) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "https" && !(m.allowHTTPForTests && parsed.Scheme == "http")) {
		return nil, fmt.Errorf("proxy CLI download URL must use https: %q", rawURL)
	}
	client := m.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	copyClient := *client
	previousRedirect := client.CheckRedirect
	copyClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > 0 && via[0].URL.Scheme == "https" && request.URL.Scheme != "https" {
			return fmt.Errorf("refusing to redirect an https download to %s", request.URL.Scheme)
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

func extractBinaryFromZip(data []byte, binaryName string) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open proxy CLI archive: %w", err)
	}
	var binary []byte
	for _, file := range reader.File {
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
	temporary, err := os.CreateTemp(filepath.Dir(path), ".proxy-cli-install-*")
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
	if goos == "windows" {
		_ = os.Remove(path)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish proxy CLI binary: %w", err)
	}
	return os.Chmod(path, 0755)
}

func validateManagedBinary(entry *InstalledTool, path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect managed proxy CLI %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("managed proxy CLI is not a regular file: %s", path)
	}
	digest, err := digestFile(path)
	if err != nil {
		return err
	}
	if digest != strings.ToLower(entry.BinarySHA256) {
		return fmt.Errorf("managed proxy CLI has local changes: %s", path)
	}
	return nil
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

func regularFileExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("proxy CLI path is not a regular file: %s", path)
	}
	return true, nil
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
