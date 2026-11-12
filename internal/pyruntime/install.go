package pyruntime

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	manifestSchemaVersion       = 1
	maxManifestBytes      int64 = 1 << 20
	maxArchiveBytes       int64 = 512 << 20
	maxExtractedBytes     int64 = 2 << 30
	maxArchiveEntries           = 50000
	markerFile                  = ".ve-runtime.json"
	downloadTimeout             = 15 * time.Minute
)

//go:embed manifest.json
var defaultManifest []byte

var (
	sha256Pattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	versionPattern = regexp.MustCompile(`^3\.\d+\.\d+$`)
	// platformPattern and buildPattern restrict the manifest fields that are
	// joined into the install directory name to one portable path segment:
	// no separators, volumes, colons or parent references.
	platformPattern = regexp.MustCompile(`^[a-z0-9]+-[a-z0-9_]+$`)
	buildPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
)

// Manifest pins downloadable runtimes per platform.
type Manifest struct {
	SchemaVersion int        `json:"schema_version"`
	Source        string     `json:"source,omitempty"`
	Runtimes      []Artifact `json:"runtimes"`
}

// Artifact is one pinned runtime archive.
type Artifact struct {
	Platform       string `json:"platform"`
	Implementation string `json:"implementation"`
	Version        string `json:"version"`
	Build          string `json:"build,omitempty"`
	Libc           string `json:"libc,omitempty"`
	URL            string `json:"url"`
	SHA256         string `json:"sha256"`
	Size           int64  `json:"size"`
	Format         string `json:"format"`
	Executable     string `json:"executable"`
}

// DefaultManifest returns the manifest embedded in the CLI.
func DefaultManifest() (*Manifest, error) {
	return ParseManifest(defaultManifest)
}

// ParseManifest decodes and validates a manifest.
func ParseManifest(data []byte) (*Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("invalid runtime manifest: %w", err)
	}
	if manifest.SchemaVersion != manifestSchemaVersion {
		return nil, fmt.Errorf("unsupported runtime manifest schema_version %d", manifest.SchemaVersion)
	}
	seen := map[string]bool{}
	for index, artifact := range manifest.Runtimes {
		if err := artifact.validate(); err != nil {
			return nil, fmt.Errorf("runtime manifest entry %d: %w", index, err)
		}
		if seen[artifact.Platform] {
			return nil, fmt.Errorf("runtime manifest has more than one entry for %s", artifact.Platform)
		}
		seen[artifact.Platform] = true
	}
	return &manifest, nil
}

func (a Artifact) validate() error {
	if !platformPattern.MatchString(a.Platform) {
		return fmt.Errorf("invalid platform %q", a.Platform)
	}
	if a.Build != "" && (!buildPattern.MatchString(a.Build) || strings.Contains(a.Build, "..")) {
		return fmt.Errorf("invalid build %q", a.Build)
	}
	if a.Implementation != "cpython" {
		return fmt.Errorf("implementation must be cpython")
	}
	if !versionPattern.MatchString(a.Version) {
		return fmt.Errorf("invalid version %q", a.Version)
	}
	parsed, err := url.Parse(a.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("url must be an absolute HTTPS URL")
	}
	if !sha256Pattern.MatchString(a.SHA256) {
		return fmt.Errorf("sha256 must be 64 lowercase hex characters")
	}
	if a.Size <= 0 || a.Size > maxArchiveBytes {
		return fmt.Errorf("size must be between 1 and %d bytes", maxArchiveBytes)
	}
	if a.Format != "tar.gz" {
		return fmt.Errorf("unsupported format %q", a.Format)
	}
	if _, err := safeRelativePath(a.Executable); err != nil {
		return fmt.Errorf("executable: %w", err)
	}
	return nil
}

// CurrentPlatform returns the manifest platform key for this binary.
func CurrentPlatform() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// muslLoaders matches the dynamic loader that only musl-based Linux
// systems, such as Alpine, install.
var muslLoaders = "/lib/ld-musl-*.so.1"

// CurrentLibc returns the C library of this Linux system, musl or glibc, and
// an empty string elsewhere. Managed runtimes are built for one of them.
func CurrentLibc() string {
	return detectLibc(runtime.GOOS, muslLoaders)
}

func detectLibc(goos, loaders string) string {
	if goos != "linux" {
		return ""
	}
	if matches, _ := filepath.Glob(loaders); len(matches) > 0 {
		return "musl"
	}
	return "glibc"
}

