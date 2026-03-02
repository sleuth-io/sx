package autoupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/cache"
)

func TestShouldCheckDevBuild(t *testing.T) {
	// Save original version
	originalVersion := buildinfo.Version
	defer func() { buildinfo.Version = originalVersion }()

	// Set version to "dev"
	buildinfo.Version = "dev"

	// Should return early for dev builds
	err := checkAndUpdate()
	if err != nil {
		t.Errorf("Expected no error for dev build, got: %v", err)
	}
}

func TestShouldCheckWithNoCache(t *testing.T) {
	// Clean up any existing cache
	cacheDir, err := cache.GetCacheDir()
	if err != nil {
		t.Fatalf("Failed to get cache dir: %v", err)
	}
	lastCheckFile := filepath.Join(cacheDir, updateCacheFile)
	_ = os.Remove(lastCheckFile)

	// Should check when there's no cache file
	if !shouldCheck() {
		t.Error("Expected shouldCheck to return true when cache file doesn't exist")
	}
}

func TestShouldCheckWithRecentCache(t *testing.T) {
	// Create a recent cache file
	cacheDir, err := cache.GetCacheDir()
	if err != nil {
		t.Fatalf("Failed to get cache dir: %v", err)
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("Failed to create cache dir: %v", err)
	}

	lastCheckFile := filepath.Join(cacheDir, updateCacheFile)

	// Create cache file with current timestamp
	if err := updateCheckTimestamp(); err != nil {
		t.Fatalf("Failed to update timestamp: %v", err)
	}

	// Should not check when cache is recent
	if shouldCheck() {
		t.Error("Expected shouldCheck to return false when cache is recent")
	}

	// Clean up
	_ = os.Remove(lastCheckFile)
}

func TestShouldCheckWithOldCache(t *testing.T) {
	// Create an old cache file
	cacheDir, err := cache.GetCacheDir()
	if err != nil {
		t.Fatalf("Failed to get cache dir: %v", err)
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("Failed to create cache dir: %v", err)
	}

	lastCheckFile := filepath.Join(cacheDir, updateCacheFile)

	// Create file and set old modification time
	f, err := os.Create(lastCheckFile)
	if err != nil {
		t.Fatalf("Failed to create cache file: %v", err)
	}
	f.Close()

	// Set modification time to 25 hours ago (past the 24 hour threshold)
	oldTime := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(lastCheckFile, oldTime, oldTime); err != nil {
		t.Fatalf("Failed to set file time: %v", err)
	}

	// Should check when cache is old
	if !shouldCheck() {
		t.Error("Expected shouldCheck to return true when cache is old")
	}

	// Clean up
	_ = os.Remove(lastCheckFile)
}

func TestUpdateCheckTimestamp(t *testing.T) {
	// Create timestamp
	if err := updateCheckTimestamp(); err != nil {
		t.Fatalf("Failed to update timestamp: %v", err)
	}

	// Verify file exists
	cacheDir, err := cache.GetCacheDir()
	if err != nil {
		t.Fatalf("Failed to get cache dir: %v", err)
	}

	lastCheckFile := filepath.Join(cacheDir, updateCacheFile)
	if _, err := os.Stat(lastCheckFile); os.IsNotExist(err) {
		t.Error("Expected cache file to exist after updateCheckTimestamp")
	}

	// Clean up
	_ = os.Remove(lastCheckFile)
}

func TestPendingUpdatePath(t *testing.T) {
	path, err := pendingUpdatePath()
	if err != nil {
		t.Fatalf("Failed to get pending update path: %v", err)
	}

	if filepath.Base(path) != pendingUpdateFile {
		t.Errorf("Expected filename %q, got %q", pendingUpdateFile, filepath.Base(path))
	}
}

func TestClearPendingUpdate(t *testing.T) {
	// Write a marker file
	markerPath, err := pendingUpdatePath()
	if err != nil {
		t.Fatalf("Failed to get marker path: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(markerPath), 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	if err := os.WriteFile(markerPath, []byte(`{"version":"1.0.0"}`), 0644); err != nil {
		t.Fatalf("Failed to write marker: %v", err)
	}

	// Verify it exists
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Fatal("Marker file should exist before clear")
	}

	// Clear it
	ClearPendingUpdate()

	// Verify it's gone
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("Marker file should not exist after ClearPendingUpdate")
	}
}

func TestClearPendingUpdateNoFile(t *testing.T) {
	// Should not panic when no marker exists
	ClearPendingUpdate()
}

func TestApplyPendingUpdateNoMarker(t *testing.T) {
	originalVersion := buildinfo.Version
	defer func() { buildinfo.Version = originalVersion }()

	buildinfo.Version = "0.10.0"

	// Make sure no marker exists
	markerPath, err := pendingUpdatePath()
	if err != nil {
		t.Fatalf("Failed to get marker path: %v", err)
	}
	_ = os.Remove(markerPath)

	// Should return false (no update applied)
	if ApplyPendingUpdate() {
		t.Error("Expected false when no marker exists")
	}
}

