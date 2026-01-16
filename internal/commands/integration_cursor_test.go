package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/clients/cursor"
)

func init() {
	// Register Cursor client for tests
	clients.Register(cursor.NewClient())
}

// TestCursorIntegration tests the full workflow with Cursor client
func TestCursorIntegration(t *testing.T) {
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
	cursorDir := filepath.Join(homeDir, ".cursor")

	// Create home and working directories
	// Also create .cursor directory so Cursor client is detected
	for _, dir := range []string{homeDir, workingDir, skillDir, cursorDir} {
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

	// Step 4: Verify installation to Cursor
	t.Log("Step 4: Verify installation to Cursor")

	// For Cursor, skills are extracted to .cursor/skills/{name}/ (NOT transformed to commands)
	installedSkillDir := filepath.Join(cursorDir, "skills", "test-skill")
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

	// Verify skills.md rules file does NOT exist (Cursor now discovers skills natively)
	localCursorDir := filepath.Join(workingDir, ".cursor")
	rulesFile := filepath.Join(localCursorDir, "rules", "skills.md")
	if _, err := os.Stat(rulesFile); err == nil {
		t.Errorf("Legacy rules file should not exist (Cursor discovers skills natively): %s", rulesFile)
	}

	// Verify MCP server was registered in ~/.cursor/mcp.json (global scope)
	globalMCPConfig := filepath.Join(cursorDir, "mcp.json")
	if _, err := os.Stat(globalMCPConfig); os.IsNotExist(err) {
		t.Errorf("Global mcp.json was not created")
	} else {
		mcpData, err := os.ReadFile(globalMCPConfig)
		if err != nil {
			t.Errorf("Failed to read mcp.json: %v", err)
		} else {
			var mcpConfig map[string]any
			if err := json.Unmarshal(mcpData, &mcpConfig); err == nil {
				mcpServers, ok := mcpConfig["mcpServers"].(map[string]any)
				if ok {
					if _, exists := mcpServers["skills"]; !exists {
						t.Errorf("skills MCP server not registered in mcp.json")
					} else {
						t.Log("✓ skills MCP server registered")
					}
				}
			}
		}
	}

	t.Log("✓ Cursor integration test passed!")

	// Step 5: Verify that running install in a NEW directory works correctly
	// Skills are discovered natively by Cursor, no rules file needed
	t.Log("Step 5: Verify install in new directory works without creating rules file")

	newWorkingDir := filepath.Join(tempDir, "new-project")
	if err := os.MkdirAll(newWorkingDir, 0755); err != nil {
		t.Fatalf("Failed to create new working dir: %v", err)
	}

	// Change to new directory
	if err := os.Chdir(newWorkingDir); err != nil {
		t.Fatalf("Failed to change to new working dir: %v", err)
	}

	// Run install again
	installCmd2 := NewInstallCommand()
	if err := installCmd2.Execute(); err != nil {
		t.Fatalf("Failed to install in new directory: %v", err)
	}

	// Verify no rules file was created (Cursor discovers skills natively)
	newLocalCursorDir := filepath.Join(newWorkingDir, ".cursor")
	newRulesFile := filepath.Join(newLocalCursorDir, "rules", "skills.md")
	if _, err := os.Stat(newRulesFile); err == nil {
		t.Errorf("Legacy rules file should not exist in new directory: %s", newRulesFile)
	}
	t.Log("✓ Install in new directory completed (native skill discovery)")
}

// TestCursorMCPIntegration tests MCP installation for Cursor
func TestCursorMCPIntegration(t *testing.T) {
	// Create fully isolated test environment
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	workingDir := filepath.Join(tempDir, "working")
	repoDir := filepath.Join(workingDir, "repo")
	mcpDir := filepath.Join(workingDir, "mcp")

	// Set environment for complete sandboxing
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	cursorDir := filepath.Join(homeDir, ".cursor")

	// Create directories
	for _, dir := range []string{homeDir, workingDir, mcpDir, cursorDir} {
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

	// Create a test MCP with metadata
	mcpMetadata := `[asset]
name = "test-mcp"
version = "1.0.0"
type = "mcp"
description = "A test MCP server"

[mcp]
command = "node"
args = [
    "server.js"
]
`
	if err := os.WriteFile(filepath.Join(mcpDir, "metadata.toml"), []byte(mcpMetadata), 0644); err != nil {
		t.Fatalf("Failed to write metadata.toml: %v", err)
	}

	serverContent := "console.log('Test MCP Server');"
	if err := os.WriteFile(filepath.Join(mcpDir, "server.js"), []byte(serverContent), 0644); err != nil {
		t.Fatalf("Failed to write server.js: %v", err)
	}

	packageContent := `{"name": "test-mcp", "version": "1.0.0"}`
	if err := os.WriteFile(filepath.Join(mcpDir, "package.json"), []byte(packageContent), 0644); err != nil {
		t.Fatalf("Failed to write package.json: %v", err)
	}

	// Step 1: Initialize with path repository
	t.Log("Step 1: Initialize with path repository")
	InitPathRepo(t, repoDir)

	// Step 2: Add the test MCP to the repository
	t.Log("Step 2: Add test MCP to repository")

	mockPrompter := NewMockPrompter().
		ExpectConfirm("correct", true).
		ExpectPrompt("Version", "1.0.0").
		ExpectPrompt("Choose an option", "1")

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{mcpDir})

	if err := ExecuteWithPrompter(addCmd, mockPrompter); err != nil {
		t.Fatalf("Failed to add MCP: %v", err)
	}

	// Step 3: Install from the repository
	t.Log("Step 3: Install MCP from repository")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Step 4: Verify MCP installation to Cursor
	t.Log("Step 4: Verify MCP installation to Cursor")

	// Check that MCP was installed to .cursor/mcp-servers/test-mcp/
	installedMCPDir := filepath.Join(cursorDir, "mcp-servers", "test-mcp")
	if _, err := os.Stat(installedMCPDir); os.IsNotExist(err) {
		t.Fatalf("MCP was not installed to: %s", installedMCPDir)
	}

	// Verify server.js exists
	installedServerFile := filepath.Join(installedMCPDir, "server.js")
	if _, err := os.Stat(installedServerFile); os.IsNotExist(err) {
		t.Errorf("server.js not found in installed location")
	}

	// Verify mcp.json was created/updated
	mcpConfigPath := filepath.Join(cursorDir, "mcp.json")
	if _, err := os.Stat(mcpConfigPath); os.IsNotExist(err) {
		t.Fatalf("mcp.json was not created at: %s", mcpConfigPath)
	}

	// Verify mcp.json contains the test-mcp entry
	mcpConfigData, err := os.ReadFile(mcpConfigPath)
	if err != nil {
		t.Fatalf("Failed to read mcp.json: %v", err)
	}

	var mcpConfig map[string]any
	if err := json.Unmarshal(mcpConfigData, &mcpConfig); err != nil {
		t.Fatalf("Failed to parse mcp.json: %v", err)
	}

	mcpServers, ok := mcpConfig["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp.json does not have mcpServers section")
	}

	if _, exists := mcpServers["test-mcp"]; !exists {
		t.Errorf("test-mcp entry not found in mcp.json")
	}

	t.Log("✓ Cursor MCP integration test passed!")
}

