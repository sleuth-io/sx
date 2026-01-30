package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/lockfile"
)

// TestAddNonInteractiveIntegration tests non-interactive add command variants
func TestAddNonInteractiveIntegration(t *testing.T) {
	env := NewTestEnv(t)

	// Setup path vault
	vaultDir := env.SetupPathVault()

	// Create source skill directory for adding
	sourceDir := env.MkdirAll(filepath.Join(env.TempDir, "source-skill"))
	env.WriteFile(filepath.Join(sourceDir, "metadata.toml"), `[asset]
name = "test-skill"
type = "skill"
description = "A test skill"

[skill]
readme = "README.md"
prompt-file = "SKILL.md"
`)
	env.WriteFile(filepath.Join(sourceDir, "README.md"), "# Test Skill")
	env.WriteFile(filepath.Join(sourceDir, "SKILL.md"), "You are a test skill")

	t.Run("add with --yes defaults to global scope", func(t *testing.T) {
		addCmd := NewAddCommand()
		addCmd.SetArgs([]string{sourceDir, "--yes"})

		if err := addCmd.Execute(); err != nil {
			t.Fatalf("Failed to add skill: %v", err)
		}

		// Verify asset added to vault
		assetsDir := filepath.Join(vaultDir, "assets", "test-skill", "1")
		env.AssertFileExists(assetsDir)

		// Verify lock file has global scope (empty scopes)
		lockData, err := os.ReadFile(filepath.Join(vaultDir, "sx.lock"))
		if err != nil {
			t.Fatalf("Failed to read lock file: %v", err)
		}

		lf, err := lockfile.Parse(lockData)
		if err != nil {
			t.Fatalf("Failed to parse lock file: %v", err)
		}

		if len(lf.Assets) == 0 {
			t.Fatal("Expected at least one asset in lock file")
		}

		asset := lf.Assets[0]
		if !asset.IsGlobal() {
			t.Errorf("Expected global scope (empty scopes), got %d scopes", len(asset.Scopes))
		}
	})

	t.Run("add with --scope-global sets global scope explicitly", func(t *testing.T) {
		// Clean up previous lock file
		os.Remove(filepath.Join(vaultDir, "sx.lock"))

		addCmd := NewAddCommand()
		addCmd.SetArgs([]string{sourceDir, "--yes", "--scope-global"})

		if err := addCmd.Execute(); err != nil {
			t.Fatalf("Failed to add skill: %v", err)
		}

		lockData, _ := os.ReadFile(filepath.Join(vaultDir, "sx.lock"))
		lf, _ := lockfile.Parse(lockData)

		if len(lf.Assets) == 0 {
			t.Fatal("Expected at least one asset in lock file")
		}

		if !lf.Assets[0].IsGlobal() {
			t.Error("Expected global scope with --scope-global")
		}
	})

	t.Run("add with --scope-repo sets repository scope", func(t *testing.T) {
		// Clean up previous lock file
		os.Remove(filepath.Join(vaultDir, "sx.lock"))

		addCmd := NewAddCommand()
		addCmd.SetArgs([]string{sourceDir, "--yes", "--scope-repo", "git@github.com:org/repo.git"})

		if err := addCmd.Execute(); err != nil {
			t.Fatalf("Failed to add skill: %v", err)
		}

		lockData, _ := os.ReadFile(filepath.Join(vaultDir, "sx.lock"))
		lf, _ := lockfile.Parse(lockData)

		if len(lf.Assets) == 0 {
			t.Fatal("Expected at least one asset in lock file")
		}

		asset := lf.Assets[0]
		if len(asset.Scopes) != 1 {
			t.Fatalf("Expected 1 scope, got %d", len(asset.Scopes))
		}

		scope := asset.Scopes[0]
		if scope.Repo != "git@github.com:org/repo.git" {
			t.Errorf("Expected repo 'git@github.com:org/repo.git', got %q", scope.Repo)
		}
		if len(scope.Paths) != 0 {
			t.Errorf("Expected no paths, got %v", scope.Paths)
		}
	})

	t.Run("add with --scope-repo and path fragment", func(t *testing.T) {
		// Clean up previous lock file
		os.Remove(filepath.Join(vaultDir, "sx.lock"))

		addCmd := NewAddCommand()
		addCmd.SetArgs([]string{sourceDir, "--yes", "--scope-repo", "git@github.com:org/repo.git#backend/services"})

		if err := addCmd.Execute(); err != nil {
			t.Fatalf("Failed to add skill: %v", err)
		}

		lockData, _ := os.ReadFile(filepath.Join(vaultDir, "sx.lock"))
		lf, _ := lockfile.Parse(lockData)

		if len(lf.Assets) == 0 {
			t.Fatal("Expected at least one asset in lock file")
		}

		asset := lf.Assets[0]
		if len(asset.Scopes) != 1 {
			t.Fatalf("Expected 1 scope, got %d", len(asset.Scopes))
		}

		scope := asset.Scopes[0]
		if scope.Repo != "git@github.com:org/repo.git" {
			t.Errorf("Expected repo 'git@github.com:org/repo.git', got %q", scope.Repo)
		}
		if len(scope.Paths) != 1 || scope.Paths[0] != "backend/services" {
			t.Errorf("Expected paths ['backend/services'], got %v", scope.Paths)
		}
	})

	t.Run("add with --scope-repo and multiple paths", func(t *testing.T) {
		// Clean up previous lock file
		os.Remove(filepath.Join(vaultDir, "sx.lock"))

		addCmd := NewAddCommand()
		addCmd.SetArgs([]string{sourceDir, "--yes", "--scope-repo", "git@github.com:org/repo.git#backend,frontend"})

		if err := addCmd.Execute(); err != nil {
			t.Fatalf("Failed to add skill: %v", err)
		}

		lockData, _ := os.ReadFile(filepath.Join(vaultDir, "sx.lock"))
		lf, _ := lockfile.Parse(lockData)

		if len(lf.Assets) == 0 {
			t.Fatal("Expected at least one asset in lock file")
		}

		scope := lf.Assets[0].Scopes[0]
		if len(scope.Paths) != 2 {
			t.Fatalf("Expected 2 paths, got %d", len(scope.Paths))
		}
		if scope.Paths[0] != "backend" || scope.Paths[1] != "frontend" {
			t.Errorf("Expected paths ['backend', 'frontend'], got %v", scope.Paths)
		}
	})

	t.Run("add with multiple --scope-repo flags", func(t *testing.T) {
		// Clean up previous lock file
		os.Remove(filepath.Join(vaultDir, "sx.lock"))

		addCmd := NewAddCommand()
		addCmd.SetArgs([]string{
			sourceDir, "--yes",
			"--scope-repo", "git@github.com:org/repo-a.git",
			"--scope-repo", "git@github.com:org/repo-b.git#backend",
		})

		if err := addCmd.Execute(); err != nil {
			t.Fatalf("Failed to add skill: %v", err)
		}

		lockData, _ := os.ReadFile(filepath.Join(vaultDir, "sx.lock"))
		lf, _ := lockfile.Parse(lockData)

		if len(lf.Assets) == 0 {
			t.Fatal("Expected at least one asset in lock file")
		}

		asset := lf.Assets[0]
		if len(asset.Scopes) != 2 {
			t.Fatalf("Expected 2 scopes, got %d", len(asset.Scopes))
		}

		// Verify first repo scope
		if asset.Scopes[0].Repo != "git@github.com:org/repo-a.git" {
			t.Errorf("Expected first repo 'git@github.com:org/repo-a.git', got %q", asset.Scopes[0].Repo)
		}
		if len(asset.Scopes[0].Paths) != 0 {
			t.Errorf("Expected first scope to have no paths, got %v", asset.Scopes[0].Paths)
		}

		// Verify second repo scope with path
		if asset.Scopes[1].Repo != "git@github.com:org/repo-b.git" {
			t.Errorf("Expected second repo 'git@github.com:org/repo-b.git', got %q", asset.Scopes[1].Repo)
		}
		if len(asset.Scopes[1].Paths) != 1 || asset.Scopes[1].Paths[0] != "backend" {
			t.Errorf("Expected second scope paths ['backend'], got %v", asset.Scopes[1].Paths)
		}
	})

	t.Run("add with --no-install skips lock file update", func(t *testing.T) {
		// Clean up previous lock file
		os.Remove(filepath.Join(vaultDir, "sx.lock"))

		addCmd := NewAddCommand()
		addCmd.SetArgs([]string{sourceDir, "--yes", "--no-install"})

		if err := addCmd.Execute(); err != nil {
			t.Fatalf("Failed to add skill: %v", err)
		}

		// Verify asset added to vault
		assetsDir := filepath.Join(vaultDir, "assets", "test-skill")
		env.AssertFileExists(assetsDir)

		// Verify NO lock file was created (--no-install means vault only)
		lockPath := filepath.Join(vaultDir, "sx.lock")
		if _, err := os.Stat(lockPath); err == nil {
			t.Error("Lock file should not exist with --no-install")
		}
	})

	t.Run("add with explicit --name and --type overrides", func(t *testing.T) {
		// Clean up previous lock file and assets
		os.Remove(filepath.Join(vaultDir, "sx.lock"))
		os.RemoveAll(filepath.Join(vaultDir, "assets", "custom-name"))

		addCmd := NewAddCommand()
		addCmd.SetArgs([]string{sourceDir, "--yes", "--name", "custom-name", "--type", "rule"})

		if err := addCmd.Execute(); err != nil {
			t.Fatalf("Failed to add skill: %v", err)
		}

		// Verify asset added with custom name
		assetsDir := filepath.Join(vaultDir, "assets", "custom-name", "1")
		env.AssertFileExists(assetsDir)

		// Verify lock file has custom name and type
		lockData, _ := os.ReadFile(filepath.Join(vaultDir, "sx.lock"))
		lf, _ := lockfile.Parse(lockData)

		if len(lf.Assets) == 0 {
			t.Fatal("Expected at least one asset in lock file")
		}

		asset := lf.Assets[0]
		if asset.Name != "custom-name" {
			t.Errorf("Expected name 'custom-name', got %q", asset.Name)
		}
		if asset.Type.Label != "Rule" {
			t.Errorf("Expected type 'Rule', got %q", asset.Type.Label)
		}
	})
}

