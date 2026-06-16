package codex

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
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
		{asset.TypeAgent, true},
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

	// For agents - uses .codex/
	target, err = client.determineTargetBase(scope, asset.TypeAgent)
	if err != nil {
		t.Fatalf("determineTargetBase error: %v", err)
	}
	if target != expected {
		t.Errorf("Repo agent target = %q, want %q", target, expected)
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

func TestCodexClient_GetAssetPath_Agent(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	client := NewClient()
	scope := &clients.InstallScope{Type: clients.ScopeGlobal}

	path, err := client.GetAssetPath(context.Background(), "security_reviewer", asset.TypeAgent, scope)
	if err != nil {
		t.Fatalf("GetAssetPath error: %v", err)
	}

	expected := filepath.Join(tempDir, ".codex", "agents", "security_reviewer.toml")
	if path != expected {
		t.Errorf("GetAssetPath(agent) = %q, want %q", path, expected)
	}
}

func TestCodexClient_GetAssetPath_UnsupportedType(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	client := NewClient()
	scope := &clients.InstallScope{Type: clients.ScopeGlobal}

	_, err := client.GetAssetPath(context.Background(), "my-rule", asset.TypeRule, scope)
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
	if caps == nil {
		t.Fatal("RuleCapabilities should be non-nil for Codex asset detection")
	}
	if caps.ClientName != clients.ClientIDCodex {
		t.Errorf("ClientName = %q, want %q", caps.ClientName, clients.ClientIDCodex)
	}
	if caps.RulesDirectory != "" {
		t.Errorf("RulesDirectory = %q, want empty", caps.RulesDirectory)
	}
	if caps.MatchesPath != nil {
		t.Error("MatchesPath should be nil - Codex doesn't support rules")
	}
	if caps.DetectAssetType == nil {
		t.Fatal("DetectAssetType should be set")
	}
	if got := caps.DetectAssetType("/home/user/.codex/agents/security-reviewer.toml", nil); got == nil || *got != asset.TypeAgent {
		t.Fatalf("DetectAssetType(.codex/agents/*.toml) = %v, want agent", got)
	}
	if got := caps.DetectAssetType("/home/user/agents/security-reviewer.toml", nil); got != nil {
		t.Fatalf("DetectAssetType(generic agents TOML) = %v, want nil", got)
	}
}

func TestCodexClient_InstallAssets_SkipsNonCodexAgentFormats(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	client := NewClient()
	resp, err := client.InstallAssets(context.Background(), clients.InstallRequest{
		Scope: &clients.InstallScope{Type: clients.ScopeGlobal},
		Assets: []*clients.AssetBundle{
			{
				Asset: &lockfile.Asset{Name: "markdown-reviewer", Type: asset.TypeAgent},
				Metadata: &metadata.Metadata{
					Asset: metadata.Asset{Name: "markdown-reviewer", Type: asset.TypeAgent},
					Agent: &metadata.AgentConfig{PromptFile: "markdown-reviewer.md"},
				},
				ZipData: []byte("not read because install is skipped"),
			},
			{
				Asset: &lockfile.Asset{Name: "missing-agent-config", Type: asset.TypeAgent},
				Metadata: &metadata.Metadata{
					Asset: metadata.Asset{Name: "missing-agent-config", Type: asset.TypeAgent},
				},
				ZipData: []byte("not read because install is skipped"),
			},
		},
	})
	if err != nil {
		t.Fatalf("InstallAssets returned error: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("got %d results, want 2", len(resp.Results))
	}
	for _, result := range resp.Results {
		if result.Status != clients.StatusSkipped {
			t.Fatalf("status for %s = %v, want skipped", result.AssetName, result.Status)
		}
	}

	for _, name := range []string{"markdown-reviewer.toml", "missing-agent-config.toml"} {
		path := filepath.Join(tempDir, ".codex", "agents", name)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be absent, stat err = %v", path, err)
		}
	}
}

func TestCodexClient_InstallAssets_InstallsCodexAgentTOML(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	agentTOML := `name = "security_reviewer"
description = "Security reviewer"
developer_instructions = "Review security risks."
`
	client := NewClient()
	resp, err := client.InstallAssets(context.Background(), clients.InstallRequest{
		Scope: &clients.InstallScope{Type: clients.ScopeGlobal},
		Assets: []*clients.AssetBundle{
			{
				Asset: &lockfile.Asset{Name: "security_reviewer", Type: asset.TypeAgent},
				Metadata: &metadata.Metadata{
					Asset: metadata.Asset{Name: "security_reviewer", Type: asset.TypeAgent},
					Agent: &metadata.AgentConfig{PromptFile: "security_reviewer.toml"},
				},
				ZipData: createZip(t, map[string]string{
					"metadata.toml": `[asset]
name = "security_reviewer"
version = "1.0.0"
type = "agent"

[agent]
prompt-file = "security_reviewer.toml"
`,
					"security_reviewer.toml": agentTOML,
				}),
			},
		},
	})
	if err != nil {
		t.Fatalf("InstallAssets returned error: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("got %d results, want 1", len(resp.Results))
	}
	if resp.Results[0].Status != clients.StatusSuccess {
		t.Fatalf("status = %v, want success: %v", resp.Results[0].Status, resp.Results[0].Error)
	}

	installedPath := filepath.Join(tempDir, ".codex", "agents", "security_reviewer.toml")
	got, err := os.ReadFile(installedPath)
	if err != nil {
		t.Fatalf("failed to read installed agent: %v", err)
	}
	if string(got) != agentTOML {
		t.Fatalf("installed TOML = %q, want %q", string(got), agentTOML)
	}
}

func TestCodexClient_UninstallAssets_SkipsNonCodexAgentFile(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	agentPath := filepath.Join(tempDir, ".codex", "agents", "markdown-reviewer.toml")
	if err := os.MkdirAll(filepath.Dir(agentPath), 0755); err != nil {
		t.Fatal(err)
	}
	content := []byte("# Markdown agent\n\nThis was not a Codex agent TOML file.")
	if err := os.WriteFile(agentPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	client := NewClient()
	resp, err := client.UninstallAssets(context.Background(), clients.UninstallRequest{
		Scope:  &clients.InstallScope{Type: clients.ScopeGlobal},
		Assets: []asset.Asset{{Name: "markdown-reviewer", Type: asset.TypeAgent}},
	})
	if err != nil {
		t.Fatalf("UninstallAssets returned error: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("got %d results, want 1", len(resp.Results))
	}
	if resp.Results[0].Status != clients.StatusSkipped {
		t.Fatalf("status = %v, want skipped", resp.Results[0].Status)
	}

	got, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("failed to read skipped file: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("file was modified during skipped uninstall")
	}
}

func TestCodexClient_UninstallAssets_RemovesCodexAgentTOML(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	agentPath := filepath.Join(tempDir, ".codex", "agents", "security_reviewer.toml")
	if err := os.MkdirAll(filepath.Dir(agentPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentPath, []byte(`name = "security_reviewer"
description = "Security reviewer"
developer_instructions = "Review security risks."
`), 0644); err != nil {
		t.Fatal(err)
	}

	client := NewClient()
	resp, err := client.UninstallAssets(context.Background(), clients.UninstallRequest{
		Scope:  &clients.InstallScope{Type: clients.ScopeGlobal},
		Assets: []asset.Asset{{Name: "security_reviewer", Type: asset.TypeAgent}},
	})
	if err != nil {
		t.Fatalf("UninstallAssets returned error: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("got %d results, want 1", len(resp.Results))
	}
	if resp.Results[0].Status != clients.StatusSuccess {
		t.Fatalf("status = %v, want success", resp.Results[0].Status)
	}
	if _, err := os.Stat(agentPath); !os.IsNotExist(err) {
		t.Fatalf("agent file still exists after uninstall, stat err = %v", err)
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

func createZip(t *testing.T, files map[string]string) []byte {
	t.Helper()

	buf := new(bytes.Buffer)
	writer := zip.NewWriter(buf)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("failed to create zip entry %q: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write zip entry %q: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}
	return buf.Bytes()
}
