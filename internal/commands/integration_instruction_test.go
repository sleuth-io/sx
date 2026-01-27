package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClaudeCodeInstructionIntegration tests the full workflow for instruction assets with Claude Code
func TestClaudeCodeInstructionIntegration(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddInstructionToVault(vaultDir, "coding-standards", "1.0.0", `Follow these coding standards:

1. Use meaningful variable names
2. Write unit tests for all functions
3. Document public APIs`)

	// Set up git repo first (needed for scope)
	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")

	// Create lock file with the instruction asset scoped to the repo
	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "coding-standards"
version = "1.0.0"
type = "instruction"

[assets.source-path]
path = "assets/coding-standards/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"
`
	env.WriteLockFile(vaultDir, lockFile)
	env.Chdir(projectDir)

	// Step 1: Install the instruction
	t.Log("Step 1: Install instruction from repository")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Step 2: Verify instruction was injected into CLAUDE.md
	t.Log("Step 2: Verify instruction injection into CLAUDE.md")
	claudeMdPath := filepath.Join(projectDir, "CLAUDE.md")
	env.AssertFileExists(claudeMdPath)

	content, err := os.ReadFile(claudeMdPath)
	if err != nil {
		t.Fatalf("Failed to read CLAUDE.md: %v", err)
	}

	// Verify the managed section exists
	if !strings.Contains(string(content), "## Shared Instructions") {
		t.Errorf("CLAUDE.md missing managed section heading")
	}

	// Verify the instruction title is present
	if !strings.Contains(string(content), "### coding-standards") {
		t.Errorf("CLAUDE.md missing instruction title")
	}

	// Verify the instruction content is present
	if !strings.Contains(string(content), "Use meaningful variable names") {
		t.Errorf("CLAUDE.md missing instruction content")
	}

	// Verify the end marker is present
	if !strings.Contains(string(content), "---") {
		t.Errorf("CLAUDE.md missing end marker")
	}

	t.Log("CLAUDE.md content:")
	t.Log(string(content))

	t.Log("✓ Claude Code instruction integration test passed!")
}

// TestClaudeCodeInstructionUpdate tests updating an existing instruction
func TestClaudeCodeInstructionUpdate(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()

	// Initial version
	env.AddInstructionToVault(vaultDir, "security-rules", "1.0.0", "Original security rules content.")

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")

	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "security-rules"
version = "1.0.0"
type = "instruction"

[assets.source-path]
path = "assets/security-rules/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"
`
	env.WriteLockFile(vaultDir, lockFile)
	env.Chdir(projectDir)

	// First install
	t.Log("Step 1: Initial install")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	claudeMdPath := filepath.Join(projectDir, "CLAUDE.md")
	content1, _ := os.ReadFile(claudeMdPath)
	if !strings.Contains(string(content1), "Original security rules content") {
		t.Errorf("Initial content not found in CLAUDE.md")
	}

	// Update the instruction in vault
	t.Log("Step 2: Update instruction in vault")
	env.AddInstructionToVault(vaultDir, "security-rules", "1.1.0", "Updated security rules with new content.")

	lockFileV2 := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "security-rules"
version = "1.1.0"
type = "instruction"

[assets.source-path]
path = "assets/security-rules/1.1.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"
`
	env.WriteLockFile(vaultDir, lockFileV2)

	// Second install (update)
	t.Log("Step 3: Re-install to get updated content")
	installCmd2 := NewInstallCommand()
	if err := installCmd2.Execute(); err != nil {
		t.Fatalf("Failed to install update: %v", err)
	}

	content2, _ := os.ReadFile(claudeMdPath)
	if !strings.Contains(string(content2), "Updated security rules with new content") {
		t.Errorf("Updated content not found in CLAUDE.md")
	}
	if strings.Contains(string(content2), "Original security rules content") {
		t.Errorf("Old content should not be in CLAUDE.md after update")
	}

	t.Log("✓ Claude Code instruction update test passed!")
}

// TestClaudeCodeMultipleInstructions tests installing multiple instructions
func TestClaudeCodeMultipleInstructions(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()

	// Add multiple instructions
	env.AddInstructionToVault(vaultDir, "api-guidelines", "1.0.0", "Follow REST API best practices.")
	env.AddInstructionToVault(vaultDir, "testing-standards", "1.0.0", "Write comprehensive tests.")

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")

	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "api-guidelines"
version = "1.0.0"
type = "instruction"

[assets.source-path]
path = "assets/api-guidelines/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"

[[assets]]
name = "testing-standards"
version = "1.0.0"
type = "instruction"

[assets.source-path]
path = "assets/testing-standards/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"
`
	env.WriteLockFile(vaultDir, lockFile)
	env.Chdir(projectDir)

	// Install
	t.Log("Installing multiple instructions")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	claudeMdPath := filepath.Join(projectDir, "CLAUDE.md")
	content, _ := os.ReadFile(claudeMdPath)

	// Both instructions should be present
	if !strings.Contains(string(content), "### api-guidelines") {
		t.Errorf("api-guidelines instruction not found")
	}
	if !strings.Contains(string(content), "### testing-standards") {
		t.Errorf("testing-standards instruction not found")
	}
	if !strings.Contains(string(content), "Follow REST API best practices") {
		t.Errorf("api-guidelines content not found")
	}
	if !strings.Contains(string(content), "Write comprehensive tests") {
		t.Errorf("testing-standards content not found")
	}

	// Instructions should be sorted alphabetically (api-guidelines before testing-standards)
	apiIdx := strings.Index(string(content), "### api-guidelines")
	testIdx := strings.Index(string(content), "### testing-standards")
	if apiIdx > testIdx {
		t.Errorf("Instructions should be sorted alphabetically: api-guidelines should come before testing-standards")
	}

	t.Log("✓ Claude Code multiple instructions test passed!")
}