// TestAddNonInteractiveErrors tests error cases for non-interactive add
func TestAddNonInteractiveErrors(t *testing.T) {
	env := NewTestEnv(t)
	env.SetupPathVault()

	t.Run("error when no input in non-interactive mode", func(t *testing.T) {
		addCmd := NewAddCommand()
		addCmd.SetArgs([]string{"--yes"}) // No input path

		err := addCmd.Execute()
		if err == nil {
			t.Error("Expected error when no input provided in non-interactive mode")
		}
	})

	t.Run("error when --scope-global and --scope-repo both used", func(t *testing.T) {
		sourceDir := env.MkdirAll(filepath.Join(env.TempDir, "source-skill"))
		env.WriteFile(filepath.Join(sourceDir, "README.md"), "# Test")

		addCmd := NewAddCommand()
		addCmd.SetArgs([]string{sourceDir, "--yes", "--scope-global", "--scope-repo", "git@github.com:org/repo.git"})

		err := addCmd.Execute()
		if err == nil {
			t.Error("Expected error when both --scope-global and --scope-repo are used")
		}
	})

	t.Run("error when invalid --type provided", func(t *testing.T) {
		sourceDir := env.MkdirAll(filepath.Join(env.TempDir, "source-skill2"))
		env.WriteFile(filepath.Join(sourceDir, "README.md"), "# Test")

		addCmd := NewAddCommand()
		addCmd.SetArgs([]string{sourceDir, "--yes", "--type", "invalid-type"})

		err := addCmd.Execute()
		if err == nil {
			t.Error("Expected error when invalid --type is provided")
		}
	})
}

