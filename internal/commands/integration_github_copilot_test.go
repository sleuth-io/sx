package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/clients"
	github_copilot "github.com/sleuth-io/sx/internal/clients/github_copilot"
)

func init() {
	// Register GitHub Copilot client for tests
	clients.Register(github_copilot.NewClient())
}

// TestGitHubCopilotIntegration tests the full workflow with GitHub Copilot client.
// Skills are installed to ~/.copilot/skills/{name}/ for global scope.
func TestGitHubCopilotIntegration(t *testing.T) {
	// Create fully isolated test environment
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	workingDir := filepath.Join(tempDir, "working")
	repoDir := filepath.Join(workingDir, "repo")
	skillDir := filepath.Join(workingDir, "skill")

	// Set environment for complete sandboxing
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))

	// Create home and working directories
	// Note: no .copilot directory needed — IsInstalled() always returns true
	for _, dir := range []string{homeDir, workingDir, skillDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Change to working directory
	originalDir, _ := os.Getwd()
	if err := os.Chdir(workingDir); err != nil {
		t.Fatalf("Failed to change to working dir: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	// Create a test skill with metadata
	skillMetadata := `[asset]
name = "test-skill"
type = "skill"
description = "A test skill"

[skill]
readme = "README.md"
prompt-file = "SKILL.md"
`
	if err := os.WriteFile(filepath.Join(skillDir, "metadata.toml"), []byte(skillMetadata), 0644); err != nil {
		t.Fatalf("Failed to write metadata.toml: %v", err)
	}

	readmeContent := "# Test Skill\n\nThis is a test skill."
	if err := os.WriteFile(filepath.Join(skillDir, "README.md"), []byte(readmeContent), 0644); err != nil {
		t.Fatalf("Failed to write README.md: %v", err)
	}

	skillPromptContent := "You are a helpful assistant for testing."
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillPromptContent), 0644); err != nil {
		t.Fatalf("Failed to write SKILL.md: %v", err)
	}

	// Step 1: Initialize with path repository
	t.Log("Step 1: Initialize with path repository")
	InitPathRepo(t, repoDir)

	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		t.Fatalf("Init did not create repo directory: %s", repoDir)
	}

	// Step 2: Add the test skill to the repository
	t.Log("Step 2: Add test skill to repository")

	mockPrompter := NewMockPrompter().
		ExpectConfirm("correct", true).       // Confirm asset name/type
		ExpectPrompt("Version", "1.0.0").     // Enter version
		ExpectPrompt("Choose an option", "1") // Installation scope: make available globally

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{skillDir})

	if err := ExecuteWithPrompter(addCmd, mockPrompter); err != nil {
		t.Fatalf("Failed to add skill: %v", err)
	}

	// Step 3: Install from the repository
	t.Log("Step 3: Install from repository")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Step 4: Verify installation to GitHub Copilot (global scope → ~/.copilot/)
	t.Log("Step 4: Verify installation to GitHub Copilot")

	copilotDir := filepath.Join(homeDir, ".copilot")
	installedSkillDir := filepath.Join(copilotDir, "skills", "test-skill")
	if _, err := os.Stat(installedSkillDir); os.IsNotExist(err) {
		t.Fatalf("Skill was not installed to: %s", installedSkillDir)
	}

	// Verify SKILL.md exists
	installedSkillFile := filepath.Join(installedSkillDir, "SKILL.md")
	if _, err := os.Stat(installedSkillFile); os.IsNotExist(err) {
		t.Errorf("SKILL.md not found in installed location")
	}

	// Verify content is correct
	content, err := os.ReadFile(installedSkillFile)
	if err != nil {
		t.Errorf("Failed to read installed skill file: %v", err)
	} else if !strings.Contains(string(content), "helpful assistant for testing") {
		t.Errorf("Skill file content doesn't match expected content. Got: %s", string(content))
	}

	t.Log("GitHub Copilot integration test passed")
}

// TestGitHubCopilotUninstall tests that skills are properly removed from Copilot
// when they are removed from the lock file and install is re-run.
func TestGitHubCopilotUninstall(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddSkillToVault(vaultDir, "test-skill", "1.0.0")
	env.AddSkillToVault(vaultDir, "keeper-skill", "1.0.0")

	lockFileWithBoth := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "test-skill"
version = "1.0.0"
type = "skill"

[assets.source-path]
path = "assets/test-skill/1.0.0"

[[assets]]
name = "keeper-skill"
version = "1.0.0"
type = "skill"

[assets.source-path]
path = "assets/keeper-skill/1.0.0"
`
	env.WriteLockFile(vaultDir, lockFileWithBoth)

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")
	env.Chdir(projectDir)

	// Step 1: Install both skills
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	copilotDir := filepath.Join(env.HomeDir, ".copilot")
	testSkillDir := filepath.Join(copilotDir, "skills", "test-skill")
	keeperSkillDir := filepath.Join(copilotDir, "skills", "keeper-skill")
	env.AssertFileExists(testSkillDir)
	env.AssertFileExists(keeperSkillDir)

	// Step 2: Remove test-skill from lock file
	lockFileWithout := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "keeper-skill"
version = "1.0.0"
type = "skill"

[assets.source-path]
path = "assets/keeper-skill/1.0.0"
`
	env.WriteLockFile(vaultDir, lockFileWithout)

	// Step 3: Run install again to trigger cleanup
	installCmd2 := NewInstallCommand()
	if err := installCmd2.Execute(); err != nil {
		t.Fatalf("Second install failed: %v", err)
	}

	// Step 4: Verify test-skill was removed, keeper-skill remains
	env.AssertFileNotExists(testSkillDir)
	env.AssertFileExists(keeperSkillDir)
}

