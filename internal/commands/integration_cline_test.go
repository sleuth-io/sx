package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	_ "github.com/sleuth-io/sx/internal/clients/cline" // Auto-registers via init()
)

// TestClineIntegration tests the full workflow with Cline client
func TestClineIntegration(t *testing.T) {
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
	clineDir := filepath.Join(homeDir, ".cline")

	// Create home and working directories
	// Also create .cline directory so Cline client is detected
	for _, dir := range []string{homeDir, workingDir, skillDir, clineDir} {
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

	// Step 4: Verify installation to Cline
	t.Log("Step 4: Verify installation to Cline")

	// For Cline, skills are extracted to .cline/skills/{name}/
	installedSkillDir := filepath.Join(clineDir, "skills", "test-skill")
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

	t.Log("✓ Cline integration test passed!")
}

// TestClineRuleIntegration tests rule installation for Cline
func TestClineRuleIntegration(t *testing.T) {
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
	clineDir := filepath.Join(homeDir, ".cline")

	// Create directories
	for _, dir := range []string{homeDir, workingDir, ruleDir, clineDir} {
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
prompt-file = "RULE.md"
globs = ["src/**/*.ts", "src/**/*.tsx"]
`
	if err := os.WriteFile(filepath.Join(ruleDir, "metadata.toml"), []byte(ruleMetadata), 0644); err != nil {
		t.Fatalf("Failed to write metadata.toml: %v", err)
	}

	ruleContent := `# TypeScript Best Practices

Always use TypeScript strict mode and proper typing.
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

	// Step 4: Verify rule installation to Cline
	t.Log("Step 4: Verify rule installation to Cline")

	// For global scope, rules go to ~/.cline/rules/
	globalRulesDir := filepath.Join(homeDir, ".cline", "rules")
	installedRuleFile := filepath.Join(globalRulesDir, "test-rule.md")

	if _, err := os.Stat(installedRuleFile); os.IsNotExist(err) {
		t.Fatalf("Rule was not installed to: %s", installedRuleFile)
	}

	// Verify rule content contains frontmatter with paths
	ruleFileContent, err := os.ReadFile(installedRuleFile)
	if err != nil {
		t.Fatalf("Failed to read installed rule file: %v", err)
	}

	ruleStr := string(ruleFileContent)
	if !strings.Contains(ruleStr, "paths:") {
		t.Errorf("Rule file missing 'paths:' frontmatter. Content: %s", ruleStr)
	}
	if !strings.Contains(ruleStr, "src/**/*.ts") {
		t.Errorf("Rule file missing expected glob pattern. Content: %s", ruleStr)
	}
	if !strings.Contains(ruleStr, "TypeScript Best Practices") {
		t.Errorf("Rule file missing expected content. Content: %s", ruleStr)
	}

	t.Log("✓ Cline rule integration test passed!")
}

// TestClineMCPIntegration tests MCP installation for Cline
func TestClineMCPIntegration(t *testing.T) {
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
	clineDir := filepath.Join(homeDir, ".cline")

	// Create VS Code globalStorage path for Cline MCP config
	var vsCodeGlobalStorage string
	switch runtime.GOOS {
	case "darwin":
		vsCodeGlobalStorage = filepath.Join(homeDir, "Library", "Application Support", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings")
	case "windows":
		vsCodeGlobalStorage = filepath.Join(homeDir, "AppData", "Roaming", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings")
	default: // Linux
		vsCodeGlobalStorage = filepath.Join(homeDir, ".config", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings")
	}

	// Create directories
	for _, dir := range []string{homeDir, workingDir, mcpDir, clineDir, vsCodeGlobalStorage} {
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

	// Step 4: Verify MCP installation to Cline
	t.Log("Step 4: Verify MCP installation to Cline")

	// Check that MCP was installed to .cline/mcp-servers/test-mcp/
	installedMCPDir := filepath.Join(clineDir, "mcp-servers", "test-mcp")
	if _, err := os.Stat(installedMCPDir); os.IsNotExist(err) {
		t.Fatalf("MCP was not installed to: %s", installedMCPDir)
	}

	// Verify server.js exists
	installedServerFile := filepath.Join(installedMCPDir, "server.js")
	if _, err := os.Stat(installedServerFile); os.IsNotExist(err) {
		t.Errorf("server.js not found in installed location")
	}

	// Verify cline_mcp_settings.json was created/updated
	mcpConfigPath := filepath.Join(vsCodeGlobalStorage, "cline_mcp_settings.json")
	if _, err := os.Stat(mcpConfigPath); os.IsNotExist(err) {
		t.Fatalf("cline_mcp_settings.json was not created at: %s", mcpConfigPath)
	}

	// Verify config contains the test-mcp entry
	mcpConfigData, err := os.ReadFile(mcpConfigPath)
	if err != nil {
		t.Fatalf("Failed to read cline_mcp_settings.json: %v", err)
	}

	var mcpConfig map[string]any
	if err := json.Unmarshal(mcpConfigData, &mcpConfig); err != nil {
		t.Fatalf("Failed to parse cline_mcp_settings.json: %v", err)
	}

	mcpServers, ok := mcpConfig["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("cline_mcp_settings.json does not have mcpServers section")
	}

	if _, exists := mcpServers["test-mcp"]; !exists {
		t.Errorf("test-mcp entry not found in cline_mcp_settings.json")
	}

	t.Log("✓ Cline MCP integration test passed!")
}

// TestClineMultipleSkillsInstallation tests installing multiple skills for Cline
func TestClineMultipleSkillsInstallation(t *testing.T) {
	// Create fully isolated test environment
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	workingDir := filepath.Join(tempDir, "working")
	repoDir := filepath.Join(workingDir, "repo")
	skill1Dir := filepath.Join(workingDir, "skill1")
	skill2Dir := filepath.Join(workingDir, "skill2")

	// Set environment for complete sandboxing
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	clineDir := filepath.Join(homeDir, ".cline")

	// Create directories
	for _, dir := range []string{homeDir, workingDir, skill1Dir, skill2Dir, clineDir} {
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

	// Create first test skill
	skill1Metadata := `[asset]
name = "skill-one"
type = "skill"
description = "First skill"

[skill]
readme = "README.md"
prompt-file = "SKILL.md"
`
	if err := os.WriteFile(filepath.Join(skill1Dir, "metadata.toml"), []byte(skill1Metadata), 0644); err != nil {
		t.Fatalf("Failed to write metadata.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skill1Dir, "README.md"), []byte("# Skill One"), 0644); err != nil {
		t.Fatalf("Failed to write README.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skill1Dir, "SKILL.md"), []byte("First skill content"), 0644); err != nil {
		t.Fatalf("Failed to write SKILL.md: %v", err)
	}

	// Create second test skill
	skill2Metadata := `[asset]
name = "skill-two"
type = "skill"
description = "Second skill"

[skill]
readme = "README.md"
prompt-file = "SKILL.md"
`
	if err := os.WriteFile(filepath.Join(skill2Dir, "metadata.toml"), []byte(skill2Metadata), 0644); err != nil {
		t.Fatalf("Failed to write metadata.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skill2Dir, "README.md"), []byte("# Skill Two"), 0644); err != nil {
		t.Fatalf("Failed to write README.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skill2Dir, "SKILL.md"), []byte("Second skill content"), 0644); err != nil {
		t.Fatalf("Failed to write SKILL.md: %v", err)
	}

	// Step 1: Initialize with path repository
	t.Log("Step 1: Initialize with path repository")
	InitPathRepo(t, repoDir)

	// Step 2: Add first skill
	t.Log("Step 2: Add first skill")
	mockPrompter1 := NewMockPrompter().
		ExpectConfirm("correct", true).
		ExpectPrompt("Version", "1.0.0").
		ExpectPrompt("Choose an option", "1") // Global

	addCmd1 := NewAddCommand()
	addCmd1.SetArgs([]string{skill1Dir})
	if err := ExecuteWithPrompter(addCmd1, mockPrompter1); err != nil {
		t.Fatalf("Failed to add skill 1: %v", err)
	}

	// Step 3: Add second skill
	t.Log("Step 3: Add second skill")
	mockPrompter2 := NewMockPrompter().
		ExpectConfirm("correct", true).
		ExpectPrompt("Version", "1.0.0").
		ExpectPrompt("Choose an option", "1") // Global

	addCmd2 := NewAddCommand()
	addCmd2.SetArgs([]string{skill2Dir})
	if err := ExecuteWithPrompter(addCmd2, mockPrompter2); err != nil {
		t.Fatalf("Failed to add skill 2: %v", err)
	}

	// Step 4: Install all skills
	t.Log("Step 4: Install all skills")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Step 5: Verify both skills were installed
	t.Log("Step 5: Verify both skills were installed")

	skill1Path := filepath.Join(clineDir, "skills", "skill-one", "SKILL.md")
	if _, err := os.Stat(skill1Path); os.IsNotExist(err) {
		t.Errorf("Skill 1 was not installed to: %s", skill1Path)
	}

	skill2Path := filepath.Join(clineDir, "skills", "skill-two", "SKILL.md")
	if _, err := os.Stat(skill2Path); os.IsNotExist(err) {
		t.Errorf("Skill 2 was not installed to: %s", skill2Path)
	}

	// Verify contents
	content1, err := os.ReadFile(skill1Path)
	if err == nil && !strings.Contains(string(content1), "First skill content") {
		t.Errorf("Skill 1 content doesn't match. Got: %s", string(content1))
	}

	content2, err := os.ReadFile(skill2Path)
	if err == nil && !strings.Contains(string(content2), "Second skill content") {
		t.Errorf("Skill 2 content doesn't match. Got: %s", string(content2))
	}

	t.Log("✓ Cline multiple skills installation test passed!")
}

// TestClineNoHookSystem verifies that Cline doesn't install hooks
func TestClineNoHookSystem(t *testing.T) {
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
	clineDir := filepath.Join(homeDir, ".cline")

	// Create directories
	for _, dir := range []string{homeDir, workingDir, skillDir, clineDir} {
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

	// Create a test skill
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
	t.Log("Step 1: Initialize and add skill")
	InitPathRepo(t, repoDir)

	mockPrompter := NewMockPrompter().
		ExpectConfirm("correct", true).
		ExpectPrompt("Version", "1.0.0").
		ExpectPrompt("Choose an option", "1")

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{skillDir})
	if err := ExecuteWithPrompter(addCmd, mockPrompter); err != nil {
		t.Fatalf("Failed to add skill: %v", err)
	}

	// Install
	t.Log("Step 2: Install skill")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Verify NO hooks.json was created (Cline doesn't have hooks)
	t.Log("Step 3: Verify no hooks.json was created")
	hooksJSONPath := filepath.Join(clineDir, "hooks.json")
	if _, err := os.Stat(hooksJSONPath); err == nil {
		t.Errorf("hooks.json should NOT exist for Cline (no hook system): %s", hooksJSONPath)
	}

	t.Log("✓ Cline no-hook-system test passed!")
}
