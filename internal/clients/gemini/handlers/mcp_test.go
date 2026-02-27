package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

func TestGeminiMCPHandler_Packaged_SubcommandArgs(t *testing.T) {
	// When args contain a mix of subcommands (e.g. "run") and actual files (e.g. "server.py"),
	// only the files should be converted to absolute paths.
	installPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(installPath, "server.py"), []byte("print('hi')"), 0644); err != nil {
		t.Fatalf("Failed to create server.py: %v", err)
	}

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "uv-server", Version: "1.0.0", Type: asset.TypeMCP},
		MCP: &metadata.MCPConfig{
			Command: "uv",
			Args:    []string{"run", "server.py"},
		},
	}

	handler := NewMCPHandler(meta)
	entry := handler.generatePackagedMCPEntry(installPath)

	// "uv" is a bare command, should stay as-is
	if entry["command"] != "uv" {
		t.Errorf("command = %q, want \"uv\"", entry["command"])
	}

	args, ok := entry["args"].([]any)
	if !ok || len(args) != 2 {
		t.Fatalf("args should have 2 elements, got %v", entry["args"])
	}

	// "run" is a uv subcommand (not a file), should stay as-is
	if args[0] != "run" {
		t.Errorf("arg[0] = %q, want \"run\"", args[0])
	}

	// "server.py" exists in install dir, should be made absolute
	expectedPath := filepath.Join(installPath, "server.py")
	if args[1] != expectedPath {
		t.Errorf("arg[1] = %q, want %q", args[1], expectedPath)
	}
}

func TestGeminiMCPHandler_Packaged_BareCommand(t *testing.T) {
	installPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(installPath, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installPath, "src", "index.js"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeMCP},
		MCP: &metadata.MCPConfig{
			Command: "node",
			Args:    []string{"src/index.js"},
		},
	}

	handler := NewMCPHandler(meta)
	entry := handler.generatePackagedMCPEntry(installPath)

	// Bare command "node" should stay as-is
	if entry["command"] != "node" {
		t.Errorf("command = %q, want \"node\"", entry["command"])
	}

	args := entry["args"].([]any)
	expectedPath := filepath.Join(installPath, "src/index.js")
	if args[0] != expectedPath {
		t.Errorf("arg[0] = %q, want %q", args[0], expectedPath)
	}
}

func TestReadWriteSettingsJSON(t *testing.T) {
	tempDir := t.TempDir()
	settingsPath := filepath.Join(tempDir, "settings.json")

	// Test reading non-existent file returns empty config
	config, err := ReadSettingsJSON(settingsPath)
	if err != nil {
		t.Fatalf("ReadSettingsJSON() error = %v", err)
	}

	if config.MCPServers == nil {
		t.Error("MCPServers should not be nil")
	}

	if len(config.MCPServers) != 0 {
		t.Errorf("MCPServers should be empty, got %d entries", len(config.MCPServers))
	}

	// Add an MCP server
	config.MCPServers["test-server"] = map[string]any{
		"command": "test-cmd",
		"args":    []string{"arg1", "arg2"},
	}

	// Write and read back
	if err := WriteSettingsJSON(settingsPath, config); err != nil {
		t.Fatalf("WriteSettingsJSON() error = %v", err)
	}

	readConfig, err := ReadSettingsJSON(settingsPath)
	if err != nil {
		t.Fatalf("ReadSettingsJSON() error = %v", err)
	}

	if len(readConfig.MCPServers) != 1 {
		t.Errorf("Expected 1 MCP server, got %d", len(readConfig.MCPServers))
	}

	serverEntry, ok := readConfig.MCPServers["test-server"].(map[string]any)
	if !ok {
		t.Fatal("test-server entry not found or wrong type")
	}

	if serverEntry["command"] != "test-cmd" {
		t.Errorf("command = %q, want %q", serverEntry["command"], "test-cmd")
	}
}

func TestAddRemoveMCPServer(t *testing.T) {
	tempDir := t.TempDir()

	// Add first server
	if err := AddMCPServer(tempDir, "server1", map[string]any{
		"command": "cmd1",
		"args":    []string{"a"},
	}); err != nil {
		t.Fatalf("AddMCPServer() error = %v", err)
	}

	// Add second server
	if err := AddMCPServer(tempDir, "server2", map[string]any{
		"command": "cmd2",
		"args":    []string{"b"},
	}); err != nil {
		t.Fatalf("AddMCPServer() error = %v", err)
	}

	// Verify both exist
	config, err := ReadSettingsJSON(filepath.Join(tempDir, "settings.json"))
	if err != nil {
		t.Fatalf("ReadSettingsJSON() error = %v", err)
	}

	if len(config.MCPServers) != 2 {
		t.Errorf("Expected 2 MCP servers, got %d", len(config.MCPServers))
	}

	// Remove first server
	if err := RemoveMCPServer(tempDir, "server1"); err != nil {
		t.Fatalf("RemoveMCPServer() error = %v", err)
	}

	// Verify only second remains
	config, err = ReadSettingsJSON(filepath.Join(tempDir, "settings.json"))
	if err != nil {
		t.Fatalf("ReadSettingsJSON() error = %v", err)
	}

	if len(config.MCPServers) != 1 {
		t.Errorf("Expected 1 MCP server, got %d", len(config.MCPServers))
	}

	if _, exists := config.MCPServers["server2"]; !exists {
		t.Error("server2 should still exist")
	}

	if _, exists := config.MCPServers["server1"]; exists {
		t.Error("server1 should have been removed")
	}
}

func TestSettingsJSONPreservesOtherFields(t *testing.T) {
	tempDir := t.TempDir()
	settingsPath := filepath.Join(tempDir, "settings.json")

	// Create settings.json with extra fields
	initialSettings := map[string]any{
		"someOtherSetting": "value1",
		"anotherField": map[string]any{
			"nested": true,
		},
		"mcpServers": map[string]any{},
	}

	data, err := json.MarshalIndent(initialSettings, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	// Add an MCP server
	if err := AddMCPServer(tempDir, "test-server", map[string]any{
		"command": "test-cmd",
	}); err != nil {
		t.Fatalf("AddMCPServer() error = %v", err)
	}

	// Read the file back and verify other fields preserved
	data, err = os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if result["someOtherSetting"] != "value1" {
		t.Error("someOtherSetting was not preserved")
	}

	nested, ok := result["anotherField"].(map[string]any)
	if !ok {
		t.Error("anotherField was not preserved")
	} else if nested["nested"] != true {
		t.Error("nested field was not preserved")
	}

	mcpServers, ok := result["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers not found")
	}

	if _, exists := mcpServers["test-server"]; !exists {
		t.Error("test-server was not added")
	}
}
