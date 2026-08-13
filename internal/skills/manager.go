package skills

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	ManifestSchemaVersion = 1
	StateSchemaVersion    = 1
	StateFileName         = "install-state.json"
	BundleFileName        = "volcengine-skill-bundle.zip"
	SourceCDN             = "cdn"
	SourceGitHub          = "github"
	SourceNPX             = "npx"

	defaultCDNManifestURL    = "https://cloudcache.volccdn.com/ve/skills/latest/manifest.json"
	defaultGitHubManifestURL = "https://github.com/volcengine/volcengine-skills/releases/latest/download/manifest.json"
	defaultNPXSkillSource    = "https://github.com/volcengine/volcengine-skills/tree/main/skills/core"
	maxManifestBytes         = 1024 * 1024
	maxBundleBytes           = 50 * 1024 * 1024
	maxExtractedBytes        = 100 * 1024 * 1024
	maxArchiveFiles          = 5000
)

type Manifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	Version       string          `json:"version"`
	Bundle        ManifestBundle  `json:"bundle"`
	Skills        []ManifestSkill `json:"skills"`
}

type ManifestBundle struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	URL    string `json:"url"`
}

type ManifestSkill struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type State struct {
	SchemaVersion       int                        `json:"schemaVersion"`
	LastResolvedVersion string                     `json:"lastResolvedVersion,omitempty"`
	LastResolvedSHA256  string                     `json:"lastResolvedSha256,omitempty"`
	Skills              map[string]*InstalledSkill `json:"skills"`
}

type InstalledSkill struct {
	Version       string                      `json:"version"`
	BundleSHA256  string                      `json:"bundleSha256"`
	ContentSHA256 string                      `json:"contentSha256"`
	Targets       map[string]*InstalledTarget `json:"targets"`
}

type InstalledTarget struct {
	Mode          string `json:"mode"`
	Path          string `json:"path"`
	ContentSHA256 string `json:"contentSha256,omitempty"`
}

type Result struct {
	Source    string
	Version   string
	Installed []string
	Updated   []string
	Removed   []string
	Skipped   []string
	Warnings  []string
}

type Manager struct {
	HomeDir           string
	ConfigDir         string
	CDNManifestURL    string
	GitHubManifestURL string
	HTTPClient        *http.Client
	ClaudeConfigDir   string
	HermesHome        string
	RunCommand        func(name string, args ...string) error
}

type release struct {
	manifest Manifest
	bundle   []byte
	source   string
}

type skillPayload struct {
	name   string
	files  map[string][]byte
	digest string
}

type targetSpec struct {
	name string
	path string
}

func NewManager() (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home: %w", err)
	}
	claudeHome := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
	if claudeHome == "" {
		claudeHome = filepath.Join(home, ".claude")
	}
	hermesHome := strings.TrimSpace(os.Getenv("HERMES_HOME"))
	if hermesHome == "" {
		hermesHome = filepath.Join(home, ".hermes")
	}
	return &Manager{
		HomeDir:           home,
		ConfigDir:         filepath.Join(home, ".volcengine", "skills"),
		CDNManifestURL:    envOrDefault("VOLCENGINE_CLI_SKILLS_MANIFEST_URL", defaultCDNManifestURL),
		GitHubManifestURL: envOrDefault("VOLCENGINE_CLI_SKILLS_GITHUB_MANIFEST_URL", defaultGitHubManifestURL),
		HTTPClient:        &http.Client{Timeout: 30 * time.Second},
		ClaudeConfigDir:   claudeHome,
		HermesHome:        hermesHome,
	}, nil
}

