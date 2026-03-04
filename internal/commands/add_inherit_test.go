package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/lockfile"
)

// TestAddYesWithoutScopeFlagsPreservesExistingScopes tests that `sx add ./skill --yes`
// without scope flags preserves existing installation scopes instead of overwriting them.
func TestAddYesWithoutScopeFlagsPreservesExistingScopes(t *testing.T) {
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
	claudeDir := filepath.Join(homeDir, ".claude")

	// Create directories
	for _, dir := range []string{homeDir, workingDir, skillDir, claudeDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Create dummy settings.json
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

	// Create test skill
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
	if err := os.WriteFile(filepath.Join(skillDir, "README.md"), []byte("# Test Skill"), 0644); err != nil {
		t.Fatalf("Failed to write README.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("You are a test assistant."), 0644); err != nil {
		t.Fatalf("Failed to write SKILL.md: %v", err)
	}

	// Initialize path repository
	t.Log("Step 1: Initialize with path repository")
	InitPathRepo(t, repoDir)

	// Step 2: Add skill with repository-specific scope
	t.Log("Step 2: Add test skill with repo-specific scope")
	mockPrompter := NewMockPrompter().
		ExpectConfirm("correct", true).                       // Confirm detected asset
		ExpectPrompt("Version", "1.0.0").                     // Enter version
		ExpectPrompt("choice", "2").                          // Option 2: Add/modify repository-specific
		ExpectPrompt("choice", "1").                          // Add new repository
		ExpectPrompt("URL", "https://github.com/org/myrepo"). // Repository URL
		ExpectConfirm("entire repository", true).             // Entire repository
		ExpectPrompt("choice", "4").                          // Done with modifications
		ExpectConfirm("Continue with these changes", true).   // Confirm changes
		ExpectConfirm("Run install now", false)               // Don't run install

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{skillDir})
	if err := ExecuteWithPrompter(addCmd, mockPrompter); err != nil {
		t.Fatalf("Failed to add skill: %v", err)
	}

	// Verify skill was added with repo scope
	lockFilePath := filepath.Join(repoDir, "sx.lock")
	asset, exists := lockfile.FindAsset(lockFilePath, "test-skill")
	if !exists {
		t.Fatalf("Asset not found in lock file")
	}
	if asset.IsGlobal() {
		t.Fatalf("Expected repository-specific scope, got global")
	}
	if len(asset.Scopes) != 1 || asset.Scopes[0].Repo != "https://github.com/org/myrepo" {
		t.Fatalf("Expected scope for https://github.com/org/myrepo, got %v", asset.Scopes)
	}
	t.Log("✓ Asset added with repo-specific scope")

	// Step 3: Re-add same skill content with --yes (no scope flags)
	// This should preserve existing scopes, not overwrite with global
	t.Log("Step 3: Re-add identical skill with --yes (no scope flags)")
	addCmd2 := NewAddCommand()
	addCmd2.SetArgs([]string{skillDir, "--yes"})
	if err := addCmd2.Execute(); err != nil {
		t.Fatalf("Failed to re-add skill with --yes: %v", err)
	}

	// Verify scopes are preserved (not overwritten to global)
	asset2, exists := lockfile.FindAsset(lockFilePath, "test-skill")
	if !exists {
		t.Fatalf("Asset not found in lock file after re-add")
	}
	if asset2.IsGlobal() {
		t.Fatalf("Scopes were overwritten to global! Expected repo-specific scope to be preserved")
	}
	if len(asset2.Scopes) != 1 || asset2.Scopes[0].Repo != "https://github.com/org/myrepo" {
		t.Fatalf("Expected preserved scope for https://github.com/org/myrepo, got %v", asset2.Scopes)
	}
	t.Log("✓ Existing scopes preserved when using --yes without scope flags")
}

// TestAddYesWithScopeGlobalOverridesExisting tests that `sx add ./skill --yes --scope-global`
// explicitly sets global scope even when repo-specific scopes existed before.
func TestAddYesWithScopeGlobalOverridesExisting(t *testing.T) {
	// Create fully isolated test environment
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	workingDir := filepath.Join(tempDir, "working")
	repoDir := filepath.Join(workingDir, "repo")
	skillDir := filepath.Join(workingDir, "skill")

	// Set environment
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	claudeDir := filepath.Join(homeDir, ".claude")

	// Create directories
	for _, dir := range []string{homeDir, workingDir, skillDir, claudeDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	settingsPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("Failed to create settings.json: %v", err)
	}

	originalDir, _ := os.Getwd()
	if err := os.Chdir(workingDir); err != nil {
		t.Fatalf("Failed to change to working dir: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	// Create test skill
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
	if err := os.WriteFile(filepath.Join(skillDir, "README.md"), []byte("# Test Skill"), 0644); err != nil {
		t.Fatalf("Failed to write README.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("You are a test assistant."), 0644); err != nil {
		t.Fatalf("Failed to write SKILL.md: %v", err)
	}

	// Initialize path repository
	InitPathRepo(t, repoDir)

	// Step 1: Add skill with repo-specific scope
	mockPrompter := NewMockPrompter().
		ExpectConfirm("correct", true).
		ExpectPrompt("Version", "1.0.0").
		ExpectPrompt("choice", "2"). // Add/modify repository-specific
		ExpectPrompt("choice", "1"). // Add new repository
		ExpectPrompt("URL", "https://github.com/org/myrepo").
		ExpectConfirm("entire repository", true).
		ExpectPrompt("choice", "4"). // Done
		ExpectConfirm("Continue with these changes", true).
		ExpectConfirm("Run install now", false)

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{skillDir})
	if err := ExecuteWithPrompter(addCmd, mockPrompter); err != nil {
		t.Fatalf("Failed to add skill: %v", err)
	}

	lockFilePath := filepath.Join(repoDir, "sx.lock")
	asset, exists := lockfile.FindAsset(lockFilePath, "test-skill")
	if !exists || asset.IsGlobal() {
		t.Fatalf("Expected repo-specific scope after initial add")
	}

	// Step 2: Re-add with --yes --scope-global → should set global on new version
	addCmd2 := NewAddCommand()
	addCmd2.SetArgs([]string{skillDir, "--yes", "--scope-global"})
	if err := addCmd2.Execute(); err != nil {
		t.Fatalf("Failed to re-add skill with --yes --scope-global: %v", err)
	}

	// Parse full lock file and find the latest version
	lf, err := lockfile.ParseFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to parse lock file: %v", err)
	}
	var latestAsset *lockfile.Asset
	for i := range lf.Assets {
		if lf.Assets[i].Name == "test-skill" {
			if latestAsset == nil || lf.Assets[i].Version > latestAsset.Version {
				latestAsset = &lf.Assets[i]
			}
		}
	}
	if latestAsset == nil {
		t.Fatalf("Asset not found after re-add with --scope-global")
	}
	if !latestAsset.IsGlobal() {
		t.Fatalf("Expected global scope with --scope-global on %s, got %v", latestAsset.Version, latestAsset.Scopes)
	}
	t.Logf("✓ --scope-global correctly sets global scope on %s", latestAsset.Version)
}

// TestAddYesWithoutScopeFlagsNewAsset tests that `sx add ./skill --yes` for a brand new
// asset (no existing lock file entry) still works and writes the asset to the lock file.
func TestAddYesWithoutScopeFlagsNewAsset(t *testing.T) {
	// Create fully isolated test environment
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	workingDir := filepath.Join(tempDir, "working")
	repoDir := filepath.Join(workingDir, "repo")
	skillDir := filepath.Join(workingDir, "skill")

	// Set environment
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	claudeDir := filepath.Join(homeDir, ".claude")

	for _, dir := range []string{homeDir, workingDir, skillDir, claudeDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	settingsPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("Failed to create settings.json: %v", err)
	}

	originalDir, _ := os.Getwd()
	if err := os.Chdir(workingDir); err != nil {
		t.Fatalf("Failed to change to working dir: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	skillMetadata := `[asset]
name = "brand-new-skill"
type = "skill"
description = "A brand new skill"

[skill]
readme = "README.md"
prompt-file = "SKILL.md"
`
	if err := os.WriteFile(filepath.Join(skillDir, "metadata.toml"), []byte(skillMetadata), 0644); err != nil {
		t.Fatalf("Failed to write metadata.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "README.md"), []byte("# Brand New Skill"), 0644); err != nil {
		t.Fatalf("Failed to write README.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("You are a test assistant."), 0644); err != nil {
		t.Fatalf("Failed to write SKILL.md: %v", err)
	}

	// Initialize path repository
	InitPathRepo(t, repoDir)

	// Add brand new skill with --yes (no scope flags)
	// No existing entry → InheritInstallations finds nothing → writes asset with no scopes (global)
	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{skillDir, "--yes"})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("Failed to add new skill with --yes: %v", err)
	}

	lockFilePath := filepath.Join(repoDir, "sx.lock")
	asset, exists := lockfile.FindAsset(lockFilePath, "brand-new-skill")
	if !exists {
		t.Fatalf("New asset not found in lock file")
	}
	// For a new asset with no existing scopes, InheritInstallations copies nothing,
	// so scopes should be nil/empty (which is treated as global by the system)
	t.Logf("✓ New asset added to lock file with scopes: %v (IsGlobal: %v)", asset.Scopes, asset.IsGlobal())
}
