package assets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/skills/internal/cache"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/utils"
)

// TrackerFormatVersion is the version of the tracker file format
const TrackerFormatVersion = "3"

// Tracker tracks all installed artifacts across all scopes
type Tracker struct {
	Version   string              `json:"version"`
	Artifacts []InstalledArtifact `json:"artifacts"`
}

// InstalledArtifact represents a single installed artifact with its scope
type InstalledArtifact struct {
	Name       string   `json:"name"`
	Version    string   `json:"version"`
	Type       string   `json:"type,omitempty"`       // Artifact type (skill, agent, mcp, etc) - added in v3
	Repository string   `json:"repository,omitempty"` // Empty for global scope
	Path       string   `json:"path,omitempty"`       // Path within repo (if path-scoped)
	Clients    []string `json:"clients"`
}

// ArtifactKey uniquely identifies an artifact by name + scope
type ArtifactKey struct {
	Name       string
	Repository string
	Path       string
}

// NewArtifactKey creates a key from name and scope
func NewArtifactKey(name string, scopeType lockfile.ScopeType, repoURL, repoPath string) ArtifactKey {
	key := ArtifactKey{Name: name}
	if scopeType == lockfile.ScopeRepo || scopeType == lockfile.ScopePath {
		key.Repository = repoURL
	}
	if scopeType == lockfile.ScopePath {
		key.Path = repoPath
	}
	return key
}

// Key returns the unique key for this artifact
func (a *InstalledArtifact) Key() ArtifactKey {
	return ArtifactKey{
		Name:       a.Name,
		Repository: a.Repository,
		Path:       a.Path,
	}
}

// IsGlobal returns true if this artifact is installed globally
func (a *InstalledArtifact) IsGlobal() bool {
	return a.Repository == ""
}

// ScopeDescription returns a human-readable scope description
func (a *InstalledArtifact) ScopeDescription() string {
	if a.Repository == "" {
		return "Global"
	}
	if a.Path != "" {
		return fmt.Sprintf("%s:%s", a.Repository, a.Path)
	}
	return a.Repository
}

// GetTrackerPath returns the path to the single tracker file
func GetTrackerPath() (string, error) {
	cacheDir, err := cache.GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "installed.json"), nil
}

// LoadTracker loads the tracker file
func LoadTracker() (*Tracker, error) {
	trackerPath, err := GetTrackerPath()
	if err != nil {
		return nil, err
	}

	if !utils.FileExists(trackerPath) {
		return &Tracker{
			Version:   TrackerFormatVersion,
			Artifacts: []InstalledArtifact{},
		}, nil
	}

	data, err := os.ReadFile(trackerPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read tracker: %w", err)
	}

	var tracker Tracker
	if err := json.Unmarshal(data, &tracker); err != nil {
		return nil, fmt.Errorf("failed to parse tracker: %w", err)
	}

	return &tracker, nil
}

// SaveTracker saves the tracker file
func SaveTracker(tracker *Tracker) error {
	trackerPath, err := GetTrackerPath()
	if err != nil {
		return err
	}

	// Ensure directory exists
	if err := utils.EnsureDir(filepath.Dir(trackerPath)); err != nil {
		return fmt.Errorf("failed to create tracker directory: %w", err)
	}

	tracker.Version = TrackerFormatVersion

	data, err := json.MarshalIndent(tracker, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tracker: %w", err)
	}

	if err := os.WriteFile(trackerPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write tracker: %w", err)
	}

	return nil
}

// FindArtifact finds an artifact by key in the tracker
func (t *Tracker) FindArtifact(key ArtifactKey) *InstalledArtifact {
	for i := range t.Artifacts {
		if t.Artifacts[i].Key() == key {
			return &t.Artifacts[i]
		}
	}
	return nil
}

// FindArtifactWithMatcher finds an artifact by name using a custom repo URL matcher function
// This is useful when the tracker URL format may differ from the search URL (e.g., SSH vs HTTPS)
func (t *Tracker) FindArtifactWithMatcher(name, repoURL, path string, matchRepo func(a, b string) bool) *InstalledArtifact {
	for i := range t.Artifacts {
		a := &t.Artifacts[i]
		if a.Name != name {
			continue
		}
		// Check if repo matches (using provided matcher for URL normalization)
		if a.Repository == "" && repoURL == "" {
			// Both global
			if a.Path == path {
				return a
			}
		} else if a.Repository != "" && repoURL != "" && matchRepo(a.Repository, repoURL) {
			// Both repo-scoped and repos match
			if a.Path == path {
				return a
			}
		}
	}
	return nil
}

