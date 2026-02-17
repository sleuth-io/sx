package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/clients"
	github_copilot "github.com/sleuth-io/sx/internal/clients/github_copilot"
	"github.com/sleuth-io/sx/internal/clients/github_copilot/handlers"
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
	// Create .copilot directory so IsInstalled() returns true for GitHub Copilot
	copilotDir := filepath.Join(homeDir, ".copilot")
	for _, dir := range []string{homeDir, copilotDir, workingDir, skillDir} {
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

// TestGitHubCopilotPathScope tests that path-scoped skills are remapped to
// repo root (.github/skills/) since Copilot only discovers assets there.
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

	// Path-scoped skills go to {repoRoot}/{path}/.github/skills/
	// (same behavior as Claude Code)
	pathSkillDir := filepath.Join(projectDir, "src", "backend", ".github", "skills", "path-skill")
	env.AssertFileExists(pathSkillDir)
	env.AssertFileExists(filepath.Join(pathSkillDir, "SKILL.md"))

	// Verify skill was NOT installed to repo root
	repoSkillDir := filepath.Join(projectDir, ".github", "skills", "path-skill")
	env.AssertFileNotExists(repoSkillDir)

	// Verify skill was NOT installed globally
	globalSkillDir := filepath.Join(env.HomeDir, ".copilot", "skills", "path-skill")
	env.AssertFileNotExists(globalSkillDir)
}

// TestGitHubCopilotUnsupportedAssetType tests that Copilot gracefully skips
// unsupported asset types while still installing supported ones correctly.
func TestGitHubCopilotUnsupportedAssetType(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddSkillToVault(vaultDir, "good-skill", "1.0.0")
	env.AddPluginToVault(vaultDir, "some-plugin", "1.0.0")

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
name = "some-plugin"
version = "1.0.0"
type = "claude-code-plugin"

[assets.source-path]
path = "assets/some-plugin/1.0.0"
`
	env.WriteLockFile(vaultDir, lockFile)

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")
	env.Chdir(projectDir)

	// Install should succeed — plugin is skipped by Copilot, skill installs fine
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Verify skill was installed to Copilot
	copilotDir := filepath.Join(env.HomeDir, ".copilot")
	env.AssertFileExists(filepath.Join(copilotDir, "skills", "good-skill"))

	// Verify plugin was NOT installed to Copilot (not a supported type)
	env.AssertFileNotExists(filepath.Join(copilotDir, "some-plugin"))
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

// TestGitHubCopilotRuleInstall tests that rules are installed as .instructions.md files
// in the instructions/ directory with YAML frontmatter.
func TestGitHubCopilotRuleInstall(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddRuleToVault(vaultDir, "coding-standards", "1.0.0", "Follow these coding standards:\n\n1. Use meaningful variable names\n2. Write unit tests")

	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "coding-standards"
version = "1.0.0"
type = "rule"

[assets.source-path]
path = "assets/coding-standards/1.0.0"
`
	env.WriteLockFile(vaultDir, lockFile)

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")
	env.Chdir(projectDir)

	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Verify rule was installed to ~/.copilot/instructions/ (global scope)
	copilotDir := filepath.Join(env.HomeDir, ".copilot")
	ruleFile := filepath.Join(copilotDir, "instructions", "coding-standards.instructions.md")
	env.AssertFileExists(ruleFile)

	content, err := os.ReadFile(ruleFile)
	if err != nil {
		t.Fatalf("Failed to read rule file: %v", err)
	}

	// Verify YAML frontmatter
	if !strings.HasPrefix(string(content), "---\n") {
		t.Errorf("Rule file should have YAML frontmatter")
	}

	// Verify title heading
	if !strings.Contains(string(content), "# coding-standards") {
		t.Errorf("Rule file should contain title heading")
	}

	// Verify rule content
	if !strings.Contains(string(content), "Use meaningful variable names") {
		t.Errorf("Rule file should contain rule content")
	}
}

