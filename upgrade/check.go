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
	"sync"
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
	// NoticedAt is when the upgrade notice was last printed (unix seconds).
	// Paired with NoticedCurrent: same running binary version is reminded at most
	// once per local calendar day; after the user upgrades (current changes), a
	// new notice is allowed the same day.
	NoticedAt      int64  `json:"noticed_at,omitempty"`
	NoticedCurrent string `json:"noticed_current,omitempty"`
}

// failureBackoff is how long to skip remote checks after a failed probe.
const failureBackoff = 15 * time.Minute

// ConfigDirFunc returns the CLI config directory (default ~/.volcengine/).
// Overridable in tests. Prefer util.GetConfigFileDir from callers when wiring.
var (
	ConfigDirFunc = defaultConfigDir

	// noticeCacheMu serializes every in-process cache read-modify-write section.
	// OS file locks provide cross-process exclusion, but their treatment of locks
	// opened through multiple descriptors in one process is platform-dependent.
	noticeCacheMu sync.Mutex

	// renameCheckCacheFile is a test seam for verifying that a failed atomic
	// replacement leaves the previous cache intact.
	renameCheckCacheFile = replaceCheckCacheFile
)

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

// sameLocalCalendarDay reports whether a and b fall on the same local date.
func sameLocalCalendarDay(a, b time.Time) bool {
	ay, am, ad := a.In(time.Local).Date()
	by, bm, bd := b.In(time.Local).Date()
	return ay == by && am == bm && ad == bd
}

// rootBoolFlags are root-level boolean flags that do not consume a following token.
// Keep in sync with cmd/cmd_root.go local flags and presence-only system flags.
// Note: root CLI --version (binary version) is boolean; action-scoped API --version
// only appears after a service positional and is not scanned here for subcommand id.
var rootBoolFlags = map[string]struct{}{
	"-h": {}, "--help": {},
	"-v": {}, "--version": {},
	// Presence-only system force (public + legacy triple-dash escape).
	"--force":  {},
	"---force": {},
}

// rootValueFlags are root-level flags that take a following argument.
// Include public system flags and triple-dash aliases so firstRootPositional does not
// treat their values as the root subcommand (e.g. ve --lang ZH upgrade).
// Keep in sync with:
//   - cmd/parser publicSystemFlags / allowedLegacyFixedFlags
//   - cmd/reservedDynamicFlags (double-dash reserved controls: --header, --body, --api-param)
var rootValueFlags = map[string]struct{}{
	"--lang":      {},
	"--profile":   {},
	"--region":    {},
	"--endpoint":  {},
	"--method":    {},
	"---lang":     {},
	"---profile":  {},
	"---region":   {},
	"---endpoint": {},
	"---version":  {},
	"---method":   {},
	// Double-dash reserved CLI controls (cmd.reservedDynamicFlags).
	"--header":    {},
	"--body":      {},
	"--api-param": {},
}

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

// loadCheckCacheFile reads the on-disk cache without freshness checks.
// ok=false when the file is missing, unreadable, or structurally invalid.
// Future-dated CheckedAt is rejected to avoid permanently disabling checks.
func loadCheckCacheFile() (versionCheckCache, bool) {
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
	now := time.Now().Unix()
	if c.CheckedAt > now+3600 {
		return versionCheckCache{}, false
	}
	return c, true
}

// lastKnownLatest returns Latest from disk when present, including stale and
// expired failure-backoff entries. Used so remote probe failures can still
// preserve and surface a previous upgrade notice.
func lastKnownLatest() string {
	c, ok := loadCheckCacheFile()
	if !ok {
		return ""
	}
	return NormalizeVersion(c.Latest)
}

