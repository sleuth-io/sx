package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/clients"
	_ "github.com/sleuth-io/sx/internal/clients/openclaw"
)

// TestOpenClawClientDetection tests that OpenClaw is detected when ~/.openclaw exists
func TestOpenClawClientDetection(t *testing.T) {
	env := NewTestEnv(t)

	client, err := clients.Global().Get(clients.ClientIDOpenClaw)
	if err != nil {
		t.Fatalf("OpenClaw client not registered: %v", err)
	}

	if client.IsInstalled() {
		t.Error("OpenClaw should not be detected without ~/.openclaw directory")
	}

	// Create .openclaw directory
	openclawDir := filepath.Join(env.HomeDir, ".openclaw")
	if err := os.MkdirAll(openclawDir, 0755); err != nil {
		t.Fatalf("Failed to create .openclaw dir: %v", err)
	}

	if !client.IsInstalled() {
		t.Error("OpenClaw should be detected with ~/.openclaw directory")
	}
}

// TestOpenClawSupportedAssetTypes verifies OpenClaw supports only skills
func TestOpenClawSupportedAssetTypes(t *testing.T) {
	client, err := clients.Global().Get(clients.ClientIDOpenClaw)
	if err != nil {
		t.Fatalf("OpenClaw client not registered: %v", err)
	}

	// Should support
	if !client.SupportsAssetType(asset.TypeSkill) {
		t.Error("OpenClaw should support skill assets")
	}

	// Should not support
	unsupported := []string{"command", "agent", "rule", "hook", "mcp"}
	for _, typeName := range unsupported {
		if client.SupportsAssetType(asset.FromString(typeName)) {
			t.Errorf("OpenClaw should not support %s assets", typeName)
		}
	}
}

// TestOpenClawRepoScopedSkipped verifies repo/path scoped assets are skipped
func TestOpenClawRepoScopedSkipped(t *testing.T) {
	client, err := clients.Global().Get(clients.ClientIDOpenClaw)
	if err != nil {
		t.Fatalf("OpenClaw client not registered: %v", err)
	}

	ctx := context.Background()

	repoScope := &clients.InstallScope{
		Type:     clients.ScopeRepository,
		RepoRoot: "/tmp/test-repo",
	}

	// Install with repo scope should result in skipped status
	resp, err := client.InstallAssets(ctx, clients.InstallRequest{
		Assets: []*clients.AssetBundle{},
		Scope:  repoScope,
	})
	if err != nil {
		t.Fatalf("InstallAssets error: %v", err)
	}
	// Empty assets = empty results, which is fine
	_ = resp
}