func (m *Manager) Install() (Result, error) {
	state, err := m.readState()
	if err != nil {
		return Result{}, err
	}
	rel, err := m.resolveRelease()
	if err != nil {
		return m.installWithNPX(err)
	}
	payloads, err := extractBundle(rel)
	if err != nil {
		return Result{}, err
	}
	result := Result{Source: rel.source, Version: rel.manifest.Version}
	for _, payload := range payloads {
		entry := state.Skills[payload.name]
		canonical := m.canonicalPath(payload.name)
		targetPayload := payload
		if entry == nil {
			if pathExists(canonical) {
				currentDigest, digestErr := digestDirectory(canonical)
				if digestErr != nil {
					result.skip(payload.name, "existing directory cannot be adopted: "+canonical)
					continue
				}
				if currentDigest != payload.digest {
					result.skip(payload.name, "existing directory is not managed by ve: "+canonical)
					continue
				}
				entry = &InstalledSkill{
					Version:       rel.manifest.Version,
					BundleSHA256:  rel.manifest.Bundle.SHA256,
					ContentSHA256: payload.digest,
					Targets:       map[string]*InstalledTarget{},
				}
				state.Skills[payload.name] = entry
				result.Installed = append(result.Installed, payload.name)
			} else {
				if err := installDirectory(canonical, payload.files); err != nil {
					return result, fmt.Errorf("install %s: %w", payload.name, err)
				}
				entry = &InstalledSkill{
					Version:       rel.manifest.Version,
					BundleSHA256:  rel.manifest.Bundle.SHA256,
					ContentSHA256: payload.digest,
					Targets:       map[string]*InstalledTarget{},
				}
				state.Skills[payload.name] = entry
				result.Installed = append(result.Installed, payload.name)
			}
		} else {
			comparison, compareErr := compareVersions(rel.manifest.Version, entry.Version)
			if compareErr != nil {
				return result, compareErr
			}
			if comparison < 0 {
				result.skip(payload.name, "resolved release is older than the installed version")
				continue
			}
			if comparison == 0 && (entry.BundleSHA256 != rel.manifest.Bundle.SHA256 || entry.ContentSHA256 != payload.digest) {
				return result, fmt.Errorf(
					"Skill release %s has different content for the same version", rel.manifest.Version,
				)
			}
			currentDigest, digestErr := digestDirectory(canonical)
			if os.IsNotExist(digestErr) {
				if err := installDirectory(canonical, payload.files); err != nil {
					return result, fmt.Errorf("restore %s: %w", payload.name, err)
				}
				entry.Version = rel.manifest.Version
				entry.BundleSHA256 = rel.manifest.Bundle.SHA256
				entry.ContentSHA256 = payload.digest
				result.Installed = append(result.Installed, payload.name)
			} else if digestErr != nil {
				return result, fmt.Errorf("inspect %s: %w", payload.name, digestErr)
			} else if currentDigest != entry.ContentSHA256 {
				result.skip(payload.name, "managed Skill has local changes: "+canonical)
				continue
			} else {
				currentFiles, readErr := readDirectoryFiles(canonical)
				if readErr != nil {
					return result, fmt.Errorf("read %s: %w", payload.name, readErr)
				}
				targetPayload = skillPayload{
					name:   payload.name,
					files:  currentFiles,
					digest: currentDigest,
				}
			}
		}
		m.ensureTargets(targetPayload, entry, &result, false)
	}
	state.LastResolvedVersion = rel.manifest.Version
	state.LastResolvedSHA256 = rel.manifest.Bundle.SHA256
	if err := m.writeState(state); err != nil {
		return result, err
	}
	return result, nil
}

