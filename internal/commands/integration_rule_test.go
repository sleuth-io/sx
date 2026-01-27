package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClaudeCodeRuleIntegration tests the full workflow for rule assets with Claude Code
func TestClaudeCodeRuleIntegration(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddRuleToVault(vaultDir, "coding-standards", "1.0.0", `Follow these coding standards:

1. Use meaningful variable names
2. Write unit tests for all functions
3. Document public APIs`)

	// Set up git repo first (needed for scope)
	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")

	// Create lock file with the rule asset scoped to the repo
	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "coding-standards"
version = "1.0.0"
type = "rule"

[assets.source-path]
path = "assets/coding-standards/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"
`
	env.WriteLockFile(vaultDir, lockFile)
	env.Chdir(projectDir)

	// Step 1: Install the rule
	t.Log("Step 1: Install rule from repository")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Step 2: Verify rule was installed to .claude/rules/ directory
	t.Log("Step 2: Verify rule installation in .claude/rules/")
	ruleFilePath := filepath.Join(projectDir, ".claude", "rules", "coding-standards.md")
	env.AssertFileExists(ruleFilePath)

	content, err := os.ReadFile(ruleFilePath)
	if err != nil {
		t.Fatalf("Failed to read rule file: %v", err)
	}

	// Verify the rule title is present
	if !strings.Contains(string(content), "# coding-standards") {
		t.Errorf("Rule file missing title heading")
	}

	// Verify the rule content is present
	if !strings.Contains(string(content), "Use meaningful variable names") {
		t.Errorf("Rule file missing rule content")
	}

	t.Log("Rule file content:")
	t.Log(string(content))

	t.Log("✓ Claude Code rule integration test passed!")
}

// TestClaudeCodeRuleUpdate tests updating an existing rule
func TestClaudeCodeRuleUpdate(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()

	// Initial version
	env.AddRuleToVault(vaultDir, "security-rules", "1.0.0", "Original security rules content.")

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")

	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "security-rules"
version = "1.0.0"
type = "rule"

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

	ruleFilePath := filepath.Join(projectDir, ".claude", "rules", "security-rules.md")
	content1, _ := os.ReadFile(ruleFilePath)
	if !strings.Contains(string(content1), "Original security rules content") {
		t.Errorf("Initial content not found in rule file")
	}

	// Update the rule in vault
	t.Log("Step 2: Update rule in vault")
	env.AddRuleToVault(vaultDir, "security-rules", "1.1.0", "Updated security rules with new content.")

	lockFileV2 := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "security-rules"
version = "1.1.0"
type = "rule"

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

	content2, _ := os.ReadFile(ruleFilePath)
	if !strings.Contains(string(content2), "Updated security rules with new content") {
		t.Errorf("Updated content not found in rule file")
	}
	if strings.Contains(string(content2), "Original security rules content") {
		t.Errorf("Old content should not be in rule file after update")
	}

	t.Log("✓ Claude Code rule update test passed!")
}

// TestClaudeCodeMultipleRules tests installing multiple rules
func TestClaudeCodeMultipleRules(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()

	// Add multiple rules
	env.AddRuleToVault(vaultDir, "api-guidelines", "1.0.0", "Follow REST API best practices.")
	env.AddRuleToVault(vaultDir, "testing-standards", "1.0.0", "Write comprehensive tests.")

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")

	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "api-guidelines"
version = "1.0.0"
type = "rule"

[assets.source-path]
path = "assets/api-guidelines/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"

[[assets]]
name = "testing-standards"
version = "1.0.0"
type = "rule"

[assets.source-path]
path = "assets/testing-standards/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"
`
	env.WriteLockFile(vaultDir, lockFile)
	env.Chdir(projectDir)

	// Install
	t.Log("Installing multiple rules")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Verify both rule files exist
	apiRulePath := filepath.Join(projectDir, ".claude", "rules", "api-guidelines.md")
	testRulePath := filepath.Join(projectDir, ".claude", "rules", "testing-standards.md")

	env.AssertFileExists(apiRulePath)
	env.AssertFileExists(testRulePath)

	apiContent, _ := os.ReadFile(apiRulePath)
	testContent, _ := os.ReadFile(testRulePath)

	if !strings.Contains(string(apiContent), "Follow REST API best practices") {
		t.Errorf("api-guidelines content not found")
	}
	if !strings.Contains(string(testContent), "Write comprehensive tests") {
		t.Errorf("testing-standards content not found")
	}

	t.Log("✓ Claude Code multiple rules test passed!")
}

