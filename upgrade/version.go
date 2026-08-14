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

// isStrictlyOlder reports whether target is strictly older than current when
// both parse as semver. Opaque/incomparable pairs return false.
func isStrictlyOlder(current, target string) bool {
	c, cok := parseSemver(ensureVPrefix(current))
	t, tok := parseSemver(ensureVPrefix(target))
	if !cok || !tok {
		return false
	}
	return compareSemver(c, t) > 0
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

	// Drop build metadata first (SemVer: +build does not affect precedence).
	// Must run before prerelease split so e.g. 1.2.3+build-1 is not treated as pre "build-1".
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}

	pre := ""
	hasPre := false
	if i := strings.IndexByte(v, '-'); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
		hasPre = true
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
		return comparePrerelease(a.pre, b.pre)
	}
	return 0
}

// comparePrerelease implements SemVer prerelease precedence identifier by identifier.
// Numeric identifiers compare numerically and always sort before non-numeric identifiers.
func comparePrerelease(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	limit := len(aParts)
	if len(bParts) < limit {
		limit = len(bParts)
	}
	for i := 0; i < limit; i++ {
		if aParts[i] == bParts[i] {
			continue
		}
		aNumeric := isNumericIdentifier(aParts[i])
		bNumeric := isNumericIdentifier(bParts[i])
		switch {
		case aNumeric && bNumeric:
			return compareNumericIdentifiers(aParts[i], bParts[i])
		case aNumeric:
			return -1
		case bNumeric:
			return 1
		default:
			return strings.Compare(aParts[i], bParts[i])
		}
	}
	return cmpInt(len(aParts), len(bParts))
}

func isNumericIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func compareNumericIdentifiers(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if a == "" {
		a = "0"
	}
	if b == "" {
		b = "0"
	}
	if len(a) != len(b) {
		return cmpInt(len(a), len(b))
	}
	return strings.Compare(a, b)
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
