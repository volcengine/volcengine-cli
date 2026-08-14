package upgrade

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Options 控制 DoUpgrade 行为。
type Options struct {
	// CurrentVersion 当前运行的 CLI 版本（必填）。
	CurrentVersion string
	// TargetVersion 目标版本；空表示升级到 latest。
	TargetVersion string
	// Yes 跳过交互确认。
	Yes bool
	// Stdout / Stderr 面向用户的输出，默认 os.Stdout/Stderr。
	Stdout io.Writer
	Stderr io.Writer
	// Stdin 确认交互输入，默认 os.Stdin。
	Stdin io.Reader
	// SkipSelfCheck 跳过安装后版本自检（测试用）。
	SkipSelfCheck bool
	// ExecPath 覆盖安装目标路径（测试用）；空则使用当前可执行文件。
	ExecPath string
}

// DoUpgrade 按安装来源升级 CLI：
//   - Homebrew：委托 brew update / brew upgrade
//   - npm：委托 npm install -g；失败则返回错误并附带手动安装命令
//   - standalone：从 CDN/GitHub 下载并原地替换当前二进制
func DoUpgrade(opts Options) error {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	stdin := opts.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}

	current := NormalizeVersion(opts.CurrentVersion)
	fmt.Fprintf(stdout, "Current version: %s\n", current)

	detectPath, replacePath, err := resolvePathsForUpgrade(opts.ExecPath)
	if err != nil {
		return err
	}
	info := DetectInstall(detectPath)

	// Homebrew：委托 brew（macOS / Linux）。
	// 不支持 --version 固定版本，避免静默忽略用户意图。
	if info.Method == MethodHomebrew {
		if pin := strings.TrimSpace(opts.TargetVersion); pin != "" {
			return fmt.Errorf(
				"Homebrew installs cannot pin a version via ve upgrade --version %s; use brew instead",
				NormalizeVersion(pin))
		}
		return upgradeViaBrew(stdout, stderr)
	}
	// npm：委托 npm install -g（不下载、不原地替换）。
	// 失败返回 error（exit 1），并附带手动安装命令。
	if info.Method == MethodNPM {
		if err := rejectOlderTarget(current, opts.TargetVersion); err != nil {
			return err
		}
		return upgradeViaNPM(stdout, stderr, opts.TargetVersion)
	}

	explicitTarget := strings.TrimSpace(opts.TargetVersion) != ""
	target := NormalizeVersion(opts.TargetVersion)
	if !explicitTarget {
		fmt.Fprintf(stdout, "Checking for latest version...\n")
		latest, err := ResolveLatestVersion()
		if err != nil {
			return fmt.Errorf("failed to check for updates: %v", err)
		}
		target = latest
	}
	if err := ValidateVersion(target); err != nil {
		return fmt.Errorf("invalid target version: %v", err)
	}
	fmt.Fprintf(stdout, "Target version:  %s\n\n", target)

	if SameVersion(current, target) {
		fmt.Fprintf(stdout, "You are already using version %s.\n", current)
		return nil
	}
	// Never install an older or non-newer target: default path and explicit --version alike.
	if !IsNewer(current, target) {
		if explicitTarget {
			return errTargetNotNewer(current, target)
		}
		fmt.Fprintf(stdout, "Current version %s is newer than the latest available version %s; no upgrade available.\n", current, target)
		return nil
	}

	if !opts.Yes {
		if !confirmUpgrade(stdout, stdin, current, target) {
			fmt.Fprintf(stdout, "Upgrade cancelled.\n")
			return nil
		}
	}

	src, err := ResolveAssetSource(target)
	if err != nil {
		return err
	}
	if src.ArchiveURL == "" {
		return fmt.Errorf("could not resolve download URL for %s; see %s", target, OfficialReleasesURL())
	}

	tmpDir, err := ioutil.TempDir("", "ve-upgrade-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %v", err)
	}
	cleanupTempDir := true
	defer func() {
		if cleanupTempDir {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	archivePath := filepath.Join(tmpDir, src.ArchiveName)
	fmt.Fprintf(stdout, "Downloading %s...\n  From: %s\n", src.ArchiveName, src.ArchiveURL)
	if err := DownloadFile(stdout, src.ArchiveURL, archivePath); err != nil {
		return fmt.Errorf("download failed: %v", err)
	}

	// Integrity: require SHA256SUMS and verify before extract/replace.
	if src.ChecksumURL == "" {
		return fmt.Errorf("checksum URL missing for %s; refusing to install without verification", target)
	}
	fmt.Fprintf(stdout, "Verifying checksum...\n")
	sumBody, err := FetchURLBytes(httpClient, src.ChecksumURL, 1<<20)
	if err != nil {
		return fmt.Errorf("failed to download checksums: %v", err)
	}
	expected, err := ChecksumForArchive(string(sumBody), src.ArchiveName)
	if err != nil {
		return err
	}
	if err := VerifyFileChecksum(archivePath, expected); err != nil {
		return fmt.Errorf("%v\nThe downloaded archive may have been tampered with. "+
			"Please download Volcengine CLI from the official releases page: %s", err, OfficialReleasesURL())
	}

	binaryName := BinaryName()
	extractedPath := filepath.Join(tmpDir, binaryName)
	if err := ExtractBinaryFromZip(archivePath, extractedPath, binaryName); err != nil {
		return fmt.Errorf("extraction failed: %v", err)
	}

	// 原地替换使用已解析的真实路径（若可用）
	execPath := replacePath
	useWindowsHelper := shouldLaunchWindowsUpgradeHelper(runtime.GOOS, opts.ExecPath)
	fmt.Fprintf(stdout, "Installing new version to %s ...\n", execPath)
	if useWindowsHelper {
		if err := launchWindowsUpgradeHelper(WindowsUpgradeLaunchOptions{
			CurrentExecutable: execPath,
			NewBinaryPath:     extractedPath,
			TargetPath:        execPath,
			WorkDir:           tmpDir,
			CurrentVersion:    current,
			ExpectedVersion:   target,
			ExplicitTarget:    explicitTarget,
			SkipSelfCheck:     opts.SkipSelfCheck,
			Stdout:            stdout,
			Stderr:            stderr,
		}); err != nil {
			return err
		}
		// The helper owns this directory after Start succeeds and schedules a
		// cleanup process from the installed binary after it exits.
		cleanupTempDir = false
		fmt.Fprintln(stdout, "The Windows upgrade helper will finish installation after this process exits.")
		return nil
	}

	if opts.SkipSelfCheck {
		if err := ReplaceBinary(extractedPath, execPath); err != nil {
			return fmt.Errorf("installation failed: %v", err)
		}
	} else {
		if err := ReplaceBinaryWithBackup(extractedPath, execPath, target); err != nil {
			return fmt.Errorf("installation failed: %v", err)
		}
	}

	// Refresh version-check cache after install:
	// - default upgrade-to-latest: latest == target, avoid immediate re-prompt
	// - pin (--version): re-resolve real latest so we do not poison the
	//   24h cache with a pinned "latest" (which would suppress real update notices)
	refreshVersionCacheAfterInstall(explicitTarget, target)

	fmt.Fprintf(stdout, "\nSuccessfully upgraded Volcengine CLI from %s to %s!\n", current, target)
	return nil
}

// rejectOlderTarget validates an optional pin for managed upgrade paths.
// Empty pin means "latest" and is allowed. Non-empty pins must pass ValidateVersion
// and be the same as or newer than current; older and incomparable pins are rejected.
// (Same-version pins are allowed so package managers can reinstall/refresh.)
func rejectOlderTarget(current, pin string) error {
	pin = strings.TrimSpace(pin)
	if pin == "" {
		return nil
	}
	if err := ValidateVersion(pin); err != nil {
		return fmt.Errorf("invalid target version: %v", err)
	}
	target := NormalizeVersion(pin)
	if SameVersion(current, target) || IsNewer(current, target) {
		return nil
	}
	return errTargetNotNewer(current, target)
}

// errTargetNotNewer explains why a pin/target cannot be installed.
// When both sides are semver and target is strictly older, the message says
// "older"; otherwise it says the target is "not newer" (covers opaque/incomparable tags).
func errTargetNotNewer(current, target string) error {
	current = NormalizeVersion(current)
	target = NormalizeVersion(target)
	releases := OfficialReleasesURL()
	if isStrictlyOlder(current, target) {
		return fmt.Errorf(
			"refusing to install older version %s (current is %s); reinstall from %s if you need a previous release",
			target, current, releases)
	}
	return fmt.Errorf(
		"refusing to install version %s (current is %s): target must be newer than the running binary; reinstall from %s if you need a previous release",
		target, current, releases)
}

func refreshVersionCacheAfterInstall(pinned bool, installed string) {
	installed = NormalizeVersion(installed)
	if !pinned {
		_ = SaveCheckCache(installed, installed)
		return
	}
	// Best-effort: discover the true latest. On failure, drop the cache so the
	// next command rechecks instead of treating the pinned version as latest.
	if realLatest, err := ResolveLatestVersionQuick(); err == nil && realLatest != "" {
		_ = SaveCheckCache(realLatest, installed)
		return
	}
	InvalidateCheckCache()
}

func confirmUpgrade(w io.Writer, r io.Reader, current, target string) bool {
	fmt.Fprintf(w, "Upgrade from %s to %s? (y/N): ", current, target)
	reader := bufio.NewReader(r)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes"
}
