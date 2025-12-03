package lockfile

import (
	"testing"
)

func TestParseValidLockFile(t *testing.T) {
	lockFileData := []byte(`
lock-version = "1.0"
version = "abc123"
created-by = "test"

[[artifacts]]
name = "test-skill"
version = "1.0.0"
type = "skill"

[artifacts.source-http]
url = "https://example.com/test.zip"
hashes = {sha256 = "abc123"}
`)

	lockFile, err := Parse(lockFileData)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if lockFile.LockVersion != "1.0" {
		t.Errorf("Expected lock-version 1.0, got %s", lockFile.LockVersion)
	}

	if len(lockFile.Artifacts) != 1 {
		t.Fatalf("Expected 1 artifact, got %d", len(lockFile.Artifacts))
	}

	artifact := &lockFile.Artifacts[0]
	if artifact.Name != "test-skill" {
		t.Errorf("Expected name test-skill, got %s", artifact.Name)
	}

	if artifact.Version != "1.0.0" {
		t.Errorf("Expected version 1.0.0, got %s", artifact.Version)
	}

	if artifact.Type != ArtifactTypeSkill {
		t.Errorf("Expected type skill, got %s", artifact.Type)
	}
}

func TestValidateLockFile(t *testing.T) {
	tests := []struct {
		name     string
		lockFile *LockFile
		wantErr  bool
	}{
		{
			name: "valid lock file",
			lockFile: &LockFile{
				LockVersion: "1.0",
				Version:     "abc",
				CreatedBy:   "test",
				Artifacts: []Artifact{
					{
						Name:    "test",
						Version: "1.0.0",
						Type:    ArtifactTypeSkill,
						SourceHTTP: &SourceHTTP{
							URL:    "https://example.com/test.zip",
							Hashes: map[string]string{"sha256": "abc"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing lock-version",
			lockFile: &LockFile{
				Version:   "abc",
				CreatedBy: "test",
			},
			wantErr: true,
		},
		{
			name: "invalid semver",
			lockFile: &LockFile{
				LockVersion: "1.0",
				Version:     "abc",
				CreatedBy:   "test",
				Artifacts: []Artifact{
					{
						Name:    "test",
						Version: "invalid",
						Type:    ArtifactTypeSkill,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "missing artifact name",
			lockFile: &LockFile{
				LockVersion: "1.0",
				Version:     "abc",
				CreatedBy:   "test",
				Artifacts: []Artifact{
					{
						Name:    "",
						Version: "1.0.0",
						Type:    ArtifactTypeSkill,
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.lockFile.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCircularDependencies(t *testing.T) {
	lockFile := &LockFile{
		LockVersion: "1.0",
		Version:     "test",
		CreatedBy:   "test",
		Artifacts: []Artifact{
			{
				Name:    "a",
				Version: "1.0.0",
				Type:    ArtifactTypeSkill,
				SourceHTTP: &SourceHTTP{
					URL:    "https://example.com/a.zip",
					Hashes: map[string]string{"sha256": "abc"},
				},
				Dependencies: []Dependency{{Name: "b"}},
			},
			{
				Name:    "b",
				Version: "1.0.0",
				Type:    ArtifactTypeSkill,
				SourceHTTP: &SourceHTTP{
					URL:    "https://example.com/b.zip",
					Hashes: map[string]string{"sha256": "abc"},
				},
				Dependencies: []Dependency{{Name: "a"}},
			},
		},
	}

	err := lockFile.ValidateDependencies()
	if err == nil {
		t.Error("Expected circular dependency error, got nil")
	}
}

func TestArtifactScopes(t *testing.T) {
	tests := []struct {
		name       string
		artifact   Artifact
		isGlobal   bool
		repoScopes []ScopeType // Scope of each repository entry
	}{
		{
			name: "global scope (no repositories)",
			artifact: Artifact{
				Name:         "test",
				Version:      "1.0.0",
				Type:         ArtifactTypeSkill,
				Repositories: []Repository{},
			},
			isGlobal: true,
		},
		{
			name: "repo scope (repo with no paths)",
			artifact: Artifact{
				Name:    "test",
				Version: "1.0.0",
				Type:    ArtifactTypeSkill,
				Repositories: []Repository{
					{Repo: "https://github.com/user/repo"},
				},
			},
			isGlobal:   false,
			repoScopes: []ScopeType{ScopeRepo},
		},
		{
			name: "path scope (repo with paths)",
			artifact: Artifact{
				Name:    "test",
				Version: "1.0.0",
				Type:    ArtifactTypeSkill,
				Repositories: []Repository{
					{Repo: "https://github.com/user/repo", Paths: []string{"src/components"}},
				},
			},
			isGlobal:   false,
			repoScopes: []ScopeType{ScopePath},
		},
		{
			name: "mixed scopes (multiple repositories)",
			artifact: Artifact{
				Name:    "test",
				Version: "1.0.0",
				Type:    ArtifactTypeSkill,
				Repositories: []Repository{
					{Repo: "https://github.com/user/repo1"},
					{Repo: "https://github.com/user/repo2", Paths: []string{"backend"}},
				},
			},
			isGlobal:   false,
			repoScopes: []ScopeType{ScopeRepo, ScopePath},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.artifact.IsGlobal() != tt.isGlobal {
				t.Errorf("IsGlobal() = %v, want %v", tt.artifact.IsGlobal(), tt.isGlobal)
			}

			if len(tt.repoScopes) > 0 {
				for i, repo := range tt.artifact.Repositories {
					scope := repo.GetScope()
					if scope != tt.repoScopes[i] {
						t.Errorf("Repository[%d].GetScope() = %s, want %s", i, scope, tt.repoScopes[i])
					}
				}
			}
		})
	}
}
