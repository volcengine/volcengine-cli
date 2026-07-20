package upgrade

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	// EnvInstallMethod 覆盖安装来源识别（测试或排障用）。
	// 取值：standalone、npm、homebrew（大小写不敏感）；linuxbrew 视为 homebrew。
	EnvInstallMethod = "VOLCENGINE_CLI_INSTALL_METHOD"

	// HomebrewFormula Homebrew 公式名，委托 brew 升级时使用。
	HomebrewFormula = "volcengine-cli"

	// NPMPackage npm 包名，生成升级提示命令时使用。
	NPMPackage = "@volcengine/cli"
)

// Method 表示当前 ve 二进制的安装来源。
type Method string

const (
	// MethodStandalone 独立安装：Release 解压、源码编译等，允许原地替换。
	MethodStandalone Method = "standalone"
	// MethodNPM 通过 npm 全局包安装。
	MethodNPM Method = "npm"
	// MethodHomebrew 通过 Homebrew / Linuxbrew 安装（macOS 与 Linux）。
	MethodHomebrew Method = "homebrew"
)

// DetectedBy 记录最终采用了哪一类识别信号。
type DetectedBy string

const (
	DetectedByEnv     DetectedBy = "env"     // 环境变量覆盖
	DetectedByPath    DetectedBy = "path"    // 可执行路径启发式
	DetectedByDefault DetectedBy = "default" // 默认 standalone
)

// InstallInfo 安装来源识别结果，供升级策略与后台提示使用。
type InstallInfo struct {
	Method      Method     // 安装来源
	ExecPath    string     // 参与识别的可执行路径
	DetectedBy  DetectedBy // 命中的识别信号
	DisplayName string     // 展示用名称（如 npm、Homebrew）
	UpgradeCmd  string     // 建议的升级命令（一行）
}

// Managed 表示是否由包管理器托管（非 standalone 即视为托管）。
func (i InstallInfo) Managed() bool {
	return i.Method != MethodStandalone && i.Method != ""
}

// Detector 单一安装来源的匹配器；defaultDetectors 中按顺序匹配，先命中先生效。
type Detector interface {
	Method() Method
	Match(normalizedPath string) bool // normalizedPath 已规范化（斜杠 + 小写）
	Info(execPath string) InstallInfo
}

// DetectContext 识别过程中的上下文（环境变量等）。
type DetectContext struct {
	ExecPath  string
	LookupEnv func(string) string
}

var (
	// detectInstallFunc 非空时覆盖 DetectInstall，仅测试使用。
	detectInstallFunc func(string) InstallInfo
	// installLookupEnv 读取环境变量，测试可替换。
	installLookupEnv = os.Getenv
)

// DetectInstall 根据可执行文件路径判断安装来源。
// 无法识别时返回 standalone，不返回 error。
func DetectInstall(execPath string) InstallInfo {
	if detectInstallFunc != nil {
		return detectInstallFunc(execPath)
	}
	return detectInstall(execPath)
}

func detectInstall(execPath string) InstallInfo {
	ctx := DetectContext{
		ExecPath:  execPath,
		LookupEnv: installLookupEnv,
	}
	if ctx.LookupEnv == nil {
		ctx.LookupEnv = os.Getenv
	}

	// 优先级 1：环境变量强制指定来源
	if v := strings.ToLower(strings.TrimSpace(ctx.LookupEnv(EnvInstallMethod))); v != "" {
		if info, ok := infoForMethod(Method(v), execPath, DetectedByEnv); ok {
			return info
		}
	}
	// 优先级 2：路径启发式（按 defaultDetectors 顺序）
	norm := normalizeInstallPath(execPath)
	for _, d := range defaultDetectors {
		if d.Match(norm) {
			info := d.Info(execPath)
			info.DetectedBy = DetectedByPath
			return info
		}
	}
	// 优先级 3：默认独立安装
	return standaloneInfo(execPath, DetectedByDefault)
}

// infoForMethod 将方法名解析为 InstallInfo；无法识别的方法名返回 false。
func infoForMethod(m Method, execPath string, by DetectedBy) (InstallInfo, bool) {
	switch m {
	case MethodStandalone:
		info := standaloneInfo(execPath, by)
		return info, true
	case MethodNPM:
		info := npmInfo(execPath)
		info.DetectedBy = by
		return info, true
	case MethodHomebrew, Method("linuxbrew"):
		// linuxbrew 与 homebrew 共用 brew 升级路径
		info := homebrewInfo(execPath)
		info.DetectedBy = by
		return info, true
	default:
		return InstallInfo{}, false
	}
}

func standaloneInfo(execPath string, by DetectedBy) InstallInfo {
	return InstallInfo{
		Method:      MethodStandalone,
		ExecPath:    execPath,
		DetectedBy:  by,
		DisplayName: "standalone",
		UpgradeCmd:  "ve upgrade",
	}
}

