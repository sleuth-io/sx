package assets

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/lockfile"
)

func TestGetTrackerPath(t *testing.T) {
	got, err := GetTrackerPath()
	if err != nil {
		t.Fatalf("GetTrackerPath() error = %v", err)
	}

	// Verify it ends with sx/installed.json. The parent cache dir is platform-
	// specific (.cache on Linux, Library/Caches on macOS, LocalAppData on
	// Windows) so assert only the suffix we always control.
	wantSuffix := filepath.Join("sx", "installed.json")
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("GetTrackerPath() = %q, want path ending with %q", got, wantSuffix)
	}
}

func TestTrackerOperations(t *testing.T) {
	// Create a fresh in-memory tracker for testing (don't load from disk)
	tracker := &Tracker{
		Version: TrackerFormatVersion,
		Assets:  []InstalledAsset{},
	}

	// Verify tracker starts empty
	if len(tracker.Assets) != 0 {
		t.Errorf("Expected empty tracker, got %d assets", len(tracker.Assets))
	}

	// Test upserting asset
	asset := InstalledAsset{
		Name:       "test-skill",
		Version:    "1.0.0",
		Repository: "",
		Path:       "",
		Clients:    []string{"claude-code"},
	}
	tracker.UpsertAsset(asset)

	if len(tracker.Assets) != 1 {
		t.Errorf("Expected 1 asset after upsert, got %d", len(tracker.Assets))
	}

	// Test find asset
	key := AssetKey{Name: "test-skill", Repository: "", Path: ""}
	found := tracker.FindAsset(key)
	if found == nil {
		t.Errorf("FindAsset() returned nil, expected asset")
	} else if found.Version != "1.0.0" {
		t.Errorf("FindAsset() version = %s, want 1.0.0", found.Version)
	}

	// Test IsGlobal
	if !found.IsGlobal() {
		t.Errorf("IsGlobal() = false, want true")
	}

	// Test scope description
	if found.ScopeDescription() != "Global" {
		t.Errorf("ScopeDescription() = %s, want 'Global'", found.ScopeDescription())
	}

	// Test repo-scoped asset
	repoAsset := InstalledAsset{
		Name:       "repo-skill",
		Version:    "2.0.0",
		Repository: "git@github.com:org/repo.git",
		Path:       "",
		Clients:    []string{"cursor"},
	}
	tracker.UpsertAsset(repoAsset)

	repoKey := AssetKey{Name: "repo-skill", Repository: "git@github.com:org/repo.git", Path: ""}
	foundRepo := tracker.FindAsset(repoKey)
	if foundRepo == nil {
		t.Errorf("FindAsset() for repo asset returned nil")
	} else {
		if foundRepo.IsGlobal() {
			t.Errorf("IsGlobal() = true for repo-scoped asset, want false")
		}
		if foundRepo.ScopeDescription() != "git@github.com:org/repo.git" {
			t.Errorf("ScopeDescription() = %s, want repo URL", foundRepo.ScopeDescription())
		}
	}

	// Test path-scoped asset
	pathAsset := InstalledAsset{
		Name:       "path-skill",
		Version:    "3.0.0",
		Repository: "git@github.com:org/repo.git",
		Path:       "/services/api",
		Clients:    []string{"claude-code", "cursor"},
	}
	tracker.UpsertAsset(pathAsset)

	pathKey := AssetKey{Name: "path-skill", Repository: "git@github.com:org/repo.git", Path: "/services/api"}
	foundPath := tracker.FindAsset(pathKey)
	if foundPath == nil {
		t.Errorf("FindAsset() for path asset returned nil")
	} else {
		if foundPath.IsGlobal() {
			t.Errorf("IsGlobal() = true for path-scoped asset, want false")
		}
		expectedDesc := "git@github.com:org/repo.git:/services/api"
		if foundPath.ScopeDescription() != expectedDesc {
			t.Errorf("ScopeDescription() = %s, want %s", foundPath.ScopeDescription(), expectedDesc)
		}
	}

	// Test remove asset
	removed := tracker.RemoveAsset(key)
	if !removed {
		t.Errorf("RemoveAsset() = false, want true")
	}
	if len(tracker.Assets) != 2 {
		t.Errorf("Expected 2 assets after remove, got %d", len(tracker.Assets))
	}

	// Test NeedsInstall
	if !tracker.NeedsInstall(key, "1.0.0", []string{"claude-code"}) {
		t.Errorf("NeedsInstall() = false for removed asset, want true")
	}
	if tracker.NeedsInstall(repoKey, "2.0.0", []string{"cursor"}) {
		t.Errorf("NeedsInstall() = true for existing asset with same version/clients, want false")
	}
	if !tracker.NeedsInstall(repoKey, "2.1.0", []string{"cursor"}) {
		t.Errorf("NeedsInstall() = false for asset with different version, want true")
	}

	// Test GroupByScope
	grouped := tracker.GroupByScope()
	if len(grouped) != 2 {
		t.Errorf("GroupByScope() returned %d groups, want 2", len(grouped))
	}

	// Test FindByScope
	repoScoped := tracker.FindByScope("git@github.com:org/repo.git", "")
	if len(repoScoped) != 1 {
		t.Errorf("FindByScope() for repo returned %d assets, want 1", len(repoScoped))
	}
}

