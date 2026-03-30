package commands

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/clients/kiro"
	"github.com/sleuth-io/sx/internal/clients/kiro/handlers"
)

func init() {
	// Register Kiro client for tests
	clients.Register(kiro.NewClient())
}

// TestKiroIntegration tests the full workflow with Kiro client
func TestKiroIntegration(t *testing.T) {
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
	kiroDir := filepath.Join(homeDir, ".kiro")

	// Create home and working directories
	// Also create .kiro directory so Kiro client is detected
	for _, dir := range []string{homeDir, workingDir, skillDir, kiroDir} {
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

	// Step 4: Verify installation to Kiro
	t.Log("Step 4: Verify installation to Kiro")

	// For Kiro, skills are extracted to .kiro/skills/{name}/
	installedSkillDir := filepath.Join(kiroDir, "skills", "test-skill")
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

	// Verify steering file is NOT created (Kiro auto-discovers skills from .kiro/skills/)
	localKiroDir := filepath.Join(workingDir, ".kiro")
	steeringFile := filepath.Join(localKiroDir, "steering", "skills.md")
	if _, err := os.Stat(steeringFile); err == nil {
		t.Errorf("Steering file should not exist (Kiro auto-discovers skills): %s", steeringFile)
	}

	// Verify MCP server was registered in ~/.kiro/settings/mcp.json (global scope)
	globalMCPConfig := filepath.Join(kiroDir, "settings", "mcp.json")
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
						t.Log("OK skills MCP server registered")
					}
				}
			}
		}
	}

	t.Log("OK Kiro integration test passed!")
}

// TestKiroMCPIntegration tests MCP installation for Kiro
func TestKiroMCPIntegration(t *testing.T) {
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
	kiroDir := filepath.Join(homeDir, ".kiro")

	// Create directories
	for _, dir := range []string{homeDir, workingDir, mcpDir, kiroDir} {
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

	// Step 4: Verify MCP installation to Kiro
	t.Log("Step 4: Verify MCP installation to Kiro")

	// Check that MCP was installed to .kiro/mcp-servers/test-mcp/
	installedMCPDir := filepath.Join(kiroDir, "mcp-servers", "test-mcp")
	if _, err := os.Stat(installedMCPDir); os.IsNotExist(err) {
		t.Fatalf("MCP was not installed to: %s", installedMCPDir)
	}

	// Verify server.js exists
	installedServerFile := filepath.Join(installedMCPDir, "server.js")
	if _, err := os.Stat(installedServerFile); os.IsNotExist(err) {
		t.Errorf("server.js not found in installed location")
	}

	// Verify mcp.json was created/updated
	mcpConfigPath := filepath.Join(kiroDir, "settings", "mcp.json")
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

	t.Log("OK Kiro MCP integration test passed!")
}

// TestKiroRuleIntegration tests rule installation for Kiro
func TestKiroRuleIntegration(t *testing.T) {
	// Create fully isolated test environment
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	workingDir := filepath.Join(tempDir, "working")
	repoDir := filepath.Join(workingDir, "repo")
	ruleDir := filepath.Join(workingDir, "rule")

	// Set environment for complete sandboxing
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	kiroDir := filepath.Join(homeDir, ".kiro")

	// Create directories
	for _, dir := range []string{homeDir, workingDir, ruleDir, kiroDir} {
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

	// Create a test rule with metadata
	ruleMetadata := `[asset]
name = "test-rule"
version = "1.0.0"
type = "rule"
description = "A test rule"

[rule]
description = "Test rule for Kiro"
globs = ["*.ts", "*.tsx"]
`
	if err := os.WriteFile(filepath.Join(ruleDir, "metadata.toml"), []byte(ruleMetadata), 0644); err != nil {
		t.Fatalf("Failed to write metadata.toml: %v", err)
	}

	ruleContent := `# Test Rule

This is a test rule for TypeScript files.

- Always use strict mode
- Use proper typing
`
	if err := os.WriteFile(filepath.Join(ruleDir, "RULE.md"), []byte(ruleContent), 0644); err != nil {
		t.Fatalf("Failed to write RULE.md: %v", err)
	}

	// Step 1: Initialize with path repository
	t.Log("Step 1: Initialize with path repository")
	InitPathRepo(t, repoDir)

	// Step 2: Add the test rule to the repository
	t.Log("Step 2: Add test rule to repository")

	mockPrompter := NewMockPrompter().
		ExpectConfirm("correct", true).
		ExpectPrompt("Version", "1.0.0").
		ExpectPrompt("Choose an option", "1")

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{ruleDir})

	if err := ExecuteWithPrompter(addCmd, mockPrompter); err != nil {
		t.Fatalf("Failed to add rule: %v", err)
	}

	// Step 3: Install from the repository
	t.Log("Step 3: Install rule from repository")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Step 4: Verify rule installation to Kiro
	t.Log("Step 4: Verify rule installation to Kiro")

	// Check that rule was installed to .kiro/steering/test-rule.md
	installedRuleFile := filepath.Join(kiroDir, "steering", "test-rule.md")
	if _, err := os.Stat(installedRuleFile); os.IsNotExist(err) {
		t.Fatalf("Rule was not installed to: %s", installedRuleFile)
	}

	// Verify content
	ruleFileContent, err := os.ReadFile(installedRuleFile)
	if err != nil {
		t.Fatalf("Failed to read installed rule file: %v", err)
	}

	contentStr := string(ruleFileContent)

	// Verify frontmatter includes Kiro-specific fields
	if !strings.Contains(contentStr, "inclusion:") {
		t.Errorf("Rule file missing 'inclusion' frontmatter")
	}

	if !strings.Contains(contentStr, "fileMatchPattern:") && !strings.Contains(contentStr, "inclusion: always") {
		t.Errorf("Rule file should have either fileMatchPattern or 'inclusion: always'")
	}

	// Verify content includes the rule body
	if !strings.Contains(contentStr, "Test Rule") {
		t.Errorf("Rule file doesn't contain expected content. Got: %s", contentStr)
	}

	t.Log("OK Kiro rule integration test passed!")
}

// TestKiroBootstrapInstall tests that bootstrap hooks are installed
// as .kiro.hook files in .kiro/hooks/
func TestKiroBootstrapInstall(t *testing.T) {
	env := NewTestEnv(t)

	// Create a git repo so findGitRoot() works
	repoDir := filepath.Join(env.TempDir, "repo")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatalf("Failed to create git directory: %v", err)
	}
	// Use TestEnv.Chdir to properly handle cwd changes with cleanup
	env.Chdir(repoDir)

	agentsDir := filepath.Join(repoDir, handlers.ConfigDir, handlers.DirAgents)

	client := kiro.NewClient()
	opts := client.GetBootstrapOptions(context.Background())

	// Verify options include session and analytics hooks
	hasSession := false
	hasAnalytics := false
	for _, opt := range opts {
		if opt.Key == "session_hook" {
			hasSession = true
		}
		if opt.Key == "analytics_hook" {
			hasAnalytics = true
		}
	}
	if !hasSession {
		t.Error("Expected session_hook option")
	}
	if !hasAnalytics {
		t.Error("Expected analytics_hook option")
	}

	// Install bootstrap
	if err := client.InstallBootstrap(context.Background(), opts); err != nil {
		t.Fatalf("Failed to install bootstrap: %v", err)
	}

	// Verify default.json was created in .kiro/agents/
	agentConfigPath := filepath.Join(agentsDir, "default.json")
	env.AssertFileExists(agentConfigPath)

	content, err := os.ReadFile(agentConfigPath)
	if err != nil {
		t.Fatalf("Failed to read agent config file: %v", err)
	}

	var config struct {
		Name  string                       `json:"name"`
		Hooks map[string][]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatalf("Failed to parse agent config file: %v", err)
	}

	// Verify agentSpawn hook exists
	spawnHooks, ok := config.Hooks["agentSpawn"]
	if !ok || len(spawnHooks) == 0 {
		t.Error("Expected agentSpawn hook")
	} else {
		var hook struct{ Command string }
		if err := json.Unmarshal(spawnHooks[0], &hook); err != nil {
			t.Fatalf("Failed to parse agentSpawn hook: %v", err)
		}
		if hook.Command != "sx install --hook-mode --client=kiro" {
			t.Errorf("agentSpawn command = %q, want %q", hook.Command, "sx install --hook-mode --client=kiro")
		}
	}

	// Verify postToolUse hook exists
	postToolHooks, ok := config.Hooks["postToolUse"]
	if !ok || len(postToolHooks) == 0 {
		t.Error("Expected postToolUse hook")
	} else {
		var hook struct{ Command string }
		if err := json.Unmarshal(postToolHooks[0], &hook); err != nil {
			t.Fatalf("Failed to parse postToolUse hook: %v", err)
		}
		if hook.Command != "sx report-usage --client=kiro" {
			t.Errorf("postToolUse command = %q, want %q", hook.Command, "sx report-usage --client=kiro")
		}
	}
}