func TestApplyPendingUpdateDevBuild(t *testing.T) {
	originalVersion := buildinfo.Version
	defer func() { buildinfo.Version = originalVersion }()

	buildinfo.Version = "dev"

	// Write a marker that should be ignored for dev builds
	markerPath, err := pendingUpdatePath()
	if err != nil {
		t.Fatalf("Failed to get marker path: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(markerPath), 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	if err := os.WriteFile(markerPath, []byte(`{"version":"1.0.0"}`), 0644); err != nil {
		t.Fatalf("Failed to write marker: %v", err)
	}

	// Should skip for dev builds without removing marker
	if ApplyPendingUpdate() {
		t.Error("Expected false for dev build")
	}

	// Marker should still exist (dev builds skip entirely)
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Error("Marker should still exist for dev builds")
	}

	// Clean up
	_ = os.Remove(markerPath)
}

func TestApplyPendingUpdateDisabled(t *testing.T) {
	originalVersion := buildinfo.Version
	defer func() { buildinfo.Version = originalVersion }()

	buildinfo.Version = "0.10.0"
	t.Setenv("DISABLE_AUTOUPDATER", "1")

	// Write a marker
	markerPath, err := pendingUpdatePath()
	if err != nil {
		t.Fatalf("Failed to get marker path: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(markerPath), 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	if err := os.WriteFile(markerPath, []byte(`{"version":"1.0.0"}`), 0644); err != nil {
		t.Fatalf("Failed to write marker: %v", err)
	}

	// Should skip when disabled
	if ApplyPendingUpdate() {
		t.Error("Expected false when disabled")
	}

	// Marker should still exist
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Error("Marker should still exist when autoupdater is disabled")
	}

	// Clean up
	_ = os.Remove(markerPath)
}

func TestApplyPendingUpdateAlreadyUpToDate(t *testing.T) {
	originalVersion := buildinfo.Version
	defer func() { buildinfo.Version = originalVersion }()

	// Set current version ahead of pending version
	buildinfo.Version = "2.0.0"

	markerPath, err := pendingUpdatePath()
	if err != nil {
		t.Fatalf("Failed to get marker path: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(markerPath), 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	pending := pendingUpdate{
		Version:   "1.0.0",
		AssetURL:  "https://example.com/asset.tar.gz",
		AssetName: "asset.tar.gz",
	}
	data, _ := json.Marshal(pending)

	if err := os.WriteFile(markerPath, data, 0644); err != nil {
		t.Fatalf("Failed to write marker: %v", err)
	}

	// Should skip and remove marker since we're already ahead
	if ApplyPendingUpdate() {
		t.Error("Expected false when already up to date")
	}

	// Marker should be removed
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("Marker should be removed when version is already at or ahead")
	}
}

func TestApplyPendingUpdateInvalidJSON(t *testing.T) {
	originalVersion := buildinfo.Version
	defer func() { buildinfo.Version = originalVersion }()

	buildinfo.Version = "0.10.0"

	markerPath, err := pendingUpdatePath()
	if err != nil {
		t.Fatalf("Failed to get marker path: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(markerPath), 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	// Write invalid JSON
	if err := os.WriteFile(markerPath, []byte(`not json`), 0644); err != nil {
		t.Fatalf("Failed to write marker: %v", err)
	}

	// Should handle gracefully and remove bad marker
	if ApplyPendingUpdate() {
		t.Error("Expected false for invalid JSON")
	}

	// Marker should be removed
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("Invalid marker should be removed")
	}
}

func TestMarkerFileFormat(t *testing.T) {
	markerPath, err := pendingUpdatePath()
	if err != nil {
		t.Fatalf("Failed to get marker path: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(markerPath), 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	pending := pendingUpdate{
		Version:   "1.2.3",
		AssetURL:  "https://github.com/sleuth-io/sx/releases/download/v1.2.3/sx_Linux_x86_64.tar.gz",
		AssetName: "sx_Linux_x86_64.tar.gz",
	}

	data, err := json.Marshal(pending)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	if err := os.WriteFile(markerPath, data, 0644); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	// Read it back
	readData, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	var readPending pendingUpdate
	if err := json.Unmarshal(readData, &readPending); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if readPending.Version != "1.2.3" {
		t.Errorf("Version = %q, want %q", readPending.Version, "1.2.3")
	}
	if readPending.AssetURL != pending.AssetURL {
		t.Errorf("AssetURL = %q, want %q", readPending.AssetURL, pending.AssetURL)
	}
	if readPending.AssetName != pending.AssetName {
		t.Errorf("AssetName = %q, want %q", readPending.AssetName, pending.AssetName)
	}

	// Clean up
	_ = os.Remove(markerPath)
}