func TestFindAssetWithMatcher(t *testing.T) {
	tracker := &Tracker{
		Version: TrackerFormatVersion,
		Assets: []InstalledAsset{
			{
				Name:       "global-skill",
				Version:    "1.0.0",
				Repository: "",
				Path:       "",
				Clients:    []string{"claude-code"},
			},
			{
				Name:       "repo-skill",
				Version:    "2.0.0",
				Repository: "git@github.com:org/repo.git",
				Path:       "",
				Clients:    []string{"cursor"},
			},
			{
				Name:       "path-skill",
				Version:    "3.0.0",
				Repository: "git@github.com:org/repo.git",
				Path:       "/services/api",
				Clients:    []string{"claude-code"},
			},
		},
	}

	// Matcher that normalizes SSH and HTTPS URLs
	matchRepo := func(a, b string) bool {
		// Simple normalization for test
		normalize := func(url string) string {
			url = strings.TrimSuffix(url, ".git")
			url = strings.Replace(url, "git@github.com:", "github.com/", 1)
			url = strings.Replace(url, "https://github.com/", "github.com/", 1)
			return url
		}
		return normalize(a) == normalize(b)
	}

	tests := []struct {
		name      string
		assetName string
		repoURL   string
		path      string
		wantName  string
	}{
		{
			name:      "find global asset",
			assetName: "global-skill",
			repoURL:   "",
			path:      "",
			wantName:  "global-skill",
		},
		{
			name:      "find repo asset with same URL format",
			assetName: "repo-skill",
			repoURL:   "git@github.com:org/repo.git",
			path:      "",
			wantName:  "repo-skill",
		},
		{
			name:      "find repo asset with different URL format (HTTPS)",
			assetName: "repo-skill",
			repoURL:   "https://github.com/org/repo",
			path:      "",
			wantName:  "repo-skill",
		},
		{
			name:      "find path asset with different URL format",
			assetName: "path-skill",
			repoURL:   "https://github.com/org/repo.git",
			path:      "/services/api",
			wantName:  "path-skill",
		},
		{
			name:      "not found - wrong path",
			assetName: "path-skill",
			repoURL:   "https://github.com/org/repo",
			path:      "/wrong/path",
			wantName:  "",
		},
		{
			name:      "not found - wrong name",
			assetName: "nonexistent",
			repoURL:   "",
			path:      "",
			wantName:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tracker.FindAssetWithMatcher(tt.assetName, tt.repoURL, tt.path, matchRepo)
			if tt.wantName == "" {
				if got != nil {
					t.Errorf("FindAssetWithMatcher() = %v, want nil", got.Name)
				}
			} else {
				if got == nil {
					t.Errorf("FindAssetWithMatcher() = nil, want %s", tt.wantName)
				} else if got.Name != tt.wantName {
					t.Errorf("FindAssetWithMatcher() = %s, want %s", got.Name, tt.wantName)
				}
			}
		})
	}
}

func TestNewAssetKey(t *testing.T) {
	tests := []struct {
		name      string
		assetName string
		scopeType lockfile.ScopeType
		repoURL   string
		repoPath  string
		want      AssetKey
	}{
		{
			name:      "global scope",
			assetName: "test",
			scopeType: lockfile.ScopeGlobal,
			repoURL:   "https://github.com/org/repo.git",
			repoPath:  "/path",
			want:      AssetKey{Name: "test", Repository: "", Path: ""},
		},
		{
			name:      "repo scope",
			assetName: "test",
			scopeType: lockfile.ScopeRepo,
			repoURL:   "https://github.com/org/repo.git",
			repoPath:  "/path",
			want:      AssetKey{Name: "test", Repository: "https://github.com/org/repo.git", Path: ""},
		},
		{
			name:      "path scope",
			assetName: "test",
			scopeType: lockfile.ScopePath,
			repoURL:   "https://github.com/org/repo.git",
			repoPath:  "/path",
			want:      AssetKey{Name: "test", Repository: "https://github.com/org/repo.git", Path: "/path"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewAssetKey(tt.assetName, tt.scopeType, tt.repoURL, tt.repoPath)
			if got != tt.want {
				t.Errorf("NewAssetKey() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
