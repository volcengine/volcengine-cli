package upgrade

import (
	"strings"
	"testing"
)

func TestNormalizeInstallPath(t *testing.T) {
	got := normalizeInstallPath(`C:\Users\x\node_modules\@volcengine\cli\bin\ve.exe`)
	if !strings.Contains(got, "/node_modules/@volcengine/cli/") {
		t.Fatalf("normalize: %q", got)
	}
	if got != strings.ToLower(got) {
		t.Fatalf("expected lower case: %q", got)
	}
}

func TestDetectInstall_PathTable(t *testing.T) {
	// 隔离 env，只测路径启发式
	origEnv := installLookupEnv
	defer func() { installLookupEnv = origEnv }()
	installLookupEnv = func(string) string { return "" }

	tests := []struct {
		path string
		want Method
	}{
		// Homebrew macOS 常见布局
		{"/opt/homebrew/Cellar/volcengine-cli/1.0.49/bin/ve", MethodHomebrew},
		{"/opt/homebrew/opt/volcengine-cli/bin/ve", MethodHomebrew},
		{"/usr/local/Cellar/volcengine-cli/1.0.49/bin/ve", MethodHomebrew},
		{"/usr/local/homebrew/bin/ve", MethodHomebrew},
		// Linux 上 Homebrew / Linuxbrew
		{"/home/linuxbrew/.linuxbrew/Cellar/volcengine-cli/1.0.49/bin/ve", MethodHomebrew},
		{"/home/linuxbrew/.linuxbrew/bin/ve", MethodHomebrew},
		// 同时含 linuxbrew 与 homebrew 字样时仍判为 brew 族
		{"/linuxbrew/prefix/homebrew/name/ve", MethodHomebrew},
		// npm
		{"/usr/local/lib/node_modules/@volcengine/cli/bin/ve", MethodNPM},
		{`C:\Users\me\AppData\Roaming\npm\node_modules\@volcengine\cli\bin\ve.exe`, MethodNPM},
		// standalone（含易误判的否定用例）
		{"/usr/local/bin/ve", MethodStandalone},
		{"/tmp/ve", MethodStandalone},
		{`C:\tools\ve.exe`, MethodStandalone},
		{"/opt/not-homebrew/bin/ve", MethodStandalone},
		{"/home/me/src/homebrew-tools/bin/ve", MethodStandalone},
		{"/tmp/homebrew-mirror/ve", MethodStandalone},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			info := detectInstall(tt.path)
			if info.Method != tt.want {
				t.Fatalf("path %q: got %q want %q (detectedBy=%s)", tt.path, info.Method, tt.want, info.DetectedBy)
			}
			if info.ExecPath != tt.path {
				t.Fatalf("ExecPath: got %q", info.ExecPath)
			}
		})
	}
}

func TestPathHasSegment(t *testing.T) {
	if !pathHasSegment("/opt/homebrew/bin/ve", "homebrew") {
		t.Fatal("expected homebrew segment")
	}
	if pathHasSegment("/opt/not-homebrew/bin/ve", "homebrew") {
		t.Fatal("not-homebrew must not match homebrew segment")
	}
	if !pathHasSegment("/usr/local/cellar/volcengine-cli/1/bin/ve", "cellar") {
		t.Fatal("expected cellar segment")
	}
}

func TestDetectInstall_EnvOverridesPath(t *testing.T) {
	origEnv := installLookupEnv
	defer func() { installLookupEnv = origEnv }()
	installLookupEnv = func(k string) string {
		if k == EnvInstallMethod {
			return "npm"
		}
		return ""
	}
	// Path looks standalone, env forces npm
	info := detectInstall("/tmp/ve")
	if info.Method != MethodNPM || info.DetectedBy != DetectedByEnv {
		t.Fatalf("got %+v", info)
	}
}

func TestDetectInstall_InvalidEnvFallsThrough(t *testing.T) {
	origEnv := installLookupEnv
	defer func() { installLookupEnv = origEnv }()
	installLookupEnv = func(k string) string {
		if k == EnvInstallMethod {
			return "not-a-real-method"
		}
		return ""
	}

	info := detectInstall("/usr/local/lib/node_modules/@volcengine/cli/bin/ve")
	if info.Method != MethodNPM || info.DetectedBy != DetectedByPath {
		t.Fatalf("got %+v", info)
	}
}

func TestDetectInstall_EnvWinsOverPath(t *testing.T) {
	origEnv := installLookupEnv
	defer func() { installLookupEnv = origEnv }()
	installLookupEnv = func(k string) string {
		if k == EnvInstallMethod {
			return "standalone"
		}
		return ""
	}
	// Path looks like npm, env forces standalone
	info := detectInstall("/usr/local/lib/node_modules/@volcengine/cli/bin/ve")
	if info.Method != MethodStandalone || info.DetectedBy != DetectedByEnv {
		t.Fatalf("got %+v", info)
	}
}

func TestDetectInstall_LinuxbrewEnvAlias(t *testing.T) {
	origEnv := installLookupEnv
	defer func() { installLookupEnv = origEnv }()
	installLookupEnv = func(k string) string {
		if k == EnvInstallMethod {
			return "linuxbrew"
		}
		return ""
	}
	info := detectInstall("/tmp/ve")
	if info.Method != MethodHomebrew || info.DetectedBy != DetectedByEnv {
		t.Fatalf("got %+v", info)
	}
}

func TestNPMUpgradeCommand(t *testing.T) {
	if got := NPMUpgradeCommand(""); got != "npm install -g @volcengine/cli@latest" {
		t.Fatalf("latest: %q", got)
	}
	if got := NPMUpgradeCommand("1.0.49"); got != "npm install -g @volcengine/cli@1.0.49" {
		t.Fatalf("pin: %q", got)
	}
}

func TestFormatManagedInstallMessage_NPM(t *testing.T) {
	info := npmInfo("/path/to/node_modules/@volcengine/cli/bin/ve")
	msg := FormatManagedInstallMessage(info, "")
	for _, want := range []string{"npm", "npm install -g @volcengine/cli@latest", "/path/to/"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("missing %q in:\n%s", want, msg)
		}
	}
	assertNoUpgradeForceGuidance(t, msg)
	msgPin := FormatManagedInstallMessage(info, "1.0.50")
	if !strings.Contains(msgPin, "npm install -g @volcengine/cli@1.0.50") {
		t.Fatalf("pin msg: %s", msgPin)
	}
	assertNoUpgradeForceGuidance(t, msgPin)
}

func TestFormatManagedInstallMessage_Homebrew(t *testing.T) {
	info := homebrewInfo("/opt/homebrew/bin/ve")
	msg := FormatManagedInstallMessage(info, "1.0.50")
	for _, want := range []string{"Homebrew", "brew upgrade", HomebrewFormula, "/opt/homebrew/bin/ve"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("missing %q in:\n%s", want, msg)
		}
	}
	// pinVersion only rewrites npm commands; brew guidance stays unpinned.
	if strings.Contains(msg, "1.0.50") {
		t.Fatalf("homebrew message should ignore pinVersion:\n%s", msg)
	}
	assertNoUpgradeForceGuidance(t, msg)
}

// assertNoUpgradeForceGuidance ensures managed-install UX never points users at
// the removed "ve upgrade --force" escape hatch.
func assertNoUpgradeForceGuidance(t *testing.T, msg string) {
	t.Helper()
	if strings.Contains(msg, "--force") || strings.Contains(msg, "ve upgrade --force") {
		t.Fatalf("message should not mention --force / ve upgrade --force:\n%s", msg)
	}
}