func (m *Manager) Update() (Result, error) {
	if _, err := os.Stat(m.statePath()); os.IsNotExist(err) {
		return m.Install()
	} else if err != nil {
		return Result{}, fmt.Errorf("inspect Skill install state: %w", err)
	}
	state, err := m.readState()
	if err != nil {
		return Result{}, err
	}
	if len(state.Skills) == 0 {
		return m.Install()
	}

	rel, err := m.resolveRelease()
	if err != nil {
		return m.installWithNPX(err)
	}
	payloads, err := extractBundle(rel)
	if err != nil {
		return Result{}, err
	}
	result := Result{Source: rel.source, Version: rel.manifest.Version}
	for _, payload := range payloads {
		entry := state.Skills[payload.name]
		if entry == nil {
			canonical := m.canonicalPath(payload.name)
			if pathExists(canonical) {
				result.skip(payload.name, "existing directory is not managed by ve: "+canonical)
				continue
			}
			if err := installDirectory(canonical, payload.files); err != nil {
				return result, fmt.Errorf("install new Skill %s: %w", payload.name, err)
			}
			entry = &InstalledSkill{
				Version:       rel.manifest.Version,
				BundleSHA256:  rel.manifest.Bundle.SHA256,
				ContentSHA256: payload.digest,
				Targets:       map[string]*InstalledTarget{},
			}
			state.Skills[payload.name] = entry
			m.ensureTargets(payload, entry, &result, false)
			result.Installed = append(result.Installed, payload.name)
			continue
		}
		comparison, compareErr := compareVersions(rel.manifest.Version, entry.Version)
		if compareErr != nil {
			return result, compareErr
		}
		if comparison < 0 {
			result.skip(payload.name, "resolved release is older than the installed version")
			continue
		}
		if comparison == 0 {
			if entry.BundleSHA256 != rel.manifest.Bundle.SHA256 || entry.ContentSHA256 != payload.digest {
				return result, fmt.Errorf(
					"Skill release %s has different content for the same version", rel.manifest.Version,
				)
			}
			m.ensureTargets(payload, entry, &result, false)
			continue
		}

		canonical := m.canonicalPath(payload.name)
		currentDigest, digestErr := digestDirectory(canonical)
		if digestErr != nil {
			if os.IsNotExist(digestErr) {
				result.skip(payload.name, "managed Skill directory is missing: "+canonical)
				continue
			}
			return result, fmt.Errorf("inspect %s: %w", payload.name, digestErr)
		}
		if currentDigest != entry.ContentSHA256 {
			result.skip(payload.name, "managed Skill has local changes: "+canonical)
			continue
		}
		if err := m.prepareCopiedTargets(entry, &result); err != nil {
			return result, err
		}
		if err := installDirectory(canonical, payload.files); err != nil {
			return result, fmt.Errorf("update %s: %w", payload.name, err)
		}
		entry.Version = rel.manifest.Version
		entry.BundleSHA256 = rel.manifest.Bundle.SHA256
		entry.ContentSHA256 = payload.digest
		m.ensureTargets(payload, entry, &result, true)
		result.Updated = append(result.Updated, payload.name)
	}
	state.LastResolvedVersion = rel.manifest.Version
	state.LastResolvedSHA256 = rel.manifest.Bundle.SHA256
	if err := m.writeState(state); err != nil {
		return result, err
	}
	return result, nil
}

func (m *Manager) Uninstall() (Result, error) {
	state, err := m.readState()
	if err != nil {
		return Result{}, err
	}
	result := Result{}
	names := sortedStateSkillNames(state.Skills)
	for _, name := range names {
		entry := state.Skills[name]
		canonical := m.canonicalPath(name)
		currentDigest, digestErr := digestDirectory(canonical)
		if digestErr != nil && !os.IsNotExist(digestErr) {
			return result, fmt.Errorf("inspect %s: %w", name, digestErr)
		}
		if digestErr == nil && currentDigest != entry.ContentSHA256 {
			result.skip(name, "managed Skill has local changes: "+canonical)
			continue
		}
		for targetName, target := range entry.Targets {
			if err := removeManagedTarget(targetName, target, canonical, entry.ContentSHA256, &result); err != nil {
				return result, err
			}
		}
		if digestErr == nil {
			if err := os.RemoveAll(canonical); err != nil {
				return result, fmt.Errorf("remove %s: %w", canonical, err)
			}
		}
		delete(state.Skills, name)
		result.Removed = append(result.Removed, name)
	}
	if len(state.Skills) == 0 {
		state.LastResolvedVersion = ""
		state.LastResolvedSHA256 = ""
	}
	if err := m.writeState(state); err != nil {
		return result, err
	}
	return result, nil
}

func (m *Manager) resolveRelease() (release, error) {
	var errors []string
	if rel, err := m.fetchRemoteRelease(m.CDNManifestURL, SourceCDN); err == nil {
		return rel, nil
	} else {
		errors = append(errors, "cdn: "+err.Error())
	}
	if rel, err := m.fetchRemoteRelease(m.GitHubManifestURL, SourceGitHub); err == nil {
		return rel, nil
	} else {
		errors = append(errors, "github: "+err.Error())
	}
	return release{}, fmt.Errorf("resolve Skill release failed (%s)", strings.Join(errors, "; "))
}

