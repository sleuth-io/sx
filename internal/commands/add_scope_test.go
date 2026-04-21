package commands

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAddScopeModification tests modifying an existing asset's scope
func TestAddScopeModification(t *testing.T) {
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
	t.Setenv("SX_CONFIG_DIR", filepath.Join(homeDir, ".config", "sx"))
	t.Setenv("SX_CACHE_DIR", filepath.Join(homeDir, ".cache", "sx"))
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

	readmeContent := "# Test Skill\n\nThis is a test skill."
	if err := os.WriteFile(filepath.Join(skillDir, "README.md"), []byte(readmeContent), 0644); err != nil {
		t.Fatalf("Failed to write README.md: %v", err)
	}

	skillPromptContent := "You are a helpful assistant for testing."
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillPromptContent), 0644); err != nil {
		t.Fatalf("Failed to write SKILL.md: %v", err)
	}

	// Initialize path repository
	t.Log("Step 1: Initialize with path repository")
	InitPathRepo(t, repoDir)

	// Step 2: Add skill with global scope
	// New UI flow: only one prompt for "Enter choice [1-3]:"
	// Option 1 = Make it available globally
	t.Log("Step 2: Add test skill with global scope")
	mockPrompter := NewMockPrompter().
		ExpectConfirm("correct", true).         // Confirm detected asset
		ExpectPrompt("Version", "1.0.0").       // Enter version
		ExpectPrompt("choice", "1").            // Option 1: Make it available globally
		ExpectConfirm("Run install now", false) // Don't run install

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{skillDir})
	if err := ExecuteWithPrompter(addCmd, mockPrompter); err != nil {
		t.Fatalf("Failed to add skill: %v", err)
	}

	// Verify skill was added globally
	asset, exists := findManifestAsset(t, repoDir, "test-skill")
	if !exists {
		t.Fatalf("Asset not found in lock file")
	}
	if !asset.IsGlobal() {
		t.Fatalf("Expected global scope, got repository-specific")
	}
	t.Log("✓ Asset added with global scope")

	// Step 3: Reconfigure to repository-specific scope
	// When configuring existing asset, it shows:
	// Option 1: Keep current settings (new option for existing assets)
	// Option 2: Make it available globally
	// Option 3: Add/modify repository-specific installations
	// Option 4: Remove from installation
	t.Log("Step 3: Reconfigure asset to repository-specific")
	mockPrompter2 := NewMockPrompter().
		ExpectPrompt("choice", "3").                         // Option 3: Add/modify repository-specific
		ExpectPrompt("choice", "1").                         // In modify menu, option 1: Add new repository
		ExpectPrompt("URL", "https://github.com/user/repo"). // Repository URL
		ExpectConfirm("entire repository", true).            // Yes, entire repository
		ExpectPrompt("choice", "4").                         // Option 4: Done with modifications
		ExpectConfirm("Continue with these changes", true).  // Confirm changes
		ExpectConfirm("Run install now", false)              // Don't run install

	addCmd2 := NewAddCommand()
	addCmd2.SetArgs([]string{"test-skill"}) // Configure existing asset by name
	if err := ExecuteWithPrompter(addCmd2, mockPrompter2); err != nil {
		t.Fatalf("Failed to reconfigure skill: %v", err)
	}

	// Verify scope changed to repository-specific
	asset2, exists := findManifestAsset(t, repoDir, "test-skill")
	if !exists {
		t.Fatalf("Asset not found in lock file after reconfiguration")
	}
	if asset2.IsGlobal() {
		t.Fatalf("Expected repository-specific scope, got global")
	}
	if len(asset2.Scopes) != 1 {
		t.Fatalf("Expected 1 repository, got %d", len(asset2.Scopes))
	}
	if asset2.Scopes[0].Repo != "https://github.com/user/repo" {
		t.Fatalf("Expected repo https://github.com/user/repo, got %s", asset2.Scopes[0].Repo)
	}
	t.Log("✓ Asset reconfigured to repository-specific scope")
}

