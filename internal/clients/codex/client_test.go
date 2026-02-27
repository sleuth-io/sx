package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/clients"
)

func TestCodexClient_ID(t *testing.T) {
	client := NewClient()
	if client.ID() != clients.ClientIDCodex {
		t.Errorf("ID() = %q, want %q", client.ID(), clients.ClientIDCodex)
	}
}

func TestCodexClient_DisplayName(t *testing.T) {
	client := NewClient()
	if client.DisplayName() != "Codex" {
		t.Errorf("DisplayName() = %q, want %q", client.DisplayName(), "Codex")
	}
}

func TestCodexClient_SupportsAssetType(t *testing.T) {
	client := NewClient()

	tests := []struct {
		assetType asset.Type
		want      bool
	}{
		{asset.TypeSkill, true},
		{asset.TypeCommand, true},
		{asset.TypeMCP, true},
		{asset.TypeAgent, false},
		{asset.TypeRule, false},
		{asset.TypeHook, false},
		{asset.TypeClaudeCodePlugin, false},
	}

	for _, tt := range tests {
		t.Run(tt.assetType.Key, func(t *testing.T) {
			got := client.SupportsAssetType(tt.assetType)
			if got != tt.want {
				t.Errorf("SupportsAssetType(%s) = %v, want %v", tt.assetType.Key, got, tt.want)
			}
		})
	}
}

func TestCodexClient_IsInstalled(t *testing.T) {
	// Create temp home
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	client := NewClient()

	// Not installed initially
	if client.IsInstalled() {
		t.Error("Should not be installed without .codex directory")
	}

	// Create .codex directory
	codexDir := filepath.Join(tempDir, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("Failed to create .codex: %v", err)
	}

	// Now should be installed
	if !client.IsInstalled() {
		t.Error("Should be installed with .codex directory")
	}
}

func TestCodexClient_DetermineTargetBase_GlobalScope(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	client := NewClient()

	scope := &clients.InstallScope{
		Type: clients.ScopeGlobal,
	}

	// For skills
	target, err := client.determineTargetBase(scope, asset.TypeSkill)
	if err != nil {
		t.Fatalf("determineTargetBase error: %v", err)
	}
	expected := filepath.Join(tempDir, ".codex")
	if target != expected {
		t.Errorf("Global skill target = %q, want %q", target, expected)
	}

	// For commands (same as skills for global)
	target, err = client.determineTargetBase(scope, asset.TypeCommand)
	if err != nil {
		t.Fatalf("determineTargetBase error: %v", err)
	}
	if target != expected {
		t.Errorf("Global command target = %q, want %q", target, expected)
	}
}

func TestCodexClient_DetermineTargetBase_RepoScope(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	client := NewClient()
	repoRoot := filepath.Join(tempDir, "myrepo")

	scope := &clients.InstallScope{
		Type:     clients.ScopeRepository,
		RepoRoot: repoRoot,
	}

	// For skills - uses .agents/ (Codex convention)
	target, err := client.determineTargetBase(scope, asset.TypeSkill)
	if err != nil {
		t.Fatalf("determineTargetBase error: %v", err)
	}
	expected := filepath.Join(repoRoot, ".agents")
	if target != expected {
		t.Errorf("Repo skill target = %q, want %q", target, expected)
	}

	// For commands - uses .codex/
	target, err = client.determineTargetBase(scope, asset.TypeCommand)
	if err != nil {
		t.Fatalf("determineTargetBase error: %v", err)
	}
	expected = filepath.Join(repoRoot, ".codex")
	if target != expected {
		t.Errorf("Repo command target = %q, want %q", target, expected)
	}
}

func TestCodexClient_DetermineTargetBase_PathScope(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	client := NewClient()
	repoRoot := filepath.Join(tempDir, "myrepo")

	scope := &clients.InstallScope{
		Type:     clients.ScopePath,
		RepoRoot: repoRoot,
		Path:     "packages/frontend",
	}

	// For skills - uses .agents/ under path
	target, err := client.determineTargetBase(scope, asset.TypeSkill)
	if err != nil {
		t.Fatalf("determineTargetBase error: %v", err)
	}
	expected := filepath.Join(repoRoot, "packages/frontend", ".agents")
	if target != expected {
		t.Errorf("Path skill target = %q, want %q", target, expected)
	}

	// For commands - uses .codex/ under path
	target, err = client.determineTargetBase(scope, asset.TypeCommand)
	if err != nil {
		t.Fatalf("determineTargetBase error: %v", err)
	}
	expected = filepath.Join(repoRoot, "packages/frontend", ".codex")
	if target != expected {
		t.Errorf("Path command target = %q, want %q", target, expected)
	}
}

func TestCodexClient_DetermineTargetBase_RepoScopeNoRoot(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	client := NewClient()

	scope := &clients.InstallScope{
		Type:     clients.ScopeRepository,
		RepoRoot: "", // Missing repo root
	}

	_, err := client.determineTargetBase(scope, asset.TypeSkill)
	if err == nil {
		t.Error("Should error when RepoRoot is missing for repo scope")
	}
}