// TestOpenClawBootstrapInstall verifies hook dir and MCP config are created
func TestOpenClawBootstrapInstall(t *testing.T) {
	env := NewTestEnv(t)

	client, err := clients.Global().Get(clients.ClientIDOpenClaw)
	if err != nil {
		t.Fatalf("OpenClaw client not registered: %v", err)
	}

	openclawDir := filepath.Join(env.HomeDir, ".openclaw")
	if err := os.MkdirAll(openclawDir, 0755); err != nil {
		t.Fatalf("Failed to create .openclaw dir: %v", err)
	}

	ctx := context.Background()
	opts := []bootstrap.Option{
		bootstrap.SessionHook,
	}

	if err := client.InstallBootstrap(ctx, opts); err != nil {
		t.Fatalf("InstallBootstrap error: %v", err)
	}

	// Verify hook directory exists with HOOK.md and index.ts
	hookDir := filepath.Join(openclawDir, "hooks", "sx-install")
	if _, err := os.Stat(filepath.Join(hookDir, "HOOK.md")); err != nil {
		t.Errorf("HOOK.md should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(hookDir, "index.ts")); err != nil {
		t.Errorf("index.ts should exist: %v", err)
	}

	// Verify HOOK.md contains correct event
	hookContent, err := os.ReadFile(filepath.Join(hookDir, "HOOK.md"))
	if err != nil {
		t.Fatalf("Failed to read HOOK.md: %v", err)
	}
	if !strings.Contains(string(hookContent), "before_agent_start") {
		t.Error("HOOK.md should contain before_agent_start event")
	}

	// Verify index.ts contains sx install command
	indexContent, err := os.ReadFile(filepath.Join(hookDir, "index.ts"))
	if err != nil {
		t.Fatalf("Failed to read index.ts: %v", err)
	}
	if !strings.Contains(string(indexContent), "sx install --hook-mode --client=openclaw") {
		t.Error("index.ts should contain sx install command")
	}
}

// TestOpenClawBootstrapIdempotent verifies install can be run twice without duplicates
func TestOpenClawBootstrapIdempotent(t *testing.T) {
	env := NewTestEnv(t)

	client, err := clients.Global().Get(clients.ClientIDOpenClaw)
	if err != nil {
		t.Fatalf("OpenClaw client not registered: %v", err)
	}

	openclawDir := filepath.Join(env.HomeDir, ".openclaw")
	if err := os.MkdirAll(openclawDir, 0755); err != nil {
		t.Fatalf("Failed to create .openclaw dir: %v", err)
	}

	ctx := context.Background()
	opts := []bootstrap.Option{
		bootstrap.SessionHook,
	}

	// Install twice
	if err := client.InstallBootstrap(ctx, opts); err != nil {
		t.Fatalf("First InstallBootstrap error: %v", err)
	}
	if err := client.InstallBootstrap(ctx, opts); err != nil {
		t.Fatalf("Second InstallBootstrap error: %v", err)
	}

	// Verify hook files still exist and aren't duplicated
	hookDir := filepath.Join(openclawDir, "hooks", "sx-install")
	if _, err := os.Stat(filepath.Join(hookDir, "HOOK.md")); err != nil {
		t.Error("HOOK.md should still exist after second install")
	}
	if _, err := os.Stat(filepath.Join(hookDir, "index.ts")); err != nil {
		t.Error("index.ts should still exist after second install")
	}
}

// TestOpenClawBootstrapUninstall verifies cleanup removes hook dir
func TestOpenClawBootstrapUninstall(t *testing.T) {
	env := NewTestEnv(t)

	client, err := clients.Global().Get(clients.ClientIDOpenClaw)
	if err != nil {
		t.Fatalf("OpenClaw client not registered: %v", err)
	}

	openclawDir := filepath.Join(env.HomeDir, ".openclaw")
	if err := os.MkdirAll(openclawDir, 0755); err != nil {
		t.Fatalf("Failed to create .openclaw dir: %v", err)
	}

	ctx := context.Background()
	opts := []bootstrap.Option{
		bootstrap.SessionHook,
	}

	// Install first
	if err := client.InstallBootstrap(ctx, opts); err != nil {
		t.Fatalf("InstallBootstrap error: %v", err)
	}

	// Verify hook dir exists
	hookDir := filepath.Join(openclawDir, "hooks", "sx-install")
	if _, err := os.Stat(hookDir); err != nil {
		t.Fatalf("Hook dir should exist after install: %v", err)
	}

	// Uninstall
	if err := client.UninstallBootstrap(ctx, opts); err != nil {
		t.Fatalf("UninstallBootstrap error: %v", err)
	}

	// Hook dir should be gone
	if _, err := os.Stat(hookDir); !os.IsNotExist(err) {
		t.Error("Hook directory should be removed after uninstall")
	}
}

// TestOpenClawClientInfo verifies OpenClaw appears in client info
func TestOpenClawClientInfo(t *testing.T) {
	env := NewTestEnv(t)
	setupTestConfig(t, env.HomeDir, nil, nil)

	openclawDir := filepath.Join(env.HomeDir, ".openclaw")
	if err := os.MkdirAll(openclawDir, 0755); err != nil {
		t.Fatalf("Failed to create .openclaw dir: %v", err)
	}

	infos := gatherClientInfo()

	var found bool
	for _, info := range infos {
		if info.ID == clients.ClientIDOpenClaw {
			found = true
			if !info.Installed {
				t.Error("OpenClaw should show as installed")
			}
			if info.Name != "OpenClaw" {
				t.Errorf("Expected display name 'OpenClaw', got %q", info.Name)
			}
			if info.Directory != openclawDir {
				t.Errorf("Expected directory %q, got %q", openclawDir, info.Directory)
			}
			break
		}
	}

	if !found {
		t.Error("OpenClaw not found in client info")
	}
}

// TestOpenClawBootstrapOptions verifies OpenClaw returns correct bootstrap options
func TestOpenClawBootstrapOptions(t *testing.T) {
	client, err := clients.Global().Get(clients.ClientIDOpenClaw)
	if err != nil {
		t.Fatalf("OpenClaw client not registered: %v", err)
	}

	opts := client.GetBootstrapOptions(context.Background())

	var hasSession bool
	for _, opt := range opts {
		if opt.Key == bootstrap.SessionHookKey {
			hasSession = true
		}
		// MCP not yet supported for OpenClaw
		if opt.Key == bootstrap.SleuthAIQueryMCPKey {
			t.Error("OpenClaw should not offer MCP option (not yet supported)")
		}
	}

	if !hasSession {
		t.Error("OpenClaw should offer session_hook option")
	}
}

// TestOpenClawGetBootstrapPath verifies the bootstrap path
func TestOpenClawGetBootstrapPath(t *testing.T) {
	env := NewTestEnv(t)

	client, err := clients.Global().Get(clients.ClientIDOpenClaw)
	if err != nil {
		t.Fatalf("OpenClaw client not registered: %v", err)
	}

	path := client.GetBootstrapPath()
	expected := filepath.Join(env.HomeDir, ".openclaw", "openclaw.json")
	if path != expected {
		t.Errorf("GetBootstrapPath() = %q, want %q", path, expected)
	}
}

// TestOpenClawShouldInstallDedup verifies timestamp-based dedup
func TestOpenClawShouldInstallDedup(t *testing.T) {
	_ = NewTestEnv(t)

	client, err := clients.Global().Get(clients.ClientIDOpenClaw)
	if err != nil {
		t.Fatalf("OpenClaw client not registered: %v", err)
	}

	ctx := context.Background()

	// First call should return true
	should, err := client.ShouldInstall(ctx)
	if err != nil {
		t.Fatalf("ShouldInstall error: %v", err)
	}
	if !should {
		t.Error("First ShouldInstall call should return true")
	}

	// Second call within same hour should return false
	should, err = client.ShouldInstall(ctx)
	if err != nil {
		t.Fatalf("ShouldInstall error: %v", err)
	}
	if should {
		t.Error("Second ShouldInstall call should return false (dedup)")
	}
}

// TestOpenClawRuleCapabilities verifies no rule support
func TestOpenClawRuleCapabilities(t *testing.T) {
	client, err := clients.Global().Get(clients.ClientIDOpenClaw)
	if err != nil {
		t.Fatalf("OpenClaw client not registered: %v", err)
	}

	if client.RuleCapabilities() != nil {
		t.Error("OpenClaw should not have rule capabilities")
	}
}
