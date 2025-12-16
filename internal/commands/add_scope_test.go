package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/skills/internal/lockfile"
)

// TestAddScopeModification tests modifying an existing artifact's scope
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
	skillMetadata := `[artifact]
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
		ExpectConfirm("correct", true).         // Confirm detected artifact
		ExpectPrompt("Version", "1.0.0").       // Enter version
		ExpectPrompt("choice", "1").            // Option 1: Make it available globally
		ExpectConfirm("Run install now", false) // Don't run install

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{skillDir})
	if err := ExecuteWithPrompter(addCmd, mockPrompter); err != nil {
		t.Fatalf("Failed to add skill: %v", err)
	}

	// Verify skill was added globally
	lockFilePath := filepath.Join(repoDir, "sx.lock")
	artifact, exists := lockfile.FindArtifact(lockFilePath, "test-skill")
	if !exists {
		t.Fatalf("Artifact not found in lock file")
	}
	if !artifact.IsGlobal() {
		t.Fatalf("Expected global scope, got repository-specific")
	}
	t.Log("✓ Artifact added with global scope")

	// Step 3: Reconfigure to repository-specific scope
	// When configuring existing artifact, it shows:
	// Option 1: Keep current settings (new option for existing artifacts)
	// Option 2: Make it available globally
	// Option 3: Add/modify repository-specific installations
	// Option 4: Remove from installation
	t.Log("Step 3: Reconfigure artifact to repository-specific")
	mockPrompter2 := NewMockPrompter().
		ExpectPrompt("choice", "3").                         // Option 3: Add/modify repository-specific
		ExpectPrompt("choice", "1").                         // In modify menu, option 1: Add new repository
		ExpectPrompt("URL", "https://github.com/user/repo"). // Repository URL
		ExpectConfirm("entire repository", true).            // Yes, entire repository
		ExpectPrompt("choice", "4").                         // Option 4: Done with modifications
		ExpectConfirm("Continue with these changes", true).  // Confirm changes
		ExpectConfirm("Run install now", false)              // Don't run install

	addCmd2 := NewAddCommand()
	addCmd2.SetArgs([]string{"test-skill"}) // Configure existing artifact by name
	if err := ExecuteWithPrompter(addCmd2, mockPrompter2); err != nil {
		t.Fatalf("Failed to reconfigure skill: %v", err)
	}

	// Verify scope changed to repository-specific
	artifact2, exists := lockfile.FindArtifact(lockFilePath, "test-skill")
	if !exists {
		t.Fatalf("Artifact not found in lock file after reconfiguration")
	}
	if artifact2.IsGlobal() {
		t.Fatalf("Expected repository-specific scope, got global")
	}
	if len(artifact2.Repositories) != 1 {
		t.Fatalf("Expected 1 repository, got %d", len(artifact2.Repositories))
	}
	if artifact2.Repositories[0].Repo != "https://github.com/user/repo" {
		t.Fatalf("Expected repo https://github.com/user/repo, got %s", artifact2.Repositories[0].Repo)
	}
	t.Log("✓ Artifact reconfigured to repository-specific scope")
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
	skillMetadata := `[artifact]
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
		ExpectConfirm("correct", true).                       // Confirm detected artifact
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
	lockFilePath := filepath.Join(repoDir, "sx.lock")
	artifact, exists := lockfile.FindArtifact(lockFilePath, "test-skill")
	if !exists {
		t.Fatalf("Artifact not found in lock file")
	}
	initialRepoCount := len(artifact.Repositories)
	t.Logf("✓ Artifact added with %d repository", initialRepoCount)

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
	artifact2, exists := lockfile.FindArtifact(lockFilePath, "test-skill")
	if !exists {
		t.Fatalf("Artifact not found in lock file after reconfiguration")
	}
	if len(artifact2.Repositories) != initialRepoCount {
		t.Fatalf("Expected %d repositories, got %d", initialRepoCount, len(artifact2.Repositories))
	}
	if artifact2.Repositories[0].Repo != artifact.Repositories[0].Repo {
		t.Fatalf("Repository changed unexpectedly")
	}
	t.Log("✓ Configuration preserved correctly")
}

