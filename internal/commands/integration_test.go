package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/clients/claude_code"
	"github.com/sleuth-io/sx/internal/clients/cursor"
)

func init() {
	// Register clients for tests
	clients.Register(claude_code.NewClient())
	clients.Register(cursor.NewClient())
}

// TestPathRepositoryIntegration tests the full workflow with a path repository
func TestPathRepositoryIntegration(t *testing.T) {
	// Create fully isolated test environment
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	workingDir := filepath.Join(tempDir, "working")
	repoDir := filepath.Join(workingDir, "repo")
	skillDir := filepath.Join(workingDir, "skill")

	// Set environment for complete sandboxing FIRST
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	claudeDir := filepath.Join(homeDir, ".claude")

	// Create home and working directories (but NOT repo - let init create it)
	// Also create .claude directory so Claude Code client is detected
	for _, dir := range []string{homeDir, workingDir, skillDir, claudeDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Create a dummy settings.json to make it look like a real Claude installation
	settingsPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("Failed to create settings.json: %v", err)
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

	// Verify repo directory was created by init
	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		t.Fatalf("Init did not create repo directory: %s", repoDir)
	}

	// Step 2: Add the test skill to the repository using 'add' command
	t.Log("Step 2: Add test skill to repository")

	// Create add command with mock prompter
	mockPrompter := NewMockPrompter().
		ExpectConfirm("correct", true).       // Confirm asset name/type
		ExpectPrompt("Version", "1.0.0").     // Enter version
		ExpectPrompt("Choose an option", "1") // Installation scope: make available globally

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{skillDir})

	if err := ExecuteWithPrompter(addCmd, mockPrompter); err != nil {
		t.Fatalf("Failed to add skill: %v", err)
	}

	// Verify assets directory was created
	assetsDir := filepath.Join(repoDir, "assets", "test-skill", "1.0.0")
	if _, err := os.Stat(assetsDir); os.IsNotExist(err) {
		t.Fatalf("Assets directory was not created: %s", assetsDir)
	}

	// Debug: List files in assets directory
	files, _ := os.ReadDir(assetsDir)
	t.Log("Files in assets directory:")
	for _, file := range files {
		t.Logf("  - %s", file.Name())
	}

	// Verify sx.lock was created in repo
	lockPath := filepath.Join(repoDir, "sx.lock")
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatalf("sx.lock was not created: %s", lockPath)
	}

	// Step 3: Install from the repository
	t.Log("Step 3: Install from repository")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Step 4: Verify installation
	t.Log("Step 4: Verify installation")

	// Check that skill was installed to ~/.claude/skills/test-skill
	// claudeDir already declared above
	installedSkillDir := filepath.Join(claudeDir, "skills", "test-skill")
	if _, err := os.Stat(installedSkillDir); os.IsNotExist(err) {
		t.Fatalf("Skill was not installed to: %s", installedSkillDir)
	}

	// Verify metadata.toml exists in installed location
	installedMetadata := filepath.Join(installedSkillDir, "metadata.toml")
	if _, err := os.Stat(installedMetadata); os.IsNotExist(err) {
		t.Errorf("metadata.toml not found in installed location")
	}

	// Verify README.md exists
	installedReadme := filepath.Join(installedSkillDir, "README.md")
	if _, err := os.Stat(installedReadme); os.IsNotExist(err) {
		t.Errorf("README.md not found in installed location")
	}

	// Verify SKILL.md exists
	installedPrompt := filepath.Join(installedSkillDir, "SKILL.md")
	if _, err := os.Stat(installedPrompt); os.IsNotExist(err) {
		t.Errorf("SKILL.md not found in installed location")
	}

	// Verify content is correct
	content, err := os.ReadFile(installedReadme)
	if err != nil {
		t.Errorf("Failed to read installed README: %v", err)
	} else if !strings.Contains(string(content), "Test Skill") {
		t.Errorf("README content doesn't match expected content")
	}

	t.Log("✓ Integration test passed!")
}

