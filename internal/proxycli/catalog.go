package proxycli

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/volcengine/volcengine-cli/upgrade"
)

const CatalogSchemaVersion = 2
const InstallerProtocolVersion = 3
const SkillsRepository = "https://github.com/volcengine/volcengine-skills"

//go:embed catalog.json
var embeddedCatalog []byte

// Catalog is the versioned publication contract. Snapshot owns a private copy.
type Catalog struct {
	SchemaVersion        int        `json:"schema_version"`
	Revision             string     `json:"catalog_revision"`
	MinInstallerProtocol int        `json:"min_installer_protocol"`
	Tools                []ToolSpec `json:"tools"`
}

type ToolSpec struct {
	ID             Tool                     `json:"id"`
	Command        string                   `json:"command"`
	Aliases        []string                 `json:"aliases"`
	Description    string                   `json:"description"`
	Readiness      string                   `json:"readiness"`
	ReleaseVersion string                   `json:"release_version,omitempty"`
	Executable     string                   `json:"executable"`
	Launch         LaunchSpec               `json:"launch"`
	SourceURL      string                   `json:"source_url"`
	Platforms      map[string]ManifestAsset `json:"platforms"`
	Skills         SkillSpec                `json:"skills"`
	Notes          []string                 `json:"notes,omitempty"`
	Gaps           []string                 `json:"gaps,omitempty"`
	PathPolicy     string                   `json:"path_policy"` // prefer or managed-only
}

type LaunchSpec struct {
	Kind string `json:"kind"`
}

type SkillSpec struct {
	Repository string   `json:"repository"`
	Names      []string `json:"names"`
}

// Snapshot is immutable; returned specifications are deep copies.
type Snapshot struct {
	catalog Catalog
	source  string
}

func (s *Snapshot) Catalog() Catalog {
	data, _ := json.Marshal(s.catalog)
	var c Catalog
	_ = json.Unmarshal(data, &c)
	return c
}
func (s *Snapshot) Source() string { return s.source }
func (s *Snapshot) Lookup(value string) (ToolSpec, bool) {
	for _, candidate := range s.catalog.Tools {
		t := candidate
		if string(t.ID) == value || t.Command == value {
			return cloneToolSpec(t), true
		}
		for _, a := range t.Aliases {
			if a == value {
				return cloneToolSpec(t), true
			}
		}
	}
	return ToolSpec{}, false
}
func (s *Snapshot) ParseTool(value string) (Tool, error) {
	if t, ok := s.Lookup(strings.ToLower(strings.TrimSpace(value))); ok {
		return t.ID, nil
	}
	return "", fmt.Errorf("unknown proxy CLI %q; run 've proxycli refresh' then 've proxycli list'", value)
}
func (s *Snapshot) Ready(tool Tool) (ToolSpec, error) {
	t, ok := s.Lookup(string(tool))
	if !ok || t.ID != tool {
		return ToolSpec{}, fmt.Errorf("unknown proxy CLI %q; run 've proxycli refresh'", tool)
	}
	if t.Readiness != "ready" {
		return ToolSpec{}, fmt.Errorf("proxy CLI %s is %s: %s; see 've proxycli info %s'", tool, t.Readiness, strings.Join(t.Gaps, "; "), tool)
	}
	return t, nil
}

var commandPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var catalogPlatformPattern = regexp.MustCompile(`^[a-z0-9]+-[a-z0-9]+$`)

var basenamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// ParseCatalog validates publication data, never executes or probes a tool.
func ParseCatalog(data []byte) (*Snapshot, error) { return parseCatalog(data, false) }
func parseCatalog(data []byte, allowHTTP bool) (*Snapshot, error) {
	if int64(len(data)) > maxManifestBytes {
		return nil, fmt.Errorf("catalog exceeds size limit")
	}
	var c Catalog
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse proxy CLI catalog: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("catalog has trailing data")
	}
	if c.SchemaVersion != CatalogSchemaVersion || c.MinInstallerProtocol != InstallerProtocolVersion {
		return nil, fmt.Errorf("incompatible catalog schema/protocol %d/%d; upgrade ve", c.SchemaVersion, c.MinInstallerProtocol)
	}
	if strings.TrimSpace(c.Revision) == "" || len(c.Revision) > 128 || len(c.Tools) == 0 || len(c.Tools) > 256 {
		return nil, fmt.Errorf("invalid catalog revision or tool count")
	}
	names := map[string]Tool{}
	executablePaths := map[string]Tool{}
	for _, t := range c.Tools {
		local := map[string]bool{}
		// An id may equal its own command, but must never resolve to another tool.
		for _, n := range append([]string{string(t.ID), t.Command}, t.Aliases...) {
			if !commandPattern.MatchString(n) || n == "ve" {
				return nil, fmt.Errorf("invalid or recursive tool name %q", n)
			}
			if owner, exists := names[n]; exists && owner != t.ID {
				return nil, fmt.Errorf("duplicate catalog name %q", n)
			}
			names[n] = t.ID
		}
		for _, n := range append([]string{t.Command}, t.Aliases...) {
			if local[n] {
				return nil, fmt.Errorf("duplicate command/alias %q", n)
			}
			local[n] = true
		}
		if !basenamePattern.MatchString(t.Executable) || strings.TrimSuffix(strings.ToLower(t.Executable), ".exe") == "ve" {
			return nil, fmt.Errorf("invalid executable for %s", t.ID)
		}
		if t.Description == "" || !validSourceURL(t.SourceURL) {
			return nil, fmt.Errorf("missing description/source for %s", t.ID)
		}
		if t.PathPolicy != "prefer" && t.PathPolicy != "managed-only" {
			return nil, fmt.Errorf("invalid path_policy for %s", t.ID)
		}
		if t.Launch.Kind != "native" && !(t.Readiness != "ready" && t.Launch.Kind == "unverified") {
			return nil, fmt.Errorf("unsupported launch kind for %s", t.ID)
		}
		if t.Skills.Repository != SkillsRepository {
			return nil, fmt.Errorf("invalid Skills repository for %s", t.ID)
		}
		skills := map[string]bool{}
		for _, name := range t.Skills.Names {
			if !commandPattern.MatchString(name) || skills[name] {
				return nil, fmt.Errorf("invalid/duplicate Skill %q", name)
			}
			skills[name] = true
		}
		switch t.Readiness {
		case "ready":
			if !officialCLISourceURL(t.SourceURL) {
				return nil, fmt.Errorf("%s requires a Volcengine official website CLI source", t.ID)
			}
			if t.Launch.Kind != "native" {
				return nil, fmt.Errorf("%s requires a standalone native executable", t.ID)
			}
			if err := upgrade.ValidateVersion(t.ReleaseVersion); err != nil {
				return nil, fmt.Errorf("%s: %w", t.ID, err)
			}
			if len(t.Platforms) == 0 {
				return nil, fmt.Errorf("ready tool %s has no platforms", t.ID)
			}
		case "discovered", "disabled":
			if len(t.Gaps) == 0 {
				return nil, fmt.Errorf("%s must describe readiness gaps", t.ID)
			}
		default:
			return nil, fmt.Errorf("invalid readiness for %s", t.ID)
		}
		for p, a := range t.Platforms {
			if !catalogPlatformPattern.MatchString(p) || !validSHA256(a.SHA256) || a.Size <= 0 || a.Size > maxArtifactBytes {
				return nil, fmt.Errorf("invalid asset for %s/%s", t.ID, p)
			}
			if _, err := resolveAssetURL(DefaultManifestURL, a.URL, allowHTTP); err != nil {
				return nil, err
			}
			if t.Readiness == "ready" {
				goos := strings.SplitN(p, "-", 2)[0]
				name := toolExecutableName(t, goos)
				// Different architectures share the same managed directory.
				// Windows and typical macOS volumes also fold filename case.
				if goos == "windows" || goos == "darwin" {
					name = strings.ToLower(name)
				}
				key := goos + "/" + name
				if owner, exists := executablePaths[key]; exists && owner != t.ID {
					return nil, fmt.Errorf("tools %s and %s share executable path %q", owner, t.ID, key)
				}
				executablePaths[key] = t.ID
			}
		}
	}
	ids := map[Tool]bool{}
	for _, t := range c.Tools {
		if ids[t.ID] {
			return nil, fmt.Errorf("duplicate tool id %q", t.ID)
		}
		ids[t.ID] = true
	}
	return &Snapshot{catalog: c, source: "local"}, nil
}
func validSourceURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
}

var embeddedSnapshotOnce sync.Once
var embeddedSnapshot *Snapshot

func EmbeddedSnapshot() *Snapshot {
	embeddedSnapshotOnce.Do(func() {
		s, err := ParseCatalog(embeddedCatalog)
		if err != nil {
			panic(err)
		}
		s.source = "embedded"
		embeddedSnapshot = s
	})
	return embeddedSnapshot
}
func cloneToolSpec(t ToolSpec) ToolSpec {
	data, _ := json.Marshal(t)
	var copy ToolSpec
	_ = json.Unmarshal(data, &copy)
	return copy
}

// Overlay preserves the original entrypoints and embedded inventory when an
// incremental catalog adds tools. Invalid merged namespaces reject the update.
func overlayCatalog(base, update *Snapshot, allowHTTP bool) (*Snapshot, error) {
	c := base.Catalog()
	c.Revision = update.catalog.Revision
	if update.catalog.MinInstallerProtocol > c.MinInstallerProtocol {
		c.MinInstallerProtocol = update.catalog.MinInstallerProtocol
	}
	for _, tool := range update.catalog.Tools {
		found := false
		for i, old := range c.Tools {
			if old.ID != tool.ID {
				continue
			}
			// A pre-protocol-2 cached inventory must not hide newly integrated tools.
			if old.Readiness == "ready" && tool.Readiness == "discovered" {
				found = true
				break
			}
			if (old.ID == ToolArkCLI || old.ID == ToolTOSUtil) && (tool.Command != old.Command || tool.Executable != old.Executable || !containsAliases(tool.Aliases, old.Aliases)) {
				return nil, fmt.Errorf("catalog changes legacy entrypoints for %s", old.ID)
			}
			if old.PathPolicy == "managed-only" && tool.PathPolicy != "managed-only" {
				return nil, fmt.Errorf("catalog cannot weaken managed-only identity policy for %s", old.ID)
			}
			c.Tools[i] = tool
			found = true
			break
		}
		if !found {
			c.Tools = append(c.Tools, tool)
		}
	}
	data, _ := json.Marshal(c)
	s, err := parseCatalog(data, allowHTTP)
	if err == nil {
		s.source = update.source
	}
	return s, err
}
func containsAliases(have, want []string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if w == h {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func officialCLISourceURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.User == nil && (u.Hostname() == "docs.volcengine.com" || u.Hostname() == "www.volcengine.com") && strings.HasPrefix(u.Path, "/docs/")
}
