package upgrade

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestShouldSkipBackgroundCheck(t *testing.T) {
	// Isolate from ambient process env (e.g. local smoke tests).
	old := os.Getenv(EnvDisableUpdateCheck)
	os.Unsetenv(EnvDisableUpdateCheck)
	defer func() {
		if old == "" {
			os.Unsetenv(EnvDisableUpdateCheck)
		} else {
			os.Setenv(EnvDisableUpdateCheck, old)
		}
	}()

	if !ShouldSkipBackgroundCheck([]string{"upgrade"}) {
		t.Fatal("expected skip for upgrade")
	}
	if !ShouldSkipBackgroundCheck([]string{"upgrade", "--yes"}) {
		t.Fatal("expected skip for upgrade --yes")
	}
	if !ShouldSkipBackgroundCheck([]string{"upgrade", "--version", "1.0.48"}) {
		t.Fatal("expected skip for upgrade --version")
	}
	// Root bool flags before subcommand still resolve first positional = upgrade.
	if !ShouldSkipBackgroundCheck([]string{"-v", "upgrade"}) {
		t.Fatal("expected skip for -v upgrade")
	}
	if !ShouldSkipBackgroundCheck([]string{"--help", "upgrade"}) {
		t.Fatal("expected skip for --help upgrade")
	}
	if !ShouldSkipBackgroundCheck([]string{"--", "upgrade"}) {
		t.Fatal("expected skip after --")
	}

	if ShouldSkipBackgroundCheck([]string{"version"}) {
		t.Fatal("version should not skip")
	}
	if ShouldSkipBackgroundCheck([]string{"sts", "GetCallerIdentity"}) {
		t.Fatal("api call should not skip")
	}
	// "upgrade" as a later parameter value must NOT skip.
	if ShouldSkipBackgroundCheck([]string{"configure", "set", "--profile", "upgrade"}) {
		t.Fatal("profile value upgrade must not skip")
	}
	if ShouldSkipBackgroundCheck([]string{"sts", "Foo", "--Name", "upgrade"}) {
		t.Fatal("API param value upgrade must not skip")
	}
	if ShouldSkipBackgroundCheck([]string{"help", "upgrade"}) {
		t.Fatal("help upgrade is not the upgrade command")
	}
	if ShouldSkipBackgroundCheck(nil) {
		t.Fatal("empty args should not skip")
	}

	os.Setenv(EnvDisableUpdateCheck, "1")
	if !ShouldSkipBackgroundCheck([]string{"version"}) {
		t.Fatal("expected skip when env set")
	}
}

func TestFirstRootPositional_ValueFlags(t *testing.T) {
	// Temporarily register a future-style root value flag.
	rootValueFlags["--config"] = struct{}{}
	defer delete(rootValueFlags, "--config")

	if got := firstRootPositional([]string{"--config", "/tmp/c.json", "upgrade"}); got != "upgrade" {
		t.Fatalf("got %q want upgrade", got)
	}
	if got := firstRootPositional([]string{"--config=/tmp/c.json", "upgrade"}); got != "upgrade" {
		t.Fatalf("got %q want upgrade", got)
	}
	// Unknown flag must not swallow the subcommand token.
	if got := firstRootPositional([]string{"--verbose", "upgrade"}); got != "upgrade" {
		t.Fatalf("got %q want upgrade", got)
	}
	if got := firstRootPositional([]string{"-v"}); got != "" {
		t.Fatalf("got %q want empty", got)
	}
}

