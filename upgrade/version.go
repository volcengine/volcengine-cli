package upgrade

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"fmt"
	"strconv"
	"strings"
)

// NormalizeVersion strips a leading "v"/"V" and surrounding space.
func NormalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if len(version) > 0 && (version[0] == 'v' || version[0] == 'V') {
		return version[1:]
	}
	return version
}

// ValidateVersion rejects empty or path-like version strings used in download URLs.
// Accepts common release forms: 1.0.49, v1.0.49, 1.0.49-rc.1.
func ValidateVersion(version string) error {
	v := NormalizeVersion(version)
	if v == "" {
		return fmt.Errorf("empty version")
	}
	if strings.ContainsAny(v, `/\`) || strings.Contains(v, "..") {
		return fmt.Errorf("invalid version %q: path separators not allowed", version)
	}
	if _, ok := parseSemver(ensureVPrefix(v)); !ok {
		// Allow opaque but safe identifiers (no slashes) for non-semver tags.
		for _, r := range v {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
				r == '.' || r == '-' || r == '_' || r == '+' {
				continue
			}
			return fmt.Errorf("invalid version %q: unsupported characters", version)
		}
	}
	return nil
}

// ensureVPrefix returns a version string with a leading "v" for comparison.
func ensureVPrefix(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "v0.0.0"
	}
	if version[0] == 'v' || version[0] == 'V' {
		return "v" + version[1:]
	}
	return "v" + version
}

// IsNewer reports whether latest is strictly newer than current.
// Uses a simplified semver compare (major.minor.patch[-prerelease]).
//
// When versions are not both valid semver:
//   - latest is valid, current is not → true (can upgrade from opaque/dev builds)
//   - otherwise → false (cannot prove "newer"; avoids false upgrade notices)
func IsNewer(current, latest string) bool {
	cv := ensureVPrefix(current)
	lv := ensureVPrefix(latest)

	c, cok := parseSemver(cv)
	l, lok := parseSemver(lv)
	if cok && lok {
		return compareSemver(c, l) < 0
	}
	// Official release available while running an opaque/dev build.
	if lok && !cok {
		return true
	}
	return false
}

// SameVersion reports whether two version strings refer to the same release.
func SameVersion(a, b string) bool {
	return NormalizeVersion(a) == NormalizeVersion(b)
}

type semver struct {
	major, minor, patch int
	pre                 string // without leading '-'
	hasPre              bool
}

func parseSemver(v string) (semver, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return semver{}, false
	}
	if v[0] == 'v' || v[0] == 'V' {
		v = v[1:]
	}
	if v == "" {
		return semver{}, false
	}

	pre := ""
	hasPre := false
	if i := strings.IndexByte(v, '-'); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
		hasPre = true
	}
	// drop build metadata if present on core
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(pre, '+'); i >= 0 {
		pre = pre[:i]
	}

	parts := strings.Split(v, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return semver{}, false
	}
	nums := make([]int, 3)
	for i := 0; i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return semver{}, false
		}
		nums[i] = n
	}
	return semver{
		major:  nums[0],
		minor:  nums[1],
		patch:  nums[2],
		pre:    pre,
		hasPre: hasPre,
	}, true
}

// compareSemver returns -1 if a<b, 0 if equal, 1 if a>b.
// Pre-release is lower than the same version without pre-release.
func compareSemver(a, b semver) int {
	if a.major != b.major {
		return cmpInt(a.major, b.major)
	}
	if a.minor != b.minor {
		return cmpInt(a.minor, b.minor)
	}
	if a.patch != b.patch {
		return cmpInt(a.patch, b.patch)
	}
	if a.hasPre != b.hasPre {
		if a.hasPre {
			return -1
		}
		return 1
	}
	if a.hasPre {
		return strings.Compare(a.pre, b.pre)
	}
	return 0
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
