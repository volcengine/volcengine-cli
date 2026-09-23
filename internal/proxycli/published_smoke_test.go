package proxycli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Opt-in integration validation against the exact first-party archives already
// downloaded and hashed during release research. No cloud business calls.
func TestPublishedProductsSmoke(t *testing.T) {
	cacheFile := os.Getenv("VE_PROXY_SMOKE_ASSET_CACHE")
	if cacheFile == "" {
		t.Skip("set VE_PROXY_SMOKE_ASSET_CACHE to a URL -> verified local archive map")
	}
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatal(err)
	}
	cache := map[string]string{}
	if err := json.Unmarshal(data, &cache); err != nil {
		t.Fatal(err)
	}
	for _, spec := range EmbeddedSnapshot().Catalog().Tools {
		if spec.Readiness != "ready" || spec.ID == ToolArkCLI || spec.ID == ToolTOSUtil {
			continue
		}
		t.Run(string(spec.ID), func(t *testing.T) {
			home := t.TempDir()
			m, err := NewManagerWithCatalog(EmbeddedSnapshot())
			if err != nil {
				t.Fatal(err)
			}
			m.HomeDir = home
			m.ConfigDir = filepath.Join(home, ".volcengine", "proxy-cli")
			m.BinDir = defaultBinDir(home)
			m.LookPath = func(name string) (string, error) {
				if name == "node" || strings.HasPrefix(name, "python") {
					return exec.LookPath(name)
				}
				return "", os.ErrNotExist
			}
			execute := true
			if requested := os.Getenv("VE_PROXY_SMOKE_PLATFORM"); requested != "" {
				parts := strings.Split(requested, "-")
				if len(parts) != 2 {
					t.Fatal("invalid smoke platform")
				}
				m.GOOS, m.GOARCH = parts[0], parts[1]
				execute = m.GOOS == runtime.GOOS && m.GOARCH == runtime.GOARCH
			}
			if _, ok := spec.Platforms[platformKey(m.GOOS, m.GOARCH)]; !ok {
				if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
					if _, ok := spec.Platforms["darwin-amd64"]; ok {
						m.GOARCH = "amd64"
						t.Log("executing audited amd64 artifact through local Rosetta")
					}
				}
				if _, ok := spec.Platforms[platformKey(m.GOOS, m.GOARCH)]; !ok {
					m.GOOS = "linux"
					m.GOARCH = "amd64"
					execute = false
				}
			}
			downloads := 0
			m.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				path, ok := cache[r.URL.String()]
				if !ok {
					return nil, fmt.Errorf("missing cached published artifact %s", r.URL)
				}
				f, err := os.Open(path)
				if err != nil {
					return nil, err
				}
				st, err := f.Stat()
				if err != nil {
					f.Close()
					return nil, err
				}
				downloads++
				return &http.Response{StatusCode: 200, Body: f, ContentLength: st.Size(), Header: http.Header{}}, nil
			})}
			resolution, err := m.Resolve(spec.ID)
			if err != nil {
				t.Fatal(err)
			}
			if resolution.Source != SourceCatalog || downloads != 1 {
				t.Fatalf("not a first installation: %+v downloads=%d", resolution, downloads)
			}
			again, err := m.Resolve(spec.ID)
			if err != nil || again.Source != SourceManaged || downloads != 1 {
				t.Fatalf("managed reuse failed: %+v %v downloads=%d", again, err, downloads)
			}
			if !execute {
				for _, p := range strings.Split(resolution.Environment["LD_LIBRARY_PATH"], string(os.PathListSeparator)) {
					if p != "" {
						if _, err := os.Stat(p); err != nil {
							t.Fatal(err)
						}
					}
				}
				t.Logf("verified ZIP extraction and flat native installation for %s/%s", m.GOOS, m.GOARCH)
				if binary := os.Getenv("VE_PROXY_SMOKE_LINUX_HOST_BINARY"); binary != "" {
					runPublishedLinuxHost(t, home, binary, spec.Command)
				} else {
					t.Log("native execution requires its declared OS")
				}
				return
			}
			args := []string{"--help"}
			if spec.ID == "viking-cli" {
				args = []string{"version"}
			}
			if spec.ID == "openviking-cli" {
				runPublishedHelp(t, home, resolution, []string{"language", "en"})
			}
			runPublishedHelp(t, home, resolution, args)
			result, err := m.Upgrade([]Tool{spec.ID})
			if err != nil || len(result.Items) != 1 || result.Items[0].Status != StatusCurrent || downloads != 1 {
				t.Fatalf("explicit upgrade/reuse: %+v %v downloads=%d", result, err, downloads)
			}
		})
	}
}
func runPublishedHelp(t *testing.T, home string, r Resolution, args []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, r.Path, append(append([]string{}, r.ArgsPrefix...), args...)...)
	child.Env = []string{"HOME=" + home, "USERPROFILE=" + home, "PATH=" + os.Getenv("PATH"), "LANG=en_US.UTF-8", "NO_COLOR=1", "VOLCENGINE_CLI_DISABLE_UPDATE_CHECK=1"}
	for key, value := range r.Environment {
		for i, entry := range child.Env {
			if strings.HasPrefix(entry, key+"=") {
				child.Env[i] = key + "=" + value
				goto next
			}
		}
		child.Env = append(child.Env, key+"="+value)
	next:
	}
	child.Stdin = strings.NewReader("")
	output, err := child.CombinedOutput()
	if err != nil {
		if len(output) > 4000 {
			output = output[:4000]
		}
		t.Fatalf("published %s %v: %v\n%s", r.Tool, args, err, output)
	}
	if len(output) == 0 {
		t.Fatal("empty help/version output")
	}
	t.Logf("%s %s: exit 0 (%d output bytes)", r.Tool, strings.Join(args, " "), len(output))
}

func runPublishedLinuxHost(t *testing.T, home, binary, command string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, "docker", "run", "--rm", "--platform=linux/amd64", "--network=none", "--read-only", "--tmpfs", "/tmp", "--mount", "type=bind,src="+home+",dst="+home, "--mount", "type=bind,src="+binary+",dst=/ve,readonly", "-e", "HOME="+home, "-e", "VOLCENGINE_CLI_DISABLE_UPDATE_CHECK=1", "ubuntu@sha256:b8b6ee6aa931ecd9d0d952abc34dc0e5f7c6a30c6bb71b079fe399fde0329c02", "/ve", command, "--help")
	output, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("Linux host forwarding failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "Usage:") {
		t.Fatalf("unexpected Linux help: %s", output)
	}
	t.Logf("Linux ve %s --help: exit 0 in isolated Ubuntu container", command)
}
