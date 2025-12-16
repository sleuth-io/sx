package assets

import (
	"strings"
	"testing"

	"github.com/sleuth-io/skills/internal/lockfile"
)

func TestGetTrackerPath(t *testing.T) {
	got, err := GetTrackerPath()
	if err != nil {
		t.Fatalf("GetTrackerPath() error = %v", err)
	}

	// Verify it's in the cache directory
	if !strings.Contains(got, ".cache/sx/installed.json") {
		t.Errorf("GetTrackerPath() = %q, want path containing '.cache/sx/installed.json'", got)
	}

	// Verify it ends with .json
	if !strings.HasSuffix(got, ".json") {
		t.Errorf("GetTrackerPath() = %q, want path ending with .json", got)
	}
}

func TestTrackerOperations(t *testing.T) {
	// Create a fresh in-memory tracker for testing (don't load from disk)
	tracker := &Tracker{
		Version:   TrackerFormatVersion,
		Artifacts: []InstalledArtifact{},
	}

	// Verify tracker starts empty
	if len(tracker.Artifacts) != 0 {
		t.Errorf("Expected empty tracker, got %d artifacts", len(tracker.Artifacts))
	}

	// Test upserting artifact
	artifact := InstalledArtifact{
		Name:       "test-skill",
		Version:    "1.0.0",
		Repository: "",
		Path:       "",
		Clients:    []string{"claude-code"},
	}
	tracker.UpsertArtifact(artifact)

	if len(tracker.Artifacts) != 1 {
		t.Errorf("Expected 1 artifact after upsert, got %d", len(tracker.Artifacts))
	}

	// Test find artifact
	key := ArtifactKey{Name: "test-skill", Repository: "", Path: ""}
	found := tracker.FindArtifact(key)
	if found == nil {
		t.Errorf("FindArtifact() returned nil, expected artifact")
	} else if found.Version != "1.0.0" {
		t.Errorf("FindArtifact() version = %s, want 1.0.0", found.Version)
	}

	// Test IsGlobal
	if !found.IsGlobal() {
		t.Errorf("IsGlobal() = false, want true")
	}

	// Test scope description
	if found.ScopeDescription() != "Global" {
		t.Errorf("ScopeDescription() = %s, want 'Global'", found.ScopeDescription())
	}

	// Test repo-scoped artifact
	repoArtifact := InstalledArtifact{
		Name:       "repo-skill",
		Version:    "2.0.0",
		Repository: "git@github.com:org/repo.git",
		Path:       "",
		Clients:    []string{"cursor"},
	}
	tracker.UpsertArtifact(repoArtifact)

	repoKey := ArtifactKey{Name: "repo-skill", Repository: "git@github.com:org/repo.git", Path: ""}
	foundRepo := tracker.FindArtifact(repoKey)
	if foundRepo == nil {
		t.Errorf("FindArtifact() for repo artifact returned nil")
	} else {
		if foundRepo.IsGlobal() {
			t.Errorf("IsGlobal() = true for repo-scoped artifact, want false")
		}
		if foundRepo.ScopeDescription() != "git@github.com:org/repo.git" {
			t.Errorf("ScopeDescription() = %s, want repo URL", foundRepo.ScopeDescription())
		}
	}

	// Test path-scoped artifact
	pathArtifact := InstalledArtifact{
		Name:       "path-skill",
		Version:    "3.0.0",
		Repository: "git@github.com:org/repo.git",
		Path:       "/services/api",
		Clients:    []string{"claude-code", "cursor"},
	}
	tracker.UpsertArtifact(pathArtifact)

	pathKey := ArtifactKey{Name: "path-skill", Repository: "git@github.com:org/repo.git", Path: "/services/api"}
	foundPath := tracker.FindArtifact(pathKey)
	if foundPath == nil {
		t.Errorf("FindArtifact() for path artifact returned nil")
	} else {
		if foundPath.IsGlobal() {
			t.Errorf("IsGlobal() = true for path-scoped artifact, want false")
		}
		expectedDesc := "git@github.com:org/repo.git:/services/api"
		if foundPath.ScopeDescription() != expectedDesc {
			t.Errorf("ScopeDescription() = %s, want %s", foundPath.ScopeDescription(), expectedDesc)
		}
	}

	// Test remove artifact
	removed := tracker.RemoveArtifact(key)
	if !removed {
		t.Errorf("RemoveArtifact() = false, want true")
	}
	if len(tracker.Artifacts) != 2 {
		t.Errorf("Expected 2 artifacts after remove, got %d", len(tracker.Artifacts))
	}

	// Test NeedsInstall
	if !tracker.NeedsInstall(key, "1.0.0", []string{"claude-code"}) {
		t.Errorf("NeedsInstall() = false for removed artifact, want true")
	}
	if tracker.NeedsInstall(repoKey, "2.0.0", []string{"cursor"}) {
		t.Errorf("NeedsInstall() = true for existing artifact with same version/clients, want false")
	}
	if !tracker.NeedsInstall(repoKey, "2.1.0", []string{"cursor"}) {
		t.Errorf("NeedsInstall() = false for artifact with different version, want true")
	}

	// Test GroupByScope
	grouped := tracker.GroupByScope()
	if len(grouped) != 2 {
		t.Errorf("GroupByScope() returned %d groups, want 2", len(grouped))
	}

	// Test FindByScope
	repoScoped := tracker.FindByScope("git@github.com:org/repo.git", "")
	if len(repoScoped) != 1 {
		t.Errorf("FindByScope() for repo returned %d artifacts, want 1", len(repoScoped))
	}
}

