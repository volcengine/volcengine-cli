package release

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCDNRefreshUsesBuiltBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release job runs in bash on Ubuntu")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is unavailable")
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(readReleaseWorkflow(t)), &workflow); err != nil {
		t.Fatal(err)
	}
	var script string
	for _, step := range workflow.Jobs["release"].Steps {
		if step.Name == "Refresh CDN release pointers" {
			script = step.Run
		}
	}
	if script == "" {
		t.Fatal("CDN refresh script is missing")
	}

	for _, test := range []struct {
		name       string
		binary     bool
		version    string
		apiStatus  int
		wantStatus int
		wantCall   bool
	}{
		{"npm unavailable", true, "1.1.7", 0, 0, true},
		{"missing binary", false, "1.1.7", 0, 1, false},
		{"wrong binary version", true, "1.1.6", 0, 1, false},
		{"CDN failure", true, "1.1.7", 23, 23, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			binDirectory := filepath.Join(directory, "bin")
			if err := os.MkdirAll(binDirectory, 0700); err != nil {
				t.Fatal(err)
			}
			npmCalls := filepath.Join(directory, "npm-called")
			apiArgs := filepath.Join(directory, "api-args")
			npmScript := "#!/bin/sh\necho called > \"$VE_TEST_NPM_CALLS\"\necho 'npm error code ETARGET' >&2\nexit 1\n"
			if err := os.WriteFile(filepath.Join(binDirectory, "npm"), []byte(npmScript), 0700); err != nil {
				t.Fatal(err)
			}
			if test.binary {
				binary := filepath.Join(directory, "dist", "volcengine-cli_linux_amd64_v1", "ve")
				if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
					t.Fatal(err)
				}
				binaryScript := `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf '%s\n' "$VE_TEST_BINARY_VERSION"
  exit 0
fi
printf '%s\n' "$@" > "$VE_TEST_API_ARGS"
exit "$VE_TEST_API_STATUS"
`
				if err := os.WriteFile(binary, []byte(binaryScript), 0700); err != nil {
					t.Fatal(err)
				}
			}
			// Generate credentials only for this local fake executable. Do not
			// inherit real cloud credentials or embed credential-shaped fixtures.
			var fixture [32]byte
			if _, err := rand.Read(fixture[:]); err != nil {
				t.Fatal(err)
			}
			t.Setenv("VOLCENGINE_ACCESS_KEY", hex.EncodeToString(fixture[:16]))
			t.Setenv("VOLCENGINE_SECRET_KEY", hex.EncodeToString(fixture[16:]))
			command := exec.Command(bash, "--noprofile", "--norc", "-e", "-o", "pipefail", "-c", script)
			command.Dir = directory
			command.Env = append(os.Environ(),
				"PATH="+binDirectory+string(os.PathListSeparator)+os.Getenv("PATH"),
				"VOLCENGINE_REGION=cn-beijing", "VERSION=1.1.7",
				"VE_TEST_NPM_CALLS="+npmCalls, "VE_TEST_API_ARGS="+apiArgs,
				"VE_TEST_BINARY_VERSION="+test.version,
				"VE_TEST_API_STATUS="+strconv.Itoa(test.apiStatus),
			)
			output, runErr := command.CombinedOutput()
			if command.ProcessState == nil || command.ProcessState.ExitCode() != test.wantStatus {
				t.Fatalf("CDN refresh exit = %v, want %d: %s", runErr, test.wantStatus, output)
			}
			if _, err := os.Stat(npmCalls); !os.IsNotExist(err) {
				t.Fatalf("CDN refresh depended on npm: %v", err)
			}
			args, err := os.ReadFile(apiArgs)
			if !test.wantCall {
				if !os.IsNotExist(err) {
					t.Fatalf("invalid artifact reached CDN call: %q, err = %v", args, err)
				}
				return
			}
			wantArgs := strings.Fields(`cdn SubmitRefreshTask --Type file
--UrlList.1 https://cloudcache.volccdn.com/ve/latest
--UrlList.2 https://cloudcache.volccdn.com/ve/version_manifest.json`)
			gotArgs := strings.Split(strings.TrimSuffix(string(args), "\n"), "\n")
			if err != nil || !reflect.DeepEqual(gotArgs, wantArgs) {
				t.Fatalf("CDN arguments = %q, err = %v; want %q", gotArgs, err, wantArgs)
			}
		})
	}
}
