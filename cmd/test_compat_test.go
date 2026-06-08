package cmd

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func tempDirForTest(t *testing.T) string {
	t.Helper()
	dir, err := ioutil.TempDir("", "volcengine-cli-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	return dir
}

func setenvForTest(t *testing.T, key, value string) func() {
	t.Helper()
	oldValue, existed := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("set env %s: %v", key, err)
	}
	return func() {
		if existed {
			_ = os.Setenv(key, oldValue)
		} else {
			_ = os.Unsetenv(key)
		}
	}
}

func cleanupDirForTest(dir string) func() {
	return func() {
		if dir != "" {
			_ = os.RemoveAll(filepath.Clean(dir))
		}
	}
}
