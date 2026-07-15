package upgrade

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
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
	s := FormatUpgradeNotice("1.0.40", "1.0.49")
	if !strings.Contains(s, "1.0.49") || !strings.Contains(s, "1.0.40") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "ve upgrade") {
		t.Fatal(s)
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