// TestAddKeepCurrentSettings tests keeping existing scope unchanged
func TestAddKeepCurrentSettings(t *testing.T) {
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
	t.Setenv("SX_CONFIG_DIR", filepath.Join(homeDir, ".config", "sx"))
	t.Setenv("SX_CACHE_DIR", filepath.Join(homeDir, ".cache", "sx"))
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

	readmeContent := "# Test Skill\n\nThis is a test skill."
	if err := os.WriteFile(filepath.Join(skillDir, "README.md"), []byte(readmeContent), 0644); err != nil {
		t.Fatalf("Failed to write README.md: %v", err)
	}

	skillPromptContent := "You are a helpful assistant for testing."
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillPromptContent), 0644); err != nil {
		t.Fatalf("Failed to write SKILL.md: %v", err)
	}

	// Initialize path repository
	t.Log("Step 1: Initialize with path repository")
	InitPathRepo(t, repoDir)

	// Step 2: Add skill with repository-specific scope
	t.Log("Step 2: Add test skill with repository-specific scope")
	mockPrompter := NewMockPrompter().
		ExpectConfirm("correct", true).                       // Confirm detected asset
		ExpectPrompt("Version", "1.0.0").                     // Enter version
		ExpectPrompt("choice", "2").                          // Option 2: Add/modify repository-specific
		ExpectPrompt("choice", "1").                          // Add new repository
		ExpectPrompt("URL", "https://github.com/user/repo1"). // Repository URL
		ExpectConfirm("entire repository", false).            // No, specific paths
		ExpectPrompt("path", "/backend").                     // Path
		ExpectConfirm("Add another path", false).             // No more paths
		ExpectPrompt("choice", "4").                          // Done with modifications
		ExpectConfirm("Continue with these changes", true).   // Confirm changes
		ExpectConfirm("Run install now", false)               // Don't run install

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{skillDir})
	if err := ExecuteWithPrompter(addCmd, mockPrompter); err != nil {
		t.Fatalf("Failed to add skill: %v", err)
	}

	// Get initial state
	asset, exists := findManifestAsset(t, repoDir, "test-skill")
	if !exists {
		t.Fatalf("Asset not found in lock file")
	}
	initialRepoCount := len(asset.Scopes)
	t.Logf("✓ Asset added with %d repository", initialRepoCount)

	// Step 3: Reconfigure but keep current settings (option 1)
	// After Step 2 added a repo, currentRepos != nil, so "Keep current" is option 1
	t.Log("Step 3: Keep current settings when reconfiguring")
	mockPrompter2 := NewMockPrompter().
		ExpectPrompt("choice", "1").            // Option 1: Keep current settings
		ExpectConfirm("Run install now", false) // Don't run install

	addCmd2 := NewAddCommand()
	addCmd2.SetArgs([]string{"test-skill"})
	if err := ExecuteWithPrompter(addCmd2, mockPrompter2); err != nil {
		t.Fatalf("Failed to reconfigure skill: %v", err)
	}

	// Verify configuration unchanged
	asset2, exists := findManifestAsset(t, repoDir, "test-skill")
	if !exists {
		t.Fatalf("Asset not found in lock file after reconfiguration")
	}
	if len(asset2.Scopes) != initialRepoCount {
		t.Fatalf("Expected %d repositories, got %d", initialRepoCount, len(asset2.Scopes))
	}
	if asset2.Scopes[0].Repo != asset.Scopes[0].Repo {
		t.Fatalf("Repository changed unexpectedly")
	}
	t.Log("✓ Configuration preserved correctly")
}

// TestAddRemoveFromInstallation tests removing asset from lock file
func TestAddRemoveFromInstallation(t *testing.T) {
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
	t.Setenv("SX_CONFIG_DIR", filepath.Join(homeDir, ".config", "sx"))
	t.Setenv("SX_CACHE_DIR", filepath.Join(homeDir, ".cache", "sx"))
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

	readmeContent := "# Test Skill\n\nThis is a test skill."
	if err := os.WriteFile(filepath.Join(skillDir, "README.md"), []byte(readmeContent), 0644); err != nil {
		t.Fatalf("Failed to write README.md: %v", err)
	}

	skillPromptContent := "You are a helpful assistant for testing."
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillPromptContent), 0644); err != nil {
		t.Fatalf("Failed to write SKILL.md: %v", err)
	}

	// Initialize path repository
	t.Log("Step 1: Initialize with path repository")
	InitPathRepo(t, repoDir)

	// Step 2: Add skill with global scope
	t.Log("Step 2: Add test skill with global scope")
	mockPrompter := NewMockPrompter().
		ExpectConfirm("correct", true).         // Confirm detected asset
		ExpectPrompt("Version", "1.0.0").       // Enter version
		ExpectPrompt("choice", "1").            // Option 1: Make it available globally
		ExpectConfirm("Run install now", false) // Don't run install

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{skillDir})
	if err := ExecuteWithPrompter(addCmd, mockPrompter); err != nil {
		t.Fatalf("Failed to add skill: %v", err)
	}

	// Verify skill was added
	_, exists := findManifestAsset(t, repoDir, "test-skill")
	if !exists {
		t.Fatalf("Asset not found in lock file")
	}
	t.Log("✓ Asset added to lock file")

	// Step 3: Remove from installation
	// For existing assets: Option 4 = Remove from installation
	t.Log("Step 3: Remove asset from installation")
	mockPrompter2 := NewMockPrompter().
		ExpectPrompt("choice", "4").            // Option 4: Remove from installation
		ExpectConfirm("Run install now", false) // Don't run install

	addCmd2 := NewAddCommand()
	addCmd2.SetArgs([]string{"test-skill"}) // Configure existing asset by name
	if err := ExecuteWithPrompter(addCmd2, mockPrompter2); err != nil {
		t.Fatalf("Failed to remove skill: %v", err)
	}

	// Verify asset removed from lock file
	_, exists = findManifestAsset(t, repoDir, "test-skill")
	if exists {
		t.Fatalf("Asset should have been removed from lock file")
	}
	t.Log("✓ Asset removed from lock file")

	// Verify asset still exists in repository
	assetDir := filepath.Join(repoDir, "assets", "test-skill", "1.0.0")
	if _, err := os.Stat(assetDir); os.IsNotExist(err) {
		t.Fatalf("Asset should still exist in repository: %s", assetDir)
	}
	t.Log("✓ Asset still available in repository")
}

