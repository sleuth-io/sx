package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPathRepositoryIntegration tests the full workflow with a path repository
func TestPathRepositoryIntegration(t *testing.T) {
	// Create fully isolated test environment
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	workingDir := filepath.Join(tempDir, "working")
	repoDir := filepath.Join(workingDir, "repo")
	skillDir := filepath.Join(workingDir, "skill")

	// Create home and working directories (but NOT repo - let init create it)
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

	// Set environment for complete sandboxing
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	t.Setenv("CLAUDE_DIR", filepath.Join(homeDir, ".claude"))

	// Create a test skill with metadata
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

	// Step 1: Initialize with path repository
	t.Log("Step 1: Initialize with path repository")

	// Use init command interactively with mock prompter
	initPrompter := NewMockPrompter().
		ExpectPrompt("Enter choice", "1").       // Choose path repository (option 1)
		ExpectPrompt("Repository path", repoDir) // Enter repo path
		// Note: Directory creation happens automatically in configurePathRepo

	initCmd := NewInitCommand()
	if err := ExecuteWithPrompter(initCmd, initPrompter); err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	// Verify repo directory was created by init
	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		t.Fatalf("Init did not create repo directory: %s", repoDir)
	}

	// Step 2: Add the test skill to the repository using 'add' command
	t.Log("Step 2: Add test skill to repository")

	// Create add command with mock prompter
	mockPrompter := NewMockPrompter().
		ExpectConfirm("correct", true).       // Confirm artifact name/type
		ExpectPrompt("Version", "1.0.0").     // Enter version
		ExpectPrompt("Choose an option", "1") // Installation scope: make available globally

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{skillDir})

	if err := ExecuteWithPrompter(addCmd, mockPrompter); err != nil {
		t.Fatalf("Failed to add skill: %v", err)
	}

	// Verify artifacts directory was created
	artifactsDir := filepath.Join(repoDir, "artifacts", "test-skill", "1.0.0")
	if _, err := os.Stat(artifactsDir); os.IsNotExist(err) {
		t.Fatalf("Artifacts directory was not created: %s", artifactsDir)
	}

	// Debug: List files in artifacts directory
	files, _ := os.ReadDir(artifactsDir)
	t.Log("Files in artifacts directory:")
	for _, file := range files {
		t.Logf("  - %s", file.Name())
	}

	// Verify skill.lock was created in repo
	lockPath := filepath.Join(repoDir, "skill.lock")
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatalf("skill.lock was not created: %s", lockPath)
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
	claudeDir := filepath.Join(homeDir, ".claude")
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
