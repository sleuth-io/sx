package artifacts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sleuth-io/skills/internal/artifact"
	"github.com/sleuth-io/skills/internal/cache"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/utils"
)

// TrackerFormatVersion is the version of the tracker file format
const TrackerFormatVersion = "1.0"

// InstalledArtifacts tracks what artifacts have been installed
type InstalledArtifacts struct {
	Version         string              `json:"version"`
	LockFileVersion string              `json:"lockFileVersion"`
	InstalledAt     time.Time           `json:"installedAt"`
	Artifacts       []InstalledArtifact `json:"artifacts"`
}

// InstalledArtifact represents a single installed artifact
type InstalledArtifact struct {
	Name        string        `json:"name"`
	Version     string        `json:"version"`
	Type        artifact.Type `json:"type"`
	InstallPath string        `json:"installPath"`
	Clients     []string      `json:"clients"` // Which clients this is installed to
}

// GetTrackerPath returns the path to the installed artifacts tracker
// Now stored in cache directory instead of alongside installed artifacts
func GetTrackerPath(targetBase string) string {
	// Generate a scope key from the targetBase path
	// For global (~/.claude), use "global"
	// For repo-scoped, use a hash of the repo path
	scopeKey := "global"

	// Check if this is the global claude directory
	if homeDir, err := os.UserHomeDir(); err == nil {
		claudeDir := filepath.Join(homeDir, ".claude")
		if targetBase != claudeDir {
			// Not the global claude dir, must be repo-scoped
			scopeKey = utils.URLHash(targetBase)
		}
	} else if !filepath.IsAbs(targetBase) {
		// If we can't get home dir and path is not absolute, it's repo-scoped
		scopeKey = utils.URLHash(targetBase)
	}

	// Get tracker path from cache
	trackerPath, err := cache.GetTrackerCachePath(scopeKey)
	if err != nil {
		// Fallback to old behavior if cache path fails
		return filepath.Join(targetBase, ".skills-installed.json")
	}
	return trackerPath
}

// LoadInstalledArtifacts loads the tracker file
func LoadInstalledArtifacts(targetBase string) (*InstalledArtifacts, error) {
	trackerPath := GetTrackerPath(targetBase)

	if !utils.FileExists(trackerPath) {
		return &InstalledArtifacts{
			Version:   TrackerFormatVersion,
			Artifacts: []InstalledArtifact{},
		}, nil
	}

	data, err := os.ReadFile(trackerPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read tracker: %w", err)
	}

	var tracker InstalledArtifacts
	if err := json.Unmarshal(data, &tracker); err != nil {
		return nil, fmt.Errorf("failed to parse tracker: %w", err)
	}

	return &tracker, nil
}

// SaveInstalledArtifacts saves the tracker file
func SaveInstalledArtifacts(targetBase string, tracker *InstalledArtifacts) error {
	trackerPath := GetTrackerPath(targetBase)

	// Ensure directory exists
	if err := utils.EnsureDir(filepath.Dir(trackerPath)); err != nil {
		return fmt.Errorf("failed to create tracker directory: %w", err)
	}

	data, err := json.MarshalIndent(tracker, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tracker: %w", err)
	}

	if err := os.WriteFile(trackerPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write tracker: %w", err)
	}

	return nil
}

// FindRemovedArtifacts compares previous and current installations
func FindRemovedArtifacts(previous *InstalledArtifacts, current []*lockfile.Artifact) []InstalledArtifact {
	currentMap := make(map[string]bool)
	for _, artifact := range current {
		currentMap[artifact.Name] = true
	}

	var removed []InstalledArtifact
	for _, installed := range previous.Artifacts {
		if !currentMap[installed.Name] {
			removed = append(removed, installed)
		}
	}

	return removed
}

