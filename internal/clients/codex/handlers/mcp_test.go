package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

func createTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("Failed to create zip entry %q: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("Failed to write zip entry %q: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Failed to close zip: %v", err)
	}
	return buf.Bytes()
}

func readTOML(t *testing.T, path string) map[string]any {
	t.Helper()
	var result map[string]any
	if _, err := toml.DecodeFile(path, &result); err != nil {
		t.Fatalf("Failed to parse %s: %v", path, err)
	}
	return result
}

func TestCodexMCPHandler_ConfigOnly_Install(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "remote-server",
			Version: "1.0.0",
			Type:    asset.TypeMCP,
		},
		MCP: &metadata.MCPConfig{
			Command: "npx",
			Args:    []string{"-y", "@example/mcp-server"},
		},
	}

	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "remote-server"
version = "1.0.0"
type = "mcp"

[mcp]
command = "npx"
args = ["-y", "@example/mcp-server"]
`,
	})

	handler := NewMCPHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify config.toml was created with [mcp_servers.remote-server] format
	config := readTOML(t, filepath.Join(targetBase, "config.toml"))
	mcpServers, ok := config["mcp_servers"].(map[string]any)
	if !ok {
		t.Fatal("mcp_servers section not found in config.toml")
	}

	server, found := mcpServers["remote-server"].(map[string]any)
	if !found {
		t.Error("remote-server not found in config.toml")
	}
	if server["command"] != "npx" {
		t.Errorf("command = %v, want npx", server["command"])
	}

	// No install directory for config-only
	installDir := filepath.Join(targetBase, DirMCPServers, "remote-server")
	if _, err := os.Stat(installDir); !os.IsNotExist(err) {
		t.Error("Config-only should not create install directory")
	}
}

func TestCodexMCPHandler_Packaged_Install(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "local-server",
			Version: "1.0.0",
			Type:    asset.TypeMCP,
		},
		MCP: &metadata.MCPConfig{
			Command: "node",
			Args:    []string{"src/index.js"},
		},
	}

	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "local-server"
version = "1.0.0"
type = "mcp"

[mcp]
command = "node"
args = ["src/index.js"]
`,
		"src/index.js": "console.log('hi')",
		"package.json": "{}",
	})

	handler := NewMCPHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify config.toml with [mcp_servers.local-server] format
	config := readTOML(t, filepath.Join(targetBase, "config.toml"))
	mcpServers, ok := config["mcp_servers"].(map[string]any)
	if !ok {
		t.Fatal("mcp_servers section not found")
	}

	server, found := mcpServers["local-server"].(map[string]any)
	if !found {
		t.Error("local-server not found")
	}

	// System commands like "node" should stay as-is (resolved from PATH)
	command, ok := server["command"].(string)
	if !ok {
		t.Fatal("command should be string")
	}
	if command != "node" {
		t.Errorf("System command should stay as 'node', got: %s", command)
	}

	// Args with paths should become absolute
	args, ok := server["args"].([]any)
	if !ok || len(args) == 0 {
		t.Fatal("args should exist")
	}
	arg0, ok := args[0].(string)
	if !ok {
		t.Fatal("arg should be string")
	}
	if !filepath.IsAbs(arg0) {
		t.Errorf("Packaged arg should be absolute, got: %s", arg0)
	}

	// Install directory should exist
	installDir := filepath.Join(targetBase, DirMCPServers, "local-server")
	if _, err := os.Stat(installDir); os.IsNotExist(err) {
		t.Error("Packaged should create install directory")
	}
}

func TestCodexMCPHandler_Remove(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "my-server", Version: "1.0.0", Type: asset.TypeMCP},
		MCP:   &metadata.MCPConfig{Command: "npx", Args: []string{"s"}},
	}

	// Pre-populate config.toml with multiple servers
	configContent := `
[mcp_servers.my-server]
command = "npx"

[mcp_servers.other-server]
command = "other"
`
	configPath := filepath.Join(targetBase, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config.toml: %v", err)
	}

	handler := NewMCPHandler(meta)
	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	config := readTOML(t, configPath)
	mcpServers, ok := config["mcp_servers"].(map[string]any)
	if !ok {
		t.Fatal("mcp_servers should exist")
	}

	if _, exists := mcpServers["my-server"]; exists {
		t.Error("my-server should be removed")
	}

	// Verify other-server is still there
	if _, exists := mcpServers["other-server"]; !exists {
		t.Error("other-server should be preserved")
	}
}

func TestCodexMCPHandler_VerifyInstalled_ConfigOnly(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "remote", Version: "1.0.0", Type: asset.TypeMCP},
		MCP:   &metadata.MCPConfig{Command: "npx", Args: []string{"s"}},
	}
	handler := NewMCPHandler(meta)

	// Not installed
	installed, _ := handler.VerifyInstalled(targetBase)
	if installed {
		t.Error("Should not be installed initially")
	}

	// Write config.toml
	configContent := `
[mcp_servers.remote]
command = "npx"
`
	if err := os.WriteFile(filepath.Join(targetBase, "config.toml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config.toml: %v", err)
	}

	installed, msg := handler.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("Should be installed, got: %s", msg)
	}
}