// TestRepoScopedAssetCleanup verifies that when a repo-scoped asset is removed
// from the lock file, cleanup correctly removes it from {repoRoot}/.claude/
// and not from ~/.claude/
//
// This tests the buggy code path in cleanupRemovedAssets which uses buildInstallScope.
// If clients.ScopeRepository doesn't match lockfile.ScopeRepo, the scope type
// won't match in determineTargetBase and cleanup will target the wrong directory.
func TestRepoScopedAssetCleanup(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddSkillToVault(vaultDir, "repo-skill", "1.0.0")
	env.AddSkillToVault(vaultDir, "global-skill", "1.0.0")

	// Lock file WITH both assets (one repo-scoped, one global)
	lockFileWithBoth := `lock-version = "1"
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

[[assets]]
name = "global-skill"
version = "1.0.0"
type = "skill"

[assets.source-path]
path = "assets/global-skill/1.0.0"
`
	env.WriteLockFile(vaultDir, lockFileWithBoth)

	// Set up git repo matching the scope
	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")
	env.Chdir(projectDir)

	// Step 1: Install both assets
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Initial install failed: %v", err)
	}

	repoSkillDir := filepath.Join(projectDir, ".claude", "skills", "repo-skill")
	globalSkillDir := filepath.Join(env.GlobalClaudeDir(), "skills", "global-skill")
	env.AssertFileExists(repoSkillDir)
	env.AssertFileExists(globalSkillDir)
	t.Log("✓ Both assets installed")

	// Step 2: Remove repo-scoped asset from lock file, keep global one
	// This ensures cleanup runs (needs at least one applicable asset)
	lockFileWithoutRepoSkill := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "global-skill"
version = "1.0.0"
type = "skill"

[assets.source-path]
path = "assets/global-skill/1.0.0"
`
	env.WriteLockFile(vaultDir, lockFileWithoutRepoSkill)

	// Step 3: Run install again - this should trigger cleanup
	installCmd2 := NewInstallCommand()
	if err := installCmd2.Execute(); err != nil {
		t.Fatalf("Second install failed: %v", err)
	}

	// Step 4: Verify asset was removed from repo's .claude (not from ~/.claude)
	// If the bug is present, cleanup targets ~/.claude instead of {repo}/.claude,
	// so the skill remains in the repo directory when it should be gone.
	if _, err := os.Stat(repoSkillDir); err == nil {
		t.Errorf("Asset should have been cleaned up from repo directory: %s", repoSkillDir)
		t.Error("This indicates cleanup targeted the wrong directory (likely ~/.claude instead of repo/.claude)")
	} else {
		t.Log("✓ Asset correctly cleaned up from repo directory")
	}
}

// TestInstallTargetFlag verifies that --target installs repo-scoped assets
// to the target directory's .claude/ instead of requiring cwd to be in the repo.
// Global assets should still go to ~/.claude/ regardless of --target.
func TestInstallTargetFlag(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddSkillToVault(vaultDir, "repo-skill", "1.0.0")
	env.AddSkillToVault(vaultDir, "global-skill", "1.0.0")

	// Lock file with one repo-scoped and one global asset
	lockContent := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "repo-skill"
version = "1.0.0"
type = "skill"

[assets.source-path]
path = "assets/repo-skill/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/targetrepo"

[[assets]]
name = "global-skill"
version = "1.0.0"
type = "skill"

[assets.source-path]
path = "assets/global-skill/1.0.0"
`
	env.WriteLockFile(vaultDir, lockContent)

	// Create a git repo that matches the scope
	projectDir := env.SetupGitRepo("targetproject", "https://github.com/testorg/targetrepo")

	// Change to a directory that is NOT the project — e.g., home dir
	env.Chdir(env.HomeDir)

	// Run install with --target pointing at the project
	installCmd := NewInstallCommand()
	installCmd.SetArgs([]string{"--target", projectDir})
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Install with --target failed: %v", err)
	}

	// Repo-scoped asset should be in the target project's .claude/
	repoSkillDir := filepath.Join(projectDir, ".claude", "skills", "repo-skill")
	env.AssertFileExists(repoSkillDir)
	t.Log("✓ Repo-scoped asset installed to target project directory")

	// Global asset should still be in ~/.claude/
	globalSkillDir := filepath.Join(env.GlobalClaudeDir(), "skills", "global-skill")
	env.AssertFileExists(globalSkillDir)
	t.Log("✓ Global asset installed to ~/.claude/")

	// Repo-scoped asset should NOT be in ~/.claude/
	globalRepoSkillDir := filepath.Join(env.GlobalClaudeDir(), "skills", "repo-skill")
	env.AssertFileNotExists(globalRepoSkillDir)
	t.Log("✓ Repo-scoped asset not in ~/.claude/")
}

// TestInstallTargetFlagInvalidDir verifies that --target with a non-existent
// directory returns an error rather than silently doing nothing.
func TestInstallTargetFlagInvalidDir(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddSkillToVault(vaultDir, "test-skill", "1.0.0")
	env.WriteLockFile(vaultDir, `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "test-skill"
version = "1.0.0"
type = "skill"

[assets.source-path]
path = "assets/test-skill/1.0.0"
`)

	env.Chdir(env.HomeDir)

	invalidTarget := filepath.Join(env.TempDir, "nonexistent")
	installCmd := NewInstallCommand()
	installCmd.SetArgs([]string{"--target", invalidTarget})
	err := installCmd.Execute()
	if err == nil {
		t.Fatal("Expected error for non-existent target directory")
	}
	t.Logf("✓ Got expected error: %v", err)
}