// FindChangedOrNewArtifacts compares previous installation with current lock file
// and returns artifacts that are new or have different versions
func FindChangedOrNewArtifacts(previous *InstalledArtifacts, current []*lockfile.Artifact) []*lockfile.Artifact {
	previousMap := make(map[string]string) // name -> version
	for _, installed := range previous.Artifacts {
		previousMap[installed.Name] = installed.Version
	}

	var changed []*lockfile.Artifact
	for _, artifact := range current {
		prevVersion, exists := previousMap[artifact.Name]
		if !exists || prevVersion != artifact.Version {
			changed = append(changed, artifact)
		}
	}

	return changed
}

// FindArtifactsToInstallForClients returns artifacts that need installation for specific clients
// An artifact needs installation if:
// - It's new (not in previous install)
// - Version changed
// - Not installed to one or more target clients
func FindArtifactsToInstallForClients(previous *InstalledArtifacts, current []*lockfile.Artifact, targetClientIDs []string) []*lockfile.Artifact {
	// Build map of previous installations: name -> artifact
	previousMap := make(map[string]InstalledArtifact)
	for _, installed := range previous.Artifacts {
		previousMap[installed.Name] = installed
	}

	var toInstall []*lockfile.Artifact
	for _, artifact := range current {
		prev, exists := previousMap[artifact.Name]

		// New artifact or version changed
		if !exists || prev.Version != artifact.Version {
			toInstall = append(toInstall, artifact)
			continue
		}

		// Check if installed to all target clients
		installedClients := make(map[string]bool)
		for _, clientID := range prev.Clients {
			installedClients[clientID] = true
		}

		needsInstall := false
		for _, targetClient := range targetClientIDs {
			if !installedClients[targetClient] {
				needsInstall = true
				break
			}
		}

		if needsInstall {
			toInstall = append(toInstall, artifact)
		}
	}

	return toInstall
}

// ValidationResult contains discrepancies between tracker and filesystem
type ValidationResult struct {
	TrackerOnly     []InstalledArtifact // In tracker but not on filesystem
	FilesystemOnly  []InstalledArtifact // On filesystem but not in tracker
	VersionMismatch []VersionMismatch   // Different versions
	Consistent      []InstalledArtifact // Everything matches
}

// VersionMismatch represents a version discrepancy between tracker and filesystem
type VersionMismatch struct {
	Name              string
	TrackerVersion    string
	FilesystemVersion string
	InstallPath       string
}

// ValidateInstalledState compares tracker file with filesystem reality
// Note: Only validates artifacts that preserve metadata (skills, agents, hooks, mcp)
func ValidateInstalledState(targetBase string, tracker *InstalledArtifacts, handlers map[string]interface{}) (*ValidationResult, error) {
	result := &ValidationResult{
		TrackerOnly:     []InstalledArtifact{},
		FilesystemOnly:  []InstalledArtifact{},
		VersionMismatch: []VersionMismatch{},
		Consistent:      []InstalledArtifact{},
	}

	// Scan filesystem for artifacts using handlers
	filesystemMap := make(map[string]InstalledArtifact)

	// Note: handlers parameter would need to be passed in with handler instances
	// For now, this is a placeholder that would be implemented in the install command

	// Build tracker map
	trackerMap := make(map[string]InstalledArtifact)
	for _, artifact := range tracker.Artifacts {
		trackerMap[artifact.Name] = artifact
	}

	// Compare tracker vs filesystem
	for name, tracked := range trackerMap {
		found, exists := filesystemMap[name]
		if !exists {
			result.TrackerOnly = append(result.TrackerOnly, tracked)
		} else if tracked.Version != found.Version {
			result.VersionMismatch = append(result.VersionMismatch, VersionMismatch{
				Name:              name,
				TrackerVersion:    tracked.Version,
				FilesystemVersion: found.Version,
				InstallPath:       found.InstallPath,
			})
		} else {
			result.Consistent = append(result.Consistent, tracked)
		}
	}

	// Find filesystem artifacts not in tracker
	for name, found := range filesystemMap {
		if _, exists := trackerMap[name]; !exists {
			result.FilesystemOnly = append(result.FilesystemOnly, found)
		}
	}

	return result, nil
}