func (m *Manager) installWithNPX(releaseErr error) (Result, error) {
	run := m.RunCommand
	if run == nil {
		run = runCommand
	}
	args := []string{
		"-y",
		"skills",
		"add",
		defaultNPXSkillSource,
		"--global",
		"--yes",
		"--copy",
		"--full-depth",
		"--skill",
		"*",
	}
	if err := run("npx", args...); err != nil {
		return Result{}, fmt.Errorf("%v; npx: %w", releaseErr, err)
	}
	return Result{Source: SourceNPX}, nil
}

func runCommand(name string, args ...string) error {
	command := exec.Command(name, args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func (m *Manager) fetchRemoteRelease(manifestURL, source string) (release, error) {
	manifestData, err := m.download(manifestURL, maxManifestBytes)
	if err != nil {
		return release{}, err
	}
	manifest, err := parseManifest(manifestData)
	if err != nil {
		return release{}, err
	}
	bundleURL, err := resolveBundleURL(manifestURL, manifest.Bundle.URL)
	if err != nil {
		return release{}, err
	}
	bundle, err := m.download(bundleURL, maxBundleBytes)
	if err != nil {
		return release{}, err
	}
	if err := verifyBundle(manifest, bundle); err != nil {
		return release{}, err
	}
	return release{manifest: manifest, bundle: bundle, source: source}, nil
}

func (m *Manager) download(rawURL string, limit int64) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid download URL %q", rawURL)
	}
	client := m.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	clientCopy := *client
	previousRedirectPolicy := client.CheckRedirect
	clientCopy.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > 0 && via[0].URL.Scheme == "https" && request.URL.Scheme != "https" {
			return fmt.Errorf("refusing to redirect an https download to %s", request.URL.Scheme)
		}
		if previousRedirectPolicy != nil {
			return previousRedirectPolicy(request, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
	response, err := clientCopy.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(ioutil.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("HTTP %d for %s", response.StatusCode, rawURL)
	}
	if response.ContentLength > limit {
		return nil, fmt.Errorf("download exceeds %d bytes", limit)
	}
	data, err := ioutil.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download exceeds %d bytes", limit)
	}
	return data, nil
}

func parseManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse Skill manifest: %w", err)
	}
	if manifest.SchemaVersion != ManifestSchemaVersion {
		return Manifest{}, fmt.Errorf("unsupported Skill manifest schema %d", manifest.SchemaVersion)
	}
	if _, err := parseVersion(manifest.Version); err != nil {
		return Manifest{}, fmt.Errorf("invalid Skill release version: %w", err)
	}
	if manifest.Bundle.File != BundleFileName {
		return Manifest{}, fmt.Errorf("unexpected Skill bundle filename %q", manifest.Bundle.File)
	}
	if !validSHA256(manifest.Bundle.SHA256) || manifest.Bundle.Size <= 0 || manifest.Bundle.Size > maxBundleBytes {
		return Manifest{}, fmt.Errorf("invalid Skill bundle metadata")
	}
	seen := map[string]bool{}
	for _, skill := range manifest.Skills {
		if !validSkillName(skill.Name) || seen[skill.Name] || !validSHA256(skill.SHA256) {
			return Manifest{}, fmt.Errorf("invalid Skill manifest entry %q", skill.Name)
		}
		seen[skill.Name] = true
	}
	if len(seen) == 0 {
		return Manifest{}, fmt.Errorf("Skill manifest must contain at least one Skill")
	}
	return manifest, nil
}

func verifyBundle(manifest Manifest, bundle []byte) error {
	if int64(len(bundle)) != manifest.Bundle.Size {
		return fmt.Errorf("Skill bundle size mismatch: expected %d, got %d", manifest.Bundle.Size, len(bundle))
	}
	actual := digestBytes(bundle)
	if actual != strings.ToLower(manifest.Bundle.SHA256) {
		return fmt.Errorf("Skill bundle sha256 mismatch: expected %s, got %s", manifest.Bundle.SHA256, actual)
	}
	return nil
}

