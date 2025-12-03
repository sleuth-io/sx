package scope

import (
	"path/filepath"
	"testing"

	"github.com/sleuth-io/skills/internal/lockfile"
)

func TestMatchesArtifact(t *testing.T) {
	tests := []struct {
		name     string
		scope    *Scope
		artifact *lockfile.Artifact
		want     bool
	}{
		{
			name: "global artifact always matches",
			scope: &Scope{
				Type: "global",
			},
			artifact: &lockfile.Artifact{
				Name:         "test",
				Repositories: []lockfile.Repository{}, // Empty = global
			},
			want: true,
		},
		{
			name: "repo artifact matches when in same repo",
			scope: &Scope{
				Type:    "repo",
				RepoURL: "https://github.com/test/repo",
			},
			artifact: &lockfile.Artifact{
				Name: "test",
				Repositories: []lockfile.Repository{
					{Repo: "https://github.com/test/repo"},
				},
			},
			want: true,
		},
		{
			name: "repo artifact doesn't match from global scope",
			scope: &Scope{
				Type: "global",
			},
			artifact: &lockfile.Artifact{
				Name: "test",
				Repositories: []lockfile.Repository{
					{Repo: "https://github.com/test/repo"},
				},
			},
			want: false,
		},
		{
			name: "path artifact matches when in matching path",
			scope: &Scope{
				Type:     "path",
				RepoURL:  "https://github.com/test/repo",
				RepoPath: "src/components",
			},
			artifact: &lockfile.Artifact{
				Name: "test",
				Repositories: []lockfile.Repository{
					{Repo: "https://github.com/test/repo", Paths: []string{"src/components"}},
				},
			},
			want: true,
		},
		{
			name: "path artifact doesn't match when in different path",
			scope: &Scope{
				Type:     "path",
				RepoURL:  "https://github.com/test/repo",
				RepoPath: "src/utils",
			},
			artifact: &lockfile.Artifact{
				Name: "test",
				Repositories: []lockfile.Repository{
					{Repo: "https://github.com/test/repo", Paths: []string{"src/components"}},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher := NewMatcher(tt.scope)
			if got := matcher.MatchesArtifact(tt.artifact); got != tt.want {
				t.Errorf("MatchesArtifact() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetInstallLocations(t *testing.T) {
	repoRoot := "/home/user/repo"
	globalBase := "/home/user/.claude"

	tests := []struct {
		name      string
		artifact  *lockfile.Artifact
		scope     *Scope
		wantPaths []string
	}{
		{
			name: "global artifact",
			artifact: &lockfile.Artifact{
				Name:         "test",
				Repositories: []lockfile.Repository{},
			},
			scope: &Scope{
				Type: "global",
			},
			wantPaths: []string{globalBase},
		},
		{
			name: "repo artifact",
			artifact: &lockfile.Artifact{
				Name: "test",
				Repositories: []lockfile.Repository{
					{Repo: "https://github.com/test/repo"},
				},
			},
			scope: &Scope{
				Type:    "repo",
				RepoURL: "https://github.com/test/repo",
			},
			wantPaths: []string{filepath.Join(repoRoot, ".claude")},
		},
		{
			name: "path artifact",
			artifact: &lockfile.Artifact{
				Name: "test",
				Repositories: []lockfile.Repository{
					{Repo: "https://github.com/test/repo", Paths: []string{"src/components"}},
				},
			},
			scope: &Scope{
				Type:     "path",
				RepoURL:  "https://github.com/test/repo",
				RepoPath: "src/components",
			},
			wantPaths: []string{filepath.Join(repoRoot, "src/components", ".claude")},
		},
		{
			name: "multiple paths",
			artifact: &lockfile.Artifact{
				Name: "test",
				Repositories: []lockfile.Repository{
					{Repo: "https://github.com/test/repo", Paths: []string{"src/components", "src/utils"}},
				},
			},
			scope: &Scope{
				Type:     "path",
				RepoURL:  "https://github.com/test/repo",
				RepoPath: "src/components",
			},
			wantPaths: []string{filepath.Join(repoRoot, "src/components", ".claude")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetInstallLocations(tt.artifact, tt.scope, repoRoot, globalBase)
			if len(got) != len(tt.wantPaths) {
				t.Errorf("GetInstallLocations() returned %d paths, want %d", len(got), len(tt.wantPaths))
				return
			}
			for i, path := range got {
				if path != tt.wantPaths[i] {
					t.Errorf("GetInstallLocations()[%d] = %v, want %v", i, path, tt.wantPaths[i])
				}
			}
		})
	}
}
