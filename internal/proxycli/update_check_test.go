package proxycli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPrepareUpdateCheckWritesBackoffLeaseBeforeWorkerStarts(t *testing.T) {
	m := testManager(t, t.TempDir(), nil)
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.Local)
	m.Now = func() time.Time { return now }
	resolution := Resolution{Tool: ToolArkCLI, Path: m.managedPath(ToolArkCLI), Version: "1.0.0", Source: SourceManaged}

	plan := m.PrepareUpdateCheck(resolution)
	if !plan.Refresh || plan.HasNotice {
		t.Fatalf("first plan = %+v", plan)
	}
	cache := m.readUpdateCheckCache()
	entry := cache.Tools[ToolArkCLI]
	if entry == nil || !entry.Failed || entry.CheckedAt != now.Unix() || entry.Current != "1.0.0" {
		t.Fatalf("synchronous lease = %+v", entry)
	}

	// Simulate ve immediately exec'ing a short-lived child before a worker can
	// finish. The next invocation must honor the lease instead of launching a
	// second CDN request.
	second := m.PrepareUpdateCheck(resolution)
	if second.Refresh || second.HasNotice {
		t.Fatalf("lease did not suppress duplicate check: %+v", second)
	}
}

func TestRefreshUpdateCheckCachesResultAndNoticeIsClaimedAtPrepare(t *testing.T) {
	home := t.TempDir()
	var requests int32
	manifest := Manifest{Tools: map[Tool]ManifestTool{
		ToolArkCLI: {
			Version: "2.0.0",
			Platforms: map[string]ManifestAsset{
				platformKey(runtime.GOOS, runtime.GOARCH): {
					URL: "https://example.com/arkcli", SHA256: digest([]byte("new")),
				},
			},
		},
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		_ = json.NewEncoder(w).Encode(manifest)
	}))
	defer server.Close()
	m := testManager(t, home, server.Client())
	m.ManifestURL = server.URL
	m.CheckHTTPClient = server.Client()
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.Local)
	m.Now = func() time.Time { return now }
	resolution := Resolution{Tool: ToolArkCLI, Path: m.managedPath(ToolArkCLI), Version: "1.0.0", Source: SourceManaged}

	if plan := m.PrepareUpdateCheck(resolution); !plan.Refresh {
		t.Fatalf("stale plan = %+v", plan)
	}
	if err := m.RefreshUpdateCheck(ToolArkCLI, "1.0.0"); err != nil {
		t.Fatalf("RefreshUpdateCheck() error = %v", err)
	}
	plan := m.PrepareUpdateCheck(resolution)
	if plan.Refresh || !plan.HasNotice || !plan.Notice.HasUpdate || plan.Notice.Latest != "2.0.0" {
		t.Fatalf("cached update plan = %+v", plan)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("manifest requests = %d, want 1", got)
	}
	if second := m.PrepareUpdateCheck(resolution); second.Refresh || second.HasNotice {
		t.Fatalf("same-day notice repeated: %+v", second)
	}

	now = now.Add(12 * time.Hour)
	third := m.PrepareUpdateCheck(resolution)
	if third.Refresh || !third.HasNotice {
		t.Fatalf("next-day cached notice = %+v", third)
	}
}

func TestPrepareUpdateCheckSkipsExternalBinary(t *testing.T) {
	m := testManager(t, t.TempDir(), nil)
	plan := m.PrepareUpdateCheck(Resolution{
		Tool: ToolArkCLI, Path: "/usr/local/bin/arkcli", Version: "1.0.0", Source: SourcePath,
	})
	if plan.Refresh || plan.HasNotice {
		t.Fatalf("external PATH binary participated in update check: %+v", plan)
	}
}

func TestPrepareUpdateCheckHonorsGlobalDisableSwitch(t *testing.T) {
	t.Setenv("VOLCENGINE_CLI_DISABLE_UPDATE_CHECK", "1")
	m := testManager(t, t.TempDir(), nil)
	plan := m.PrepareUpdateCheck(Resolution{
		Tool: ToolArkCLI, Path: m.managedPath(ToolArkCLI), Version: "1.0.0", Source: SourceManaged,
	})
	if plan.Refresh || plan.HasNotice {
		t.Fatalf("disabled update check returned plan %+v", plan)
	}
}

func TestUpdateCheckUsesGlobalTTLOverrideAndVeFailureBackoff(t *testing.T) {
	t.Setenv("VOLCENGINE_CLI_UPDATE_CHECK_TTL_HOURS", "2")
	m := testManager(t, t.TempDir(), nil)
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.Local)
	m.Now = func() time.Time { return now }
	resolution := Resolution{Tool: ToolArkCLI, Path: m.managedPath(ToolArkCLI), Version: "1.0.0", Source: SourceManaged}

	cache := updateCheckCache{Tools: map[Tool]*updateCheckEntry{
		ToolArkCLI: {Current: "1.0.0", Latest: "1.0.0", CheckedAt: now.Add(-90 * time.Minute).Unix()},
	}}
	if err := m.writeUpdateCheckCache(cache); err != nil {
		t.Fatal(err)
	}
	if plan := m.PrepareUpdateCheck(resolution); plan.Refresh {
		t.Fatalf("TTL override was ignored: %+v", plan)
	}

	cache.Tools[ToolArkCLI] = &updateCheckEntry{Current: "1.0.0", CheckedAt: now.Add(-14 * time.Minute).Unix(), Failed: true}
	if err := m.writeUpdateCheckCache(cache); err != nil {
		t.Fatal(err)
	}
	if plan := m.PrepareUpdateCheck(resolution); plan.Refresh {
		t.Fatalf("15-minute failure backoff was ignored: %+v", plan)
	}
	now = now.Add(2 * time.Minute)
	if plan := m.PrepareUpdateCheck(resolution); !plan.Refresh {
		t.Fatalf("expired failure backoff did not refresh: %+v", plan)
	}
}

func TestUpdateCheckCacheAllowsAdditiveFields(t *testing.T) {
	m := testManager(t, t.TempDir(), nil)
	if err := os.MkdirAll(m.ConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"future":true,"tools":{"arkcli":{"current":"1.0.0","latest":"1.0.0","checkedAt":1,"futureEntry":"ok"}}}`)
	if err := os.WriteFile(m.updateCheckPath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	cache := m.readUpdateCheckCache()
	if cache.Tools[ToolArkCLI] == nil || cache.Tools[ToolArkCLI].Current != "1.0.0" {
		t.Fatalf("cache = %+v", cache)
	}
}

func TestUpdateCheckTemporaryPrefixesDoNotOverlapAcrossProxyPaths(t *testing.T) {
	home := t.TempDir()
	defaultManager := testManager(t, home, nil)
	customManager := testManager(t, home, nil)
	customManager.BinDir = filepath.Join(home, "custom-bin")
	defaultPrefix := defaultManager.updateCheckTempPrefix()
	customPrefix := customManager.updateCheckTempPrefix()
	if defaultPrefix == customPrefix || strings.HasPrefix(customPrefix, defaultPrefix) || strings.HasPrefix(defaultPrefix, customPrefix) {
		t.Fatalf("update-check temporary prefixes overlap: default=%q custom=%q", defaultPrefix, customPrefix)
	}
}