func resolveBundleURL(manifestURL, bundleURL string) (string, error) {
	base, err := url.Parse(manifestURL)
	if err != nil {
		return "", err
	}
	reference, err := url.Parse(bundleURL)
	if err != nil {
		return "", err
	}
	resolved := base.ResolveReference(reference)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return "", fmt.Errorf("invalid bundle URL %q", bundleURL)
	}
	if base.Scheme == "https" && resolved.Scheme != "https" {
		return "", fmt.Errorf("refusing to resolve an https manifest to %s", resolved.Scheme)
	}
	return resolved.String(), nil
}

func extractBundle(rel release) ([]skillPayload, error) {
	reader, err := zip.NewReader(bytes.NewReader(rel.bundle), int64(len(rel.bundle)))
	if err != nil {
		return nil, fmt.Errorf("open Skill bundle: %w", err)
	}
	filesBySkill := map[string]map[string][]byte{}
	manifestSkills := map[string]ManifestSkill{}
	for _, skill := range rel.manifest.Skills {
		manifestSkills[skill.Name] = skill
	}
	var extractedBytes int64
	var fileCount int
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unsafe Skill bundle link: %s", file.Name)
		}
		parts, err := safeArchiveParts(file.Name)
		if err != nil || len(parts) < 2 {
			return nil, fmt.Errorf("unsafe or unexpected Skill bundle path: %s", file.Name)
		}
		if _, expected := manifestSkills[parts[0]]; !expected {
			return nil, fmt.Errorf("Skill bundle contains a Skill not listed in manifest: %s", parts[0])
		}
		fileCount++
		if fileCount > maxArchiveFiles {
			return nil, fmt.Errorf("Skill bundle contains too many files")
		}
		extractedBytes += int64(file.UncompressedSize64)
		if extractedBytes > maxExtractedBytes {
			return nil, fmt.Errorf("Skill bundle expands beyond %d bytes", maxExtractedBytes)
		}
		stream, err := file.Open()
		if err != nil {
			return nil, err
		}
		content, readErr := ioutil.ReadAll(io.LimitReader(stream, int64(file.UncompressedSize64)+1))
		closeErr := stream.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if uint64(len(content)) != file.UncompressedSize64 {
			return nil, fmt.Errorf("Skill bundle entry size mismatch: %s", file.Name)
		}
		relative := filepath.Join(parts[1:]...)
		if filesBySkill[parts[0]] == nil {
			filesBySkill[parts[0]] = map[string][]byte{}
		}
		if _, duplicate := filesBySkill[parts[0]][relative]; duplicate {
			return nil, fmt.Errorf("duplicate Skill bundle path: %s", file.Name)
		}
		filesBySkill[parts[0]][relative] = content
	}

	names := make([]string, 0, len(manifestSkills))
	for name := range manifestSkills {
		names = append(names, name)
	}
	sort.Strings(names)
	payloads := make([]skillPayload, 0, len(names))
	for _, name := range names {
		files := filesBySkill[name]
		if files == nil || files["SKILL.md"] == nil {
			return nil, fmt.Errorf("Skill bundle is missing %s/SKILL.md", name)
		}
		digest := digestFiles(files)
		if digest != strings.ToLower(manifestSkills[name].SHA256) {
			return nil, fmt.Errorf("Skill content sha256 mismatch for %s", name)
		}
		payloads = append(payloads, skillPayload{name: name, files: files, digest: digest})
	}
	return payloads, nil
}

func (m *Manager) ensureTargets(payload skillPayload, entry *InstalledSkill, result *Result, refreshing bool) {
	if entry.Targets == nil {
		entry.Targets = map[string]*InstalledTarget{}
	}
	canonical := m.canonicalPath(payload.name)
	for _, target := range m.targets(payload.name) {
		existing := entry.Targets[target.name]
		if existing != nil && existing.Mode == "copy" && refreshing {
			if existingDigest, err := digestDirectory(target.path); err == nil && existingDigest != existing.ContentSHA256 {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s target has local changes and was not refreshed: %s", target.name, target.path))
				continue
			}
		}
		mode, err := ensureTarget(target.path, canonical, payload.files, existing)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s target was skipped: %v", target.name, err))
			continue
		}
		installedTarget := &InstalledTarget{Mode: mode, Path: target.path}
		if mode == "copy" {
			installedTarget.ContentSHA256 = payload.digest
		}
		entry.Targets[target.name] = installedTarget
	}
}