func TestCodexMCPHandler_ConfigOnly_RemoteMCP_Install(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "remote-sse",
			Version: "1.0.0",
			Type:    asset.TypeMCP,
		},
		MCP: &metadata.MCPConfig{
			Transport: "sse",
			URL:       "https://example.com/mcp/sse",
		},
	}

	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "remote-sse"
version = "1.0.0"
type = "mcp"

[mcp]
transport = "sse"
url = "https://example.com/mcp/sse"
`,
	})

	handler := NewMCPHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	config := readTOML(t, filepath.Join(targetBase, "config.toml"))
	mcpServers, ok := config["mcp_servers"].(map[string]any)
	if !ok {
		t.Fatal("mcp_servers section not found")
	}

	server, found := mcpServers["remote-sse"].(map[string]any)
	if !found {
		t.Fatal("remote-sse not found")
	}

	if server["url"] != "https://example.com/mcp/sse" {
		t.Errorf("url = %v, want https://example.com/mcp/sse", server["url"])
	}
	// Should NOT have command
	if _, hasCommand := server["command"]; hasCommand {
		t.Error("Remote MCP should not have command field")
	}
}

func TestCodexMCPHandler_ConfigOnly_WithEnv(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeMCP},
		MCP: &metadata.MCPConfig{
			Command: "docker",
			Args:    []string{"run", "server"},
			Env:     map[string]string{"TOKEN": "abc"},
		},
	}

	handler := NewMCPHandler(meta)
	entry := handler.generateConfigOnlyMCPEntry()

	if entry.Command != "docker" {
		t.Errorf("command = %v, want docker", entry.Command)
	}
	if entry.Env["TOKEN"] != "abc" {
		t.Errorf("env not correct: %v", entry.Env)
	}
}

func TestCodexMCPHandler_PreservesExistingConfig(t *testing.T) {
	targetBase := t.TempDir()

	// Create existing config.toml with other settings
	existingConfig := `
model = "gpt-4"
approval_policy = "unless-allow-listed"

[mcp_servers.existing-server]
command = "existing"
`
	configPath := filepath.Join(targetBase, "config.toml")
	if err := os.WriteFile(configPath, []byte(existingConfig), 0644); err != nil {
		t.Fatalf("Failed to write config.toml: %v", err)
	}

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "new-server", Version: "1.0.0", Type: asset.TypeMCP},
		MCP:   &metadata.MCPConfig{Command: "npx", Args: []string{"new"}},
	}

	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "new-server"
version = "1.0.0"
type = "mcp"

[mcp]
command = "npx"
args = ["new"]
`,
	})

	handler := NewMCPHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Read and verify config
	config := readTOML(t, configPath)

	// Other settings should be preserved
	if config["model"] != "gpt-4" {
		t.Errorf("model setting should be preserved, got: %v", config["model"])
	}
	if config["approval_policy"] != "unless-allow-listed" {
		t.Errorf("approval_policy should be preserved, got: %v", config["approval_policy"])
	}

	// Both servers should exist in mcp_servers map
	mcpServers := config["mcp_servers"].(map[string]any)
	if _, found := mcpServers["existing-server"]; !found {
		t.Error("existing-server should be preserved")
	}
	if _, found := mcpServers["new-server"]; !found {
		t.Error("new-server should be added")
	}
}

func TestCodexMCPHandler_UpdateExistingServer(t *testing.T) {
	targetBase := t.TempDir()

	// Create existing config.toml with a server
	existingConfig := `
[mcp_servers.my-server]
command = "old-command"
args = ["old-arg"]
`
	configPath := filepath.Join(targetBase, "config.toml")
	if err := os.WriteFile(configPath, []byte(existingConfig), 0644); err != nil {
		t.Fatalf("Failed to write config.toml: %v", err)
	}

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "my-server", Version: "2.0.0", Type: asset.TypeMCP},
		MCP:   &metadata.MCPConfig{Command: "new-command", Args: []string{"new-arg"}},
	}

	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "my-server"
version = "2.0.0"
type = "mcp"

