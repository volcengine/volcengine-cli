// Package pyruntime resolves and installs the CPython interpreter used by
// run_script. A local interpreter is preferred: an explicit, validated path
// supplied by the user, otherwise a usable CPython found on PATH. Without one,
// the ve-managed runtime installed from a pinned manifest (artifacts verified
// by SHA-256 before extraction) is used. Nothing is downloaded while
// resolving; installation is an explicit preparation step.
package pyruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// MinimumMajor and MinimumMinor define the supported script contract:
	// CPython 3.9 or newer.
	MinimumMajor = 3
	MinimumMinor = 9

	SourceExplicit = "explicit" // --python, --script-python or VOLCENGINE_SCRIPT_PYTHON
	SourceSystem   = "system"   // python3 or python found on PATH
	SourceManaged  = "managed"  // installed by ve script runtime install

	probeTimeout      = 20 * time.Second
	localProbeTimeout = 10 * time.Second
	stateFile         = "state.json"
)

// ErrNotPrepared means no explicit interpreter was given, no usable local
// interpreter was discovered and no managed runtime is installed.
var ErrNotPrepared = errors.New("the run_script Python runtime is not prepared")

// Interpreter is a validated CPython interpreter.
type Interpreter struct {
	Path           string `json:"path"`
	Implementation string `json:"implementation"`
	Version        string `json:"version"`
	Source         string `json:"source"`
}

const probeScript = `import json, platform, sys
print(json.dumps({"implementation": sys.implementation.name, "version": platform.python_version(), "major": sys.version_info[0], "minor": sys.version_info[1]}))`

// Probe runs path with the same isolation flags as the worker and checks that
// it is CPython within the supported version range.
func Probe(ctx context.Context, path string) (*Interpreter, error) {
	return probeWithTimeout(ctx, path, probeTimeout)
}

func probeWithTimeout(ctx context.Context, path string, timeout time.Duration) (*Interpreter, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("python interpreter path must be absolute: %s", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("python interpreter %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("python interpreter is not a regular file: %s", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0 {
		return nil, fmt.Errorf("python interpreter is not executable: %s", path)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(ctx, path, "-I", "-S", "-B", "-c", probeScript)
	command.Env = probeEnvironment()
	command.Dir = os.TempDir()
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 500 {
			detail = detail[:500]
		}
		return nil, fmt.Errorf("python interpreter %s failed validation: %v %s", path, err, detail)
	}
	var probe struct {
		Implementation string `json:"implementation"`
		Version        string `json:"version"`
		Major          int    `json:"major"`
		Minor          int    `json:"minor"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &probe); err != nil {
		return nil, fmt.Errorf("python interpreter %s returned an invalid probe result: %w", path, err)
	}
	if probe.Implementation != "cpython" {
		return nil, fmt.Errorf("python interpreter %s is %s; run_script requires CPython", path, probe.Implementation)
	}
	if probe.Major != MinimumMajor || probe.Minor < MinimumMinor {
		return nil, fmt.Errorf("python interpreter %s is %s; run_script requires CPython %d.%d or newer 3.x", path, probe.Version, MinimumMajor, MinimumMinor)
	}
	return &Interpreter{Path: path, Implementation: probe.Implementation, Version: probe.Version}, nil
}

func probeEnvironment() []string {
	names := []string{"PATH", "SYSTEMROOT", "WINDIR"}
	environment := []string{}
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}

// State records the active managed runtime.
type State struct {
	Python         string `json:"python"`
	Implementation string `json:"implementation"`
	Version        string `json:"version"`
	Build          string `json:"build,omitempty"`
	Platform       string `json:"platform"`
	SHA256         string `json:"sha256"`
	InstalledAt    string `json:"installed_at"`
}

// Root returns the managed runtime directory below the CLI config directory.
func Root(configDir string) string {
	return filepath.Join(configDir, "script-runtime")
}

// ReadState reads the active managed runtime record. It returns nil, nil when
// no managed runtime was installed.
func ReadState(root string) (*State, error) {
	data, err := os.ReadFile(filepath.Join(root, stateFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("managed Python runtime state is corrupted (%s): %w", filepath.Join(root, stateFile), err)
	}
	if state.Python == "" {
		return nil, fmt.Errorf("managed Python runtime state has no interpreter path")
	}
	return &state, nil
}

// Resolve returns the interpreter for script runs without downloading
// anything. An explicit path must validate and never falls back. Otherwise
// the first usable CPython named python3 or python on PATH is used, and only
// then the active ve-managed runtime.
func Resolve(ctx context.Context, explicit, root string) (*Interpreter, error) {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		interpreter, err := Probe(ctx, explicit)
		if err != nil {
			return nil, err
		}
		interpreter.Source = SourceExplicit
		return interpreter, nil
	}
	interpreter, skipped := findLocalPython(ctx, os.Getenv("PATH"), runtime.GOOS)
	if interpreter != nil {
		return interpreter, nil
	}
	state, err := ReadState(root)
	if err != nil {
		return nil, err
	}
	if state == nil {
		if len(skipped) > 0 {
			return nil, fmt.Errorf("%w; unusable local Python: %s", ErrNotPrepared, strings.Join(skipped, "; "))
		}
		return nil, ErrNotPrepared
	}
	if !isWithin(root, state.Python) {
		return nil, fmt.Errorf("managed Python runtime path %s is outside %s", state.Python, root)
	}
	interpreter, err = Probe(ctx, state.Python)
	if err != nil {
		return nil, fmt.Errorf("managed Python runtime is unusable, reinstall it: %w", err)
	}
	if interpreter.Version != state.Version {
		return nil, fmt.Errorf("managed Python runtime reports %s but %s was installed; reinstall it", interpreter.Version, state.Version)
	}
	interpreter.Source = SourceManaged
	return interpreter, nil
}

// findLocalPython probes python3 and python in each absolute PATH directory,
// in PATH order, and returns the first supported CPython. Empty and relative
// entries are skipped so the current directory is never searched. skipped
// describes candidates that exist but failed validation.
func findLocalPython(ctx context.Context, pathEnv, goos string) (*Interpreter, []string) {
	var skipped []string
	seen := map[string]bool{}
	for _, directory := range filepath.SplitList(pathEnv) {
		if directory == "" || !filepath.IsAbs(directory) {
			continue
		}
		for _, name := range candidateNames(goos) {
			candidate := filepath.Join(directory, name)
			info, err := os.Stat(candidate)
			if err != nil || info.IsDir() {
				continue
			}
			key := candidate
			if real, err := filepath.EvalSymlinks(candidate); err == nil {
				key = real
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			if reason := skipCandidate(goos, candidate); reason != "" {
				skipped = append(skipped, candidate+": "+reason)
				continue
			}
			interpreter, err := probeWithTimeout(ctx, candidate, localProbeTimeout)
			if err != nil {
				skipped = append(skipped, err.Error())
				continue
			}
			interpreter.Source = SourceSystem
			return interpreter, nil
		}
	}
	return nil, skipped
}

func candidateNames(goos string) []string {
	if goos == "windows" {
		return []string{"python.exe", "python3.exe"}
	}
	return []string{"python3", "python"}
}

// skipCandidate rejects the Microsoft Store python.exe aliases, which open the
// Store instead of running Python.
func skipCandidate(goos, candidate string) string {
	if goos == "windows" && strings.Contains(strings.ToLower(candidate), `\microsoft\windowsapps\`) {
		return "Microsoft Store app execution alias"
	}
	return ""
}

func isWithin(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