// TestGitHubCopilotRuleWithGlobs tests that rules with globs get applyTo frontmatter.
func TestGitHubCopilotRuleWithGlobs(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddRuleToVaultWithGlobs(vaultDir, "go-standards", "1.0.0", "Follow Go coding standards.", []string{"**/*.go", "go.mod"})

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")

	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "go-standards"
version = "1.0.0"
type = "rule"

[assets.source-path]
path = "assets/go-standards/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"
`
	env.WriteLockFile(vaultDir, lockFile)
	env.Chdir(projectDir)

	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Verify rule was installed to {repoRoot}/.github/instructions/ (repo scope)
	ruleFile := filepath.Join(projectDir, ".github", "instructions", "go-standards.instructions.md")
	env.AssertFileExists(ruleFile)

	content, err := os.ReadFile(ruleFile)
	if err != nil {
		t.Fatalf("Failed to read rule file: %v", err)
	}

	// Verify YAML frontmatter with applyTo
	if !strings.HasPrefix(string(content), "---\n") {
		t.Errorf("Rule with globs should have YAML frontmatter")
	}
	if !strings.Contains(string(content), "applyTo:") {
		t.Errorf("Rule should have applyTo in frontmatter")
	}
	if !strings.Contains(string(content), "**/*.go") {
		t.Errorf("Rule should contain the go glob pattern")
	}
	if !strings.Contains(string(content), "go.mod") {
		t.Errorf("Rule should contain the go.mod glob pattern")
	}

	// Verify rule content
	if !strings.Contains(string(content), "Follow Go coding standards") {
		t.Errorf("Rule file should contain rule content")
	}
}

// TestGitHubCopilotRuleUninstall tests that rules are properly removed when
// they are removed from the lock file and install is re-run.
func TestGitHubCopilotRuleUninstall(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddRuleToVault(vaultDir, "temp-rule", "1.0.0", "Temporary rule content")
	env.AddSkillToVault(vaultDir, "keeper-skill", "1.0.0")

	lockFileWithBoth := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "temp-rule"
version = "1.0.0"
type = "rule"

[assets.source-path]
path = "assets/temp-rule/1.0.0"

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

	// Step 1: Install both assets
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	copilotDir := filepath.Join(env.HomeDir, ".copilot")
	ruleFile := filepath.Join(copilotDir, "instructions", "temp-rule.instructions.md")
	skillDir := filepath.Join(copilotDir, "skills", "keeper-skill")
	env.AssertFileExists(ruleFile)
	env.AssertFileExists(skillDir)

	// Step 2: Remove rule from lock file
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

	// Step 3: Re-install to trigger cleanup
	installCmd2 := NewInstallCommand()
	if err := installCmd2.Execute(); err != nil {
		t.Fatalf("Second install failed: %v", err)
	}

	// Step 4: Verify rule was removed, skill remains
	env.AssertFileNotExists(ruleFile)
	env.AssertFileExists(skillDir)
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

// TestGitHubCopilotCommandInstall tests that commands are installed as .prompt.md files
// in the prompts/ directory with description frontmatter.
func TestGitHubCopilotCommandInstall(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddCommandToVault(vaultDir, "my-prompt", "1.0.0", "Generate a unit test for the selected code.\n\nInclude edge cases and error handling.")

	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "my-prompt"
version = "1.0.0"
type = "command"

[assets.source-path]
path = "assets/my-prompt/1.0.0"
`
	env.WriteLockFile(vaultDir, lockFile)

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")
	env.Chdir(projectDir)

	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Verify command was installed to ~/.copilot/prompts/ (global scope)
	copilotDir := filepath.Join(env.HomeDir, ".copilot")
	promptFile := filepath.Join(copilotDir, "prompts", "my-prompt.prompt.md")
	env.AssertFileExists(promptFile)

	content, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatalf("Failed to read prompt file: %v", err)
	}

	// Verify YAML frontmatter with description
	if !strings.HasPrefix(string(content), "---\n") {
		t.Errorf("Prompt file should have YAML frontmatter")
	}
	if !strings.Contains(string(content), "description:") {
		t.Errorf("Prompt file should contain description in frontmatter")
	}

	// Verify prompt content
	if !strings.Contains(string(content), "Generate a unit test") {
		t.Errorf("Prompt file should contain prompt content")
	}
}