// TestClaudeCodeRulePathScoped tests installing rule to a specific path
func TestClaudeCodeRulePathScoped(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddRuleToVault(vaultDir, "backend-guidelines", "1.0.0", "Follow these backend guidelines.")

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")

	// Create the backend directory
	env.MkdirAll(filepath.Join(projectDir, "backend"))

	// Lock file with path-scoped asset
	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "backend-guidelines"
version = "1.0.0"
type = "rule"

[assets.source-path]
path = "assets/backend-guidelines/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"
paths = ["backend/"]
`
	env.WriteLockFile(vaultDir, lockFile)

	// Install from repo root - should install to backend/.claude/rules/
	env.Chdir(projectDir)

	t.Log("Installing path-scoped rule from repo root")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Verify rule was installed in backend/.claude/rules/
	backendRulePath := filepath.Join(projectDir, "backend", ".claude", "rules", "backend-guidelines.md")
	env.AssertFileExists(backendRulePath)

	content, err := os.ReadFile(backendRulePath)
	if err != nil {
		t.Fatalf("Failed to read backend rule file: %v", err)
	}

	if !strings.Contains(string(content), "Follow these backend guidelines") {
		t.Errorf("backend/.claude/rules/backend-guidelines.md should contain rule content")
	}

	// Root .claude/rules/ should NOT have this rule
	rootRulePath := filepath.Join(projectDir, ".claude", "rules", "backend-guidelines.md")
	env.AssertFileNotExists(rootRulePath)

	t.Log("✓ Claude Code path-scoped rule test passed!")
}

// TestClaudeCodeRuleMultiplePaths tests installing rule to multiple paths
func TestClaudeCodeRuleMultiplePaths(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddRuleToVault(vaultDir, "service-guidelines", "1.0.0", "Follow these service guidelines.")

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")

	// Create multiple service directories
	env.MkdirAll(filepath.Join(projectDir, "services", "api"))
	env.MkdirAll(filepath.Join(projectDir, "services", "worker"))

	// Lock file with rule scoped to multiple paths
	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "service-guidelines"
version = "1.0.0"
type = "rule"

[assets.source-path]
path = "assets/service-guidelines/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"
paths = ["services/api/", "services/worker/"]
`
	env.WriteLockFile(vaultDir, lockFile)

	// Install from repo root
	env.Chdir(projectDir)

	t.Log("Installing rule to multiple paths from repo root")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Verify rule was installed in both locations
	apiRulePath := filepath.Join(projectDir, "services", "api", ".claude", "rules", "service-guidelines.md")
	workerRulePath := filepath.Join(projectDir, "services", "worker", ".claude", "rules", "service-guidelines.md")

	env.AssertFileExists(apiRulePath)
	env.AssertFileExists(workerRulePath)

	apiContent, _ := os.ReadFile(apiRulePath)
	workerContent, _ := os.ReadFile(workerRulePath)

	if !strings.Contains(string(apiContent), "Follow these service guidelines") {
		t.Errorf("services/api/.claude/rules/ should contain rule content")
	}
	if !strings.Contains(string(workerContent), "Follow these service guidelines") {
		t.Errorf("services/worker/.claude/rules/ should contain rule content")
	}

	// Root .claude/rules/ should NOT have this rule
	rootRulePath := filepath.Join(projectDir, ".claude", "rules", "service-guidelines.md")
	env.AssertFileNotExists(rootRulePath)

	t.Log("✓ Claude Code multiple paths rule test passed!")
}