// TestAddFirstTimeNoScopes tests first-time installation workflow
func TestAddFirstTimeNoScopes(t *testing.T) {
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
	t.Setenv("SX_CONFIG_DIR", filepath.Join(homeDir, ".config", "sx"))
	t.Setenv("SX_CACHE_DIR", filepath.Join(homeDir, ".cache", "sx"))
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

	readmeContent := "# Test Skill\n\nThis is a test skill."
	if err := os.WriteFile(filepath.Join(skillDir, "README.md"), []byte(readmeContent), 0644); err != nil {
		t.Fatalf("Failed to write README.md: %v", err)
	}

	skillPromptContent := "You are a helpful assistant for testing."
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillPromptContent), 0644); err != nil {
		t.Fatalf("Failed to write SKILL.md: %v", err)
	}

	// Initialize path repository
	t.Log("Step 1: Initialize with path repository")
	InitPathRepo(t, repoDir)

	// Step 2: Add skill but choose not to install
	// For new assets: Option 3 = Remove from installation (don't install)
	t.Log("Step 2: Add test skill but don't install")
	mockPrompter := NewMockPrompter().
		ExpectConfirm("correct", true).         // Confirm detected asset
		ExpectPrompt("Version", "1.0.0").       // Enter version
		ExpectPrompt("choice", "3").            // Option 3: Remove from installation (don't install)
		ExpectConfirm("Run install now", false) // Don't run install

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{skillDir})
	if err := ExecuteWithPrompter(addCmd, mockPrompter); err != nil {
		t.Fatalf("Failed to add skill: %v", err)
	}

	// Verify asset NOT in lock file
	_, exists := findManifestAsset(t, repoDir, "test-skill")
	if exists {
		t.Fatalf("Asset should not be in lock file")
	}
	t.Log("✓ Asset not added to lock file (as expected)")

	// Verify asset exists in repository
	assetDir := filepath.Join(repoDir, "assets", "test-skill", "1.0.0")
	if _, err := os.Stat(assetDir); os.IsNotExist(err) {
		t.Fatalf("Asset should exist in repository: %s", assetDir)
	}
	t.Log("✓ Asset available in repository only")

	// Step 3: Now install it globally by name
	// When asset exists in repo but not in lock file, it shows options for first-time install
	t.Log("Step 3: Install asset globally by name")
	mockPrompter2 := NewMockPrompter().
		ExpectPrompt("choice", "1").            // Option 1: Make it available globally
		ExpectConfirm("Run install now", false) // Don't run install

	addCmd2 := NewAddCommand()
	addCmd2.SetArgs([]string{"test-skill"}) // Configure by name
	if err := ExecuteWithPrompter(addCmd2, mockPrompter2); err != nil {
		t.Fatalf("Failed to install skill: %v", err)
	}

	// Verify asset now in lock file as global
	asset, exists := findManifestAsset(t, repoDir, "test-skill")
	if !exists {
		t.Fatalf("Asset not found in lock file after installation")
	}
	if !asset.IsGlobal() {
		t.Fatalf("Expected global scope")
	}
	t.Log("✓ Asset successfully installed globally")
}