// TestAddRemoveFromInstallation tests removing artifact from lock file
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
	skillMetadata := `[artifact]
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
		ExpectConfirm("correct", true).         // Confirm detected artifact
		ExpectPrompt("Version", "1.0.0").       // Enter version
		ExpectPrompt("choice", "1").            // Option 1: Make it available globally
		ExpectConfirm("Run install now", false) // Don't run install

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{skillDir})
	if err := ExecuteWithPrompter(addCmd, mockPrompter); err != nil {
		t.Fatalf("Failed to add skill: %v", err)
	}

	// Verify skill was added
	lockFilePath := filepath.Join(repoDir, "sx.lock")
	_, exists := lockfile.FindArtifact(lockFilePath, "test-skill")
	if !exists {
		t.Fatalf("Artifact not found in lock file")
	}
	t.Log("✓ Artifact added to lock file")

	// Step 3: Remove from installation
	// For existing artifacts: Option 4 = Remove from installation
	t.Log("Step 3: Remove artifact from installation")
	mockPrompter2 := NewMockPrompter().
		ExpectPrompt("choice", "4").            // Option 4: Remove from installation
		ExpectConfirm("Run install now", false) // Don't run install

	addCmd2 := NewAddCommand()
	addCmd2.SetArgs([]string{"test-skill"}) // Configure existing artifact by name
	if err := ExecuteWithPrompter(addCmd2, mockPrompter2); err != nil {
		t.Fatalf("Failed to remove skill: %v", err)
	}

	// Verify artifact removed from lock file
	_, exists = lockfile.FindArtifact(lockFilePath, "test-skill")
	if exists {
		t.Fatalf("Artifact should have been removed from lock file")
	}
	t.Log("✓ Artifact removed from lock file")

	// Verify artifact still exists in repository
	artifactDir := filepath.Join(repoDir, "artifacts", "test-skill", "1.0.0")
	if _, err := os.Stat(artifactDir); os.IsNotExist(err) {
		t.Fatalf("Artifact should still exist in repository: %s", artifactDir)
	}
	t.Log("✓ Artifact still available in repository")
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
	skillMetadata := `[artifact]
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
	// For new artifacts: Option 3 = Remove from installation (don't install)
	t.Log("Step 2: Add test skill but don't install")
	mockPrompter := NewMockPrompter().
		ExpectConfirm("correct", true).         // Confirm detected artifact
		ExpectPrompt("Version", "1.0.0").       // Enter version
		ExpectPrompt("choice", "3").            // Option 3: Remove from installation (don't install)
		ExpectConfirm("Run install now", false) // Don't run install

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{skillDir})
	if err := ExecuteWithPrompter(addCmd, mockPrompter); err != nil {
		t.Fatalf("Failed to add skill: %v", err)
	}

	// Verify artifact NOT in lock file
	lockFilePath := filepath.Join(repoDir, "sx.lock")
	_, exists := lockfile.FindArtifact(lockFilePath, "test-skill")
	if exists {
		t.Fatalf("Artifact should not be in lock file")
	}
	t.Log("✓ Artifact not added to lock file (as expected)")

	// Verify artifact exists in repository
	artifactDir := filepath.Join(repoDir, "artifacts", "test-skill", "1.0.0")
	if _, err := os.Stat(artifactDir); os.IsNotExist(err) {
		t.Fatalf("Artifact should exist in repository: %s", artifactDir)
	}
	t.Log("✓ Artifact available in repository only")

	// Step 3: Now install it globally by name
	// When artifact exists in repo but not in lock file, it shows options for first-time install
	t.Log("Step 3: Install artifact globally by name")
	mockPrompter2 := NewMockPrompter().
		ExpectPrompt("choice", "1").            // Option 1: Make it available globally
		ExpectConfirm("Run install now", false) // Don't run install

	addCmd2 := NewAddCommand()
	addCmd2.SetArgs([]string{"test-skill"}) // Configure by name
	if err := ExecuteWithPrompter(addCmd2, mockPrompter2); err != nil {
		t.Fatalf("Failed to install skill: %v", err)
	}

	// Verify artifact now in lock file as global
	artifact, exists := lockfile.FindArtifact(lockFilePath, "test-skill")
	if !exists {
		t.Fatalf("Artifact not found in lock file after installation")
	}
	if !artifact.IsGlobal() {
		t.Fatalf("Expected global scope")
	}
	t.Log("✓ Artifact successfully installed globally")
}