// TestGitHubCopilotCommandUninstall tests that commands are properly removed when
// they are removed from the lock file and install is re-run.
func TestGitHubCopilotCommandUninstall(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddCommandToVault(vaultDir, "temp-prompt", "1.0.0", "Temporary prompt content")
	env.AddSkillToVault(vaultDir, "keeper-skill", "1.0.0")

	lockFileWithBoth := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "temp-prompt"
version = "1.0.0"
type = "command"

[assets.source-path]
path = "assets/temp-prompt/1.0.0"

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

	// Step 1: Install both assets
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	copilotDir := filepath.Join(env.HomeDir, ".copilot")
	promptFile := filepath.Join(copilotDir, "prompts", "temp-prompt.prompt.md")
	skillDir := filepath.Join(copilotDir, "skills", "keeper-skill")
	env.AssertFileExists(promptFile)
	env.AssertFileExists(skillDir)

	// Step 2: Remove command from lock file
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

	// Step 3: Re-install to trigger cleanup
	installCmd2 := NewInstallCommand()
	if err := installCmd2.Execute(); err != nil {
		t.Fatalf("Second install failed: %v", err)
	}

	// Step 4: Verify command was removed, skill remains
	env.AssertFileNotExists(promptFile)
	env.AssertFileExists(skillDir)
}

// TestGitHubCopilotAgentInstall tests that agents are installed as .agent.md files
// in the agents/ directory with description frontmatter.
func TestGitHubCopilotAgentInstall(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddAgentToVault(vaultDir, "test-specialist", "1.0.0", "You are a testing expert.\n\nWhen asked to write tests:\n1. Analyze the code\n2. Identify edge cases\n3. Write comprehensive tests")

	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "test-specialist"
version = "1.0.0"
type = "agent"

[assets.source-path]
path = "assets/test-specialist/1.0.0"
`
	env.WriteLockFile(vaultDir, lockFile)

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")
	env.Chdir(projectDir)

	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Verify agent was installed to ~/.copilot/agents/ (global scope)
	copilotDir := filepath.Join(env.HomeDir, ".copilot")
	agentFile := filepath.Join(copilotDir, "agents", "test-specialist.agent.md")
	env.AssertFileExists(agentFile)

	content, err := os.ReadFile(agentFile)
	if err != nil {
		t.Fatalf("Failed to read agent file: %v", err)
	}

	// Verify YAML frontmatter with description
	if !strings.HasPrefix(string(content), "---\n") {
		t.Errorf("Agent file should have YAML frontmatter")
	}
	if !strings.Contains(string(content), "description:") {
		t.Errorf("Agent file should contain description in frontmatter")
	}

	// Verify agent content
	if !strings.Contains(string(content), "You are a testing expert") {
		t.Errorf("Agent file should contain agent content")
	}
}

// TestGitHubCopilotAgentUninstall tests that agents are properly removed when
// they are removed from the lock file and install is re-run.
func TestGitHubCopilotAgentUninstall(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddAgentToVault(vaultDir, "temp-agent", "1.0.0", "Temporary agent content")
	env.AddSkillToVault(vaultDir, "keeper-skill", "1.0.0")

	lockFileWithBoth := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "temp-agent"
version = "1.0.0"
type = "agent"

[assets.source-path]
path = "assets/temp-agent/1.0.0"

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

	// Step 1: Install both assets
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	copilotDir := filepath.Join(env.HomeDir, ".copilot")
	agentFile := filepath.Join(copilotDir, "agents", "temp-agent.agent.md")
	skillDir := filepath.Join(copilotDir, "skills", "keeper-skill")
	env.AssertFileExists(agentFile)
	env.AssertFileExists(skillDir)

	// Step 2: Remove agent from lock file
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

	// Step 3: Re-install to trigger cleanup
	installCmd2 := NewInstallCommand()
	if err := installCmd2.Execute(); err != nil {
		t.Fatalf("Second install failed: %v", err)
	}

	// Step 4: Verify agent was removed, skill remains
	env.AssertFileNotExists(agentFile)
	env.AssertFileExists(skillDir)
}

func TestGitHubCopilotMCPInstall(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddMCPToVault(vaultDir, "test-mcp", "1.0.0", "node", []string{"server.js"})

	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "test-mcp"
version = "1.0.0"
type = "mcp"

[assets.source-path]
path = "assets/test-mcp/1.0.0"
`
	env.WriteLockFile(vaultDir, lockFile)

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")
	env.Chdir(projectDir)

	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// MCP servers go to ~/.vscode/ in global scope (not .github/ or ~/.copilot/)
	vscodeDir := filepath.Join(env.HomeDir, ".vscode")
	mcpConfigFile := filepath.Join(vscodeDir, "mcp.json")
	mcpServerDir := filepath.Join(vscodeDir, "mcp-servers", "test-mcp")

	// Verify mcp.json was created
	env.AssertFileExists(mcpConfigFile)

	// Verify server files were extracted
	env.AssertFileExists(mcpServerDir)
	env.AssertFileExists(filepath.Join(mcpServerDir, "server.js"))

	// Verify mcp.json contains the server entry
	mcpContent, err := os.ReadFile(mcpConfigFile)
	if err != nil {
		t.Fatalf("Failed to read mcp.json: %v", err)
	}
	if !strings.Contains(string(mcpContent), "test-mcp") {
		t.Errorf("mcp.json should contain 'test-mcp' entry, got: %s", mcpContent)
	}
	if !strings.Contains(string(mcpContent), "node") {
		t.Errorf("mcp.json should contain 'node' command, got: %s", mcpContent)
	}
}