func TestShouldSkipBackgroundCheck_TripleDashFixedFlags(t *testing.T) {
	old := os.Getenv(EnvDisableUpdateCheck)
	os.Unsetenv(EnvDisableUpdateCheck)
	defer func() {
		if old == "" {
			os.Unsetenv(EnvDisableUpdateCheck)
		} else {
			os.Setenv(EnvDisableUpdateCheck, old)
		}
	}()

	// S8/S9: value-taking system flags must not turn their values into the root positional.
	if !ShouldSkipBackgroundCheck([]string{"---lang", "ZH", "upgrade"}) {
		t.Fatal("expected skip for ---lang ZH upgrade")
	}
	if !ShouldSkipBackgroundCheck([]string{"--lang", "ZH", "upgrade"}) {
		t.Fatal("expected skip for --lang ZH upgrade")
	}
	if !ShouldSkipBackgroundCheck([]string{"---profile", "default", "upgrade", "--yes"}) {
		t.Fatal("expected skip for ---profile default upgrade")
	}
	if !ShouldSkipBackgroundCheck([]string{"--profile", "default", "upgrade", "--yes"}) {
		t.Fatal("expected skip for --profile default upgrade")
	}
	if !ShouldSkipBackgroundCheck([]string{"---force", "upgrade"}) {
		t.Fatal("expected skip for ---force upgrade (presence-only)")
	}
	if !ShouldSkipBackgroundCheck([]string{"--force", "upgrade"}) {
		t.Fatal("expected skip for --force upgrade (presence-only)")
	}
	if !ShouldSkipBackgroundCheck([]string{"--header", "X-Foo=bar", "upgrade"}) {
		t.Fatal("expected skip for --header … upgrade")
	}
	// ---lang consumes the next token as its value, so "upgrade" is not the subcommand.
	if ShouldSkipBackgroundCheck([]string{"---lang", "upgrade"}) {
		t.Fatal("---lang upgrade should treat upgrade as lang value, not skip")
	}
	if ShouldSkipBackgroundCheck([]string{"---lang", "ZH", "version"}) {
		t.Fatal("version must not skip even with ---lang")
	}
	if got := firstRootPositional([]string{"---lang", "ZH", "upgrade"}); got != "upgrade" {
		t.Fatalf("firstRootPositional = %q, want upgrade", got)
	}
	if got := firstRootPositional([]string{"---lang", "ZH"}); got != "" {
		t.Fatalf("firstRootPositional with only flags = %q, want empty", got)
	}
}

func TestCheckCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	orig := ConfigDirFunc
	ConfigDirFunc = func() (string, error) { return dir, nil }
	defer func() { ConfigDirFunc = orig }()

	if _, ok := LoadCheckCache(); ok {
		t.Fatal("expected empty cache miss")
	}
	if err := SaveCheckCache("1.0.50", "1.0.49"); err != nil {
		t.Fatal(err)
	}
	c, ok := LoadCheckCache()
	if !ok {
		t.Fatal("expected cache hit")
	}
	if c.Latest != "1.0.50" {
		t.Fatalf("latest=%s", c.Latest)
	}

	// expire cache
	path, _ := CheckCachePath()
	stale := `{"checked_at":1,"latest":"1.0.50"}`
	if err := ioutil.WriteFile(path, []byte(stale), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadCheckCache(); ok {
		t.Fatal("expected stale cache miss")
	}
}

