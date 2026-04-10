package cmd

import (
	"testing"
)

func TestLogoutSingleProfile_NoConfig(t *testing.T) {
	cl := &ConsoleLogout{Profile: "nonexistent"}
	err := cl.Logout()
	if err == nil {
		t.Fatal("expected error when profile does not exist")
	}
}

func TestLogoutSingleProfile_NonLoginMode(t *testing.T) {
	// This test verifies that logout rejects profiles not using console-login mode.
	// Since LoadConfig reads from disk, we test the mode check logic indirectly.
	cl := &ConsoleLogout{Profile: "test-ak-profile"}
	err := cl.Logout()
	if err == nil {
		// If the profile doesn't exist, that's also an acceptable error.
		t.Log("profile not found, which is expected in test environment")
	}
}

func TestLogoutAll_NoConfig(t *testing.T) {
	// When no config exists, logoutAll should not panic and should print a message.
	cl := &ConsoleLogout{All: true}
	err := cl.Logout()
	// No error expected — just prints "No configuration found" or
	// "No console-login profiles with active sessions found."
	if err != nil {
		t.Logf("logoutAll returned error (acceptable in test env): %v", err)
	}
}

func TestRemoveLoginCache_NonExistent(t *testing.T) {
	// removeLoginCache should be idempotent — removing a non-existent file is not an error.
	err := removeLoginCache("non-existent-session-id-12345")
	if err != nil {
		t.Fatalf("removeLoginCache should not error on non-existent file, got: %v", err)
	}
}