func TestGitHubCopilotMCPUninstall(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddMCPToVault(vaultDir, "temp-mcp", "1.0.0", "npx", []string{"-y", "server"})
	env.AddSkillToVault(vaultDir, "keeper-skill", "1.0.0")

	lockFileWithBoth := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "temp-mcp"
version = "1.0.0"
type = "mcp"

[assets.source-path]
path = "assets/temp-mcp/1.0.0"

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

	// Step 1: Install both
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// MCP servers go to ~/.vscode/ in global scope
	vscodeDir := filepath.Join(env.HomeDir, ".vscode")
	mcpConfigFile := filepath.Join(vscodeDir, "mcp.json")
	mcpServerDir := filepath.Join(vscodeDir, "mcp-servers", "temp-mcp")
	copilotDir := filepath.Join(env.HomeDir, ".copilot")
	skillDir := filepath.Join(copilotDir, "skills", "keeper-skill")

	// Verify MCP and skill were installed
	env.AssertFileExists(mcpConfigFile)
	env.AssertFileExists(mcpServerDir)
	env.AssertFileExists(skillDir)

	// Step 2: Remove MCP from lock file
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

	// Step 3: Re-install to trigger cleanup
	installCmd2 := NewInstallCommand()
	if err := installCmd2.Execute(); err != nil {
		t.Fatalf("Second install failed: %v", err)
	}

	// Step 4: Verify MCP was removed, skill remains
	env.AssertFileNotExists(mcpServerDir)
	env.AssertFileExists(skillDir)

	// Verify mcp.json no longer contains temp-mcp
	mcpContent, err := os.ReadFile(mcpConfigFile)
	if err != nil {
		t.Fatalf("Failed to read mcp.json: %v", err)
	}
	if strings.Contains(string(mcpContent), "temp-mcp") {
		t.Errorf("mcp.json should not contain 'temp-mcp' after uninstall, got: %s", mcpContent)
	}
}

