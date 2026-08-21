package cmd

import (
	"errors"
	"fmt"
	"os"
	"sort"
)

var (
	writeLogoutConfigTransaction = writeConfigTransaction
	removeLoginCacheFile         = os.Remove
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
func (cl *ConsoleLogout) logoutSingleProfile() (returnErr error) {
	cfg := runtimeConfig()
	if cfg == nil || cfg.Profiles == nil {
		return trErrorf("no configuration found; nothing to log out")
	}
	if err := prepareConfigForMutation(cfg); err != nil {
		return fmt.Errorf("preparing config update: %w", err)
	}

	profileName := cl.Profile
	if profileName == "" {
		profileName = "default"
	}

	profile, ok := cfg.Profiles[profileName]
	if !ok || profile == nil {
		return trErrorf("profile %q not found in configuration", profileName)
	}

	if profile.Mode != ModeConsoleLogin {
		return trErrorf(
			"profile %q is using %q mode, not %q mode. "+
				"Only console-login profiles can be logged out with this command",
			profileName,
			profile.Mode,
			ModeConsoleLogin,
		)
	}

	if profile.LoginSession == "" {
		fmt.Printf(tr("Profile %q does not have an active login session. Nothing to do.\n"), profileName)
		return nil
	}

	loginSession := profile.LoginSession
	cachePath, err := loginCacheFilePath(loginSession)
	if err != nil {
		return trErrorf("resolving cache file path for profile %q: %w", profileName, err)
	}
	cacheLock, err := acquireCredentialCacheLock(cachePath)
	if err != nil {
		return trErrorf("locking cached token for profile %q: %w", profileName, err)
	}
	defer func() {
		if err := cacheLock.release(); err != nil {
			returnErr = combineLogoutErrors(returnErr, trErrorf("releasing cache lock for profile %q: %w", profileName, err))
		}
	}()

	// Disconnect the profile from the cached credentials before deleting them.
	// The cache lock prevents a concurrent login from replacing this exact
	// cache instance between the config commit and removal.
	profile.LoginSession = ""
	cfg.Profiles[profileName] = profile

	configErr := writeLogoutConfigTransaction(cfg)
	if configErr != nil && !configMutationCommitted(configErr) {
		profile.LoginSession = loginSession
		cfg.Profiles[profileName] = profile
		return trErrorf("updating config before logout: %w", configErr)
	}
	setRuntimeConfig(cfg)

	cacheErr := removeLoginCacheAtPath(cachePath)
	if cacheErr != nil {
		cacheErr = trErrorf("removing cached token for profile %q after config was cleared: %w", profileName, cacheErr)
	}
	if err := combineLogoutErrors(configErr, cacheErr); err != nil {
		return err
	}

	fmt.Printf(tr("Successfully logged out of profile %q.\n"), profileName)
	printPostLogoutHint()
	return nil
}

// logoutAll iterates all profiles in config, finds every console-login profile
// with an active login-session, removes the corresponding cache file, and
// clears the login-session field. This is config-driven rather than
// filesystem-scanning, ensuring we only touch files that belong to known profiles.
func (cl *ConsoleLogout) logoutAll() (returnErr error) {
	cfg := runtimeConfig()
	if cfg == nil || cfg.Profiles == nil {
		fmt.Println(tr("No configuration found; nothing to log out."))
		return nil
	}
	if err := prepareConfigForMutation(cfg); err != nil {
		return fmt.Errorf("preparing config update: %w", err)
	}

	type profileSession struct {
		name      string
		session   string
		cachePath string
		profile   *Profile
	}
	profiles := make([]profileSession, 0)
	for name, profile := range cfg.Profiles {
		if profile == nil || profile.Mode != ModeConsoleLogin || profile.LoginSession == "" {
			continue
		}
		cachePath, err := loginCacheFilePath(profile.LoginSession)
		if err != nil {
			return trErrorf("resolving cache file path for profile %q: %w", name, err)
		}
		profiles = append(profiles, profileSession{name: name, session: profile.LoginSession, cachePath: cachePath, profile: profile})
	}
	if len(profiles) == 0 {
		fmt.Println(tr("No console-login profiles with active sessions found. Nothing to do."))
		return nil
	}

	// A deterministic order prevents two logout-all processes from deadlocking
	// while protecting overlapping sets of cache files.
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].cachePath < profiles[j].cachePath })
	locks := make(map[string]*configFileLock)
	lockPaths := make([]string, 0, len(profiles))
	for _, item := range profiles {
		if _, exists := locks[item.cachePath]; exists {
			continue
		}
		cacheLock, err := acquireCredentialCacheLock(item.cachePath)
		if err != nil {
			for i := len(lockPaths) - 1; i >= 0; i-- {
				_ = locks[lockPaths[i]].release()
			}
			return trErrorf("locking cached token for profile %q: %w", item.name, err)
		}
		locks[item.cachePath] = cacheLock
		lockPaths = append(lockPaths, item.cachePath)
	}
	defer func() {
		for i := len(lockPaths) - 1; i >= 0; i-- {
			if err := locks[lockPaths[i]].release(); err != nil {
				returnErr = combineLogoutErrors(returnErr, trErrorf("releasing credential cache lock: %w", err))
			}
		}
	}()

	for _, item := range profiles {
		item.profile.LoginSession = ""
		cfg.Profiles[item.name] = item.profile
	}

	configErr := writeLogoutConfigTransaction(cfg)
	if configErr != nil && !configMutationCommitted(configErr) {
		for _, item := range profiles {
			item.profile.LoginSession = item.session
			cfg.Profiles[item.name] = item.profile
		}
		return trErrorf("updating config before logout: %w", configErr)
	}
	setRuntimeConfig(cfg)

	var firstCacheErr error
	for _, item := range profiles {
		if err := removeLoginCacheAtPath(item.cachePath); err != nil {
			fmt.Fprintf(os.Stderr, tr("Warning: failed to remove cache for profile %q after config was cleared: %v\n"), item.name, err)
			if firstCacheErr == nil {
				firstCacheErr = trErrorf("removing cached token for profile %q after config was cleared: %w", item.name, err)
			}
			continue
		}
		fmt.Printf(tr("  Logged out profile %q\n"), item.name)
	}

	if configErr == nil && firstCacheErr == nil {
		fmt.Printf("\n"+tr("Successfully logged out %d console-login profile(s).\n"), len(profiles))
		printPostLogoutHint()
	}

	return combineLogoutErrors(configErr, firstCacheErr)
}