// artifactLibc returns the C library an artifact was built for. A Linux
// artifact that does not say is taken as glibc, which is what
// python-build-standalone's default Linux builds use, so an older or custom
// manifest without the field is not offered on musl systems.
func artifactLibc(artifact Artifact) string {
	if artifact.Libc == "" && strings.HasPrefix(artifact.Platform, "linux-") {
		return "glibc"
	}
	return artifact.Libc
}

// Select returns the artifact for platform and libc (empty outside Linux) or
// an explicit unsupported error. An artifact built for another C library is
// not selected.
func (m *Manifest) Select(platform, libc string) (Artifact, error) {
	for _, artifact := range m.Runtimes {
		if artifact.Platform == platform && (libc == "" || artifactLibc(artifact) == "" || artifactLibc(artifact) == libc) {
			return artifact, nil
		}
	}
	supported := make([]string, 0, len(m.Runtimes))
	for _, artifact := range m.Runtimes {
		name := artifact.Platform
		if libc := artifactLibc(artifact); libc != "" {
			name += " (" + libc + ")"
		}
		supported = append(supported, name)
	}
	if libc != "" {
		platform += " (" + libc + ")"
	}
	return Artifact{}, fmt.Errorf("no managed Python runtime is available for %s (supported: %s); set VOLCENGINE_SCRIPT_PYTHON to a CPython %d.%d+ interpreter instead", platform, strings.Join(supported, ", "), MinimumMajor, MinimumMinor)
}

// Installer downloads, verifies, extracts and activates managed runtimes.
type Installer struct {
	Root   string
	Client *http.Client
}

// NewInstaller returns an installer that only follows HTTPS redirects.
func NewInstaller(root string) *Installer {
	return &Installer{Root: root, Client: &http.Client{Timeout: downloadTimeout, CheckRedirect: httpsOnlyRedirect}}
}

func httpsOnlyRedirect(request *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("too many redirects")
	}
	if request.URL.Scheme != "https" {
		return fmt.Errorf("refusing non-HTTPS redirect to %s", request.URL.Redacted())
	}
	return nil
}

// LoadManifest reads a manifest from an HTTPS URL or a local file. When
// expectedSHA256 is set, the manifest bytes must match it.
func (i *Installer) LoadManifest(ctx context.Context, source, expectedSHA256 string) (*Manifest, error) {
	var data []byte
	parsed, err := url.Parse(source)
	switch {
	case err == nil && parsed.Scheme == "https":
		data, err = i.fetch(ctx, source, maxManifestBytes)
		if err != nil {
			return nil, err
		}
	case err == nil && parsed.Scheme != "" && len(parsed.Scheme) > 1:
		return nil, fmt.Errorf("runtime manifest must be an HTTPS URL or a local file")
	default:
		file, err := os.Open(source)
		if err != nil {
			return nil, fmt.Errorf("open runtime manifest: %w", err)
		}
		defer file.Close()
		data, err = io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > maxManifestBytes {
			return nil, fmt.Errorf("runtime manifest exceeds %d bytes", maxManifestBytes)
		}
	}
	if expectedSHA256 = strings.ToLower(strings.TrimSpace(expectedSHA256)); expectedSHA256 != "" {
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != expectedSHA256 {
			return nil, fmt.Errorf("runtime manifest SHA-256 mismatch")
		}
	}
	return ParseManifest(data)
}

func (i *Installer) fetch(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	response, err := i.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", rawURL, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download %s exceeds %d bytes", rawURL, limit)
	}
	return data, nil
}

func (i *Installer) get(ctx context.Context, rawURL string) (*http.Response, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" {
		return nil, fmt.Errorf("refusing non-HTTPS download %s", rawURL)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	client := i.Client
	if client == nil {
		client = NewInstaller(i.Root).Client
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", rawURL, err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("download %s: HTTP %d", rawURL, response.StatusCode)
	}
	return response, nil
}

// installDirectory returns root/name after checking that it is a direct
// child of root, so activation and cleanup can never touch another path.
func installDirectory(root, name string) (string, error) {
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	final := filepath.Join(cleanRoot, name)
	if name == "" || name != filepath.Base(name) || filepath.Dir(final) != cleanRoot || name == "." || name == ".." || filepath.VolumeName(name) != "" || strings.ContainsAny(name, `/\:`) {
		return "", fmt.Errorf("runtime install directory %q is not inside %s", name, cleanRoot)
	}
	return final, nil
}

