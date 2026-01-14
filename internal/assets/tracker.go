package assets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/cache"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/utils"
)

// TrackerFormatVersion is the version of the tracker file format
const TrackerFormatVersion = "3"

// Tracker tracks all installed assets across all scopes
type Tracker struct {
	Version string           `json:"version"`
	Assets  []InstalledAsset `json:"assets"`
}

// InstalledAsset represents a single installed asset with its scope
// (formerly InstalledArtifact)
type InstalledAsset struct {
	Name       string            `json:"name"`
	Version    string            `json:"version"`
	Type       string            `json:"type,omitempty"`       // Asset type (skill, agent, mcp, etc) - added in v3
	Repository string            `json:"repository,omitempty"` // Empty for global scope
	Path       string            `json:"path,omitempty"`       // Path within repo (if path-scoped)
	Clients    []string          `json:"clients"`
	Config     map[string]string `json:"config,omitempty"` // Type-specific config (e.g., marketplace for plugins)
}

// AssetKey uniquely identifies an asset by name + scope
// (formerly ArtifactKey)
type AssetKey struct {
	Name       string
	Repository string
	Path       string
}

// NewAssetKey creates a key from name and scope
func NewAssetKey(name string, scopeType lockfile.ScopeType, repoURL, repoPath string) AssetKey {
	key := AssetKey{Name: name}
	if scopeType == lockfile.ScopeRepo || scopeType == lockfile.ScopePath {
		key.Repository = repoURL
	}
	if scopeType == lockfile.ScopePath {
		key.Path = repoPath
	}
	return key
}

// Key returns the unique key for this asset
func (a *InstalledAsset) Key() AssetKey {
	return AssetKey{
		Name:       a.Name,
		Repository: a.Repository,
		Path:       a.Path,
	}
}

// IsGlobal returns true if this asset is installed globally
func (a *InstalledAsset) IsGlobal() bool {
	return a.Repository == ""
}

// ScopeDescription returns a human-readable scope description
func (a *InstalledAsset) ScopeDescription() string {
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

// trackerCompat is used for parsing old tracker files with "artifacts" field
type trackerCompat struct {
	Version   string           `json:"version"`
	Artifacts []InstalledAsset `json:"artifacts"` // Old name for backwards compatibility
}

// LoadTracker loads the tracker file
// Supports both new "assets" and old "artifacts" field names
func LoadTracker() (*Tracker, error) {
	trackerPath, err := GetTrackerPath()
	if err != nil {
		return nil, err
	}

	if !utils.FileExists(trackerPath) {
		return &Tracker{
			Version: TrackerFormatVersion,
			Assets:  []InstalledAsset{},
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

	// Check if we got data from new "assets" field
	if len(tracker.Assets) == 0 {
		// Try parsing with old "artifacts" field name
		var compat trackerCompat
		if err := json.Unmarshal(data, &compat); err == nil && len(compat.Artifacts) > 0 {
			tracker.Version = compat.Version
			tracker.Assets = compat.Artifacts
		}
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

// FindAsset finds an asset by key in the tracker
func (t *Tracker) FindAsset(key AssetKey) *InstalledAsset {
	for i := range t.Assets {
		if t.Assets[i].Key() == key {
			return &t.Assets[i]
		}
	}
	return nil
}

// FindAssetWithMatcher finds an asset by name using a custom repo URL matcher function
// This is useful when the tracker URL format may differ from the search URL (e.g., SSH vs HTTPS)
func (t *Tracker) FindAssetWithMatcher(name, repoURL, path string, matchRepo func(a, b string) bool) *InstalledAsset {
	for i := range t.Assets {
		a := &t.Assets[i]
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

// FindByScope returns all assets matching a specific scope
func (t *Tracker) FindByScope(repository, path string) []InstalledAsset {
	var result []InstalledAsset
	for _, a := range t.Assets {
		if a.Repository == repository && a.Path == path {
			result = append(result, a)
		}
	}
	return result
}

// FindGlobal returns all globally-scoped assets
func (t *Tracker) FindGlobal() []InstalledAsset {
	return t.FindByScope("", "")
}

// FindForScope returns assets relevant to a given scope:
// - All global assets
// - Assets matching the repo (with URL normalization)
// - For path scopes, assets whose path contains or equals the current path
func (t *Tracker) FindForScope(repoURL, repoPath string, matchRepo func(a, b string) bool) []InstalledAsset {
	var result []InstalledAsset
	for _, a := range t.Assets {
		// Include global assets
		if a.IsGlobal() {
			result = append(result, a)
			continue
		}
		// Check repo match using provided matcher
		if a.Repository != "" && matchRepo(a.Repository, repoURL) {
			// If asset has no path restriction, include it
			if a.Path == "" {
				result = append(result, a)
				continue
			}
			// If asset has path, check if current path is within it
			if repoPath != "" && (strings.HasPrefix(repoPath, a.Path) || repoPath == a.Path) {
				result = append(result, a)
			}
		}
	}
	return result
}

// UpsertAsset adds or updates an asset in the tracker
func (t *Tracker) UpsertAsset(asset InstalledAsset) {
	key := asset.Key()
	for i := range t.Assets {
		if t.Assets[i].Key() == key {
			t.Assets[i] = asset
			return
		}
	}
	t.Assets = append(t.Assets, asset)
}

// RemoveAsset removes an asset from the tracker by key
func (t *Tracker) RemoveAsset(key AssetKey) bool {
	for i := range t.Assets {
		if t.Assets[i].Key() == key {
			t.Assets = append(t.Assets[:i], t.Assets[i+1:]...)
			return true
		}
	}
	return false
}

// RemoveByScope removes all assets for a specific scope
func (t *Tracker) RemoveByScope(repository, path string) int {
	var remaining []InstalledAsset
	removed := 0
	for _, a := range t.Assets {
		if a.Repository == repository && a.Path == path {
			removed++
		} else {
			remaining = append(remaining, a)
		}
	}
	t.Assets = remaining
	return removed
}

// GroupByScope returns assets grouped by their scope
func (t *Tracker) GroupByScope() map[string][]InstalledAsset {
	result := make(map[string][]InstalledAsset)
	for _, a := range t.Assets {
		scope := a.ScopeDescription()
		result[scope] = append(result[scope], a)
	}
	return result
}

// NeedsInstall checks if an asset needs to be installed or updated
// Returns true if the asset is new, has a different version, or is missing clients
func (t *Tracker) NeedsInstall(key AssetKey, version string, targetClients []string) bool {
	existing := t.FindAsset(key)

	if existing == nil {
		return true // New asset
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