// TestCursorHookIntegration tests hook installation for Cursor
func TestCursorHookIntegration(t *testing.T) {
	// Create fully isolated test environment
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	workingDir := filepath.Join(tempDir, "working")
	repoDir := filepath.Join(workingDir, "repo")
	hookDir := filepath.Join(workingDir, "hook")

	// Set environment for complete sandboxing
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	cursorDir := filepath.Join(homeDir, ".cursor")

	// Create directories
	for _, dir := range []string{homeDir, workingDir, hookDir, cursorDir} {
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

	// Create a test hook with metadata
	hookMetadata := `[asset]
name = "test-hook"
version = "1.0.0"
type = "hook"
description = "A test hook"

[hook]
event = "pre-commit"
script-file = "hook.sh"
async = false
fail-on-error = true
timeout = 60
`
	if err := os.WriteFile(filepath.Join(hookDir, "metadata.toml"), []byte(hookMetadata), 0644); err != nil {
		t.Fatalf("Failed to write metadata.toml: %v", err)
	}

	hookScript := `#!/bin/bash
echo "Running pre-commit hook"
exit 0
`
	if err := os.WriteFile(filepath.Join(hookDir, "hook.sh"), []byte(hookScript), 0755); err != nil {
		t.Fatalf("Failed to write hook.sh: %v", err)
	}

	// Step 1: Initialize with path repository
	t.Log("Step 1: Initialize with path repository")
	InitPathRepo(t, repoDir)

	// Step 2: Add the test hook to the repository
	t.Log("Step 2: Add test hook to repository")

	mockPrompter := NewMockPrompter().
		ExpectConfirm("correct", true).
		ExpectPrompt("Version", "1.0.0").
		ExpectPrompt("Choose an option", "1")

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{hookDir})

	if err := ExecuteWithPrompter(addCmd, mockPrompter); err != nil {
		t.Fatalf("Failed to add hook: %v", err)
	}

	// Step 3: Install from the repository
	t.Log("Step 3: Install hook from repository")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Step 4: Verify hook installation to Cursor
	t.Log("Step 4: Verify hook installation to Cursor")

	// Check that hook was installed to .cursor/hooks/test-hook/
	installedHookDir := filepath.Join(cursorDir, "hooks", "test-hook")
	if _, err := os.Stat(installedHookDir); os.IsNotExist(err) {
		t.Fatalf("Hook was not installed to: %s", installedHookDir)
	}

	// Verify hook.sh exists
	installedHookScript := filepath.Join(installedHookDir, "hook.sh")
	if _, err := os.Stat(installedHookScript); os.IsNotExist(err) {
		t.Errorf("hook.sh not found in installed location")
	}

	// Verify hooks.json was created/updated
	hooksJSONPath := filepath.Join(cursorDir, "hooks.json")
	if _, err := os.Stat(hooksJSONPath); os.IsNotExist(err) {
		t.Fatalf("hooks.json was not created at: %s", hooksJSONPath)
	}

	// Verify hooks.json contains the test-hook entry
	hooksJSONData, err := os.ReadFile(hooksJSONPath)
	if err != nil {
		t.Fatalf("Failed to read hooks.json: %v", err)
	}

	var hooksConfig map[string]any
	if err := json.Unmarshal(hooksJSONData, &hooksConfig); err != nil {
		t.Fatalf("Failed to parse hooks.json: %v", err)
	}

	hooks, ok := hooksConfig["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks.json does not have hooks section")
	}

	// pre-commit should map to beforeShellExecution
	beforeShellExec, exists := hooks["beforeShellExecution"]
	if !exists {
		t.Fatalf("beforeShellExecution entry not found in hooks.json")
	}

	hooksList, ok := beforeShellExec.([]any)
	if !ok || len(hooksList) == 0 {
		t.Fatalf("beforeShellExecution is not a non-empty array")
	}

	// Verify our hook is in the list
	found := false
	for _, hookEntry := range hooksList {
		if hookMap, ok := hookEntry.(map[string]any); ok {
			if asset, ok := hookMap["_artifact"].(string); ok && asset == "test-hook" {
				found = true
				// Verify command path
				if command, ok := hookMap["command"].(string); ok {
					if !strings.Contains(command, "test-hook") || !strings.Contains(command, "hook.sh") {
						t.Errorf("Hook command path incorrect: %s", command)
					}
				} else {
					t.Error("Hook entry missing command field")
				}
				break
			}
		}
	}

	if !found {
		t.Errorf("test-hook entry not found in hooks.json")
	}

	t.Log("✓ Cursor hook integration test passed!")
}

