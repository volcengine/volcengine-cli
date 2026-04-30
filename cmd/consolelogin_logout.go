package cmd

import (
	"fmt"
	"os"
)

// ConsoleLogout holds runtime state for the volcengine logout flow.
type ConsoleLogout struct {
	Profile string // profile name, default "default"
	All     bool   // true = clear all login caches
}

// ---------------------------------------------------------------------------
// Logout orchestrates the full console logout flow.
// This is a purely local file-cleanup operation — no network requests are made.
// ---------------------------------------------------------------------------

func (cl *ConsoleLogout) Logout() error {
	if cl.All {
		return cl.logoutAll()
	}
	return cl.logoutSingleProfile()
}

// logoutSingleProfile logs out the specified (or current) profile by removing
// its cached login token file and clearing the login_session in config.
func (cl *ConsoleLogout) logoutSingleProfile() error {
	cfg := runtimeConfig()
	if cfg == nil || cfg.Profiles == nil {
		return fmt.Errorf("no configuration found; nothing to log out")
	}

	profileName := cl.Profile
	if profileName == "" {
		profileName = "default"
	}

	profile, ok := cfg.Profiles[profileName]
	if !ok || profile == nil {
		return fmt.Errorf("profile %q not found in configuration", profileName)
	}

	if profile.Mode != ModeConsoleLogin {
		return fmt.Errorf(
			"profile %q is using %q mode, not %q mode. "+
				"Only console-login profiles can be logged out with this command",
			profileName,
			profile.Mode,
			ModeConsoleLogin,
		)
	}

	if profile.LoginSession == "" {
		fmt.Printf("Profile %q does not have an active login session. Nothing to do.\n", profileName)
		return nil
	}

	// Attempt to delete the cached token file.
	if err := removeLoginCache(profile.LoginSession); err != nil {
		return fmt.Errorf("removing cached token for profile %q: %w", profileName, err)
	}

	// Clear the login_session field in the profile config.
	profile.LoginSession = ""
	cfg.Profiles[profileName] = profile

	if err := WriteConfigToFile(cfg); err != nil {
		return fmt.Errorf("updating config after logout: %w", err)
	}
	setRuntimeConfig(cfg)

	fmt.Printf("Successfully logged out of profile %q.\n", profileName)
	printPostLogoutHint()
	return nil
}

// logoutAll iterates all profiles in config, finds every console-login profile
// with an active login-session, removes the corresponding cache file, and
// clears the login-session field. This is config-driven rather than
// filesystem-scanning, ensuring we only touch files that belong to known profiles.
func (cl *ConsoleLogout) logoutAll() error {
	cfg := runtimeConfig()
	if cfg == nil || cfg.Profiles == nil {
		fmt.Println("No configuration found; nothing to log out.")
		return nil
	}

	deletedCount := 0
	var firstErr error

	for name, profile := range cfg.Profiles {
		if profile == nil || profile.Mode != ModeConsoleLogin || profile.LoginSession == "" {
			continue
		}

		// Attempt to delete the cached token file for this profile.
		if err := removeLoginCache(profile.LoginSession); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove cache for profile %q: %v\n", name, err)
			if firstErr == nil {
				firstErr = err
			}
			// Continue to process remaining profiles.
			continue
		}

		// Clear login-session in config.
		profile.LoginSession = ""
		deletedCount++
		fmt.Printf("  Logged out profile %q\n", name)
	}

	// Persist config changes (even if some removals failed, clear the ones that succeeded).
	if err := WriteConfigToFile(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update config after logout: %v\n", err)
	} else {
		setRuntimeConfig(cfg)
	}

	if deletedCount > 0 {
		fmt.Printf("\nSuccessfully logged out %d console-login profile(s).\n", deletedCount)
		printPostLogoutHint()
	} else {
		fmt.Println("No console-login profiles with active sessions found. Nothing to do.")
	}

	return firstErr
}

// removeLoginCache resolves the cache file path for a login-session and removes it.
// Returns nil if the file does not exist (idempotent).
func removeLoginCache(loginSession string) error {
	cachePath, err := loginCacheFilePath(loginSession)
	if err != nil {
		return fmt.Errorf("resolving cache file path: %w", err)
	}

	if err := os.Remove(cachePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing %s: %w", cachePath, err)
	}
	return nil
}

// printPostLogoutHint prints a security reminder after logout.
func printPostLogoutHint() {
	fmt.Println()
	fmt.Println("Note: Local cache has been removed for future CLI sessions.")
	fmt.Println("Already-running tools that loaded temporary STS credentials before logout")
	fmt.Println("may continue to use them until those credentials expire.")
}
