package upgrade

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"archive/zip"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// BinaryName returns the CLI binary name for the current OS.
func BinaryName() string {
	if runtime.GOOS == "windows" {
		return "ve.exe"
	}
	return "ve"
}

// ArchiveName builds the release zip name for version/os/arch.
// Matches goreleaser / npm: volcengine-cli_{version}_{os}_{arch}.zip
func ArchiveName(version, goos, goarch string) string {
	version = NormalizeVersion(version)
	return fmt.Sprintf("volcengine-cli_%s_%s_%s.zip", version, goos, goarch)
}

// ChecksumName builds the SHA256SUMS asset name for a version.
func ChecksumName(version string) string {
	version = NormalizeVersion(version)
	return fmt.Sprintf("volcengine-cli_%s_SHA256SUMS", version)
}

// ExtractBinaryFromZip finds binaryName in the zip and writes it to destPath with 0755.
// Writes via a same-directory temp file then renames, so a failed extract never leaves a
// truncated destPath.
func ExtractBinaryFromZip(archivePath, destPath, binaryName string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if filepath.Base(f.Name) != binaryName {
			continue
		}
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}

		tmpDir := filepath.Dir(destPath)
		if tmpDir == "" {
			tmpDir = "."
		}
		tmpFile, err := ioutil.TempFile(tmpDir, ".ve-extract-*")
		if err != nil {
			rc.Close()
			return err
		}
		tmpPath := tmpFile.Name()

		_, copyErr := io.Copy(tmpFile, rc)
		rc.Close()
		closeErr := tmpFile.Close()
		if copyErr != nil {
			_ = os.Remove(tmpPath)
			return copyErr
		}
		if closeErr != nil {
			_ = os.Remove(tmpPath)
			return closeErr
		}
		// Ensure executable bit before publish (TempFile is typically 0600).
		if err := os.Chmod(tmpPath, 0755); err != nil {
			_ = os.Remove(tmpPath)
			return err
		}
		// Replace dest atomically when possible.
		_ = os.Remove(destPath)
		if err := os.Rename(tmpPath, destPath); err != nil {
			_ = os.Remove(tmpPath)
			return err
		}
		return nil
	}
	return fmt.Errorf("binary %q not found in archive %s", binaryName, filepath.Base(archivePath))
}

// IsZipPath reports whether path looks like a zip archive.
func IsZipPath(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".zip")
}