// LoadCheckCache reads the local version check cache.
// ok=true only for a fresh success entry or an in-window soft-failure backoff.
// Stale entries return ok=false; callers that need last-known Latest on remote
// failure should use lastKnownLatest().
func LoadCheckCache() (versionCheckCache, bool) {
	c, ok := loadCheckCacheFile()
	if !ok {
		return versionCheckCache{}, false
	}
	now := time.Now().Unix()
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
// Preserves notice throttle fields so a refresh check does not re-enable spam.
func SaveCheckCache(latest, current string) error {
	return saveVersionCheckCache(latest, current, false)
}

// SaveCheckFailure persists a short backoff marker after a failed remote check.
// Keeps the previous Latest when available so we can still surface a notice
// from the last known good value without hitting the network.
// Preserves notice throttle fields so throttling survives a soft failure.
func SaveCheckFailure(previousLatest, current string) error {
	return saveVersionCheckCache(previousLatest, current, true)
}

func saveVersionCheckCache(latest, current string, failed bool) error {
	// Serialize with notice claims so a concurrent SaveCheckCache cannot clobber
	// NoticedAt/NoticedCurrent mid-claim (which would re-enable same-day spam).
	return withNoticeCacheLock(func() error {
		prev, _ := loadCheckCacheFile()
		return writeCheckCacheUnlocked(versionCheckCache{
			CheckedAt:      time.Now().Unix(),
			Latest:         NormalizeVersion(latest),
			Current:        NormalizeVersion(current),
			Failed:         failed,
			NoticedAt:      prev.NoticedAt,
			NoticedCurrent: prev.NoticedCurrent,
		})
	})
}

// noticeAlreadyClaimedToday reports whether currentVersion was already reminded
// on the local calendar day of now (and NoticedAt is not absurdly in the future).
func noticeAlreadyClaimedToday(c versionCheckCache, currentVersion string, now time.Time) bool {
	if c.NoticedAt <= 0 {
		return false
	}
	// Same guard as CheckedAt: future timestamps must not permanently silence notices.
	if c.NoticedAt > now.Unix()+3600 {
		return false
	}
	if NormalizeVersion(c.NoticedCurrent) != NormalizeVersion(currentVersion) {
		return false
	}
	return sameLocalCalendarDay(time.Unix(c.NoticedAt, 0), now)
}

// noticeCacheLockWait is how long claim/save wait for the cross-process lock.
// Bounded so a stuck holder cannot hang every ve process on the notice path.
const noticeCacheLockWait = 500 * time.Millisecond

var acquireNoticeCacheFileLock = acquireExclusiveFileLockWithWait

// noticeCacheLockPath is a sibling lock file for version_check.json cross-process claim.
// The lock file is never unlinked: flock/LockFileEx bind to the open inode/handle;
// deleting a held lock path lets another process create a new inode and dual-hold.
func noticeCacheLockPath() (string, error) {
	path, err := CheckCachePath()
	if err != nil {
		return "", err
	}
	return path + ".lock", nil
}

// withNoticeCacheLock serializes fn in-process and, when possible, holds an
// exclusive cross-process lock on the notice cache.
//
// Waits up to noticeCacheLockWait using non-blocking lock attempts.
// A busy-lock timeout must not run fn: doing so would bypass a known holder and
// permit lost read-modify-write updates. If file locking is unavailable for a
// non-contention reason (permissions, unsupported filesystem, missing config
// dir), fn still runs under the process mutex as a best-effort fallback.
//
// Not re-entrant: do not call writeCheckCache / SaveCheckCache / this helper
// again from inside fn. Locked sections must use writeCheckCacheUnlocked only.
func withNoticeCacheLock(fn func() error) error {
	noticeCacheMu.Lock()
	defer noticeCacheMu.Unlock()

	lockPath, err := noticeCacheLockPath()
	if err != nil {
		return fn()
	}
	lock, err := acquireNoticeCacheFileLock(lockPath, noticeCacheLockWait)
	if err != nil {
		if isUpgradeLockBusy(err) {
			return fmt.Errorf("timed out waiting for notice cache lock %s: %w", lockPath, err)
		}
		return fn()
	}
	defer lock.release()
	return fn()
}

// acquireExclusiveFileLockWithWait polls a non-blocking exclusive lock until
// success, a non-busy error, or deadline. Avoids unbounded flock/LockFileEx waits.
func acquireExclusiveFileLockWithWait(lockPath string, wait time.Duration) (*upgradeLock, error) {
	deadline := time.Now().Add(wait)
	var lastErr error
	for {
		lock, err := acquireExclusiveFileLock(lockPath)
		if err == nil {
			return lock, nil
		}
		lastErr = err
		if !isUpgradeLockBusy(err) {
			return nil, err
		}
		if !time.Now().Before(deadline) {
			return nil, lastErr
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// tryClaimUpgradeNotice claims the once-per-day notice slot for currentVersion.
// Returns true if the caller should print; false if already claimed today for
// this running version.
//
// Cross-process: claim is serialized with a file lock so concurrent ve processes
// (see 14_upgrade_e2e-10) observe at most one winner per current+day when the
// lock is acquired. If a busy lock times out, the caller may still print without
// updating the cache so upgrades are not hidden, but it must not bypass the lock
// holder and clobber its cache update.
// Best-effort write: a disk error still returns true so a transient FS issue does
// not permanently hide upgrades, at the cost of possible re-print on the next command.
func tryClaimUpgradeNotice(latest, current string) bool {
	current = NormalizeVersion(current)
	now := time.Now()
	var shouldPrint bool
	err := withNoticeCacheLock(func() error {
		shouldPrint = claimUpgradeNoticeLocked(latest, current, now)
		return nil
	})
	if err != nil {
		return true
	}
	return shouldPrint
}

// claimUpgradeNoticeLocked must run under withNoticeCacheLock. It re-reads the
// cache so concurrent waiters see the winner's stamp.
func claimUpgradeNoticeLocked(latest, current string, now time.Time) bool {
	c, ok := loadCheckCacheFile()
	if ok && noticeAlreadyClaimedToday(c, current, now) {
		return false
	}
	ts := now.Unix()
	if ok {
		c.NoticedAt = ts
		c.NoticedCurrent = current
		// Write failure: still print (do not hide upgrades), may re-print later.
		_ = writeCheckCacheUnlocked(c)
		return true
	}
	// Rare: print path without a readable cache. Persist enough state to throttle.
	_ = writeCheckCacheUnlocked(versionCheckCache{
		CheckedAt:      ts,
		Latest:         NormalizeVersion(latest),
		Current:        current,
		NoticedAt:      ts,
		NoticedCurrent: current,
	})
	return true
}

// InvalidateCheckCache removes the version check cache (best-effort).
// Does not delete the sibling .lock file: unlinking a held lock path allows a
// second process to create a new inode and take a concurrent "exclusive" lock.
func InvalidateCheckCache() {
	_ = withNoticeCacheLock(func() error {
		path, err := CheckCachePath()
		if err != nil {
			return nil
		}
		_ = os.Remove(path)
		return nil
	})
}

func writeCheckCache(c versionCheckCache) error {
	return withNoticeCacheLock(func() error {
		return writeCheckCacheUnlocked(c)
	})
}

func writeCheckCacheUnlocked(c versionCheckCache) error {
	path, err := CheckCachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	// Refuse existing non-regular paths (symlink/FIFO/dir). Atomic rename below
	// replaces a raced symlink rather than following it, but reject an existing
	// one explicitly instead of silently changing its directory entry.
	if err := rejectNonRegularPath(path); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := ioutil.TempFile(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	tmpOpen := true
	defer func() {
		if tmpOpen {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpPath)
	}()

	if err := tmp.Chmod(0600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		tmpOpen = false
		return err
	}
	tmpOpen = false

	// Recheck immediately before replacement. Rename is same-directory and
	// therefore atomic: readers observe either the complete old JSON or the
	// complete new JSON. A failed replacement leaves the old cache untouched.
	if err := rejectNonRegularPath(path); err != nil {
		return err
	}
	if err := renameCheckCacheFile(tmpPath, path); err != nil {
		return err
	}
	return nil
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
	// Preserve any on-disk Latest (including TTL-expired) so failure backoff
	// can still surface an upgrade notice without wiping history.
	previousLatest := lastKnownLatest()
	latest, err := ResolveLatestVersionQuick()
	if err != nil {
		_ = SaveCheckFailure(previousLatest, currentVersion)
		// Keep Err nil when we still have a last-known version so
		// MaybePrintUpgradeNotice can print during the failure window.
		if previousLatest != "" {
			return CheckResult{
				Current:   currentVersion,
				Latest:    previousLatest,
				HasUpdate: IsNewer(currentVersion, previousLatest),
				CheckedAt: time.Now(),
				FromCache: true,
			}
		}
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

// FormatUpgradeNotice 生成单行 stderr 升级提示（无尾部换行）。
// 建议命令会随当前二进制的安装来源变化（npm / brew / ve upgrade）。
func FormatUpgradeNotice(current, latest string) string {
	return FormatUpgradeNoticeFor(current, latest, DetectInstallFromRunningBinary())
}

// FormatUpgradeNoticeFor 与 FormatUpgradeNotice 相同，但安装来源由调用方显式传入（便于测试）。
func FormatUpgradeNoticeFor(current, latest string, info InstallInfo) string {
	current = NormalizeVersion(current)
	latest = NormalizeVersion(latest)
	cmd := "ve upgrade"
	if info.Managed() && strings.TrimSpace(info.UpgradeCmd) != "" {
		cmd = info.UpgradeCmd
	}
	// 升级命令用黄色 ANSI 高亮
	const yellow = "\033[33m"
	const reset = "\033[0m"
	return fmt.Sprintf(
		"A new version of ve is available: %s (current: %s). Run: %s%s%s",
		latest, current, yellow, cmd, reset,
	)
}

// MaybePrintUpgradeNotice prints to stderr when an update is available.
// Throttle: the same running current version is reminded at most once per local
// calendar day; after current changes (upgrade), another notice is allowed even
// the same day. Claims the slot under a cross-process file lock before printing
// so concurrent ve processes print at most once when locking succeeds. Never
// writes to stdout.
func MaybePrintUpgradeNotice(stderr *os.File, currentVersion string, ac *AsyncCheck) {
	if ac == nil || stderr == nil {
		return
	}
	res, ready := ac.TryResult()
	if !ready || res.Err != nil || !res.HasUpdate {
		return
	}
	if !tryClaimUpgradeNotice(res.Latest, currentVersion) {
		return
	}
	fmt.Fprintln(stderr, FormatUpgradeNotice(currentVersion, res.Latest))
}
