package upgrade

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

// Options controls DoUpgrade behavior.
type Options struct {
	// CurrentVersion is the running CLI version (required).
	CurrentVersion string
	// TargetVersion empty means "latest".
	TargetVersion string
	// Yes skips the interactive confirmation prompt.
	Yes bool
	// Stdout / Stderr for user-facing messages. Defaults to os.Stdout/Stderr.
	Stdout io.Writer
	Stderr io.Writer
	// Stdin for confirmation. Defaults to os.Stdin.
	Stdin io.Reader
	// SkipSelfCheck disables post-install version self-check (tests).
	SkipSelfCheck bool
	// ExecPath overrides the install destination (tests). Empty = current executable.
	ExecPath string
}

// DoUpgrade downloads, verifies, and installs the target CLI version.
func DoUpgrade(opts Options) error {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stdin := opts.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}

	current := NormalizeVersion(opts.CurrentVersion)
	fmt.Fprintf(stdout, "Current version: %s\n", current)

	target := NormalizeVersion(opts.TargetVersion)
	if target == "" {
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
	defer os.RemoveAll(tmpDir)

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

	execPath := opts.ExecPath
	if execPath == "" {
		execPath, err = ResolveExecPath()
		if err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "Installing new version to %s ...\n", execPath)

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
	// - pin/downgrade (--version): re-resolve real latest so we do not poison the
	//   24h cache with an older "latest" (which would suppress real update notices)
	refreshVersionCacheAfterInstall(opts.TargetVersion != "", target)

	fmt.Fprintf(stdout, "\nSuccessfully upgraded Volcengine CLI from %s to %s!\n", current, target)
	return nil
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