func npmInfo(execPath string) InstallInfo {
	return InstallInfo{
		Method:      MethodNPM,
		ExecPath:    execPath,
		DisplayName: "npm",
		UpgradeCmd:  NPMUpgradeCommand(""),
	}
}

func homebrewInfo(execPath string) InstallInfo {
	return InstallInfo{
		Method:      MethodHomebrew,
		ExecPath:    execPath,
		DisplayName: "Homebrew",
		UpgradeCmd:  HomebrewUpgradeCommand(),
	}
}

// NPMUpgradeCommand 生成推荐的 npm 升级命令；pinVersion 非空则固定该版本。
func NPMUpgradeCommand(pinVersion string) string {
	pinVersion = NormalizeVersion(pinVersion)
	if pinVersion == "" {
		return "npm install -g " + NPMPackage + "@latest"
	}
	return "npm install -g " + NPMPackage + "@" + pinVersion
}

// HomebrewUpgradeCommand 生成推荐的 brew 升级命令。
func HomebrewUpgradeCommand() string {
	return "brew upgrade " + HomebrewFormula
}

// FormatManagedInstallMessage 生成托管安装（如 npm）禁止默认原地升级时的说明文案。
// pinVersion 仅对 npm 生效，用于在提示中固定包版本。
func FormatManagedInstallMessage(info InstallInfo, pinVersion string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "This ve binary appears to be installed via %s\n", info.DisplayName)
	if strings.TrimSpace(info.ExecPath) != "" {
		fmt.Fprintf(&b, "  (path: %s).\n\n", info.ExecPath)
	} else {
		b.WriteString(".\n\n")
	}
	b.WriteString("In-place upgrade is disabled so the package manager stays the source of truth.\n\n")
	b.WriteString("To upgrade, run:\n")
	cmd := info.UpgradeCmd
	if info.Method == MethodNPM {
		cmd = NPMUpgradeCommand(pinVersion)
	}
	fmt.Fprintf(&b, "  %s\n", cmd)
	return b.String()
}

// upgradeViaBrew 将升级委托给 Homebrew：先 brew update，再 brew upgrade <公式名>。
// 适用于 macOS 与 Linux 上的 Homebrew / Linuxbrew（二者均提供 brew 命令）。
func upgradeViaBrew(stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	fmt.Fprint(stdout, "Detected Homebrew installation, delegating to brew...\n\n")

	fmt.Fprintln(stdout, "==> brew update")
	update := execCommand("brew", "update")
	update.Stdout = stdout
	update.Stderr = stderr
	if err := update.Run(); err != nil {
		return fmt.Errorf("brew update failed: %v", err)
	}

	fmt.Fprintf(stdout, "\n==> brew upgrade %s\n", HomebrewFormula)
	upgrade := execCommand("brew", "upgrade", HomebrewFormula)
	upgrade.Stdout = stdout
	upgrade.Stderr = stderr
	if err := upgrade.Run(); err != nil {
		return fmt.Errorf("brew upgrade failed: %v", err)
	}

	fmt.Fprintln(stdout, "\nHomebrew upgrade complete!")
	return nil
}

// DetectInstallFromRunningBinary 识别当前正在运行的 ve 的安装来源。
// 解析失败时回退为 standalone，供后台升级提示尽力而为。
func DetectInstallFromRunningBinary() InstallInfo {
	detectPath, _, err := resolvePathsForUpgrade("")
	if err != nil || strings.TrimSpace(detectPath) == "" {
		return standaloneInfo("", DetectedByDefault)
	}
	return DetectInstall(detectPath)
}

// resolvePathsForUpgrade 返回 (detectPath, replacePath, err)。
//
//   - detectPath：用于安装来源识别；EvalSymlinks 失败时保留原始路径，避免影响 brew 委托判断。
//   - replacePath：用于原地替换；能解析符号链接时尽量使用真实路径。
//   - execPathOverride 非空时（测试），两者均使用覆盖路径。
func resolvePathsForUpgrade(execPathOverride string) (detectPath, replacePath string, err error) {
	if strings.TrimSpace(execPathOverride) != "" {
		return execPathOverride, execPathOverride, nil
	}
	raw, err := osExecutable()
	if err != nil {
		return "", "", fmt.Errorf("failed to get executable path: %v", err)
	}
	detectPath = raw
	replacePath = raw
	if resolved, e := evalSymlinks(raw); e == nil && strings.TrimSpace(resolved) != "" {
		detectPath = resolved
		replacePath = resolved
	}
	return detectPath, replacePath, nil
}

// withPinnedNPMUpgradeCmd 返回副本，并将 npm 的 UpgradeCmd 按 pinVersion 固定版本。
func withPinnedNPMUpgradeCmd(info InstallInfo, pinVersion string) InstallInfo {
	if info.Method != MethodNPM {
		return info
	}
	info.UpgradeCmd = NPMUpgradeCommand(pinVersion)
	return info
}
