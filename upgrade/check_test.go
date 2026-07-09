package upgrade

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
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
	if ShouldSkipBackgroundCheck([]string{"version"}) {
		t.Fatal("version should not skip")
	}
	if ShouldSkipBackgroundCheck([]string{"sts", "GetCallerIdentity"}) {
		t.Fatal("api call should not skip")
	}

	os.Setenv(EnvDisableUpdateCheck, "1")
	if !ShouldSkipBackgroundCheck([]string{"version"}) {
		t.Fatal("expected skip when env set")
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
