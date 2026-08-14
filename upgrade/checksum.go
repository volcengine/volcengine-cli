package upgrade

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ParseChecksumEntries parses a SHA256SUMS file (goreleaser / npm format).
// Each non-empty line: "<64-hex>  filename" or "<64-hex> *filename".
func ParseChecksumEntries(content string) (map[string]string, error) {
	out := make(map[string]string)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// split on first whitespace run
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("invalid checksum line %d", i+1)
		}
		hash := strings.ToLower(fields[0])
		if len(hash) != 64 {
			return nil, fmt.Errorf("invalid checksum line %d: hash length", i+1)
		}
		for _, ch := range hash {
			if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
				return nil, fmt.Errorf("invalid checksum line %d: non-hex hash", i+1)
			}
		}
		name := fields[1]
		if strings.HasPrefix(name, "*") {
			name = name[1:]
		}
		name = filepath.Base(name)
		out[name] = hash
	}
	return out, nil
}

// ChecksumForArchive returns the expected SHA256 hex for archiveName.
func ChecksumForArchive(content, archiveName string) (string, error) {
	entries, err := ParseChecksumEntries(content)
	if err != nil {
		return "", err
	}
	base := filepath.Base(archiveName)
	if hash, ok := entries[base]; ok {
		return hash, nil
	}
	if hash, ok := entries[archiveName]; ok {
		return hash, nil
	}
	return "", fmt.Errorf("checksum entry not found for %s", archiveName)
}

// FileSHA256 returns the hex-encoded SHA256 of a file.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyFileChecksum checks path against expected hex SHA256.
func VerifyFileChecksum(path, expectedHex string) error {
	actual, err := FileSHA256(path)
	if err != nil {
		return err
	}
	expectedHex = strings.ToLower(strings.TrimSpace(expectedHex))
	if actual != expectedHex {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s",
			filepath.Base(path), expectedHex, actual)
	}
	return nil
}
