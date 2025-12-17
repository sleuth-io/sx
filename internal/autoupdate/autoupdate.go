package autoupdate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/creativeprojects/go-selfupdate"

	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/cache"
	"github.com/sleuth-io/sx/internal/logger"
)

const (
	githubOwner     = "sleuth-io"
	githubRepo      = "sx"
	checkInterval   = 24 * time.Hour
	updateCacheFile = "last-update-check"
	updateTimeout   = 30 * time.Second
)

// CheckAndUpdateInBackground checks for updates and installs them automatically if found.
// It only checks once per day (tracked via cache file).
// This function returns immediately and doesn't block.
func CheckAndUpdateInBackground() {
	// Run in background goroutine
	go func() {
		// Silently ignore errors - we don't want to disrupt the user's workflow
		_ = checkAndUpdate()
	}()
}

// checkAndUpdate performs the actual update check and installation
func checkAndUpdate() error {
	// Only check if we're running a real release (not dev build)
	currentVersion := buildinfo.Version
	if currentVersion == "dev" || currentVersion == "" {
		return nil
	}

	// Check if we've checked recently
	if !shouldCheck() {
		return nil
	}

	// Create a short timeout context - don't want to hang
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	// Use the library's UpdateSelf function which handles everything:
	// - Detecting latest version
	// - Comparing versions
	// - Downloading the right binary for OS/arch
	// - Replacing the executable
	release, err := selfupdate.UpdateSelf(ctx, currentVersion, selfupdate.ParseSlug(fmt.Sprintf("%s/%s", githubOwner, githubRepo)))
	if err != nil {
		// Network error, GitHub API issue, or already up to date - just skip silently
		_ = updateCheckTimestamp()
		return err
	}

	// Update the last check time
	_ = updateCheckTimestamp()

	// Log the successful update
	log := logger.Get()
	log.Info("autoupdate completed", "old_version", currentVersion, "new_version", release.Version)

	// Note: We don't exec into the new binary because it can interrupt
	// critical operations like git clones. The new version will be used
	// on the next invocation.
	_ = release

	return nil
}

// shouldCheck returns true if we should check for updates
func shouldCheck() bool {
	cacheDir, err := cache.GetCacheDir()
	if err != nil {
		return true // If we can't determine cache dir, check anyway
	}

	lastCheckFile := filepath.Join(cacheDir, updateCacheFile)

	info, err := os.Stat(lastCheckFile)
	if err != nil {
		// File doesn't exist, we should check
		return true
	}

	// Check if it's been more than checkInterval since last check
	return time.Since(info.ModTime()) > checkInterval
}

// updateCheckTimestamp updates the timestamp of the last update check
func updateCheckTimestamp() error {
	cacheDir, err := cache.GetCacheDir()
	if err != nil {
		return err
	}

	// Ensure cache directory exists
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}

	lastCheckFile := filepath.Join(cacheDir, updateCacheFile)

	// Create or update the file
	f, err := os.Create(lastCheckFile)
	if err != nil {
		return err
	}
	defer f.Close()

	// Write a simple timestamp
	_, err = f.WriteString(time.Now().Format(time.RFC3339))
	return err
}
