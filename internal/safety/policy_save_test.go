package safety

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSavePolicyRenameFailurePreservesTarget(t *testing.T) {
	dir := t.TempDir()
	target := PolicyFilePath(dir)
	// A file cannot be renamed over a directory. This triggers a real rename
	// failure while keeping the target removable, exposing delete-and-retry.
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	err := SavePolicy(dir, &Policy{Enabled: true, Rules: []Rule{{Pattern: "sts:*", Action: ActionDeny}}})
	var renameErr *os.LinkError
	if !errors.As(err, &renameErr) || renameErr.Op != "rename" || renameErr.New != target {
		t.Errorf("SavePolicy error=%v, want the original rename error", err)
	}
	info, statErr := os.Stat(target)
	if statErr != nil || !info.IsDir() {
		t.Errorf("failed rename removed or replaced the original target: %v", statErr)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".tmp-safety-policy-*"))
	if err != nil || len(leftovers) != 0 {
		t.Errorf("temporary files left after failure: %v, error=%v", leftovers, err)
	}
}
