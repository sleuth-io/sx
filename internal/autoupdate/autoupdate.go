package autoupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/creativeprojects/go-selfupdate"

	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/cache"
	"github.com/sleuth-io/sx/internal/logger"
)

const (
	GithubOwner       = "sleuth-io"
	GithubRepo        = "sx"
	checkInterval     = 24 * time.Hour
	updateCacheFile   = "last-update-check"
	pendingUpdateFile = "pending-update.json"
	updateTimeout     = 30 * time.Second
)

// pendingUpdate represents a pending update marker file
type pendingUpdate struct {
	Version   string `json:"version"`
	AssetURL  string `json:"asset_url"`
	AssetName string `json:"asset_name"`
}

// isEnvTrue checks if an environment variable is set to a truthy value
func isEnvTrue(key string) bool {
	val := os.Getenv(key)
	switch val {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	}
	return false
}

// isDevBuild returns true if this is a development build
func isDevBuild() bool {
	v := buildinfo.Version
	return v == "dev" || v == "" || strings.Contains(v, "-dirty")
}

// pendingUpdatePath returns the path to the pending update marker file
func pendingUpdatePath() (string, error) {
	cacheDir, err := cache.GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, pendingUpdateFile), nil
}

// ApplyPendingUpdate checks for a pending update marker and applies it.
// This should be called at startup before CheckAndUpdateInBackground.
// The fast path (no marker) is a single os.Stat call.
// Returns true if an update was applied (caller should re-exec).
func ApplyPendingUpdate() bool {
	if isEnvTrue("DISABLE_AUTOUPDATER") || isDevBuild() {
		return false
	}

	log := logger.Get()

	markerPath, err := pendingUpdatePath()
	if err != nil {
		return false
	}

	// Fast path: no marker means no pending update
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		return false
	}

	data, err := os.ReadFile(markerPath)
	if err != nil {
		// Can't read marker, remove it and move on
		_ = os.Remove(markerPath)
		return false
	}

	var pending pendingUpdate
	if err := json.Unmarshal(data, &pending); err != nil {
		_ = os.Remove(markerPath)
		return false
	}

	// Check if we're already at or ahead of the pending version
	currentV, err := semver.NewVersion(buildinfo.Version)
	if err != nil {
		_ = os.Remove(markerPath)
		return false
	}
	pendingV, err := semver.NewVersion(pending.Version)
	if err != nil {
		_ = os.Remove(markerPath)
		return false
	}
	if !currentV.LessThan(pendingV) {
		// Already at or ahead — user may have run `sx update` or Homebrew upgrade
		_ = os.Remove(markerPath)
		return false
	}

	// Get path to current executable for replacement
	execPath, err := os.Executable()
	if err != nil {
		_ = os.Remove(markerPath)
		return false
	}

	// Apply the update with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	// Suppress stdout during update (deferred to guarantee restoration on panic)
	restoreStdout := suppressStdout()
	defer restoreStdout()
	err = selfupdate.UpdateTo(ctx, pending.AssetURL, pending.AssetName, execPath)

	// Always remove the marker — if the update failed, the next background check
	// will detect a newer version and create a new marker
	_ = os.Remove(markerPath)

	if err != nil {
		log.Error("failed to apply pending update", "version", pending.Version, "error", err)
		return false
	}

	log.Info("applied pending update", "old_version", buildinfo.Version, "new_version", pending.Version)
	return true
}

// ClearPendingUpdate removes the pending update marker file.
// Call this after a successful manual update (e.g., `sx update`).
func ClearPendingUpdate() {
	markerPath, err := pendingUpdatePath()
	if err != nil {
		return
	}
	_ = os.Remove(markerPath)
}

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

// checkAndUpdate performs the actual update check and installation.
// Phase 1: Detect latest release, write marker, attempt UpdateTo.
// If the goroutine gets killed mid-download, the marker survives for Phase 2.
func checkAndUpdate() error {
	// Skip if auto-update is disabled via environment (e.g., Homebrew installations)
	if isEnvTrue("DISABLE_AUTOUPDATER") {
		return nil
	}

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

	// Use the library's Updater with silent output
	source, _ := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	updater, _ := selfupdate.NewUpdater(selfupdate.Config{
		Source: source,
	})

	// Detect latest release
	release, found, err := updater.DetectLatest(ctx, selfupdate.ParseSlug(fmt.Sprintf("%s/%s", GithubOwner, GithubRepo)))
	if err != nil {
		// Don't update timestamp on error — allow retry next time
		return err
	}
	if !found {
		_ = updateCheckTimestamp()
		return nil
	}

	// Check if update is needed
	if release.LessOrEqual(currentVersion) {
		_ = updateCheckTimestamp()
		return nil
	}

	// Write marker file so Phase 2 can pick up if this goroutine gets killed
	if err := writePendingUpdate(release); err != nil {
		// Non-fatal: we can still try the update directly
		log := logger.Get()
		log.Error("failed to write pending update marker", "error", err)
	}

	// Update check timestamp — we successfully checked
	_ = updateCheckTimestamp()

	// Suppress stdout during update (deferred to guarantee restoration on panic)
	restoreStdout := suppressStdout()
	defer restoreStdout()

	// Attempt the update — may not complete if process exits
	err = updater.UpdateTo(ctx, release, "")

	if err != nil {
		// Marker remains for Phase 2 to retry
		return err
	}

	// Update succeeded — remove marker
	ClearPendingUpdate()

	log := logger.Get()
	log.Info("autoupdate completed", "old_version", currentVersion, "new_version", release.Version())

	return nil
}

// writePendingUpdate writes the pending update marker file
func writePendingUpdate(release *selfupdate.Release) error {
	markerPath, err := pendingUpdatePath()
	if err != nil {
		return err
	}

	// Ensure cache directory exists
	if err := os.MkdirAll(filepath.Dir(markerPath), 0755); err != nil {
		return err
	}

	pending := pendingUpdate{
		Version:   release.Version(),
		AssetURL:  release.AssetURL,
		AssetName: release.AssetName,
	}

	data, err := json.Marshal(pending)
	if err != nil {
		return err
	}

	return os.WriteFile(markerPath, data, 0644)
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