// FindByScope returns all artifacts matching a specific scope
func (t *Tracker) FindByScope(repository, path string) []InstalledArtifact {
	var result []InstalledArtifact
	for _, a := range t.Artifacts {
		if a.Repository == repository && a.Path == path {
			result = append(result, a)
		}
	}
	return result
}

// FindGlobal returns all globally-scoped artifacts
func (t *Tracker) FindGlobal() []InstalledArtifact {
	return t.FindByScope("", "")
}

// FindForScope returns artifacts relevant to a given scope:
// - All global artifacts
// - Artifacts matching the repo (with URL normalization)
// - For path scopes, artifacts whose path contains or equals the current path
func (t *Tracker) FindForScope(repoURL, repoPath string, matchRepo func(a, b string) bool) []InstalledArtifact {
	var result []InstalledArtifact
	for _, a := range t.Artifacts {
		// Include global artifacts
		if a.IsGlobal() {
			result = append(result, a)
			continue
		}
		// Check repo match using provided matcher
		if a.Repository != "" && matchRepo(a.Repository, repoURL) {
			// If artifact has no path restriction, include it
			if a.Path == "" {
				result = append(result, a)
				continue
			}
			// If artifact has path, check if current path is within it
			if repoPath != "" && (strings.HasPrefix(repoPath, a.Path) || repoPath == a.Path) {
				result = append(result, a)
			}
		}
	}
	return result
}

// UpsertArtifact adds or updates an artifact in the tracker
func (t *Tracker) UpsertArtifact(artifact InstalledArtifact) {
	key := artifact.Key()
	for i := range t.Artifacts {
		if t.Artifacts[i].Key() == key {
			t.Artifacts[i] = artifact
			return
		}
	}
	t.Artifacts = append(t.Artifacts, artifact)
}

// RemoveArtifact removes an artifact from the tracker by key
func (t *Tracker) RemoveArtifact(key ArtifactKey) bool {
	for i := range t.Artifacts {
		if t.Artifacts[i].Key() == key {
			t.Artifacts = append(t.Artifacts[:i], t.Artifacts[i+1:]...)
			return true
		}
	}
	return false
}

// RemoveByScope removes all artifacts for a specific scope
func (t *Tracker) RemoveByScope(repository, path string) int {
	var remaining []InstalledArtifact
	removed := 0
	for _, a := range t.Artifacts {
		if a.Repository == repository && a.Path == path {
			removed++
		} else {
			remaining = append(remaining, a)
		}
	}
	t.Artifacts = remaining
	return removed
}

// GroupByScope returns artifacts grouped by their scope
func (t *Tracker) GroupByScope() map[string][]InstalledArtifact {
	result := make(map[string][]InstalledArtifact)
	for _, a := range t.Artifacts {
		scope := a.ScopeDescription()
		result[scope] = append(result[scope], a)
	}
	return result
}

// NeedsInstall checks if an artifact needs to be installed or updated
// Returns true if the artifact is new, has a different version, or is missing clients
func (t *Tracker) NeedsInstall(key ArtifactKey, version string, targetClients []string) bool {
	existing := t.FindArtifact(key)

	if existing == nil {
		return true // New artifact
	}

	if existing.Version != version {
		return true // Version changed
	}

	// Check if all target clients are covered
	installedClients := make(map[string]bool)
	for _, c := range existing.Clients {
		installedClients[c] = true
	}

	for _, c := range targetClients {
		if !installedClients[c] {
			return true // Missing a client
		}
	}

	return false
}

// DeleteTracker removes the tracker file completely
func DeleteTracker() error {
	trackerPath, err := GetTrackerPath()
	if err != nil {
		return err
	}

	if !utils.FileExists(trackerPath) {
		return nil
	}

	if err := os.Remove(trackerPath); err != nil {
		return fmt.Errorf("failed to delete tracker: %w", err)
	}

	return nil
}