func TestCodexClient_GetBootstrapOptions(t *testing.T) {
	client := NewClient()
	opts := client.GetBootstrapOptions(context.Background())

	// Should have analytics and MCP options but NOT session hook
	var hasAnalytics, hasMCP bool
	for _, opt := range opts {
		if opt.Key == bootstrap.AnalyticsHookKey {
			hasAnalytics = true
		}
		if opt.Key == bootstrap.SleuthAIQueryMCPKey {
			hasMCP = true
		}
		if opt.Key == bootstrap.SessionHookKey {
			t.Error("Should not have session hook - Codex doesn't support it")
		}
	}

	if !hasAnalytics {
		t.Error("Should have analytics hook option")
	}
	if !hasMCP {
		t.Error("Should have MCP option")
	}
}

func TestCodexClient_GetBootstrapPath(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	client := NewClient()
	path := client.GetBootstrapPath()

	expected := filepath.Join(tempDir, ".codex", "config.toml")
	if path != expected {
		t.Errorf("GetBootstrapPath() = %q, want %q", path, expected)
	}
}

func TestCodexClient_ShouldInstall(t *testing.T) {
	client := NewClient()

	should, err := client.ShouldInstall(context.Background())
	if err != nil {
		t.Fatalf("ShouldInstall error: %v", err)
	}
	if !should {
		t.Error("ShouldInstall should always return true for Codex")
	}
}

func TestCodexClient_GetAssetPath_Skill(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	client := NewClient()
	scope := &clients.InstallScope{Type: clients.ScopeGlobal}

	path, err := client.GetAssetPath(context.Background(), "my-skill", asset.TypeSkill, scope)
	if err != nil {
		t.Fatalf("GetAssetPath error: %v", err)
	}

	expected := filepath.Join(tempDir, ".codex", "skills", "my-skill")
	if path != expected {
		t.Errorf("GetAssetPath(skill) = %q, want %q", path, expected)
	}
}

func TestCodexClient_GetAssetPath_Command(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	client := NewClient()
	scope := &clients.InstallScope{Type: clients.ScopeGlobal}

	path, err := client.GetAssetPath(context.Background(), "my-cmd", asset.TypeCommand, scope)
	if err != nil {
		t.Fatalf("GetAssetPath error: %v", err)
	}

	expected := filepath.Join(tempDir, ".codex", "commands", "my-cmd.md")
	if path != expected {
		t.Errorf("GetAssetPath(command) = %q, want %q", path, expected)
	}
}

func TestCodexClient_GetAssetPath_UnsupportedType(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	client := NewClient()
	scope := &clients.InstallScope{Type: clients.ScopeGlobal}

	_, err := client.GetAssetPath(context.Background(), "my-agent", asset.TypeAgent, scope)
	if err == nil {
		t.Error("GetAssetPath should error for unsupported type")
	}
}

func TestCodexClient_EnsureAssetSupport(t *testing.T) {
	client := NewClient()
	scope := &clients.InstallScope{Type: clients.ScopeGlobal}

	// Should be a no-op (Codex has native support)
	err := client.EnsureAssetSupport(context.Background(), scope)
	if err != nil {
		t.Errorf("EnsureAssetSupport should not error: %v", err)
	}
}

func TestCodexClient_RuleCapabilities(t *testing.T) {
	client := NewClient()

	caps := client.RuleCapabilities()
	if caps != nil {
		t.Error("RuleCapabilities should be nil - Codex doesn't support rules")
	}
}

func TestCodexClient_InstallBootstrap_MCP(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	// Create .codex directory
	codexDir := filepath.Join(tempDir, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}

	client := NewClient()

	opts := []bootstrap.Option{
		{
			Key:         "test_mcp",
			Description: "Test MCP",
			MCPConfig: &bootstrap.MCPServerConfig{
				Name:    "test-server",
				Command: "/usr/bin/test",
				Args:    []string{"--serve"},
			},
		},
	}

	if err := client.InstallBootstrap(context.Background(), opts); err != nil {
		t.Fatalf("InstallBootstrap error: %v", err)
	}

	// Verify config.toml was created with MCP entry
	configPath := filepath.Join(codexDir, "config.toml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config.toml: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "test-server") {
		t.Error("config.toml should contain test-server")
	}
	if !strings.Contains(contentStr, "/usr/bin/test") {
		t.Error("config.toml should contain command")
	}
}

func TestCodexClient_UninstallBootstrap_MCP(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	// Create .codex directory with config
	codexDir := filepath.Join(tempDir, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}

	configContent := `
[mcp_servers.test-server]
command = "/usr/bin/test"
`
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	client := NewClient()

	opts := []bootstrap.Option{
		{
			Key: "test_mcp",
			MCPConfig: &bootstrap.MCPServerConfig{
				Name: "test-server",
			},
		},
	}

	if err := client.UninstallBootstrap(context.Background(), opts); err != nil {
		t.Fatalf("UninstallBootstrap error: %v", err)
	}

	// Verify test-server was removed
	content, _ := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if strings.Contains(string(content), "test-server") {
		t.Error("test-server should be removed from config.toml")
	}
}