// removeLoginCache resolves the cache file path for a login-session and removes it.
// Returns nil if the file does not exist (idempotent).
func removeLoginCache(loginSession string) (returnErr error) {
	cachePath, err := loginCacheFilePath(loginSession)
	if err != nil {
		return trErrorf("resolving cache file path: %w", err)
	}
	cacheLock, err := acquireCredentialCacheLock(cachePath)
	if err != nil {
		return trErrorf("locking cache file: %w", err)
	}
	defer func() {
		if err := cacheLock.release(); err != nil {
			returnErr = combineLogoutErrors(returnErr, trErrorf("releasing cache lock: %w", err))
		}
	}()
	return removeLoginCacheAtPath(cachePath)
}

func removeLoginCacheAtPath(cachePath string) error {
	if err := removeLoginCacheFile(cachePath); err != nil && !os.IsNotExist(err) {
		return trErrorf("removing %s: %w", cachePath, err)
	}
	return nil
}

func configMutationCommitted(err error) bool {
	if err == nil {
		return true
	}
	var partial *PartialCommitError
	return errors.As(err, &partial) && partial.Committed()
}

// combineLogoutErrors keeps the primary error available to errors.Is/As while
// also reporting a later cleanup failure. errors.Join is unavailable on Go 1.17.
func combineLogoutErrors(primary, additional error) error {
	if primary == nil {
		return additional
	}
	if additional == nil {
		return primary
	}
	return fmt.Errorf("%w; additional logout cleanup error: %v", primary, additional)
}

// printPostLogoutHint prints a security reminder after logout.
func printPostLogoutHint() {
	fmt.Println()
	fmt.Println(tr("Note: Local cache has been removed for future CLI sessions."))
	fmt.Println(tr("Already-running tools that loaded temporary STS credentials before logout"))
	fmt.Println(tr("may continue to use them until those credentials expire."))
}