func TestCheckFailureBackoff(t *testing.T) {
	dir := t.TempDir()
	orig := ConfigDirFunc
	ConfigDirFunc = func() (string, error) { return dir, nil }
	defer func() { ConfigDirFunc = orig }()

	if err := SaveCheckFailure("", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	c, ok := LoadCheckCache()
	if !ok || !c.Failed {
		t.Fatalf("expected failure backoff hit, got ok=%v failed=%v", ok, c.Failed)
	}

	// StartBackgroundCheck should not open network; HasUpdate false, Err set when no latest.
	ac := StartBackgroundCheck("1.0.0", []string{"version"})
	if ac == nil {
		t.Fatal("expected async check object")
	}
	res := ac.Wait(50 * time.Millisecond)
	if res.HasUpdate {
		t.Fatal("expected no update during empty failure backoff")
	}
	if res.Err == nil {
		t.Fatal("expected err during empty failure backoff")
	}
}

// TestCheckForUpdate_PreservesStaleLatestOnRemoteFailure 覆盖：
// 成功缓存过 TTL 后远程探测失败时，应保留历史 Latest，并在 failure backoff
// 期间仍可展示升级提示（SaveCheckFailure 不得把 Latest 写成空）。
func TestCheckForUpdate_PreservesStaleLatestOnRemoteFailure(t *testing.T) {
	dir := t.TempDir()
	orig := ConfigDirFunc
	ConfigDirFunc = func() (string, error) { return dir, nil }
	defer func() { ConfigDirFunc = orig }()

	oldDisable := os.Getenv(EnvDisableUpdateCheck)
	os.Unsetenv(EnvDisableUpdateCheck)
	defer func() {
		if oldDisable == "" {
			os.Unsetenv(EnvDisableUpdateCheck)
		} else {
			os.Setenv(EnvDisableUpdateCheck, oldDisable)
		}
	}()

	if err := SaveCheckCache("9.9.9", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	// 人为过期成功缓存。
	path, err := CheckCachePath()
	if err != nil {
		t.Fatal(err)
	}
	stale := `{"checked_at":1,"latest":"9.9.9","current":"1.0.0"}`
	if err := ioutil.WriteFile(path, []byte(stale), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadCheckCache(); ok {
		t.Fatal("expected stale cache miss before remote probe")
	}
	if got := lastKnownLatest(); got != "9.9.9" {
		t.Fatalf("lastKnownLatest=%q, want 9.9.9", got)
	}

	// 强制 CDN + GitHub 探测全部失败（避免仅关 CDN 时 GitHub 回退仍成功）。
	SetCheckHTTPClient(&http.Client{
		Timeout: 200 * time.Millisecond,
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("forced network failure")
		}),
	})
	defer SetCheckHTTPClient(&http.Client{Timeout: CheckHTTPTimeout})

	res := CheckForUpdate("1.0.0")
	if res.Latest != "9.9.9" {
		t.Fatalf("expected stale latest preserved, got %+v", res)
	}
	if !res.HasUpdate {
		t.Fatalf("expected HasUpdate from stale latest, got %+v", res)
	}
	if res.Err != nil {
		t.Fatalf("expected nil Err so notice can print, got %+v", res)
	}

	// 失败写回应带上 previous Latest，后续 15min backoff 仍可提示。
	c, ok := LoadCheckCache()
	if !ok || !c.Failed {
		t.Fatalf("expected failure backoff cache, ok=%v failed=%v", ok, c.Failed)
	}
	if NormalizeVersion(c.Latest) != "9.9.9" {
		t.Fatalf("failure cache latest=%q, want 9.9.9", c.Latest)
	}

	ac := StartBackgroundCheck("1.0.0", []string{"version"})
	if ac == nil {
		t.Fatal("expected async check")
	}
	bg := ac.Wait(50 * time.Millisecond)
	if !bg.HasUpdate || bg.Latest != "9.9.9" {
		t.Fatalf("expected notice-capable backoff result, got %+v", bg)
	}
}

func TestInvalidateCheckCache(t *testing.T) {
	dir := t.TempDir()
	orig := ConfigDirFunc
	ConfigDirFunc = func() (string, error) { return dir, nil }
	defer func() { ConfigDirFunc = orig }()

	if err := SaveCheckCache("1.0.50", "1.0.49"); err != nil {
		t.Fatal(err)
	}
	InvalidateCheckCache()
	if _, ok := LoadCheckCache(); ok {
		t.Fatal("expected miss after invalidate")
	}
}

func TestFormatUpgradeNotice(t *testing.T) {
	s := FormatUpgradeNoticeFor("1.0.40", "1.0.49", standaloneInfo("", DetectedByDefault))
	if !strings.Contains(s, "1.0.49") || !strings.Contains(s, "1.0.40") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "ve upgrade") {
		t.Fatal(s)
	}

	npmNotice := FormatUpgradeNoticeFor("1.0.40", "1.0.49", npmInfo("/x/node_modules/@volcengine/cli/bin/ve"))
	if !strings.Contains(npmNotice, "npm install -g @volcengine/cli@latest") {
		t.Fatal(npmNotice)
	}
	brewNotice := FormatUpgradeNoticeFor("1.0.40", "1.0.49", homebrewInfo("/opt/homebrew/Cellar/volcengine-cli/1.0.40/bin/ve"))
	if !strings.Contains(brewNotice, "brew upgrade volcengine-cli") {
		t.Fatal(brewNotice)
	}
}