func TestGitHubCopilotMCPRemoteInstall(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	// MCP-Remote uses external commands like npx, no local server files
	env.AddMCPRemoteToVault(vaultDir, "remote-github", "1.0.0", "npx", []string{"-y", "@modelcontextprotocol/server-github"})

	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "remote-github"
version = "1.0.0"
type = "mcp-remote"

[assets.source-path]
path = "assets/remote-github/1.0.0"
`
	env.WriteLockFile(vaultDir, lockFile)

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")
	env.Chdir(projectDir)

	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// MCP-Remote goes to ~/.vscode/ in global scope
	vscodeDir := filepath.Join(env.HomeDir, ".vscode")
	mcpConfigFile := filepath.Join(vscodeDir, "mcp.json")

	// Verify mcp.json was created with the entry
	env.AssertFileExists(mcpConfigFile)

	// MCP-Remote doesn't extract files, just adds config
	mcpServerDir := filepath.Join(vscodeDir, "mcp-servers", "remote-github")
	env.AssertFileNotExists(mcpServerDir) // No server directory for remote

	// Verify mcp.json contains the server entry
	mcpContent, err := os.ReadFile(mcpConfigFile)
	if err != nil {
		t.Fatalf("Failed to read mcp.json: %v", err)
	}
	if !strings.Contains(string(mcpContent), "remote-github") {
		t.Errorf("mcp.json should contain 'remote-github' entry, got: %s", mcpContent)
	}
	if !strings.Contains(string(mcpContent), "npx") {
		t.Errorf("mcp.json should contain 'npx' command, got: %s", mcpContent)
	}
}

func TestGitHubCopilotMCPRemoteUninstall(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddMCPRemoteToVault(vaultDir, "temp-remote", "1.0.0", "npx", []string{"-y", "some-server"})
	env.AddSkillToVault(vaultDir, "keeper-skill", "1.0.0")

	lockFileWithBoth := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "temp-remote"
version = "1.0.0"
type = "mcp-remote"

[assets.source-path]
path = "assets/temp-remote/1.0.0"

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

	// Step 1: Install both
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	vscodeDir := filepath.Join(env.HomeDir, ".vscode")
	mcpConfigFile := filepath.Join(vscodeDir, "mcp.json")
	copilotDir := filepath.Join(env.HomeDir, ".copilot")
	skillDir := filepath.Join(copilotDir, "skills", "keeper-skill")

	// Verify both were installed
	env.AssertFileExists(mcpConfigFile)
	env.AssertFileExists(skillDir)

	// Verify mcp.json contains temp-remote
	mcpContent, err := os.ReadFile(mcpConfigFile)
	if err != nil {
		t.Fatalf("Failed to read mcp.json: %v", err)
	}
	if !strings.Contains(string(mcpContent), "temp-remote") {
		t.Errorf("mcp.json should contain 'temp-remote', got: %s", mcpContent)
	}

	// Step 2: Remove MCP-Remote from lock file
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

	// Step 3: Re-install to trigger cleanup
	installCmd2 := NewInstallCommand()
	if err := installCmd2.Execute(); err != nil {
		t.Fatalf("Second install failed: %v", err)
	}

	// Step 4: Verify MCP-Remote was removed, skill remains
	env.AssertFileExists(skillDir)

	// Verify mcp.json no longer contains temp-remote
	mcpContent, err = os.ReadFile(mcpConfigFile)
	if err != nil {
		t.Fatalf("Failed to read mcp.json: %v", err)
	}
	if strings.Contains(string(mcpContent), "temp-remote") {
		t.Errorf("mcp.json should not contain 'temp-remote' after uninstall, got: %s", mcpContent)
	}
}

// TestGitHubCopilotBootstrapInstall tests that bootstrap hooks are installed
// to .github/hooks/sx.json when enabled (Copilot CLI format).
func TestGitHubCopilotBootstrapInstall(t *testing.T) {
	env := NewTestEnv(t)

	// Create a git repo in TempDir for the hooks to be installed
	repoDir := filepath.Join(env.TempDir, "repo")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatalf("Failed to create git directory: %v", err)
	}
	// Change to repo dir so findGitRoot() works
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("Failed to chdir to repo: %v", err)
	}

	// Copilot CLI hooks go to workspace .github/hooks/sx.json
	hooksDir := filepath.Join(repoDir, ".github", handlers.DirHooks)

	// Get the GitHub Copilot client
	client := github_copilot.NewClient()

	// Install bootstrap with session hook enabled
	opts := client.GetBootstrapOptions(context.Background())
	if len(opts) == 0 {
		t.Fatal("Expected at least one bootstrap option, got none")
	}

	// Verify the option is the session hook
	if opts[0].Key != "session_hook" {
		t.Errorf("Expected session_hook option, got %s", opts[0].Key)
	}

	// Install the bootstrap
	err := client.InstallBootstrap(context.Background(), opts)
	if err != nil {
		t.Fatalf("Failed to install bootstrap: %v", err)
	}

	// Verify the hooks file was created
	hookFile := filepath.Join(hooksDir, handlers.FileHooks)
	env.AssertFileExists(hookFile)

	// Read and verify content
	content, err := os.ReadFile(hookFile)
	if err != nil {
		t.Fatalf("Failed to read hooks file: %v", err)
	}

	// Verify it contains sessionStart hook (camelCase for Copilot CLI)
	if !strings.Contains(string(content), "sessionStart") {
		t.Errorf("Hooks file should contain sessionStart hook, got: %s", content)
	}
	if !strings.Contains(string(content), "sx install --hook-mode --client=github-copilot") {
		t.Errorf("Hooks file should contain sx install command, got: %s", content)
	}
	// Verify version field
	if !strings.Contains(string(content), `"version": 1`) {
		t.Errorf("Hooks file should contain version: 1, got: %s", content)
	}
}

// TestGitHubCopilotBootstrapUninstall tests that bootstrap hooks are removed
// from .github/hooks/sx.json when uninstalled.
func TestGitHubCopilotBootstrapUninstall(t *testing.T) {
	env := NewTestEnv(t)

	// Create a git repo in TempDir for the hooks to be installed
	repoDir := filepath.Join(env.TempDir, "repo")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatalf("Failed to create git directory: %v", err)
	}
	// Change to repo dir so findGitRoot() works
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("Failed to chdir to repo: %v", err)
	}

	// Copilot CLI hooks go to workspace .github/hooks/sx.json
	hooksDir := filepath.Join(repoDir, ".github", handlers.DirHooks)

	// Get the GitHub Copilot client
	client := github_copilot.NewClient()
	opts := client.GetBootstrapOptions(context.Background())

	// First install the bootstrap
	err := client.InstallBootstrap(context.Background(), opts)
	if err != nil {
		t.Fatalf("Failed to install bootstrap: %v", err)
	}

	hookFile := filepath.Join(hooksDir, handlers.FileHooks)
	env.AssertFileExists(hookFile)

	// Now uninstall
	err = client.UninstallBootstrap(context.Background(), opts)
	if err != nil {
		t.Fatalf("Failed to uninstall bootstrap: %v", err)
	}

	// Verify the hooks were removed from the file
	// (file may still exist with version field but no hooks)
	content, err := os.ReadFile(hookFile)
	if err == nil {
		if strings.Contains(string(content), "sx install") {
			t.Errorf("Hooks file should not contain sx install after uninstall, got: %s", content)
		}
	}
}

// TestGitHubCopilotBootstrapIdempotent tests that installing bootstrap
// multiple times is idempotent (doesn't duplicate hooks).
func TestGitHubCopilotBootstrapIdempotent(t *testing.T) {
	env := NewTestEnv(t)

	// Create a git repo in TempDir for the hooks to be installed
	repoDir := filepath.Join(env.TempDir, "repo")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatalf("Failed to create git directory: %v", err)
	}
	// Change to repo dir so findGitRoot() works
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("Failed to chdir to repo: %v", err)
	}

	// Copilot CLI hooks go to workspace .github/hooks/sx.json
	hooksDir := filepath.Join(repoDir, ".github", handlers.DirHooks)

	// Get the GitHub Copilot client
	client := github_copilot.NewClient()
	opts := client.GetBootstrapOptions(context.Background())

	// Install bootstrap multiple times
	for i := range 3 {
		err := client.InstallBootstrap(context.Background(), opts)
		if err != nil {
			t.Fatalf("Failed to install bootstrap (iteration %d): %v", i, err)
		}
	}

	// Read the hooks file
	hookFile := filepath.Join(hooksDir, handlers.FileHooks)
	content, err := os.ReadFile(hookFile)
	if err != nil {
		t.Fatalf("Failed to read hooks file: %v", err)
	}

	// Count occurrences of the command - should only appear once
	count := strings.Count(string(content), "sx install --hook-mode --client=github-copilot")
	if count != 1 {
		t.Errorf("Expected exactly 1 hook entry, found %d. Content: %s", count, content)
	}
}
