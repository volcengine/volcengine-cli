// Package clientid stores a random identifier for a local CLI installation.
package clientid

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// HeaderName identifies a local CLI installation in outgoing HTTP requests.
const HeaderName = "X-Client-Id"

// LoadOrCreate returns the UUIDv4 stored in directory/client_id. The ID is
// independent of credentials, profiles, and CLI versions. It is never silently
// replaced: an unreadable or invalid existing file is an error.
//
// A fully written temporary file is published with an atomic, non-replacing
// hard link. Concurrent first callers, including separate processes, all use
// the winning file; no caller can observe a partially written ID. The directory
// must support hard links when creating an ID for the first time.
func LoadOrCreate(directory string) (string, error) {
	if strings.TrimSpace(directory) == "" {
		return "", fmt.Errorf("client ID directory is empty")
	}
	path := filepath.Join(directory, "client_id")
	id, err := readID(path)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", fmt.Errorf("create client ID directory: %w", err)
	}

	// NewRandom uses crypto/rand and returns errors instead of panicking.
	generated, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("generate client ID: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".client_id-*")
	if err != nil {
		return "", fmt.Errorf("create client ID temporary file: %w", err)
	}
	defer os.Remove(temporary.Name())
	if _, err := io.WriteString(temporary, generated.String()+"\n"); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("write client ID: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("sync client ID: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close client ID: %w", err)
	}
	if err := os.Link(temporary.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return readID(path)
		}
		return "", fmt.Errorf("persist client ID %q (atomic hard-link creation required): %w", path, err)
	}
	return generated.String(), nil
}

func readID(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("read client ID %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("client ID %q must be a regular file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read client ID %q: %w", path, err)
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 129))
	if err != nil {
		return "", fmt.Errorf("read client ID %q: %w", path, err)
	}
	value := strings.TrimSpace(string(content))
	id, err := uuid.Parse(value)
	if len(content) > 128 || len(value) != 36 || err != nil || id.Version() != 4 || id.Variant() != uuid.RFC4122 {
		return "", fmt.Errorf("invalid client ID file %q: expected an RFC 9562 UUIDv4", path)
	}
	return id.String(), nil
}