func (m *Manager) prepareCopiedTargets(entry *InstalledSkill, result *Result) error {
	for name, target := range entry.Targets {
		if target.Mode != "copy" {
			continue
		}
		digest, err := digestDirectory(target.Path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect %s target: %w", name, err)
		}
		if digest != target.ContentSHA256 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s target has local changes: %s", name, target.Path))
		}
	}
	return nil
}

func ensureTarget(targetPath, canonical string, files map[string][]byte, existing *InstalledTarget) (string, error) {
	if existing != nil && existing.Path != targetPath {
		return "", fmt.Errorf("recorded target path changed from %s to %s", existing.Path, targetPath)
	}
	if info, err := os.Lstat(targetPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, resolveErr := filepath.EvalSymlinks(targetPath)
			canonicalResolved, canonicalErr := filepath.EvalSymlinks(canonical)
			if resolveErr == nil && canonicalErr == nil && resolved == canonicalResolved {
				return "symlink", nil
			}
			return "", fmt.Errorf("existing symlink does not point to the managed Skill: %s", targetPath)
		}
		current, digestErr := digestDirectory(targetPath)
		if digestErr != nil {
			return "", digestErr
		}
		expected := digestFiles(files)
		if existing != nil {
			if existing.Mode != "copy" {
				return "", fmt.Errorf("existing path is not a managed copy: %s", targetPath)
			}
			expected = existing.ContentSHA256
		}
		if current != expected {
			return "", fmt.Errorf("managed copy has local changes: %s", targetPath)
		}
		if err := installDirectory(targetPath, files); err != nil {
			return "", err
		}
		return "copy", nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return "", err
	}
	if err := os.Symlink(canonical, targetPath); err == nil {
		return "symlink", nil
	}
	if err := installDirectory(targetPath, files); err != nil {
		return "", err
	}
	return "copy", nil
}

func removeManagedTarget(name string, target *InstalledTarget, canonical, contentDigest string, result *Result) error {
	info, err := os.Lstat(target.Path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if target.Mode == "symlink" {
		if info.Mode()&os.ModeSymlink == 0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s target is no longer a symlink: %s", name, target.Path))
			return nil
		}
		if !symlinkPointsTo(target.Path, canonical) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s target symlink changed: %s", name, target.Path))
			return nil
		}
		return os.Remove(target.Path)
	}
	if target.Mode != "copy" {
		return fmt.Errorf("unknown target mode %q", target.Mode)
	}
	digest, digestErr := digestDirectory(target.Path)
	if digestErr != nil {
		return digestErr
	}
	expected := target.ContentSHA256
	if expected == "" {
		expected = contentDigest
	}
	if digest != expected {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%s target copy has local changes: %s", name, target.Path))
		return nil
	}
	return os.RemoveAll(target.Path)
}

func symlinkPointsTo(linkPath, expectedPath string) bool {
	destination, err := os.Readlink(linkPath)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(destination) {
		destination = filepath.Join(filepath.Dir(linkPath), destination)
	}
	destinationAbsolute, destinationErr := filepath.Abs(destination)
	expectedAbsolute, expectedErr := filepath.Abs(expectedPath)
	if destinationErr == nil && expectedErr == nil && filepath.Clean(destinationAbsolute) == filepath.Clean(expectedAbsolute) {
		return true
	}
	resolved, resolveErr := filepath.EvalSymlinks(linkPath)
	expectedResolved, expectedResolveErr := filepath.EvalSymlinks(expectedPath)
	return resolveErr == nil && expectedResolveErr == nil && resolved == expectedResolved
}

func (m *Manager) targets(skillName string) []targetSpec {
	return []targetSpec{
		{name: "claude-code", path: filepath.Join(m.ClaudeConfigDir, "skills", skillName)},
		{name: "openclaw", path: filepath.Join(m.openClawHome(), "skills", skillName)},
		{name: "hermes-agent", path: filepath.Join(m.HermesHome, "skills", skillName)},
		{name: "trae", path: filepath.Join(m.HomeDir, ".trae", "skills", skillName)},
	}
}

