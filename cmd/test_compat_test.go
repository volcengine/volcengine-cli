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

func unsetenvForTest(t *testing.T, key string) func() {
	t.Helper()
	oldValue, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset env %s: %v", key, err)
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

func withConfigDirForTest(dir string) func() {
	oldFunc := configFileDirFunc
	configFileDirFunc = func() (string, error) {
		return dir, nil
	}
	return func() {
		configFileDirFunc = oldFunc
	}
}
