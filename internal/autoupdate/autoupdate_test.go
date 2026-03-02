package autoupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sleuth-io/sx/internal/buildinfo"
)

// useTempCache isolates the test from the real cache directory
func useTempCache(t *testing.T) {
	t.Helper()
	t.Setenv("SX_CACHE_DIR", t.TempDir())
}

func TestShouldCheckDevBuild(t *testing.T) {
	useTempCache(t)
	originalVersion := buildinfo.Version
	defer func() { buildinfo.Version = originalVersion }()

	buildinfo.Version = "dev"

	err := checkAndUpdate()
	if err != nil {
		t.Errorf("Expected no error for dev build, got: %v", err)
	}
}

func TestShouldCheckWithNoCache(t *testing.T) {
	useTempCache(t)

	if !shouldCheck() {
		t.Error("Expected shouldCheck to return true when cache file doesn't exist")
	}
}

func TestShouldCheckWithRecentCache(t *testing.T) {
	useTempCache(t)

	if err := updateCheckTimestamp(); err != nil {
		t.Fatalf("Failed to update timestamp: %v", err)
	}

	if shouldCheck() {
		t.Error("Expected shouldCheck to return false when cache is recent")
	}
}

func TestShouldCheckWithOldCache(t *testing.T) {
	useTempCache(t)

	if err := updateCheckTimestamp(); err != nil {
		t.Fatalf("Failed to update timestamp: %v", err)
	}

	// Set modification time to 25 hours ago (past the 24 hour threshold)
	cacheDir := os.Getenv("SX_CACHE_DIR")
	lastCheckFile := filepath.Join(cacheDir, updateCacheFile)
	oldTime := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(lastCheckFile, oldTime, oldTime); err != nil {
		t.Fatalf("Failed to set file time: %v", err)
	}

	if !shouldCheck() {
		t.Error("Expected shouldCheck to return true when cache is old")
	}
}

func TestUpdateCheckTimestamp(t *testing.T) {
	useTempCache(t)

	if err := updateCheckTimestamp(); err != nil {
		t.Fatalf("Failed to update timestamp: %v", err)
	}

	cacheDir := os.Getenv("SX_CACHE_DIR")
	lastCheckFile := filepath.Join(cacheDir, updateCacheFile)
	if _, err := os.Stat(lastCheckFile); os.IsNotExist(err) {
		t.Error("Expected cache file to exist after updateCheckTimestamp")
	}
}

func TestPendingUpdatePath(t *testing.T) {
	useTempCache(t)

	path, err := pendingUpdatePath()
	if err != nil {
		t.Fatalf("Failed to get pending update path: %v", err)
	}

	if filepath.Base(path) != pendingUpdateFile {
		t.Errorf("Expected filename %q, got %q", pendingUpdateFile, filepath.Base(path))
	}
}

func TestClearPendingUpdate(t *testing.T) {
	useTempCache(t)

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

	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Fatal("Marker file should exist before clear")
	}

	ClearPendingUpdate()

	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("Marker file should not exist after ClearPendingUpdate")
	}
}

func TestClearPendingUpdateNoFile(t *testing.T) {
	useTempCache(t)
	ClearPendingUpdate()
}

func TestApplyPendingUpdateNoMarker(t *testing.T) {
	useTempCache(t)
	originalVersion := buildinfo.Version
	defer func() { buildinfo.Version = originalVersion }()

	buildinfo.Version = "0.10.0"

	if ApplyPendingUpdate() {
		t.Error("Expected false when no marker exists")
	}
}

func TestApplyPendingUpdateDevBuild(t *testing.T) {
	useTempCache(t)
	originalVersion := buildinfo.Version
	defer func() { buildinfo.Version = originalVersion }()

	buildinfo.Version = "dev"

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

	if ApplyPendingUpdate() {
		t.Error("Expected false for dev build")
	}

	// Marker should still exist (dev builds skip entirely)
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Error("Marker should still exist for dev builds")
	}
}

func TestApplyPendingUpdateDisabled(t *testing.T) {
	useTempCache(t)
	originalVersion := buildinfo.Version
	defer func() { buildinfo.Version = originalVersion }()

	buildinfo.Version = "0.10.0"
	t.Setenv("DISABLE_AUTOUPDATER", "1")

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

	if ApplyPendingUpdate() {
		t.Error("Expected false when disabled")
	}

	// Marker should still exist
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Error("Marker should still exist when autoupdater is disabled")
	}
}

func TestApplyPendingUpdateAlreadyUpToDate(t *testing.T) {
	useTempCache(t)
	originalVersion := buildinfo.Version
	defer func() { buildinfo.Version = originalVersion }()

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

	if ApplyPendingUpdate() {
		t.Error("Expected false when already up to date")
	}

	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("Marker should be removed when version is already at or ahead")
	}
}

func TestApplyPendingUpdateInvalidJSON(t *testing.T) {
	useTempCache(t)
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

	if err := os.WriteFile(markerPath, []byte(`not json`), 0644); err != nil {
		t.Fatalf("Failed to write marker: %v", err)
	}

	if ApplyPendingUpdate() {
		t.Error("Expected false for invalid JSON")
	}

	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("Invalid marker should be removed")
	}
}

func TestMarkerFileFormat(t *testing.T) {
	useTempCache(t)

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
}