func (m *Manager) openClawHome() string {
	for _, name := range []string{".openclaw", ".clawdbot", ".moltbot"} {
		candidate := filepath.Join(m.HomeDir, name)
		if pathExists(candidate) {
			return candidate
		}
	}
	return filepath.Join(m.HomeDir, ".openclaw")
}

func (m *Manager) canonicalPath(skillName string) string {
	return filepath.Join(m.HomeDir, ".agents", "skills", skillName)
}

func (m *Manager) statePath() string {
	return filepath.Join(m.ConfigDir, StateFileName)
}

func (m *Manager) readState() (State, error) {
	state := State{SchemaVersion: StateSchemaVersion, Skills: map[string]*InstalledSkill{}}
	data, err := ioutil.ReadFile(m.statePath())
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read Skill install state: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("parse Skill install state: %w", err)
	}
	if state.SchemaVersion != StateSchemaVersion || state.Skills == nil {
		return State{}, fmt.Errorf("unsupported Skill install state schema %d", state.SchemaVersion)
	}
	for name, skill := range state.Skills {
		if !validSkillName(name) || skill == nil {
			return State{}, fmt.Errorf("invalid Skill install state entry %q", name)
		}
		if _, versionErr := parseVersion(skill.Version); versionErr != nil || !validSHA256(skill.BundleSHA256) || !validSHA256(skill.ContentSHA256) {
			return State{}, fmt.Errorf("invalid Skill install state metadata for %q", name)
		}
		if skill.Targets == nil {
			return State{}, fmt.Errorf("invalid Skill install state targets for %q", name)
		}
		for targetName, target := range skill.Targets {
			if target == nil || (target.Mode != "symlink" && target.Mode != "copy") || !filepath.IsAbs(target.Path) {
				return State{}, fmt.Errorf("invalid Skill install state target %q for %q", targetName, name)
			}
			if target.Mode == "copy" && !validSHA256(target.ContentSHA256) {
				return State{}, fmt.Errorf("invalid Skill install state target hash %q for %q", targetName, name)
			}
		}
	}
	return state, nil
}

