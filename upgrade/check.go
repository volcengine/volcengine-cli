package upgrade

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// EnvDisableUpdateCheck disables background version checks when set to 1/true/yes.
	EnvDisableUpdateCheck = "VOLCENGINE_CLI_DISABLE_UPDATE_CHECK"
	// EnvUpdateCheckTTLHours overrides the default 24h cache TTL.
	EnvUpdateCheckTTLHours = "VOLCENGINE_CLI_UPDATE_CHECK_TTL_HOURS"

	defaultCheckTTL      = 24 * time.Hour
	versionCheckFileName = "version_check.json"
	cliSubdir            = "cli"
)

// CheckResult is the outcome of a version check.
type CheckResult struct {
	Current   string
	Latest    string
	HasUpdate bool
	CheckedAt time.Time
	FromCache bool
	Err       error
}

type versionCheckCache struct {
	CheckedAt int64  `json:"checked_at"` // unix seconds
	Latest    string `json:"latest"`
	Current   string `json:"current,omitempty"`
	// Failed marks a soft failure (network/timeout). Used only for short backoff so
	// offline clients do not pay CheckHTTPTimeout on every command.
	Failed bool `json:"failed,omitempty"`
}

// failureBackoff is how long to skip remote checks after a failed probe.
const failureBackoff = 15 * time.Minute

// ConfigDirFunc returns the CLI config directory (default ~/.volcengine/).
// Overridable in tests. Prefer util.GetConfigFileDir from callers when wiring.
var ConfigDirFunc = defaultConfigDir

func defaultConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".volcengine"), nil
}

// CheckCachePath returns ~/.volcengine/cli/version_check.json
func CheckCachePath() (string, error) {
	dir, err := ConfigDirFunc()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, cliSubdir, versionCheckFileName), nil
}

// UpdateCheckDisabled reports whether background checks are disabled via env.
func UpdateCheckDisabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(EnvDisableUpdateCheck)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func checkTTL() time.Duration {
	v := strings.TrimSpace(os.Getenv(EnvUpdateCheckTTLHours))
	if v == "" {
		return defaultCheckTTL
	}
	hours, err := strconv.Atoi(v)
	if err != nil || hours <= 0 {
		return defaultCheckTTL
	}
	return time.Duration(hours) * time.Hour
}

// rootBoolFlags are root-level boolean flags that do not consume a following token.
// Keep in sync with cmd/cmd_root.go local flags.
var rootBoolFlags = map[string]struct{}{
	"-h": {}, "--help": {},
	"-v": {}, "--version": {},
}

// rootValueFlags are root-level flags that take a following argument.
// Currently empty; extend when root gains persistent string flags (e.g. --config).
var rootValueFlags = map[string]struct{}{}

// ShouldSkipBackgroundCheck returns true when the current invocation should not
// run a background version check (e.g. ve upgrade itself).
//
// Only the first root positional argument is considered: if it is "upgrade",
// skip. Flag values (and future root value-flags) are not mistaken for the
// subcommand, and parameter values like --profile upgrade are not either.
func ShouldSkipBackgroundCheck(args []string) bool {
	if UpdateCheckDisabled() {
		return true
	}
	return firstRootPositional(args) == "upgrade"
}

// firstRootPositional returns the first non-flag argv token after skipping root
// flags (and their values when applicable). Empty if none.
func firstRootPositional(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "" {
			continue
		}
		// Explicit end of flags: next token is positional.
		if a == "--" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if !strings.HasPrefix(a, "-") {
			return a
		}

		// --flag=value / -o=value: value is embedded, no extra token.
		if strings.Contains(a, "=") {
			continue
		}

		if _, ok := rootBoolFlags[a]; ok {
			continue
		}
		if _, ok := rootValueFlags[a]; ok {
			// Consume the following token as the flag value when present.
			if i+1 < len(args) {
				i++
			}
			continue
		}

		// Unknown flag (e.g. not yet registered here, or user typo). Treat as
		// boolean-like and do NOT swallow the next token — that token may be
		// the real subcommand (ve --verbose upgrade).
		continue
	}
	return ""
}

// LoadCheckCache reads the local version check cache. ok=false if missing/stale/invalid.
func LoadCheckCache() (versionCheckCache, bool) {
	path, err := CheckCachePath()
	if err != nil {
		return versionCheckCache{}, false
	}
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return versionCheckCache{}, false
	}
	var c versionCheckCache
	if err := json.Unmarshal(data, &c); err != nil {
		return versionCheckCache{}, false
	}
	if c.CheckedAt <= 0 {
		return versionCheckCache{}, false
	}
	// reject future timestamps that would permanently disable checks
	now := time.Now().Unix()
	if c.CheckedAt > now+3600 {
		return versionCheckCache{}, false
	}
	age := time.Duration(now-c.CheckedAt) * time.Second

	// Soft-failure entries only suppress remote checks briefly.
	if c.Failed {
		if age > failureBackoff {
			return versionCheckCache{}, false
		}
		return c, true
	}

	if strings.TrimSpace(c.Latest) == "" {
		return versionCheckCache{}, false
	}
	if age > checkTTL() {
		return versionCheckCache{}, false
	}
	return c, true
}

// SaveCheckCache persists a successful check result.
func SaveCheckCache(latest, current string) error {
	return writeCheckCache(versionCheckCache{
		CheckedAt: time.Now().Unix(),
		Latest:    NormalizeVersion(latest),
		Current:   NormalizeVersion(current),
		Failed:    false,
	})
}