func TestFindArtifactWithMatcher(t *testing.T) {
	tracker := &Tracker{
		Version: TrackerFormatVersion,
		Artifacts: []InstalledArtifact{
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
		name     string
		artName  string
		repoURL  string
		path     string
		wantName string
	}{
		{
			name:     "find global artifact",
			artName:  "global-skill",
			repoURL:  "",
			path:     "",
			wantName: "global-skill",
		},
		{
			name:     "find repo artifact with same URL format",
			artName:  "repo-skill",
			repoURL:  "git@github.com:org/repo.git",
			path:     "",
			wantName: "repo-skill",
		},
		{
			name:     "find repo artifact with different URL format (HTTPS)",
			artName:  "repo-skill",
			repoURL:  "https://github.com/org/repo",
			path:     "",
			wantName: "repo-skill",
		},
		{
			name:     "find path artifact with different URL format",
			artName:  "path-skill",
			repoURL:  "https://github.com/org/repo.git",
			path:     "/services/api",
			wantName: "path-skill",
		},
		{
			name:     "not found - wrong path",
			artName:  "path-skill",
			repoURL:  "https://github.com/org/repo",
			path:     "/wrong/path",
			wantName: "",
		},
		{
			name:     "not found - wrong name",
			artName:  "nonexistent",
			repoURL:  "",
			path:     "",
			wantName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tracker.FindArtifactWithMatcher(tt.artName, tt.repoURL, tt.path, matchRepo)
			if tt.wantName == "" {
				if got != nil {
					t.Errorf("FindArtifactWithMatcher() = %v, want nil", got.Name)
				}
			} else {
				if got == nil {
					t.Errorf("FindArtifactWithMatcher() = nil, want %s", tt.wantName)
				} else if got.Name != tt.wantName {
					t.Errorf("FindArtifactWithMatcher() = %s, want %s", got.Name, tt.wantName)
				}
			}
		})
	}
}

func TestNewArtifactKey(t *testing.T) {
	tests := []struct {
		name      string
		artName   string
		scopeType lockfile.ScopeType
		repoURL   string
		repoPath  string
		want      ArtifactKey
	}{
		{
			name:      "global scope",
			artName:   "test",
			scopeType: lockfile.ScopeGlobal,
			repoURL:   "https://github.com/org/repo.git",
			repoPath:  "/path",
			want:      ArtifactKey{Name: "test", Repository: "", Path: ""},
		},
		{
			name:      "repo scope",
			artName:   "test",
			scopeType: lockfile.ScopeRepo,
			repoURL:   "https://github.com/org/repo.git",
			repoPath:  "/path",
			want:      ArtifactKey{Name: "test", Repository: "https://github.com/org/repo.git", Path: ""},
		},
		{
			name:      "path scope",
			artName:   "test",
			scopeType: lockfile.ScopePath,
			repoURL:   "https://github.com/org/repo.git",
			repoPath:  "/path",
			want:      ArtifactKey{Name: "test", Repository: "https://github.com/org/repo.git", Path: "/path"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewArtifactKey(tt.artName, tt.scopeType, tt.repoURL, tt.repoPath)
			if got != tt.want {
				t.Errorf("NewArtifactKey() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
