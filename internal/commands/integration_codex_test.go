package commands

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/clients/codex"
)

func init() {
	// Ensure codex client is registered for these tests
	clients.Register(codex.NewClient())
}

// TestCodexClientDetection tests that Codex is detected when ~/.codex exists
func TestCodexClientDetection(t *testing.T) {
	env := NewTestEnv(t)

	// Initially Codex should not be detected
	client, err := clients.Global().Get(clients.ClientIDCodex)
	if err != nil {
		t.Fatalf("Codex client not registered: %v", err)
	}

	if client.IsInstalled() {
		t.Error("Codex should not be detected without ~/.codex directory")
	}

	// Create .codex directory
	codexDir := filepath.Join(env.HomeDir, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("Failed to create .codex dir: %v", err)
	}

	// Now Codex should be detected
	if !client.IsInstalled() {
		t.Error("Codex should be detected with ~/.codex directory")
	}

	t.Log("✓ Codex client detection test passed!")
}

// TestCodexSupportedAssetTypes verifies Codex supports the expected asset types
func TestCodexSupportedAssetTypes(t *testing.T) {
	client, err := clients.Global().Get(clients.ClientIDCodex)
	if err != nil {
		t.Fatalf("Codex client not registered: %v", err)
	}

	// Should support
	supported := []string{"skill", "command", "mcp"}
	for _, typeName := range supported {
		if !client.SupportsAssetType(asset.FromString(typeName)) {
			t.Errorf("Codex should support %s assets", typeName)
		}
	}

	// Should not support
	unsupported := []string{"agent", "rule", "hook"}
	for _, typeName := range unsupported {
		if client.SupportsAssetType(asset.FromString(typeName)) {
			t.Errorf("Codex should not support %s assets", typeName)
		}
	}

	t.Log("✓ Codex asset type support test passed!")
}

// TestCodexClientInfo verifies Codex appears in client info
func TestCodexClientInfo(t *testing.T) {
	env := NewTestEnv(t)
	setupTestConfig(t, env.HomeDir, nil, nil)

	// Create .codex directory so it's detected
	codexDir := filepath.Join(env.HomeDir, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("Failed to create .codex dir: %v", err)
	}

	infos := gatherClientInfo()

	var found bool
	for _, info := range infos {
		if info.ID == clients.ClientIDCodex {
			found = true
			if !info.Installed {
				t.Error("Codex should show as installed")
			}
			if info.Name != "Codex" {
				t.Errorf("Expected display name 'Codex', got %q", info.Name)
			}
			if info.Directory != codexDir {
				t.Errorf("Expected directory %q, got %q", codexDir, info.Directory)
			}
			break
		}
	}

	if !found {
		t.Error("Codex not found in client info")
	}

	t.Log("✓ Codex client info test passed!")
}

// TestCodexBootstrapOptions verifies Codex returns correct bootstrap options
func TestCodexBootstrapOptions(t *testing.T) {
	client, err := clients.Global().Get(clients.ClientIDCodex)
	if err != nil {
		t.Fatalf("Codex client not registered: %v", err)
	}

	opts := client.GetBootstrapOptions(context.Background())

	// Should have analytics hook and MCP option
	var hasAnalytics, hasMCP bool
	for _, opt := range opts {
		if opt.Key == bootstrap.AnalyticsHookKey {
			hasAnalytics = true
		}
		if opt.Key == bootstrap.SleuthAIQueryMCPKey {
			hasMCP = true
		}
		// Should NOT have session hook (Codex doesn't support it)
		if opt.Key == "session_hook" {
			t.Error("Codex should not offer session_hook option")
		}
	}

	if !hasAnalytics {
		t.Error("Codex should offer analytics_hook option")
	}
	if !hasMCP {
		t.Error("Codex should offer sleuth_ai_query_mcp option")
	}

	t.Log("✓ Codex bootstrap options test passed!")
}

// TestCodexGetBootstrapPath verifies the bootstrap path
func TestCodexGetBootstrapPath(t *testing.T) {
	env := NewTestEnv(t)

	client, err := clients.Global().Get(clients.ClientIDCodex)
	if err != nil {
		t.Fatalf("Codex client not registered: %v", err)
	}

	path := client.GetBootstrapPath()
	expected := filepath.Join(env.HomeDir, ".codex", "config.toml")
	if path != expected {
		t.Errorf("GetBootstrapPath() = %q, want %q", path, expected)
	}

	t.Log("✓ Codex bootstrap path test passed!")
}

// TestCodexShouldInstall verifies ShouldInstall always returns true
func TestCodexShouldInstall(t *testing.T) {
	client, err := clients.Global().Get(clients.ClientIDCodex)
	if err != nil {
		t.Fatalf("Codex client not registered: %v", err)
	}

	should, err := client.ShouldInstall(context.Background())
	if err != nil {
		t.Fatalf("ShouldInstall error: %v", err)
	}
	if !should {
		t.Error("ShouldInstall should always return true for Codex")
	}

	t.Log("✓ Codex ShouldInstall test passed!")
}