// SaveCheckFailure persists a short backoff marker after a failed remote check.
// Keeps the previous Latest when available so we can still surface a notice
// from the last known good value without hitting the network.
func SaveCheckFailure(previousLatest, current string) error {
	return writeCheckCache(versionCheckCache{
		CheckedAt: time.Now().Unix(),
		Latest:    NormalizeVersion(previousLatest),
		Current:   NormalizeVersion(current),
		Failed:    true,
	})
}

// InvalidateCheckCache removes the version check cache (best-effort).
func InvalidateCheckCache() {
	path, err := CheckCachePath()
	if err != nil {
		return
	}
	_ = os.Remove(path)
}

func writeCheckCache(c versionCheckCache) error {
	path, err := CheckCachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(path, data, 0600)
}

// CheckForUpdate resolves latest version (using cache when fresh).
func CheckForUpdate(currentVersion string) CheckResult {
	currentVersion = NormalizeVersion(currentVersion)
	if cache, ok := LoadCheckCache(); ok {
		latest := NormalizeVersion(cache.Latest)
		// Failed backoff with no known latest: skip network, no notice.
		if cache.Failed && latest == "" {
			return CheckResult{
				Current:   currentVersion,
				FromCache: true,
				Err:       fmt.Errorf("version check backoff after recent failure"),
			}
		}
		return CheckResult{
			Current:   currentVersion,
			Latest:    latest,
			HasUpdate: latest != "" && IsNewer(currentVersion, latest),
			CheckedAt: time.Unix(cache.CheckedAt, 0),
			FromCache: true,
			// Surface last-known update during failure backoff; Err stays nil so notice can print.
		}
	}

	// Background / cache-fill path must stay on the short-timeout client.
	latest, err := ResolveLatestVersionQuick()
	if err != nil {
		_ = SaveCheckFailure("", currentVersion)
		return CheckResult{Current: currentVersion, Err: err}
	}
	_ = SaveCheckCache(latest, currentVersion)
	return CheckResult{
		Current:   currentVersion,
		Latest:    latest,
		HasUpdate: IsNewer(currentVersion, latest),
		CheckedAt: time.Now(),
		FromCache: false,
	}
}

// AsyncCheck is a background version check started at process entry.
type AsyncCheck struct {
	done   chan struct{}
	result CheckResult
}

// complete publishes the background result before closing done. Receiving from
// done establishes the happens-before relationship needed to read result safely.
func (a *AsyncCheck) complete(result CheckResult) {
	a.result = result
	close(a.done)
}

// StartBackgroundCheck launches a non-blocking version check when needed.
// Returns nil if checks are skipped or cache is already fresh with no need to re-fetch
// (still returns an AsyncCheck when cache has an update to report).
func StartBackgroundCheck(currentVersion string, args []string) *AsyncCheck {
	if ShouldSkipBackgroundCheck(args) {
		return nil
	}

	// Fast path: fresh cache / short failure backoff — no network.
	if cache, ok := LoadCheckCache(); ok {
		latest := NormalizeVersion(cache.Latest)
		res := CheckResult{
			Current:   NormalizeVersion(currentVersion),
			Latest:    latest,
			HasUpdate: !cache.Failed && latest != "" && IsNewer(currentVersion, latest),
			CheckedAt: time.Unix(cache.CheckedAt, 0),
			FromCache: true,
		}
		// During failure backoff keep last-known latest notice if we still have one.
		if cache.Failed && latest != "" {
			res.HasUpdate = IsNewer(currentVersion, latest)
		}
		if cache.Failed && latest == "" {
			res.Err = fmt.Errorf("version check backoff after recent failure")
		}
		ac := &AsyncCheck{done: make(chan struct{})}
		ac.complete(res)
		return ac
	}

	ac := &AsyncCheck{done: make(chan struct{})}
	go func() {
		ac.complete(CheckForUpdate(currentVersion))
	}()
	return ac
}

// Wait returns the check result, waiting up to timeout for in-flight network checks.
func (a *AsyncCheck) Wait(timeout time.Duration) CheckResult {
	if a == nil {
		return CheckResult{}
	}
	if timeout <= 0 {
		timeout = CheckHTTPTimeout
	}
	select {
	case <-a.done:
		return a.result
	case <-time.After(timeout):
		return CheckResult{Err: fmt.Errorf("version check timed out")}
	}
}

// TryResult returns a completed check without waiting for network I/O.
func (a *AsyncCheck) TryResult() (CheckResult, bool) {
	if a == nil {
		return CheckResult{}, false
	}
	select {
	case <-a.done:
		return a.result, true
	default:
		return CheckResult{}, false
	}
}

// FormatUpgradeNotice builds a single-line stderr notice (no trailing newline).
func FormatUpgradeNotice(current, latest string) string {
	current = NormalizeVersion(current)
	latest = NormalizeVersion(latest)
	// Yellow-ish ANSI for the upgrade command hint at the end.
	const yellow = "\033[33m"
	const reset = "\033[0m"
	return fmt.Sprintf(
		"A new version of ve is available: %s (current: %s). Run: %sve upgrade%s",
		latest, current, yellow, reset,
	)
}

// MaybePrintUpgradeNotice prints to stderr when an update is available.
// Never writes to stdout. Silent on errors / no update.
func MaybePrintUpgradeNotice(stderr *os.File, currentVersion string, ac *AsyncCheck) {
	if ac == nil || stderr == nil {
		return
	}
	res, ready := ac.TryResult()
	if !ready || res.Err != nil || !res.HasUpdate {
		return
	}
	fmt.Fprintln(stderr, FormatUpgradeNotice(currentVersion, res.Latest))
}
