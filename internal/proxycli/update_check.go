package proxycli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/volcengine/volcengine-cli/upgrade"
)

const updateCheckLockTimeout = 5 * time.Second

type UpdateCheckResult struct {
	Tool      Tool
	Current   string
	Latest    string
	HasUpdate bool
}

type UpdateCheckPlan struct {
	Refresh   bool
	HasNotice bool
	Notice    UpdateCheckResult
}

type updateCheckCache struct {
	Tools map[Tool]*updateCheckEntry `json:"tools"`
}

type updateCheckEntry struct {
	Current        string `json:"current"`
	Latest         string `json:"latest,omitempty"`
	CheckedAt      int64  `json:"checkedAt"`
	Failed         bool   `json:"failed,omitempty"`
	NoticedAt      string `json:"noticedAt,omitempty"`
	NoticedCurrent string `json:"noticedCurrent,omitempty"`
}

// PrepareUpdateCheck performs only local work before process delegation. A
// stale cache is synchronously marked with a failure-backoff lease so an exec
// of a short-lived child cannot cause every invocation to launch another CDN
// request. The caller starts an independent helper when Refresh is true.
func (m *Manager) PrepareUpdateCheck(resolution Resolution) UpdateCheckPlan {
	if upgrade.UpdateCheckDisabled() || resolution.Source != SourceManaged || strings.TrimSpace(resolution.Version) == "" {
		return UpdateCheckPlan{}
	}
	lock, err := acquireFileLock(m.updateCheckPath()+".lock", updateCheckLockTimeout)
	if err != nil {
		return UpdateCheckPlan{}
	}
	defer lock.release()
	removeFilesWithPrefix(m.ConfigDir, m.updateCheckTempPrefix())

	now := m.currentTime()
	cache := m.readUpdateCheckCache()
	entry := cache.Tools[resolution.Tool]
	if isFreshProxyCheck(entry, resolution.Version, now) {
		plan := claimProxyUpdateNotice(resolution, entry, now)
		if plan.HasNotice {
			if err := m.writeUpdateCheckCache(cache); err != nil {
				return UpdateCheckPlan{}
			}
		}
		return plan
	}

	lease := &updateCheckEntry{
		Current:   resolution.Version,
		CheckedAt: now.Unix(),
		Failed:    true,
	}
	if entry != nil {
		lease.Latest = entry.Latest
		lease.NoticedAt = entry.NoticedAt
		lease.NoticedCurrent = entry.NoticedCurrent
	}
	cache.Tools[resolution.Tool] = lease
	plan := claimProxyUpdateNotice(resolution, lease, now)
	plan.Refresh = true
	if err := m.writeUpdateCheckCache(cache); err != nil {
		// The check is advisory. If the lease cannot be persisted, do not launch
		// a worker that would be repeated by every short-lived invocation.
		return UpdateCheckPlan{}
	}
	return plan
}

func claimProxyUpdateNotice(resolution Resolution, entry *updateCheckEntry, now time.Time) UpdateCheckPlan {
	if entry == nil || entry.Failed || !upgrade.IsNewer(resolution.Version, entry.Latest) {
		return UpdateCheckPlan{}
	}
	date := now.Format("2006-01-02")
	if entry.NoticedAt == date && entry.NoticedCurrent == resolution.Version {
		return UpdateCheckPlan{}
	}
	entry.NoticedAt = date
	entry.NoticedCurrent = resolution.Version
	return UpdateCheckPlan{
		HasNotice: true,
		Notice: UpdateCheckResult{
			Tool: resolution.Tool, Current: resolution.Version, Latest: entry.Latest, HasUpdate: true,
		},
	}
}

// RefreshUpdateCheck is run by the detached helper. It performs the remote
// probe and replaces the synchronous lease with a success or failure cache
// entry while preserving the daily notice throttle.
func (m *Manager) RefreshUpdateCheck(tool Tool, current string) error {
	latest, checkErr := m.fetchLatestProxyVersion(tool, current)
	lock, err := acquireFileLock(m.updateCheckPath()+".lock", updateCheckLockTimeout)
	if err != nil {
		return err
	}
	defer lock.release()
	removeFilesWithPrefix(m.ConfigDir, m.updateCheckTempPrefix())
	cache := m.readUpdateCheckCache()
	previous := cache.Tools[tool]
	entry := &updateCheckEntry{
		Current:   current,
		Latest:    latest,
		CheckedAt: m.currentTime().Unix(),
		Failed:    checkErr != nil,
	}
	if previous != nil {
		entry.NoticedAt = previous.NoticedAt
		entry.NoticedCurrent = previous.NoticedCurrent
		if checkErr != nil {
			entry.Latest = previous.Latest
		}
	}
	cache.Tools[tool] = entry
	if err := m.writeUpdateCheckCache(cache); err != nil {
		return err
	}
	return checkErr
}

func isFreshProxyCheck(entry *updateCheckEntry, current string, now time.Time) bool {
	if entry == nil || entry.Current != current || entry.CheckedAt <= 0 {
		return false
	}
	ttl := upgrade.UpdateCheckTTL()
	if entry.Failed {
		ttl = upgrade.UpdateCheckFailureBackoff()
	}
	age := now.Sub(time.Unix(entry.CheckedAt, 0))
	return age >= 0 && age < ttl
}

func (m *Manager) fetchLatestProxyVersion(tool Tool, current string) (string, error) {
	client := m.CheckHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 1500 * time.Millisecond}
	}
	data, err := m.downloadWithClient(m.ManifestURL, maxManifestBytes, client)
	if err != nil {
		return "", err
	}
	manifest, err := parseManifest(data)
	if err != nil {
		return "", err
	}
	release, ok := manifest.Tools[tool]
	if !ok {
		return "", fmt.Errorf("proxy CLI manifest has no entry for %s", tool)
	}
	if _, supported := release.Platforms[platformKey(m.GOOS, m.GOARCH)]; !supported {
		return current, nil
	}
	return upgrade.NormalizeVersion(release.Version), nil
}

func (m *Manager) updateCheckPath() string {
	stateBase := filepath.Base(m.statePath())
	if stateBase == StateFileName {
		return filepath.Join(m.ConfigDir, "version-check.json")
	}
	name := strings.TrimSuffix(stateBase, filepath.Ext(stateBase))
	return filepath.Join(m.ConfigDir, strings.Replace(name, "install-state", "version-check", 1)+".json")
}

func (m *Manager) currentTime() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) readUpdateCheckCache() updateCheckCache {
	empty := updateCheckCache{Tools: map[Tool]*updateCheckEntry{}}
	data, err := os.ReadFile(m.updateCheckPath())
	if err != nil {
		return empty
	}
	var cache updateCheckCache
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&cache); err != nil {
		return empty
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return empty
	}
	if cache.Tools == nil {
		return empty
	}
	return cache
}

func (m *Manager) writeUpdateCheckCache(cache updateCheckCache) error {
	if cache.Tools == nil {
		cache.Tools = map[Tool]*updateCheckEntry{}
	}
	if err := os.MkdirAll(m.ConfigDir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(m.ConfigDir, m.updateCheckTempPrefix()+"*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(m.updateCheckPath())
	}
	return os.Rename(temporaryPath, m.updateCheckPath())
}

func (m *Manager) updateCheckTempPrefix() string {
	return uniqueTempPrefix("version-check", m.updateCheckPath())
}
