package opencode

import (
	"context"
	"encoding/json"
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

func TestOpenCodeClient_ID(t *testing.T) {
	c := NewClient()
	if c.ID() != clients.ClientIDOpenCode {
		t.Errorf("ID() = %q, want %q", c.ID(), clients.ClientIDOpenCode)
	}
	if c.DisplayName() != "OpenCode" {
		t.Errorf("DisplayName() = %q, want OpenCode", c.DisplayName())
	}
}

func TestOpenCodeClient_SupportsAssetType(t *testing.T) {
	c := NewClient()
	cases := []struct {
		t    asset.Type
		want bool
	}{
		{asset.TypeSkill, true},
		{asset.TypeCommand, true},
		{asset.TypeMCP, true},
		{asset.TypeRule, true},
		{asset.TypeAgent, true},
		{asset.TypeHook, false},
	}
	for _, tc := range cases {
		if got := c.SupportsAssetType(tc.t); got != tc.want {
			t.Errorf("SupportsAssetType(%s) = %v, want %v", tc.t.Key, got, tc.want)
		}
	}
}

func TestOpenCodeClient_IsInstalled(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("PATH", "/nonexistent") // hide any system opencode binary

	c := NewClient()
	if c.IsInstalled() {
		t.Error("should not be installed without config dir or binary")
	}

	configDir := filepath.Join(tempDir, ".config", "opencode")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if !c.IsInstalled() {
		t.Error("should be installed once config dir exists")
	}
}

func TestOpenCodeClient_DetermineTargetBase(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	c := NewClient()

	t.Run("global", func(t *testing.T) {
		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		want := filepath.Join(tempDir, ".config", "opencode")
		if got != want {
			t.Errorf("global target = %q, want %q", got, want)
		}
	})

	t.Run("repo", func(t *testing.T) {
		repoRoot := filepath.Join(tempDir, "myrepo")
		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeRepository, RepoRoot: repoRoot})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		want := filepath.Join(repoRoot, ".opencode")
		if got != want {
			t.Errorf("repo target = %q, want %q", got, want)
		}
	})

	t.Run("path", func(t *testing.T) {
		repoRoot := filepath.Join(tempDir, "myrepo")
		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopePath, RepoRoot: repoRoot, Path: "packages/frontend"})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		want := filepath.Join(repoRoot, "packages/frontend", ".opencode")
		if got != want {
			t.Errorf("path target = %q, want %q", got, want)
		}
	})

	t.Run("repo without root errors", func(t *testing.T) {
		_, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeRepository})
		if err == nil {
			t.Error("expected error for repo scope without RepoRoot")
		}
	})
}

func TestOpenCodeClient_GetAssetPath(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	c := NewClient()
	scope := &clients.InstallScope{Type: clients.ScopeGlobal}

	skill, err := c.GetAssetPath(context.Background(), "my-skill", asset.TypeSkill, scope)
	if err != nil {
		t.Fatalf("skill path: %v", err)
	}
	wantSkill := filepath.Join(tempDir, ".config", "opencode", "skills", "my-skill")
	if skill != wantSkill {
		t.Errorf("skill path = %q, want %q", skill, wantSkill)
	}

	cmd, err := c.GetAssetPath(context.Background(), "my-cmd", asset.TypeCommand, scope)
	if err != nil {
		t.Fatalf("cmd path: %v", err)
	}
	wantCmd := filepath.Join(tempDir, ".config", "opencode", "commands", "my-cmd.md")
	if cmd != wantCmd {
		t.Errorf("cmd path = %q, want %q", cmd, wantCmd)
	}

	agentPath, err := c.GetAssetPath(context.Background(), "my-agent", asset.TypeAgent, scope)
	if err != nil {
		t.Fatalf("agent path: %v", err)
	}
	wantAgent := filepath.Join(tempDir, ".config", "opencode", "agent", "my-agent.md")
	if agentPath != wantAgent {
		t.Errorf("agent path = %q, want %q", agentPath, wantAgent)
	}

	rulePath, err := c.GetAssetPath(context.Background(), "my-rule", asset.TypeRule, scope)
	if err != nil {
		t.Fatalf("rule path: %v", err)
	}
	wantRule := filepath.Join(tempDir, ".config", "opencode", "rules", "my-rule.md")
	if rulePath != wantRule {
		t.Errorf("rule path = %q, want %q", rulePath, wantRule)
	}

	if _, err := c.GetAssetPath(context.Background(), "x", asset.TypeHook, scope); err == nil {
		t.Error("expected error for unsupported asset type")
	}
}