// TestClaudeCodeRuleMixedScopes tests installing repo-scoped and path-scoped rules together
func TestClaudeCodeRuleMixedScopes(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()

	// Add a global rule and a path-scoped rule
	env.AddRuleToVault(vaultDir, "global-standards", "1.0.0", "Global coding standards.")
	env.AddRuleToVault(vaultDir, "frontend-rules", "1.0.0", "Frontend-specific rules.")

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")

	// Create frontend directory
	env.MkdirAll(filepath.Join(projectDir, "frontend"))

	// Lock file with mixed scopes
	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "global-standards"
version = "1.0.0"
type = "rule"

[assets.source-path]
path = "assets/global-standards/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"

[[assets]]
name = "frontend-rules"
version = "1.0.0"
type = "rule"

[assets.source-path]
path = "assets/frontend-rules/1.0.0"

[[assets.scopes]]
repo = "https://github.com/testorg/testrepo"
paths = ["frontend/"]
`
	env.WriteLockFile(vaultDir, lockFile)

	// Install from repo root
	env.Chdir(projectDir)

	t.Log("Installing mixed scope rules from repo root")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Verify global rule was installed at root
	rootRulePath := filepath.Join(projectDir, ".claude", "rules", "global-standards.md")
	env.AssertFileExists(rootRulePath)

	rootContent, _ := os.ReadFile(rootRulePath)
	if !strings.Contains(string(rootContent), "Global coding standards") {
		t.Errorf("Root .claude/rules/ should contain global rule")
	}

	// Verify frontend rule was installed in frontend/
	frontendRulePath := filepath.Join(projectDir, "frontend", ".claude", "rules", "frontend-rules.md")
	env.AssertFileExists(frontendRulePath)

	frontendContent, _ := os.ReadFile(frontendRulePath)
	if !strings.Contains(string(frontendContent), "Frontend-specific rules") {
		t.Errorf("frontend/.claude/rules/ should contain frontend rule")
	}

	// Root should NOT have frontend rules
	wrongRootPath := filepath.Join(projectDir, ".claude", "rules", "frontend-rules.md")
	env.AssertFileNotExists(wrongRootPath)

	// Frontend should NOT have global rules
	wrongFrontendPath := filepath.Join(projectDir, "frontend", ".claude", "rules", "global-standards.md")
	env.AssertFileNotExists(wrongFrontendPath)

	t.Log("✓ Claude Code mixed scope rules test passed!")
}

// TestClaudeCodeRuleWithGlobs tests installing a rule with globs (should have frontmatter)
func TestClaudeCodeRuleWithGlobs(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddRuleToVaultWithGlobs(vaultDir, "go-standards", "1.0.0", "Follow Go coding standards.", []string{"**/*.go"})

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

	t.Log("Installing rule with globs")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	ruleFilePath := filepath.Join(projectDir, ".claude", "rules", "go-standards.md")
	env.AssertFileExists(ruleFilePath)

	content, _ := os.ReadFile(ruleFilePath)

	// Should have YAML frontmatter with paths
	if !strings.HasPrefix(string(content), "---\n") {
		t.Errorf("Rule with globs should have YAML frontmatter")
	}
	if !strings.Contains(string(content), "paths:") {
		t.Errorf("Rule should have paths in frontmatter")
	}
	if !strings.Contains(string(content), "**/*.go") {
		t.Errorf("Rule should contain the glob pattern")
	}
	if !strings.Contains(string(content), "Follow Go coding standards") {
		t.Errorf("Rule should contain the rule content")
	}

	t.Log("Rule file content:")
	t.Log(string(content))

	t.Log("✓ Claude Code rule with globs test passed!")
}