// InstallResult describes an activated managed runtime.
type InstallResult struct {
	State       State `json:"state"`
	Downloaded  bool  `json:"downloaded"`
	ArchiveSize int64 `json:"archive_size"`
}

// Install makes artifact the active managed runtime. An already extracted and
// validated copy of the same artifact is reused without downloading.
func (i *Installer) Install(ctx context.Context, artifact Artifact) (*InstallResult, error) {
	if err := artifact.validate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(i.Root, 0700); err != nil {
		return nil, err
	}
	executable, _ := safeRelativePath(artifact.Executable)
	name := fmt.Sprintf("%s-%s-%s-%s-%s", artifact.Implementation, artifact.Version, artifact.Build, artifact.Platform, artifact.SHA256[:12])
	final, err := installDirectory(i.Root, name)
	if err != nil {
		return nil, err
	}
	result := &InstallResult{ArchiveSize: artifact.Size}

	if !i.validInstallation(ctx, final, artifact, executable) {
		if err := i.downloadAndExtract(ctx, artifact, executable, final); err != nil {
			return nil, err
		}
		result.Downloaded = true
	}
	result.State = State{
		Python: filepath.Join(final, executable), Implementation: artifact.Implementation,
		Version: artifact.Version, Build: artifact.Build, Platform: artifact.Platform,
		SHA256: artifact.SHA256, InstalledAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeJSONAtomic(filepath.Join(i.Root, stateFile), result.State); err != nil {
		return nil, err
	}
	return result, nil
}

func (i *Installer) validInstallation(ctx context.Context, directory string, artifact Artifact, executable string) bool {
	data, err := os.ReadFile(filepath.Join(directory, markerFile))
	if err != nil {
		return false
	}
	var marker Artifact
	if json.Unmarshal(data, &marker) != nil || marker != artifact {
		return false
	}
	interpreter, err := Probe(ctx, filepath.Join(directory, executable))
	return err == nil && interpreter.Version == artifact.Version
}

func (i *Installer) downloadAndExtract(ctx context.Context, artifact Artifact, executable, final string) error {
	archive, err := os.CreateTemp(i.Root, ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()

	response, err := i.get(ctx, artifact.URL)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(archive, hash), io.LimitReader(response.Body, artifact.Size+1))
	response.Body.Close()
	if err != nil {
		return fmt.Errorf("download %s: %w", artifact.URL, err)
	}
	if written != artifact.Size {
		return fmt.Errorf("downloaded %d bytes but the manifest pins %d bytes", written, artifact.Size)
	}
	if digest := hex.EncodeToString(hash.Sum(nil)); digest != artifact.SHA256 {
		return fmt.Errorf("SHA-256 mismatch for %s: got %s, want %s", artifact.URL, digest, artifact.SHA256)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}

	staging, err := os.MkdirTemp(i.Root, ".staging-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := extractTarGz(archive, staging); err != nil {
		return fmt.Errorf("extract %s: %w", artifact.URL, err)
	}
	interpreter, err := Probe(ctx, filepath.Join(staging, executable))
	if err != nil {
		return fmt.Errorf("installed runtime failed validation: %w", err)
	}
	if interpreter.Version != artifact.Version {
		return fmt.Errorf("installed runtime reports %s but the manifest pins %s", interpreter.Version, artifact.Version)
	}
	if err := writeJSONAtomic(filepath.Join(staging, markerFile), artifact); err != nil {
		return err
	}
	if err := os.RemoveAll(final); err != nil {
		return err
	}
	if err := os.Rename(staging, final); err != nil {
		// A concurrent installer may have activated the same artifact.
		if i.validInstallation(ctx, final, artifact, executable) {
			return nil
		}
		return fmt.Errorf("activate runtime: %w", err)
	}
	return nil
}

type archiveLink struct{ name, target string }

// extractTarGz extracts regular files, directories and relative symbolic
// links. Absolute or parent-escaping names, hard links, devices and links
// whose resolved target leaves the root are rejected. Links are created after
// all files so no file is ever written through a link.
func extractTarGz(source io.Reader, root string) error {
	compressed, err := gzip.NewReader(source)
	if err != nil {
		return err
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	var links []archiveLink
	var total int64
	entries := 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if entries++; entries > maxArchiveEntries {
			return fmt.Errorf("archive has more than %d entries", maxArchiveEntries)
		}
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		relative, err := safeRelativePath(header.Name)
		if err != nil {
			return fmt.Errorf("entry %q: %w", header.Name, err)
		}
		target := filepath.Join(root, relative)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if total += header.Size; total > maxExtractedBytes {
				return fmt.Errorf("archive expands beyond %d bytes", maxExtractedBytes)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			if err := ensureRealDirectories(root, filepath.Dir(target)); err != nil {
				return err
			}
			mode := os.FileMode(0644)
			if header.FileInfo().Mode().Perm()&0111 != 0 {
				mode = 0755
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(file, io.LimitReader(reader, header.Size)); err != nil {
				file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if header.Linkname == "" || path.IsAbs(header.Linkname) || strings.Contains(header.Linkname, `\`) || filepath.VolumeName(header.Linkname) != "" {
				return fmt.Errorf("entry %q: symbolic link target %q must be relative", header.Name, header.Linkname)
			}
			if lexical := path.Join(path.Dir(filepath.ToSlash(relative)), header.Linkname); lexical == ".." || strings.HasPrefix(lexical, "../") {
				return fmt.Errorf("entry %q: symbolic link target %q escapes the runtime directory", header.Name, header.Linkname)
			}
			links = append(links, archiveLink{name: target, target: header.Linkname})
		default:
			return fmt.Errorf("entry %q has unsupported type %q", header.Name, string(header.Typeflag))
		}
	}
	if len(links) > 0 && runtime.GOOS == "windows" {
		return fmt.Errorf("archive contains symbolic links, which are not supported on Windows")
	}
	for _, entry := range links {
		if err := os.MkdirAll(filepath.Dir(entry.name), 0755); err != nil {
			return err
		}
		if err := ensureRealDirectories(root, filepath.Dir(entry.name)); err != nil {
			return err
		}
		if err := os.Symlink(entry.target, entry.name); err != nil {
			return err
		}
	}
	return verifyLinks(root, links)
}

// verifyLinks resolves every created link component by component and rejects
// any whose resolution leaves root at any step, including chains through
// other links and dangling targets.
func verifyLinks(root string, links []archiveLink) error {
	for _, entry := range links {
		relative, err := filepath.Rel(root, entry.name)
		if err != nil {
			return err
		}
		if err := resolveWithinRoot(root, filepath.ToSlash(relative)); err != nil {
			return fmt.Errorf("symbolic link %s: %w", relative, err)
		}
	}
	return nil
}

func resolveWithinRoot(root, relative string) error {
	pending := strings.Split(relative, "/")
	var resolved []string
	hops := 0
	for len(pending) > 0 {
		part := pending[0]
		pending = pending[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			if len(resolved) == 0 {
				return fmt.Errorf("resolves outside the runtime directory")
			}
			resolved = resolved[:len(resolved)-1]
			continue
		}
		candidate := filepath.Join(append([]string{root}, append(resolved, part)...)...)
		info, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			resolved = append(resolved, part)
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			resolved = append(resolved, part)
			continue
		}
		if hops++; hops > 64 {
			return fmt.Errorf("too many levels of symbolic links")
		}
		target, err := os.Readlink(candidate)
		if err != nil {
			return err
		}
		if path.IsAbs(target) || filepath.IsAbs(target) {
			return fmt.Errorf("links to absolute path %q", target)
		}
		pending = append(strings.Split(filepath.ToSlash(target), "/"), pending...)
	}
	return nil
}

func ensureRealDirectories(root, directory string) error {
	relative, err := filepath.Rel(root, directory)
	if err != nil {
		return err
	}
	current := root
	if relative == "." {
		return nil
	}
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%s is not a real directory", current)
		}
	}
	return nil
}

func safeRelativePath(name string) (string, error) {
	if name == "" || strings.Contains(name, `\`) || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("invalid path %q", name)
	}
	if path.IsAbs(name) || filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return "", fmt.Errorf("absolute path %q", name)
	}
	cleaned := path.Clean(strings.TrimPrefix(name, "./"))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path %q escapes the runtime directory", name)
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == ".." {
			return "", fmt.Errorf("path %q escapes the runtime directory", name)
		}
	}
	return filepath.FromSlash(cleaned), nil
}

func writeJSONAtomic(target string, value interface{}) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".tmp-*")
	if err != nil {
		return err
	}
	name := file.Name()
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		os.Remove(name)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(name)
		return err
	}
	// os.Rename replaces an existing target atomically on every platform.
	if err := os.Rename(name, target); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
