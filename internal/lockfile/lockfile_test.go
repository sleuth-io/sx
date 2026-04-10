package lockfile

import (
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
)

func TestParseValidLockFile(t *testing.T) {
	lockFileData := []byte(`
lock-version = "1.0"
version = "abc123"
created-by = "test"

[[assets]]
name = "test-skill"
version = "1.0.0"
type = "skill"

[assets.source-http]
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

	if len(lockFile.Assets) != 1 {
		t.Fatalf("Expected 1 asset, got %d", len(lockFile.Assets))
	}

	lockAsset := &lockFile.Assets[0]
	if lockAsset.Name != "test-skill" {
		t.Errorf("Expected name test-skill, got %s", lockAsset.Name)
	}

	if lockAsset.Version != "1.0.0" {
		t.Errorf("Expected version 1.0.0, got %s", lockAsset.Version)
	}

	if lockAsset.Type != asset.TypeSkill {
		t.Errorf("Expected type skill, got %s", lockAsset.Type)
	}
}

func TestParseAssetWithNoHashes(t *testing.T) {
	lockFileData := []byte(`
lock-version = "1.0"
version = "abc123"
created-by = "test"

[[assets]]
name = "my-rule"
version = "1"
type = "rule"

[assets.source-http]
url = "https://example.com/my-rule-1.zip"
`)

	lockFile, err := Parse(lockFileData)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(lockFile.Assets) != 1 {
		t.Fatalf("Expected 1 asset, got %d", len(lockFile.Assets))
	}

	lockAsset := &lockFile.Assets[0]
	if lockAsset.SourceHTTP == nil {
		t.Fatal("Expected source-http to be present")
	}

	if lockAsset.SourceHTTP.URL != "https://example.com/my-rule-1.zip" {
		t.Errorf("Expected URL https://example.com/my-rule-1.zip, got %s", lockAsset.SourceHTTP.URL)
	}

	if len(lockAsset.SourceHTTP.Hashes) != 0 {
		t.Errorf("Expected empty hashes, got %v", lockAsset.SourceHTTP.Hashes)
	}

	if lockAsset.SourceHTTP.Size != 0 {
		t.Errorf("Expected size 0, got %d", lockAsset.SourceHTTP.Size)
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
				Assets: []Asset{
					{
						Name:    "test",
						Version: "1.0.0",
						Type:    asset.TypeSkill,
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
				Assets: []Asset{
					{
						Name:    "test",
						Version: "invalid",
						Type:    asset.TypeSkill,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "missing asset name",
			lockFile: &LockFile{
				LockVersion: "1.0",
				Version:     "abc",
				CreatedBy:   "test",
				Assets: []Asset{
					{
						Name:    "",
						Version: "1.0.0",
						Type:    asset.TypeSkill,
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
		Assets: []Asset{
			{
				Name:    "a",
				Version: "1.0.0",
				Type:    asset.TypeSkill,
				SourceHTTP: &SourceHTTP{
					URL:    "https://example.com/a.zip",
					Hashes: map[string]string{"sha256": "abc"},
				},
				Dependencies: []Dependency{{Name: "b"}},
			},
			{
				Name:    "b",
				Version: "1.0.0",
				Type:    asset.TypeSkill,
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

func TestAssetScopes(t *testing.T) {
	tests := []struct {
		name       string
		asset      Asset
		isGlobal   bool
		repoScopes []ScopeType // Scope of each repository entry
	}{
		{
			name: "global scope (no repositories)",
			asset: Asset{
				Name:    "test",
				Version: "1.0.0",
				Type:    asset.TypeSkill,
				Scopes:  []Scope{},
			},
			isGlobal: true,
		},
		{
			name: "repo scope (repo with no paths)",
			asset: Asset{
				Name:    "test",
				Version: "1.0.0",
				Type:    asset.TypeSkill,
				Scopes: []Scope{
					{Repo: "https://github.com/user/repo"},
				},
			},
			isGlobal:   false,
			repoScopes: []ScopeType{ScopeRepo},
		},
		{
			name: "path scope (repo with paths)",
			asset: Asset{
				Name:    "test",
				Version: "1.0.0",
				Type:    asset.TypeSkill,
				Scopes: []Scope{
					{Repo: "https://github.com/user/repo", Paths: []string{"src/components"}},
				},
			},
			isGlobal:   false,
			repoScopes: []ScopeType{ScopePath},
		},
		{
			name: "mixed scopes (multiple repositories)",
			asset: Asset{
				Name:    "test",
				Version: "1.0.0",
				Type:    asset.TypeSkill,
				Scopes: []Scope{
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
			if tt.asset.IsGlobal() != tt.isGlobal {
				t.Errorf("IsGlobal() = %v, want %v", tt.asset.IsGlobal(), tt.isGlobal)
			}

			if len(tt.repoScopes) > 0 {
				for i, repo := range tt.asset.Scopes {
					scope := repo.GetScopeType()
					if scope != tt.repoScopes[i] {
						t.Errorf("Scope[%d].GetScopeType() = %s, want %s", i, scope, tt.repoScopes[i])
					}
				}
			}
		})
	}
}