// TestKiroBootstrapUninstall tests that bootstrap hooks are removed
// when uninstalled.
func TestKiroBootstrapUninstall(t *testing.T) {
	env := NewTestEnv(t)

	// Create a git repo so findGitRoot() works
	repoDir := filepath.Join(env.TempDir, "repo")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatalf("Failed to create git directory: %v", err)
	}
	// Use TestEnv.Chdir to properly handle cwd changes with cleanup
	env.Chdir(repoDir)

	client := kiro.NewClient()
	opts := client.GetBootstrapOptions(context.Background())

	// Install first
	if err := client.InstallBootstrap(context.Background(), opts); err != nil {
		t.Fatalf("Failed to install bootstrap: %v", err)
	}

	agentsDir := filepath.Join(repoDir, handlers.ConfigDir, handlers.DirAgents)
	agentConfigPath := filepath.Join(agentsDir, "default.json")

	env.AssertFileExists(agentConfigPath)

	// Now uninstall
	if err := client.UninstallBootstrap(context.Background(), opts); err != nil {
		t.Fatalf("Failed to uninstall bootstrap: %v", err)
	}

	// Verify hooks were removed from config (file still exists but hooks are empty)
	content, err := os.ReadFile(agentConfigPath)
	if err != nil {
		t.Fatalf("Failed to read agent config after uninstall: %v", err)
	}

	var config struct {
		Hooks map[string][]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatalf("Failed to parse agent config after uninstall: %v", err)
	}

	if len(config.Hooks["agentSpawn"]) > 0 {
		t.Error("agentSpawn hooks should be empty after uninstall")
	}
	if len(config.Hooks["postToolUse"]) > 0 {
		t.Error("postToolUse hooks should be empty after uninstall")
	}
}