func TestStartBackgroundCheck_FromCache(t *testing.T) {
	old := os.Getenv(EnvDisableUpdateCheck)
	os.Unsetenv(EnvDisableUpdateCheck)
	defer func() {
		if old == "" {
			os.Unsetenv(EnvDisableUpdateCheck)
		} else {
			os.Setenv(EnvDisableUpdateCheck, old)
		}
	}()

	dir := t.TempDir()
	orig := ConfigDirFunc
	ConfigDirFunc = func() (string, error) { return dir, nil }
	defer func() { ConfigDirFunc = orig }()

	if err := SaveCheckCache("9.9.9", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	ac := StartBackgroundCheck("1.0.0", []string{"version"})
	if ac == nil {
		t.Fatal("expected async check")
	}
	res := ac.Wait(100 * time.Millisecond)
	if !res.HasUpdate || res.Latest != "9.9.9" {
		t.Fatalf("%+v", res)
	}
	if !res.FromCache {
		t.Fatal("expected from cache")
	}
}

func TestMaybePrintUpgradeNotice_DoesNotWaitForInFlightCheck(t *testing.T) {
	dir := t.TempDir()
	origConfigDirFunc := ConfigDirFunc
	ConfigDirFunc = func() (string, error) { return dir, nil }
	defer func() { ConfigDirFunc = origConfigDirFunc }()

	oldDisable := os.Getenv(EnvDisableUpdateCheck)
	os.Unsetenv(EnvDisableUpdateCheck)
	defer func() {
		if oldDisable == "" {
			os.Unsetenv(EnvDisableUpdateCheck)
		} else {
			os.Setenv(EnvDisableUpdateCheck, oldDisable)
		}
	}()

	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseRequest) })
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-requestStarted:
		default:
			close(requestStarted)
		}
		<-releaseRequest
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"latest":"9.9.9"}`)
	}))
	defer func() {
		release()
		server.Close()
	}()

	oldDownloadBase := os.Getenv(EnvDownloadBaseURL)
	os.Setenv(EnvDownloadBaseURL, server.URL)
	defer os.Setenv(EnvDownloadBaseURL, oldDownloadBase)
	SetCheckHTTPClient(server.Client())
	defer SetCheckHTTPClient(&http.Client{Timeout: CheckHTTPTimeout})

	asyncCheck := StartBackgroundCheck("1.0.0", []string{"version"})
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("background check did not start")
	}

	stderr, err := os.CreateTemp(t.TempDir(), "stderr-*")
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()

	startedAt := time.Now()
	MaybePrintUpgradeNotice(stderr, "1.0.0", asyncCheck)
	elapsed := time.Since(startedAt)
	if elapsed >= 100*time.Millisecond {
		t.Fatalf("notice path blocked for %s while check was still in flight", elapsed)
	}

	release()
	_ = asyncCheck.Wait(time.Second)
}

func TestMaybePrintUpgradeNotice_PrintsReadyCachedUpdate(t *testing.T) {
	dir := t.TempDir()
	origConfigDirFunc := ConfigDirFunc
	ConfigDirFunc = func() (string, error) { return dir, nil }
	defer func() { ConfigDirFunc = origConfigDirFunc }()

	oldDisable := os.Getenv(EnvDisableUpdateCheck)
	os.Unsetenv(EnvDisableUpdateCheck)
	defer func() {
		if oldDisable == "" {
			os.Unsetenv(EnvDisableUpdateCheck)
		} else {
			os.Setenv(EnvDisableUpdateCheck, oldDisable)
		}
	}()

	if err := SaveCheckCache("9.9.9", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	asyncCheck := StartBackgroundCheck("1.0.0", []string{"version"})

	stderrPath := filepath.Join(t.TempDir(), "stderr.txt")
	stderr, err := os.Create(stderrPath)
	if err != nil {
		t.Fatal(err)
	}
	MaybePrintUpgradeNotice(stderr, "1.0.0", asyncCheck)
	if err := stderr.Close(); err != nil {
		t.Fatal(err)
	}

	output, err := ioutil.ReadFile(stderrPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "9.9.9") || !strings.Contains(string(output), "ve upgrade") {
		t.Fatalf("expected cached upgrade notice, got %q", output)
	}

	c, ok := loadCheckCacheFile()
	if !ok || c.NoticedAt <= 0 || c.NoticedCurrent != "1.0.0" {
		t.Fatalf("expected notice stamp after print, got ok=%v noticed_at=%d noticed_current=%q",
			ok, c.NoticedAt, c.NoticedCurrent)
	}
}

func TestMaybePrintUpgradeNotice_OncePerDayPerCurrent(t *testing.T) {
	dir := t.TempDir()
	origConfigDirFunc := ConfigDirFunc
	ConfigDirFunc = func() (string, error) { return dir, nil }
	defer func() { ConfigDirFunc = origConfigDirFunc }()

	oldDisable := os.Getenv(EnvDisableUpdateCheck)
	os.Unsetenv(EnvDisableUpdateCheck)
	defer func() {
		if oldDisable == "" {
			os.Unsetenv(EnvDisableUpdateCheck)
		} else {
			os.Setenv(EnvDisableUpdateCheck, oldDisable)
		}
	}()

	readNotice := func(ac *AsyncCheck, current string) string {
		stderrPath := filepath.Join(t.TempDir(), "stderr.txt")
		stderr, err := os.Create(stderrPath)
		if err != nil {
			t.Fatal(err)
		}
		MaybePrintUpgradeNotice(stderr, current, ac)
		if err := stderr.Close(); err != nil {
			t.Fatal(err)
		}
		output, err := ioutil.ReadFile(stderrPath)
		if err != nil {
			t.Fatal(err)
		}
		return string(output)
	}

	if err := SaveCheckCache("1.0.51", "1.0.50"); err != nil {
		t.Fatal(err)
	}
	ac50 := StartBackgroundCheck("1.0.50", []string{"version"})

	// current=1.0.50, latest=1.0.51: first command reminds, rest of day silent.
	first := readNotice(ac50, "1.0.50")
	if !strings.Contains(first, "1.0.51") {
		t.Fatalf("expected first notice, got %q", first)
	}
	second := readNotice(ac50, "1.0.50")
	if second != "" {
		t.Fatalf("expected same-day same-current suppressed, got %q", second)
	}

	// User upgrades to 1.0.51 but latest is now 1.0.52: current changed → remind again today.
	if err := SaveCheckCache("1.0.52", "1.0.51"); err != nil {
		t.Fatal(err)
	}
	ac51 := StartBackgroundCheck("1.0.51", []string{"version"})
	afterUpgrade := readNotice(ac51, "1.0.51")
	if !strings.Contains(afterUpgrade, "1.0.52") {
		t.Fatalf("expected re-notice after current changed, got %q", afterUpgrade)
	}
	if again := readNotice(ac51, "1.0.51"); again != "" {
		t.Fatalf("expected same-day suppress for 1.0.51, got %q", again)
	}

	// Previous local calendar day (not rolling hours), same current: remind again.
	c, ok := loadCheckCacheFile()
	if !ok {
		t.Fatal("expected cache after notice")
	}
	y, m, d := time.Now().In(time.Local).Date()
	c.NoticedAt = time.Date(y, m, d-1, 12, 0, 0, 0, time.Local).Unix()
	if err := writeCheckCache(c); err != nil {
		t.Fatal(err)
	}
	nextDay := readNotice(ac51, "1.0.51")
	if !strings.Contains(nextDay, "1.0.52") {
		t.Fatalf("expected notice on a new calendar day, got %q", nextDay)
	}
}

func TestSaveCheckCache_PreservesNoticeThrottle(t *testing.T) {
	dir := t.TempDir()
	orig := ConfigDirFunc
	ConfigDirFunc = func() (string, error) { return dir, nil }
	defer func() { ConfigDirFunc = orig }()

	if err := SaveCheckCache("9.9.9", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	c, ok := loadCheckCacheFile()
	if !ok {
		t.Fatal("expected cache")
	}
	c.NoticedAt = time.Now().Add(-time.Hour).Unix()
	c.NoticedCurrent = "1.0.0"
	if err := writeCheckCache(c); err != nil {
		t.Fatal(err)
	}
	wantAt := c.NoticedAt
	wantCur := c.NoticedCurrent

	if err := SaveCheckCache("9.9.10", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	got, ok := loadCheckCacheFile()
	if !ok {
		t.Fatal("expected cache after save")
	}
	if got.NoticedAt != wantAt || got.NoticedCurrent != wantCur {
		t.Fatalf("notice stamp=%d/%q, want %d/%q preserved across SaveCheckCache",
			got.NoticedAt, got.NoticedCurrent, wantAt, wantCur)
	}
	if got.Latest != "9.9.10" {
		t.Fatalf("latest=%s", got.Latest)
	}
}

func TestSameLocalCalendarDay(t *testing.T) {
	y, m, d := time.Now().In(time.Local).Date()
	todayMorning := time.Date(y, m, d, 1, 0, 0, 0, time.Local)
	todayEvening := time.Date(y, m, d, 23, 0, 0, 0, time.Local)
	yesterday := time.Date(y, m, d-1, 12, 0, 0, 0, time.Local)
	if !sameLocalCalendarDay(todayMorning, todayEvening) {
		t.Fatal("same calendar day expected")
	}
	if sameLocalCalendarDay(todayMorning, yesterday) {
		t.Fatal("different calendar day expected")
	}
}

func TestNoticeAlreadyClaimedToday(t *testing.T) {
	now := time.Now()
	y, m, d := now.In(time.Local).Date()
	today := time.Date(y, m, d, 10, 0, 0, 0, time.Local).Unix()
	yesterday := time.Date(y, m, d-1, 10, 0, 0, 0, time.Local).Unix()

	if !noticeAlreadyClaimedToday(versionCheckCache{NoticedAt: today, NoticedCurrent: "1.0.50"}, "1.0.50", now) {
		t.Fatal("same current same day should claim")
	}
	if noticeAlreadyClaimedToday(versionCheckCache{NoticedAt: today, NoticedCurrent: "1.0.50"}, "1.0.51", now) {
		t.Fatal("different current should not claim")
	}
	if noticeAlreadyClaimedToday(versionCheckCache{NoticedAt: yesterday, NoticedCurrent: "1.0.50"}, "1.0.50", now) {
		t.Fatal("previous day should not claim")
	}
	if noticeAlreadyClaimedToday(versionCheckCache{}, "1.0.50", now) {
		t.Fatal("empty stamp should not claim")
	}
}

// TestTryClaimUpgradeNotice_ConcurrentGoroutines verifies in-process serialization
// of the once-per-day claim under a shared cache directory.
func TestTryClaimUpgradeNotice_ConcurrentGoroutines(t *testing.T) {
	dir := t.TempDir()
	orig := ConfigDirFunc
	ConfigDirFunc = func() (string, error) { return dir, nil }
	defer func() { ConfigDirFunc = orig }()

	if err := SaveCheckCache("9.9.9", "1.0.0"); err != nil {
		t.Fatal(err)
	}

	const n = 32
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed int
	)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if tryClaimUpgradeNotice("9.9.9", "1.0.0") {
				mu.Lock()
				claimed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if claimed != 1 {
		t.Fatalf("claimed=%d, want exactly 1 under concurrent goroutines", claimed)
	}
	c, ok := loadCheckCacheFile()
	if !ok || c.NoticedCurrent != "1.0.0" || c.NoticedAt <= 0 {
		t.Fatalf("cache after claim: ok=%v noticed_current=%q noticed_at=%d", ok, c.NoticedCurrent, c.NoticedAt)
	}
}

// TestTryClaimUpgradeNotice_ConcurrentProcesses mirrors 14_upgrade_e2e-10:
// many real OS processes race on the same HOME cache; only one may claim.
// Worker mode is activated via VE_TEST_NOTICE_CLAIM_WORKER=1 (re-exec of this test).
func TestTryClaimUpgradeNotice_ConcurrentProcesses(t *testing.T) {
	if os.Getenv("VE_TEST_NOTICE_CLAIM_WORKER") == "1" {
		// Subprocess worker: ConfigDir is ~/.volcengine via HOME/USERPROFILE.
		if tryClaimUpgradeNotice("9.9.9", "1.0.0") {
			fmt.Print("1")
		} else {
			fmt.Print("0")
		}
		os.Exit(0)
	}

	home := t.TempDir()
	cliDir := filepath.Join(home, ".volcengine", "cli")
	if err := os.MkdirAll(cliDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Seed a readable cache so claim path updates NoticedAt rather than synthesizing.
	seed := versionCheckCache{
		CheckedAt: time.Now().Unix(),
		Latest:    "9.9.9",
		Current:   "1.0.0",
	}
	data, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(filepath.Join(cliDir, versionCheckFileName), data, 0600); err != nil {
		t.Fatal(err)
	}

	const (
		rounds    = 5
		processes = 16
	)
	for round := 0; round < rounds; round++ {
		// Reset notice throttle between rounds (same current, new day stamp cleared).
		seed.NoticedAt = 0
		seed.NoticedCurrent = ""
		seed.CheckedAt = time.Now().Unix()
		data, err = json.Marshal(seed)
		if err != nil {
			t.Fatal(err)
		}
		if err := ioutil.WriteFile(filepath.Join(cliDir, versionCheckFileName), data, 0600); err != nil {
			t.Fatal(err)
		}

		type result struct {
			out []byte
			err error
		}
		results := make(chan result, processes)
		for i := 0; i < processes; i++ {
			go func() {
				cmd := exec.Command(os.Args[0], "-test.run=^TestTryClaimUpgradeNotice_ConcurrentProcesses$", "-test.v=false")
				cmd.Env = noticeClaimWorkerEnv(home)
				out, err := cmd.Output()
				results <- result{out: out, err: err}
			}()
		}
		claimed := 0
		for i := 0; i < processes; i++ {
			r := <-results
			if r.err != nil {
				t.Fatalf("round %d worker failed: %v out=%q", round+1, r.err, r.out)
			}
			switch strings.TrimSpace(string(r.out)) {
			case "1":
				claimed++
			case "0":
			default:
				t.Fatalf("round %d unexpected worker output %q", round+1, r.out)
			}
		}
		if claimed != 1 {
			t.Fatalf("round %d claimed=%d processes=%d, want exactly 1", round+1, claimed, processes)
		}
	}
}

// noticeClaimWorkerEnv builds a scrubbed env so Windows/Unix workers use the
// temp home (not the developer's real profile) for version_check.json.
func noticeClaimWorkerEnv(home string) []string {
	env := make([]string, 0, len(os.Environ())+4)
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		switch strings.ToUpper(kv[:eq]) {
		case "HOME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "VE_TEST_NOTICE_CLAIM_WORKER":
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"VE_TEST_NOTICE_CLAIM_WORKER=1",
		"HOME="+home,
		"USERPROFILE="+home,
	)
}

func TestWriteCheckCache_RejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	orig := ConfigDirFunc
	ConfigDirFunc = func() (string, error) { return dir, nil }
	defer func() { ConfigDirFunc = orig }()

	cliDir := filepath.Join(dir, cliSubdir)
	if err := os.MkdirAll(cliDir, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "secret.txt")
	if err := ioutil.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(cliDir, versionCheckFileName)
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	err := writeCheckCache(versionCheckCache{CheckedAt: time.Now().Unix(), Latest: "1.0.0"})
	if err == nil {
		t.Fatal("expected symlink cache path to be rejected")
	}
	body, readErr := ioutil.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(body) != "keep" {
		t.Fatalf("symlink target was clobbered: %q", body)
	}
}

// TestSaveCheckCache_ConcurrentWithClaim ensures SaveCheckCache cannot clobber a
// same-day Noticed* stamp while concurrent claims race for the slot.
func TestSaveCheckCache_ConcurrentWithClaim(t *testing.T) {
	dir := t.TempDir()
	orig := ConfigDirFunc
	ConfigDirFunc = func() (string, error) { return dir, nil }
	defer func() { ConfigDirFunc = orig }()

	if err := SaveCheckCache("9.9.9", "1.0.0"); err != nil {
		t.Fatal(err)
	}

	const n = 24
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed int
	)
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				_ = SaveCheckCache(fmt.Sprintf("9.9.%d", i), "1.0.0")
				return
			}
			if tryClaimUpgradeNotice("9.9.9", "1.0.0") {
				mu.Lock()
				claimed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if claimed != 1 {
		t.Fatalf("claimed=%d, want 1 while SaveCheckCache races", claimed)
	}
	c, ok := loadCheckCacheFile()
	if !ok || c.NoticedCurrent != "1.0.0" || c.NoticedAt <= 0 {
		t.Fatalf("notice stamp lost under concurrent save: ok=%v c=%+v", ok, c)
	}
	// Second claim same day must still be suppressed after mixed saves.
	if tryClaimUpgradeNotice("9.9.9", "1.0.0") {
		t.Fatal("expected same-day claim suppressed after concurrent save/claim")
	}
}

func TestCheckCachePath(t *testing.T) {
	dir := t.TempDir()
	orig := ConfigDirFunc
	ConfigDirFunc = func() (string, error) { return dir, nil }
	defer func() { ConfigDirFunc = orig }()

	p, err := CheckCachePath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "cli", "version_check.json")
	if p != want {
		t.Fatalf("got %s want %s", p, want)
	}
}