func (m *Manager) writeState(state State) error {
	if err := os.MkdirAll(m.ConfigDir, 0700); err != nil {
		return fmt.Errorf("create Skill state directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := ioutil.TempFile(m.ConfigDir, ".install-state-*.tmp")
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
		return fmt.Errorf("replace Skill install state: %w", err)
	}
	return nil
}

func installDirectory(target string, files map[string][]byte) error {
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	staging, err := ioutil.TempDir(parent, ".skill-install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	for relative, content := range files {
		parts, pathErr := safeArchiveParts(filepath.ToSlash(relative))
		if pathErr != nil || len(parts) == 0 {
			return fmt.Errorf("unsafe Skill path %q", relative)
		}
		destination := filepath.Join(append([]string{staging}, parts...)...)
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if filepath.Ext(destination) == ".sh" || filepath.Ext(destination) == ".py" {
			mode = 0755
		}
		if err := ioutil.WriteFile(destination, content, mode); err != nil {
			return err
		}
	}
	backup := target + ".ve-backup"
	if pathExists(backup) {
		return fmt.Errorf("stale backup exists: %s", backup)
	}
	hadTarget := pathExists(target)
	if hadTarget {
		if err := os.Rename(target, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(staging, target); err != nil {
		if hadTarget {
			_ = os.Rename(backup, target)
		}
		return err
	}
	if hadTarget {
		if err := os.RemoveAll(backup); err != nil {
			return err
		}
	}
	return nil
}

func digestDirectory(root string) (string, error) {
	files, err := readDirectoryFiles(root)
	if err != nil {
		return "", err
	}
	return digestFiles(files), nil
}

func readDirectoryFiles(root string) (map[string][]byte, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}
	files := map[string][]byte{}
	err = filepath.Walk(root, func(path string, entry os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unexpected symbolic link inside Skill: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Mode().IsRegular() {
			return fmt.Errorf("unexpected non-regular Skill file: %s", path)
		}
		relative, relativeErr := filepath.Rel(root, path)
		if relativeErr != nil {
			return relativeErr
		}
		content, readErr := ioutil.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		files[relative] = content
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func digestFiles(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, filepath.ToSlash(name))
	}
	sort.Strings(names)
	digest := sha256.New()
	for _, name := range names {
		_, _ = digest.Write([]byte(name))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write(files[filepath.FromSlash(name)])
		_, _ = digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validSkillName(value string) bool {
	if value == "" || value == "." || value == ".." || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= '0' && character <= '9') && character != '-' {
			return false
		}
	}
	return true
}

func safeArchiveParts(name string) ([]string, error) {
	if name == "" || strings.ContainsRune(name, 0) || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return nil, fmt.Errorf("unsafe path")
	}
	if len(name) >= 2 && name[1] == ':' {
		return nil, fmt.Errorf("unsafe path")
	}
	parts := strings.Split(strings.TrimSuffix(name, "/"), "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, fmt.Errorf("unsafe path")
		}
	}
	return parts, nil
}

type semanticVersion struct {
	major      int
	minor      int
	patch      int
	preRelease []string
}

func compareVersions(left, right string) (int, error) {
	a, err := parseVersion(left)
	if err != nil {
		return 0, err
	}
	b, err := parseVersion(right)
	if err != nil {
		return 0, err
	}
	for _, pair := range [][2]int{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if pair[0] < pair[1] {
			return -1, nil
		}
		if pair[0] > pair[1] {
			return 1, nil
		}
	}
	if len(a.preRelease) == 0 && len(b.preRelease) > 0 {
		return 1, nil
	}
	if len(a.preRelease) > 0 && len(b.preRelease) == 0 {
		return -1, nil
	}
	for index := 0; index < len(a.preRelease) && index < len(b.preRelease); index++ {
		leftPart, rightPart := a.preRelease[index], b.preRelease[index]
		if leftPart == rightPart {
			continue
		}
		leftNumber, leftErr := strconv.Atoi(leftPart)
		rightNumber, rightErr := strconv.Atoi(rightPart)
		if leftErr == nil && rightErr == nil {
			if leftNumber < rightNumber {
				return -1, nil
			}
			return 1, nil
		}
		if leftErr == nil {
			return -1, nil
		}
		if rightErr == nil {
			return 1, nil
		}
		if leftPart < rightPart {
			return -1, nil
		}
		return 1, nil
	}
	if len(a.preRelease) < len(b.preRelease) {
		return -1, nil
	}
	if len(a.preRelease) > len(b.preRelease) {
		return 1, nil
	}
	return 0, nil
}

func parseVersion(value string) (semanticVersion, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if plus := strings.Index(value, "+"); plus >= 0 {
		value = value[:plus]
	}
	main := value
	var pre []string
	if dash := strings.Index(value, "-"); dash >= 0 {
		main = value[:dash]
		pre = strings.Split(value[dash+1:], ".")
		if len(pre) == 0 || pre[0] == "" {
			return semanticVersion{}, fmt.Errorf("invalid version %q", value)
		}
	}
	parts := strings.Split(main, ".")
	if len(parts) != 3 {
		return semanticVersion{}, fmt.Errorf("invalid version %q", value)
	}
	numbers := make([]int, 3)
	for index, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return semanticVersion{}, fmt.Errorf("invalid version %q", value)
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return semanticVersion{}, fmt.Errorf("invalid version %q", value)
		}
		numbers[index] = number
	}
	for _, identifier := range pre {
		if identifier == "" {
			return semanticVersion{}, fmt.Errorf("invalid version %q", value)
		}
		for _, character := range identifier {
			if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && character != '-' {
				return semanticVersion{}, fmt.Errorf("invalid version %q", value)
			}
		}
	}
	return semanticVersion{major: numbers[0], minor: numbers[1], patch: numbers[2], preRelease: pre}, nil
}

func (r *Result) skip(name, warning string) {
	r.Skipped = append(r.Skipped, name)
	r.Warnings = append(r.Warnings, warning)
}

func sortedStateSkillNames(skills map[string]*InstalledSkill) []string {
	names := make([]string, 0, len(skills))
	for name := range skills {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
