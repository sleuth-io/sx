package claude_code

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/bootstrap"
)

// TestInstallBootstrapMCP_WritesToClaudeJSON verifies that bootstrap MCP install
// writes to ~/.claude.json (global scope), not {parent}/.mcp.json.
// Regression test: previously homeDir was passed instead of claudeDir to
// AddMCPServer, causing mcpConfigPath to resolve filepath.Dir(homeDir)/.mcp.json
// (e.g., /Users/.mcp.json) instead of ~/.claude.json.
func TestInstallBootstrapMCP_WritesToClaudeJSON(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home", "testuser")
	claudeDir := filepath.Join(homeDir, ".claude")

	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("Failed to create .claude directory: %v", err)
	}

	mcpOpt := bootstrap.Option{
		Key:         bootstrap.SleuthAIQueryMCPKey,
		Description: "Test MCP",
		MCPConfig: &bootstrap.MCPServerConfig{
			Name:    "sx",
			Command: "test-sx",
			Args:    []string{"serve"},
		},
	}

	if err := installBootstrap([]bootstrap.Option{mcpOpt}); err != nil {
		t.Fatalf("installBootstrap failed: %v", err)
	}

	// The MCP server should be written to ~/.claude.json (global scope)
	claudeJSON := filepath.Join(homeDir, ".claude.json")
	if _, err := os.Stat(claudeJSON); os.IsNotExist(err) {
		parentMCP := filepath.Join(filepath.Dir(homeDir), ".mcp.json")
		if _, err := os.Stat(parentMCP); err == nil {
			t.Fatalf("MCP config written to %s instead of %s — homeDir passed instead of claudeDir", parentMCP, claudeJSON)
		}
		t.Fatalf("Expected %s to exist after bootstrap MCP install", claudeJSON)
	}

	data, err := os.ReadFile(claudeJSON)
	if err != nil {
		t.Fatalf("Failed to read .claude.json: %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("Failed to parse .claude.json: %v", err)
	}

	mcpServers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers section not found in .claude.json")
	}

	if _, ok := mcpServers["sx"].(map[string]any); !ok {
		t.Fatal("sx server not found in mcpServers")
	}
}

// TestUninstallBootstrapMCP_RemovesFromClaudeJSON verifies that bootstrap MCP
// uninstall removes from ~/.claude.json (global scope).
func TestUninstallBootstrapMCP_RemovesFromClaudeJSON(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home", "testuser")
	claudeDir := filepath.Join(homeDir, ".claude")

	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("Failed to create .claude directory: %v", err)
	}

	// Pre-populate ~/.claude.json with an MCP server
	claudeJSON := filepath.Join(homeDir, ".claude.json")
	config := map[string]any{
		"mcpServers": map[string]any{
			"sx": map[string]any{
				"command": "test-sx",
				"args":    []any{"serve"},
				"type":    "stdio",
			},
		},
	}
	data, _ := json.MarshalIndent(config, "", "  ")
	if err := os.WriteFile(claudeJSON, data, 0644); err != nil {
		t.Fatalf("Failed to write .claude.json: %v", err)
	}

	mcpOpt := bootstrap.Option{
		Key:         bootstrap.SleuthAIQueryMCPKey,
		Description: "Test MCP",
		MCPConfig: &bootstrap.MCPServerConfig{
			Name:    "sx",
			Command: "test-sx",
			Args:    []string{"serve"},
		},
	}

	if err := uninstallBootstrap([]bootstrap.Option{mcpOpt}); err != nil {
		t.Fatalf("uninstallBootstrap failed: %v", err)
	}

	data, err := os.ReadFile(claudeJSON)
	if err != nil {
		t.Fatalf("Failed to read .claude.json: %v", err)
	}

	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("Failed to parse .claude.json: %v", err)
	}

	mcpServers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers section not found")
	}

	if _, exists := mcpServers["sx"]; exists {
		t.Error("sx server should have been removed from .claude.json")
	}
}