// TestParseRepoSpec tests the repo#path parsing
func TestParseRepoSpec(t *testing.T) {
	tests := []struct {
		name          string
		spec          string
		expectedRepo  string
		expectedPaths []string
	}{
		{
			name:          "repo only",
			spec:          "git@github.com:org/repo.git",
			expectedRepo:  "git@github.com:org/repo.git",
			expectedPaths: nil,
		},
		{
			name:          "repo with single path",
			spec:          "git@github.com:org/repo.git#backend",
			expectedRepo:  "git@github.com:org/repo.git",
			expectedPaths: []string{"backend"},
		},
		{
			name:          "repo with multiple paths",
			spec:          "git@github.com:org/repo.git#backend,frontend,libs",
			expectedRepo:  "git@github.com:org/repo.git",
			expectedPaths: []string{"backend", "frontend", "libs"},
		},
		{
			name:          "repo with path containing slash",
			spec:          "git@github.com:org/repo.git#backend/services",
			expectedRepo:  "git@github.com:org/repo.git",
			expectedPaths: []string{"backend/services"},
		},
		{
			name:          "repo with empty fragment",
			spec:          "git@github.com:org/repo.git#",
			expectedRepo:  "git@github.com:org/repo.git",
			expectedPaths: nil,
		},
		{
			name:          "https URL with path",
			spec:          "https://github.com/org/repo#src/main",
			expectedRepo:  "https://github.com/org/repo",
			expectedPaths: []string{"src/main"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo, paths := parseRepoSpec(tc.spec)

			if repo != tc.expectedRepo {
				t.Errorf("Expected repo %q, got %q", tc.expectedRepo, repo)
			}

			if len(paths) != len(tc.expectedPaths) {
				t.Errorf("Expected %d paths, got %d", len(tc.expectedPaths), len(paths))
				return
			}

			for i, expected := range tc.expectedPaths {
				if paths[i] != expected {
					t.Errorf("Expected path[%d] = %q, got %q", i, expected, paths[i])
				}
			}
		})
	}
}