[mcp]
command = "new-command"
args = ["new-arg"]
`,
	})

	handler := NewMCPHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	config := readTOML(t, configPath)
	mcpServers := config["mcp_servers"].(map[string]any)

	// Should only have one server entry
	if len(mcpServers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(mcpServers))
	}

	server := mcpServers["my-server"].(map[string]any)
	if server["command"] != "new-command" {
		t.Errorf("command should be updated, got: %v", server["command"])
	}
	args := server["args"].([]any)
	if len(args) != 1 || args[0] != "new-arg" {
		t.Errorf("args should be updated, got: %v", args)
	}
}

func TestAddMCPServer(t *testing.T) {
	targetBase := t.TempDir()
	configPath := filepath.Join(targetBase, "config.toml")

	// Add first server
	entry1 := MCPServerEntry{
		Command: "cmd1",
		Args:    []string{"arg1"},
	}
	if err := AddMCPServer(configPath, "server1", entry1); err != nil {
		t.Fatalf("AddMCPServer failed: %v", err)
	}

	// Add second server
	entry2 := MCPServerEntry{
		Command: "cmd2",
	}
	if err := AddMCPServer(configPath, "server2", entry2); err != nil {
		t.Fatalf("AddMCPServer failed: %v", err)
	}

	// Verify both exist
	config, _, err := ReadCodexConfig(configPath)
	if err != nil {
		t.Fatalf("ReadCodexConfig failed: %v", err)
	}

	if len(config.MCPServers) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(config.MCPServers))
	}

	_, found1 := config.MCPServers["server1"]
	_, found2 := config.MCPServers["server2"]
	if !found1 || !found2 {
		t.Error("Both servers should exist")
	}
}

func TestRemoveMCPServer(t *testing.T) {
	targetBase := t.TempDir()
	configPath := filepath.Join(targetBase, "config.toml")

	// Add two servers
	AddMCPServer(configPath, "keep", MCPServerEntry{Command: "cmd"})
	AddMCPServer(configPath, "remove", MCPServerEntry{Command: "cmd"})

	// Remove one
	if err := RemoveMCPServer(configPath, "remove"); err != nil {
		t.Fatalf("RemoveMCPServer failed: %v", err)
	}

	config, _, _ := ReadCodexConfig(configPath)
	if len(config.MCPServers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(config.MCPServers))
	}
	if _, exists := config.MCPServers["keep"]; !exists {
		t.Error("Wrong server removed")
	}
}

func TestVerifyMCPServerInstalled(t *testing.T) {
	targetBase := t.TempDir()
	configPath := filepath.Join(targetBase, "config.toml")

	// Not installed
	installed, _ := VerifyMCPServerInstalled(configPath, "test")
	if installed {
		t.Error("Should not be installed")
	}

	// Add server
	AddMCPServer(configPath, "test", MCPServerEntry{Command: "cmd"})

	installed, msg := VerifyMCPServerInstalled(configPath, "test")
	if !installed {
		t.Errorf("Should be installed: %s", msg)
	}

	// Different server
	installed, _ = VerifyMCPServerInstalled(configPath, "other")
	if installed {
		t.Error("other should not be installed")
	}
}

func TestReadCodexConfig_EmptyFile(t *testing.T) {
	targetBase := t.TempDir()
	configPath := filepath.Join(targetBase, "config.toml")

	// Non-existent file
	config, raw, err := ReadCodexConfig(configPath)
	if err != nil {
		t.Fatalf("Should not error on non-existent: %v", err)
	}
	if len(config.MCPServers) != 0 {
		t.Error("MCPServers should be empty")
	}
	if len(raw) != 0 {
		t.Error("raw should be empty")
	}
}

func TestReadCodexConfig_WithMCP(t *testing.T) {
	targetBase := t.TempDir()
	configPath := filepath.Join(targetBase, "config.toml")

	content := `
model = "gpt-4"

[mcp_servers.server1]
command = "/usr/bin/server"
args = ["--port", "8080"]

[mcp_servers.server2]
url = "https://example.com"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	config, raw, err := ReadCodexConfig(configPath)
	if err != nil {
		t.Fatalf("ReadCodexConfig failed: %v", err)
	}

	if raw["model"] != "gpt-4" {
		t.Errorf("model not preserved: %v", raw["model"])
	}

	if len(config.MCPServers) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(config.MCPServers))
	}

	// Check server1
	s1, exists := config.MCPServers["server1"]
	if !exists {
		t.Fatal("server1 not found")
	}
	if s1.Command != "/usr/bin/server" {
		t.Errorf("server1 command = %q", s1.Command)
	}
	if len(s1.Args) != 2 || s1.Args[0] != "--port" {
		t.Errorf("server1 args = %v", s1.Args)
	}
}

func TestWriteCodexConfig_RoundTrip(t *testing.T) {
	targetBase := t.TempDir()
	configPath := filepath.Join(targetBase, "config.toml")

	// Create config with MCP and other settings
	config := &CodexConfig{
		MCPServers: map[string]MCPServerEntry{
			"test": {Command: "cmd", Args: []string{"a", "b"}},
		},
	}
	raw := map[string]any{
		"model":           "gpt-4",
		"approval_policy": "unless-allow-listed",
	}

	if err := WriteCodexConfig(configPath, config, raw); err != nil {
		t.Fatalf("WriteCodexConfig failed: %v", err)
	}

	// Read back
	config2, raw2, err := ReadCodexConfig(configPath)
	if err != nil {
		t.Fatalf("ReadCodexConfig failed: %v", err)
	}

	if raw2["model"] != "gpt-4" {
		t.Errorf("model not preserved: %v", raw2["model"])
	}
	if len(config2.MCPServers) != 1 {
		t.Errorf("MCPServers not preserved: %d", len(config2.MCPServers))
	}
	if _, exists := config2.MCPServers["test"]; !exists {
		t.Error("test server not found")
	}
}

func TestMCPConfigPath(t *testing.T) {
	path := mcpConfigPath("/home/user/.codex")
	if !strings.HasSuffix(path, "config.toml") {
		t.Errorf("Expected config.toml, got: %s", path)
	}
}