// TestGitHubCopilotRepoScope tests that repo-scoped skills are installed
// to {repoRoot}/.github/skills/ instead of ~/.copilot/skills/.
func TestGitHubCopilotRepoScope(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddSkillToVault(vaultDir, "repo-skill", "1.0.0")

	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "repo-skill"
version = "1.0.0"
type = "skill"

[assets.source-path]
path = "assets/repo-skill/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"
`
	env.WriteLockFile(vaultDir, lockFile)

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")
	env.Chdir(projectDir)

	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Verify skill was installed to {repoRoot}/.github/skills/ (Copilot repo scope)
	repoSkillDir := filepath.Join(projectDir, ".github", "skills", "repo-skill")
	env.AssertFileExists(repoSkillDir)
	env.AssertFileExists(filepath.Join(repoSkillDir, "SKILL.md"))
	env.AssertFileExists(filepath.Join(repoSkillDir, "metadata.toml"))

	// Verify skill was NOT installed to global ~/.copilot/
	globalSkillDir := filepath.Join(env.HomeDir, ".copilot", "skills", "repo-skill")
	env.AssertFileNotExists(globalSkillDir)
}

// TestGitHubCopilotPathScope tests that path-scoped skills are installed
// to {repoRoot}/{path}/.github/skills/ instead of ~/.copilot/skills/.
func TestGitHubCopilotPathScope(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddSkillToVault(vaultDir, "path-skill", "1.0.0")

	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "path-skill"
version = "1.0.0"
type = "skill"

[assets.source-path]
path = "assets/path-skill/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"
paths = ["src/backend"]
`
	env.WriteLockFile(vaultDir, lockFile)

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")
	env.Chdir(projectDir)

	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Verify skill was installed to {repoRoot}/src/backend/.github/skills/
	pathSkillDir := filepath.Join(projectDir, "src", "backend", ".github", "skills", "path-skill")
	env.AssertFileExists(pathSkillDir)
	env.AssertFileExists(filepath.Join(pathSkillDir, "SKILL.md"))

	// Verify skill was NOT installed globally
	globalSkillDir := filepath.Join(env.HomeDir, ".copilot", "skills", "path-skill")
	env.AssertFileNotExists(globalSkillDir)

	// Verify skill was NOT installed at repo root
	repoSkillDir := filepath.Join(projectDir, ".github", "skills", "path-skill")
	env.AssertFileNotExists(repoSkillDir)
}

// TestGitHubCopilotUnsupportedAssetType tests that Copilot gracefully skips
// non-skill assets while still installing skills correctly.
func TestGitHubCopilotUnsupportedAssetType(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddSkillToVault(vaultDir, "good-skill", "1.0.0")
	env.AddRuleToVault(vaultDir, "some-rule", "1.0.0", "Always be helpful")

	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "good-skill"
version = "1.0.0"
type = "skill"

[assets.source-path]
path = "assets/good-skill/1.0.0"

[[assets]]
name = "some-rule"
version = "1.0.0"
type = "rule"

[assets.source-path]
path = "assets/some-rule/1.0.0"
`
	env.WriteLockFile(vaultDir, lockFile)

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")
	env.Chdir(projectDir)

	// Install should succeed — rule is skipped by Copilot, skill installs fine
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Verify skill was installed to Copilot
	copilotDir := filepath.Join(env.HomeDir, ".copilot")
	env.AssertFileExists(filepath.Join(copilotDir, "skills", "good-skill"))

	// Verify rule was NOT installed to Copilot (not a supported type)
	env.AssertFileNotExists(filepath.Join(copilotDir, "rules"))
	env.AssertFileNotExists(filepath.Join(copilotDir, "some-rule"))
}

// TestGitHubCopilotMissingPromptFile tests that install fails when a skill's
// metadata references a prompt file that doesn't exist in the asset directory.
func TestGitHubCopilotMissingPromptFile(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()

	// Create skill asset manually with metadata but no SKILL.md
	skillDir := env.MkdirAll(filepath.Join(vaultDir, "assets", "broken-skill", "1.0.0"))
	env.WriteFile(filepath.Join(skillDir, "metadata.toml"), `[asset]
name = "broken-skill"
type = "skill"
version = "1.0.0"
description = "A broken skill"

[skill]
readme = "README.md"
prompt-file = "SKILL.md"
`)
	// Deliberately don't create SKILL.md

	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "broken-skill"
version = "1.0.0"
type = "skill"

[assets.source-path]
path = "assets/broken-skill/1.0.0"
`
	env.WriteLockFile(vaultDir, lockFile)

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")
	env.Chdir(projectDir)

	// Install should fail because SKILL.md is missing from the asset
	installCmd := NewInstallCommand()
	err := installCmd.Execute()
	if err == nil {
		t.Fatal("Expected install to fail due to missing prompt file, but it succeeded")
	}
}

// TestGitHubCopilotMissingMetadata tests that install fails when a skill
// asset directory has no metadata.toml file.
func TestGitHubCopilotMissingMetadata(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()

	// Create skill directory without metadata.toml
	skillDir := env.MkdirAll(filepath.Join(vaultDir, "assets", "no-meta-skill", "1.0.0"))
	env.WriteFile(filepath.Join(skillDir, "SKILL.md"), "You are a skill without metadata")

	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "no-meta-skill"
version = "1.0.0"
type = "skill"

[assets.source-path]
path = "assets/no-meta-skill/1.0.0"
`
	env.WriteLockFile(vaultDir, lockFile)

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")
	env.Chdir(projectDir)

	// Install should fail because metadata.toml is missing
	installCmd := NewInstallCommand()
	err := installCmd.Execute()
	if err == nil {
		t.Fatal("Expected install to fail due to missing metadata.toml, but it succeeded")
	}
}