func TestOpenCodeClient_GetBootstrapPath(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	c := NewClient()
	got := c.GetBootstrapPath()
	want := filepath.Join(tempDir, ".config", "opencode", "opencode.json")
	if got != want {
		t.Errorf("GetBootstrapPath = %q, want %q", got, want)
	}
}

func TestOpenCodeClient_InstallBootstrap_MCP(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	c := NewClient()
	opts := []bootstrap.Option{{
		Key: "test_mcp",
		MCPConfig: &bootstrap.MCPServerConfig{
			Name:    "test-server",
			Command: "/usr/bin/test",
			Args:    []string{"--serve"},
		},
	}}

	if err := c.InstallBootstrap(context.Background(), opts); err != nil {
		t.Fatalf("InstallBootstrap: %v", err)
	}

	configPath := filepath.Join(tempDir, ".config", "opencode", "opencode.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read opencode.json: %v", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	mcp, ok := cfg["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("no mcp section: %s", data)
	}
	if _, ok := mcp["test-server"].(map[string]any); !ok {
		t.Errorf("expected test-server entry, got: %s", data)
	}

	if !strings.Contains(string(data), "/usr/bin/test") {
		t.Errorf("config should contain command path, got: %s", data)
	}

	// Uninstall and verify cleanup
	if err := c.UninstallBootstrap(context.Background(), opts); err != nil {
		t.Fatalf("UninstallBootstrap: %v", err)
	}
	data, _ = os.ReadFile(configPath)
	if strings.Contains(string(data), "test-server") {
		t.Errorf("test-server should be gone, got: %s", data)
	}
}

func TestOpenCodeClient_ShouldInstall(t *testing.T) {
	c := NewClient()
	ok, err := c.ShouldInstall(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok {
		t.Error("ShouldInstall should return true")
	}
}

func TestOpenCodeClient_RuleCapabilities(t *testing.T) {
	c := NewClient()
	caps := c.RuleCapabilities()
	if caps == nil {
		t.Fatal("RuleCapabilities should be non-nil now that opencode supports rules")
	}
	if caps.ClientName != "opencode" {
		t.Errorf("ClientName = %q, want opencode", caps.ClientName)
	}
	if caps.RulesDirectory != ".opencode/rules" {
		t.Errorf("RulesDirectory = %q, want .opencode/rules", caps.RulesDirectory)
	}
	if !caps.MatchesPath(".opencode/rules/style.md") {
		t.Error("MatchesPath should accept .opencode/rules/style.md")
	}
	if caps.DetectAssetType(".opencode/agent/deploy.md", nil) == nil {
		t.Error("DetectAssetType should claim .opencode/agent/ paths")
	}
}

func TestOpenCodeClient_InstallAssets_UnsupportedType(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	c := NewClient()
	req := clients.InstallRequest{
		Scope: &clients.InstallScope{Type: clients.ScopeGlobal},
		Assets: []*clients.AssetBundle{
			{
				Asset: &lockfile.Asset{Name: "x", Type: asset.TypeHook},
				Metadata: &metadata.Metadata{
					Asset: metadata.Asset{Name: "x", Type: asset.TypeHook},
				},
				ZipData: []byte("dummy"),
			},
		},
	}
	resp, err := c.InstallAssets(context.Background(), req)
	if err != nil {
		t.Fatalf("InstallAssets returned error: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Status != clients.StatusSkipped {
		t.Errorf("expected one skipped result, got: %#v", resp.Results)
	}
}

func TestOpenCodeClient_VerifyAssets_NoRepoRoot(t *testing.T) {
	c := NewClient()
	scope := &clients.InstallScope{Type: clients.ScopeRepository}
	results := c.VerifyAssets(context.Background(), []*lockfile.Asset{
		{Name: "x", Version: "1.0.0", Type: asset.TypeSkill},
	}, scope)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Installed {
		t.Error("expected Installed=false when target dir cannot be determined")
	}
}

func TestOpenCodeClient_UninstallAssets_UnsupportedType(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	c := NewClient()
	req := clients.UninstallRequest{
		Scope: &clients.InstallScope{Type: clients.ScopeGlobal},
		Assets: []asset.Asset{
			{Name: "x", Type: asset.TypeHook},
		},
	}
	resp, err := c.UninstallAssets(context.Background(), req)
	if err != nil {
		t.Fatalf("UninstallAssets returned error: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Status != clients.StatusSkipped {
		t.Errorf("expected one skipped result, got: %#v", resp.Results)
	}
}