// ReconcileState decides what to do with validation results
// preferTracker=false means filesystem is source of truth (self-healing)
func ReconcileState(validation *ValidationResult, preferTracker bool) *InstalledArtifacts {
	reconciled := &InstalledArtifacts{
		Version:   TrackerFormatVersion,
		Artifacts: []InstalledArtifact{},
	}

	// Always include consistent ones
	reconciled.Artifacts = append(reconciled.Artifacts, validation.Consistent...)

	if preferTracker {
		// Tracker is source of truth - include tracker-only artifacts
		reconciled.Artifacts = append(reconciled.Artifacts, validation.TrackerOnly...)

		// For version mismatches, prefer tracker version
		for _, mismatch := range validation.VersionMismatch {
			reconciled.Artifacts = append(reconciled.Artifacts, InstalledArtifact{
				Name:    mismatch.Name,
				Version: mismatch.TrackerVersion,
			})
		}
	} else {
		// Filesystem is source of truth - include filesystem-only artifacts
		reconciled.Artifacts = append(reconciled.Artifacts, validation.FilesystemOnly...)

		// For version mismatches, prefer filesystem version
		for _, mismatch := range validation.VersionMismatch {
			reconciled.Artifacts = append(reconciled.Artifacts, InstalledArtifact{
				Name:        mismatch.Name,
				Version:     mismatch.FilesystemVersion,
				InstallPath: mismatch.InstallPath,
			})
		}
	}

	return reconciled
}

// TrackerFileInfo contains information about a tracker file
type TrackerFileInfo struct {
	Path     string
	ScopeKey string
}

// ListAllTrackerFiles returns all tracker files in the cache directory
func ListAllTrackerFiles() ([]TrackerFileInfo, error) {
	trackerDir, err := cache.GetTrackerCacheDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get tracker cache dir: %w", err)
	}

	// Check if directory exists
	if _, err := os.Stat(trackerDir); os.IsNotExist(err) {
		return []TrackerFileInfo{}, nil
	}

	entries, err := os.ReadDir(trackerDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read tracker directory: %w", err)
	}

	var trackers []TrackerFileInfo
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			// Extract scope key from filename (remove .json extension)
			scopeKey := entry.Name()[:len(entry.Name())-5]
			trackers = append(trackers, TrackerFileInfo{
				Path:     filepath.Join(trackerDir, entry.Name()),
				ScopeKey: scopeKey,
			})
		}
	}

	return trackers, nil
}

// RemoveArtifactsFromTracker removes specified artifacts from the tracker
// Returns the updated tracker, or nil if no artifacts remain
func RemoveArtifactsFromTracker(tracker *InstalledArtifacts, names []string) *InstalledArtifacts {
	if tracker == nil {
		return nil
	}

	// Build a set of names to remove
	removeSet := make(map[string]bool)
	for _, name := range names {
		removeSet[name] = true
	}

	// Filter out artifacts to be removed
	var remaining []InstalledArtifact
	for _, artifact := range tracker.Artifacts {
		if !removeSet[artifact.Name] {
			remaining = append(remaining, artifact)
		}
	}

	// Return nil if no artifacts remain
	if len(remaining) == 0 {
		return nil
	}

	// Return updated tracker
	return &InstalledArtifacts{
		Version:         tracker.Version,
		LockFileVersion: tracker.LockFileVersion,
		InstalledAt:     tracker.InstalledAt,
		Artifacts:       remaining,
	}
}

// DeleteTracker removes a tracker file completely
func DeleteTracker(trackerPath string) error {
	if !utils.FileExists(trackerPath) {
		// Already deleted, not an error
		return nil
	}

	if err := os.Remove(trackerPath); err != nil {
		return fmt.Errorf("failed to delete tracker: %w", err)
	}

	return nil
}
