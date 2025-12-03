package artifacts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/utils"
)

// InstalledArtifacts tracks what artifacts have been installed
type InstalledArtifacts struct {
	Version         string              `json:"version"`
	LockFileVersion string              `json:"lockFileVersion"`
	InstalledAt     time.Time           `json:"installedAt"`
	Artifacts       []InstalledArtifact `json:"artifacts"`
}

// InstalledArtifact represents a single installed artifact
type InstalledArtifact struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Type        string `json:"type"`
	InstallPath string `json:"installPath"`
}

// GetTrackerPath returns the path to the installed artifacts tracker
func GetTrackerPath(targetBase string) string {
	return filepath.Join(targetBase, ".claude", ".sleuth-installed.json")
}

// LoadInstalledArtifacts loads the tracker file
func LoadInstalledArtifacts(targetBase string) (*InstalledArtifacts, error) {
	trackerPath := GetTrackerPath(targetBase)

	if !utils.FileExists(trackerPath) {
		return &InstalledArtifacts{
			Version:   "1.0",
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