// TestClaudeCodeInstructionWithExistingClaudeMd tests injecting into an existing CLAUDE.md file
func TestClaudeCodeInstructionWithExistingClaudeMd(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddInstructionToVault(vaultDir, "new-instruction", "1.0.0", "New instruction content.")

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")

	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "new-instruction"
version = "1.0.0"
type = "instruction"

[assets.source-path]
path = "assets/new-instruction/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"
`
	env.WriteLockFile(vaultDir, lockFile)

	// Create existing CLAUDE.md with some content
	existingContent := `# My Project

This is my project's CLAUDE.md file with existing content.

## Custom Section

Some custom instructions here.
`
	env.WriteFile(filepath.Join(projectDir, "CLAUDE.md"), existingContent)

	env.Chdir(projectDir)

	// Install
	t.Log("Installing instruction into existing CLAUDE.md")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	claudeMdPath := filepath.Join(projectDir, "CLAUDE.md")
	content, _ := os.ReadFile(claudeMdPath)

	// Original content should be preserved
	if !strings.Contains(string(content), "# My Project") {
		t.Errorf("Original heading not preserved")
	}
	if !strings.Contains(string(content), "Some custom instructions here") {
		t.Errorf("Original custom section not preserved")
	}

	// New instruction should be added
	if !strings.Contains(string(content), "## Shared Instructions") {
		t.Errorf("Managed section not added")
	}
	if !strings.Contains(string(content), "New instruction content") {
		t.Errorf("New instruction content not added")
	}

	t.Log("✓ Claude Code instruction with existing CLAUDE.md test passed!")
}

// TestClaudeCodeInstructionPathScoped tests installing instruction to a specific path
func TestClaudeCodeInstructionPathScoped(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddInstructionToVault(vaultDir, "backend-guidelines", "1.0.0", "Follow these backend guidelines.")

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")

	// Create the backend directory
	backendDir := env.MkdirAll(filepath.Join(projectDir, "backend"))

	// Lock file with path-scoped asset
	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "backend-guidelines"
version = "1.0.0"
type = "instruction"

[assets.source-path]
path = "assets/backend-guidelines/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"
paths = ["backend/"]
`
	env.WriteLockFile(vaultDir, lockFile)

	// Install from repo root - should install to backend/CLAUDE.md
	env.Chdir(projectDir)

	t.Log("Installing path-scoped instruction from repo root")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Verify instruction was installed in backend/CLAUDE.md
	backendClaudeMd := filepath.Join(backendDir, "CLAUDE.md")
	env.AssertFileExists(backendClaudeMd)

	content, err := os.ReadFile(backendClaudeMd)
	if err != nil {
		t.Fatalf("Failed to read backend/CLAUDE.md: %v", err)
	}

	if !strings.Contains(string(content), "Follow these backend guidelines") {
		t.Errorf("backend/CLAUDE.md should contain instruction content")
	}

	// Root CLAUDE.md should NOT exist (instruction is path-scoped only)
	rootClaudeMd := filepath.Join(projectDir, "CLAUDE.md")
	env.AssertFileNotExists(rootClaudeMd)

	t.Log("✓ Claude Code path-scoped instruction test passed!")
}