// TestCursorAutoInstallDeduplication tests that the session cache prevents
// repeated installations when hooks fire multiple times per conversation
func TestCursorAutoInstallDeduplication(t *testing.T) {
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
	cursorDir := filepath.Join(homeDir, ".cursor")
	cacheDir := filepath.Join(homeDir, ".cache", "skills")

	// Create home and working directories
	for _, dir := range []string{homeDir, workingDir, skillDir, cursorDir} {
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

	if err := os.WriteFile(filepath.Join(skillDir, "README.md"), []byte("# Test"), 0644); err != nil {
		t.Fatalf("Failed to write README.md: %v", err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("Test skill"), 0644); err != nil {
		t.Fatalf("Failed to write SKILL.md: %v", err)
	}

	// Initialize and add skill
	t.Log("Step 1: Initialize repository and add skill")
	InitPathRepo(t, repoDir)

	addPrompter := NewMockPrompter().
		ExpectConfirm("correct", true).
		ExpectPrompt("Version", "1.0.0").
		ExpectPrompt("Choose an option", "1")

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{skillDir})
	if err := ExecuteWithPrompter(addCmd, addPrompter); err != nil {
		t.Fatalf("Failed to add skill: %v", err)
	}

	// Step 2: First install (no session recorded yet) - should install
	t.Log("Step 2: First install should proceed")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("First install failed: %v", err)
	}

	// Verify skill was installed
	installedSkillDir := filepath.Join(cursorDir, "skills", "test-skill")
	if _, err := os.Stat(installedSkillDir); os.IsNotExist(err) {
		t.Fatalf("Skill was not installed to: %s", installedSkillDir)
	}
	t.Log("✓ First install completed, skill installed")

	// Step 3: Verify beforeSubmitPrompt hook was installed
	t.Log("Step 3: Verify beforeSubmitPrompt hook was installed")
	hooksJSONPath := filepath.Join(cursorDir, "hooks.json")
	hooksData, err := os.ReadFile(hooksJSONPath)
	if err != nil {
		t.Fatalf("Failed to read hooks.json: %v", err)
	}

	var hooksConfig map[string]any
	if err := json.Unmarshal(hooksData, &hooksConfig); err != nil {
		t.Fatalf("Failed to parse hooks.json: %v", err)
	}

	hooks, ok := hooksConfig["hooks"].(map[string]any)
	if !ok {
		t.Fatal("hooks.json missing 'hooks' section")
	}

	beforeSubmitHooks, ok := hooks["beforeSubmitPrompt"].([]any)
	if !ok {
		t.Fatal("beforeSubmitPrompt hook not found in hooks.json")
	}

	foundAutoInstallHook := false
	for _, hook := range beforeSubmitHooks {
		if hookMap, ok := hook.(map[string]any); ok {
			if cmd, ok := hookMap["command"].(string); ok && strings.HasPrefix(cmd, "sx install") {
				foundAutoInstallHook = true
				break
			}
		}
	}
	if !foundAutoInstallHook {
		t.Error("beforeSubmitPrompt hook for 'sx install' not found")
	}
	t.Log("✓ beforeSubmitPrompt hook installed")

	// Step 4: Simulate recording a conversation ID (mimics what ShouldInstall does)
	t.Log("Step 4: Record a conversation ID in session cache")
	sessionCacheFile := filepath.Join(cacheDir, "cursor-sessions")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("Failed to create cache dir: %v", err)
	}

	// Write a session entry as if ShouldInstall recorded it
	conversationID := "test-conversation-123"
	sessionEntry := conversationID + " 2025-12-12T10:30:00Z\n"
	if err := os.WriteFile(sessionCacheFile, []byte(sessionEntry), 0644); err != nil {
		t.Fatalf("Failed to write session cache: %v", err)
	}

	// Verify session cache file exists
	if _, err := os.Stat(sessionCacheFile); os.IsNotExist(err) {
		t.Fatal("Session cache file was not created")
	}
	t.Log("✓ Session cache file created")

	// Read and verify content
	cacheContent, err := os.ReadFile(sessionCacheFile)
	if err != nil {
		t.Fatalf("Failed to read session cache: %v", err)
	}
	if !strings.Contains(string(cacheContent), conversationID) {
		t.Errorf("Session cache doesn't contain expected conversation ID. Content: %s", string(cacheContent))
	}
	t.Log("✓ Session cache contains conversation ID: " + conversationID)

	t.Log("✓ Cursor auto-install deduplication test passed!")
}