// TestClaudeCodeInstructionMultiplePaths tests installing instruction to multiple paths
func TestClaudeCodeInstructionMultiplePaths(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddInstructionToVault(vaultDir, "service-guidelines", "1.0.0", "Follow these service guidelines.")

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")

	// Create multiple service directories
	env.MkdirAll(filepath.Join(projectDir, "services", "api"))
	env.MkdirAll(filepath.Join(projectDir, "services", "worker"))

	// Lock file with instruction scoped to multiple paths
	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "service-guidelines"
version = "1.0.0"
type = "instruction"

[assets.source-path]
path = "assets/service-guidelines/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"
paths = ["services/api/", "services/worker/"]
`
	env.WriteLockFile(vaultDir, lockFile)

	// Install from repo root
	env.Chdir(projectDir)

	t.Log("Installing instruction to multiple paths from repo root")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Verify instruction was installed in both locations
	apiClaudeMd := filepath.Join(projectDir, "services", "api", "CLAUDE.md")
	workerClaudeMd := filepath.Join(projectDir, "services", "worker", "CLAUDE.md")

	env.AssertFileExists(apiClaudeMd)
	env.AssertFileExists(workerClaudeMd)

	apiContent, _ := os.ReadFile(apiClaudeMd)
	workerContent, _ := os.ReadFile(workerClaudeMd)

	if !strings.Contains(string(apiContent), "Follow these service guidelines") {
		t.Errorf("services/api/CLAUDE.md should contain instruction content")
	}
	if !strings.Contains(string(workerContent), "Follow these service guidelines") {
		t.Errorf("services/worker/CLAUDE.md should contain instruction content")
	}

	// Root CLAUDE.md should NOT exist
	rootClaudeMd := filepath.Join(projectDir, "CLAUDE.md")
	env.AssertFileNotExists(rootClaudeMd)

	t.Log("✓ Claude Code multiple paths instruction test passed!")
}

// TestClaudeCodeInstructionMixedScopes tests installing repo-scoped and path-scoped instructions together
func TestClaudeCodeInstructionMixedScopes(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()

	// Add a global instruction and a path-scoped instruction
	env.AddInstructionToVault(vaultDir, "global-standards", "1.0.0", "Global coding standards.")
	env.AddInstructionToVault(vaultDir, "frontend-rules", "1.0.0", "Frontend-specific rules.")

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")

	// Create frontend directory
	frontendDir := env.MkdirAll(filepath.Join(projectDir, "frontend"))

	// Lock file with mixed scopes
	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "global-standards"
version = "1.0.0"
type = "instruction"

[assets.source-path]
path = "assets/global-standards/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"

[[assets]]
name = "frontend-rules"
version = "1.0.0"
type = "instruction"

[assets.source-path]
path = "assets/frontend-rules/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"
paths = ["frontend/"]
`
	env.WriteLockFile(vaultDir, lockFile)

	// Install from repo root
	env.Chdir(projectDir)

	t.Log("Installing mixed scope instructions from repo root")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Verify global instruction was installed at root
	rootClaudeMd := filepath.Join(projectDir, "CLAUDE.md")
	env.AssertFileExists(rootClaudeMd)

	rootContent, _ := os.ReadFile(rootClaudeMd)
	if !strings.Contains(string(rootContent), "Global coding standards") {
		t.Errorf("Root CLAUDE.md should contain global instruction")
	}

	// Verify frontend instruction was installed in frontend/
	frontendClaudeMd := filepath.Join(frontendDir, "CLAUDE.md")
	env.AssertFileExists(frontendClaudeMd)

	frontendContent, _ := os.ReadFile(frontendClaudeMd)
	if !strings.Contains(string(frontendContent), "Frontend-specific rules") {
		t.Errorf("frontend/CLAUDE.md should contain frontend instruction")
	}

	// Root CLAUDE.md should NOT contain frontend rules
	if strings.Contains(string(rootContent), "Frontend-specific rules") {
		t.Errorf("Root CLAUDE.md should NOT contain frontend-specific rules")
	}

	// Frontend CLAUDE.md should NOT contain global rules
	if strings.Contains(string(frontendContent), "Global coding standards") {
		t.Errorf("frontend/CLAUDE.md should NOT contain global rules")
	}

	t.Log("✓ Claude Code mixed scope instructions test passed!")
}

// TestClaudeCodeInstructionWithAgentsMd tests installing instruction when CLAUDE.md references AGENTS.md
func TestClaudeCodeInstructionWithAgentsMd(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddInstructionToVault(vaultDir, "cross-tool-instruction", "1.0.0", "Works across tools.")

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")

	// Create AGENTS.md first
	env.WriteFile(filepath.Join(projectDir, "AGENTS.md"), "# Agents\n\nShared instructions.\n")

	// Create CLAUDE.md with @AGENTS.md reference
	env.WriteFile(filepath.Join(projectDir, "CLAUDE.md"), "@AGENTS.md\n")

	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "cross-tool-instruction"
version = "1.0.0"
type = "instruction"

[assets.source-path]
path = "assets/cross-tool-instruction/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"
`
	env.WriteLockFile(vaultDir, lockFile)

	env.Chdir(projectDir)

	t.Log("Installing instruction when CLAUDE.md references AGENTS.md")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Verify instruction was installed in AGENTS.md, not CLAUDE.md
	agentsMdPath := filepath.Join(projectDir, "AGENTS.md")
	agentsContent, _ := os.ReadFile(agentsMdPath)

	if !strings.Contains(string(agentsContent), "Works across tools") {
		t.Errorf("AGENTS.md should contain the instruction content")
	}

	// CLAUDE.md should still just reference AGENTS.md
	claudeMdPath := filepath.Join(projectDir, "CLAUDE.md")
	claudeContent, _ := os.ReadFile(claudeMdPath)

	if !strings.Contains(string(claudeContent), "@AGENTS.md") {
		t.Errorf("CLAUDE.md should still contain @AGENTS.md reference")
	}
	if strings.Contains(string(claudeContent), "Works across tools") {
		t.Errorf("CLAUDE.md should NOT contain instruction content directly")
	}

	t.Log("✓ Claude Code instruction with AGENTS.md test passed!")
}
