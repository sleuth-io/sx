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

	// Verify config.toml was created
	config := readTOML(t, filepath.Join(targetBase, "config.toml"))
	mcpList, ok := config["mcp"].([]map[string]any)
	if !ok {
		t.Fatal("mcp section not found in config.toml")
	}

	var found bool
	for _, server := range mcpList {
		if server["name"] == "remote-server" {
			found = true
			if server["command"] != "npx" {
				t.Errorf("command = %v, want npx", server["command"])
			}
			if server["transport"] != "stdio" {
				t.Errorf("transport = %v, want stdio", server["transport"])
			}
			break
		}
	}
	if !found {
		t.Error("remote-server not found in config.toml")
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

	// Verify config.toml
	config := readTOML(t, filepath.Join(targetBase, "config.toml"))
	mcpList := config["mcp"].([]map[string]any)

	var found bool
	for _, server := range mcpList {
		if server["name"] == "local-server" {
			found = true
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
			break
		}
	}
	if !found {
		t.Error("local-server not found")
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
[[mcp]]
name = "my-server"
transport = "stdio"
command = "npx"

[[mcp]]
name = "other-server"
transport = "stdio"
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
	mcpList := config["mcp"].([]map[string]any)

	for _, server := range mcpList {
		if server["name"] == "my-server" {
			t.Error("my-server should be removed")
		}
	}

	// Verify other-server is still there
	var otherFound bool
	for _, server := range mcpList {
		if server["name"] == "other-server" {
			otherFound = true
			break
		}
	}
	if !otherFound {
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
[[mcp]]
name = "remote"
transport = "stdio"
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
	mcpList := config["mcp"].([]map[string]any)

	var found bool
	for _, server := range mcpList {
		if server["name"] == "remote-sse" {
			found = true
			if server["url"] != "https://example.com/mcp/sse" {
				t.Errorf("url = %v, want https://example.com/mcp/sse", server["url"])
			}
			if server["transport"] != "sse" {
				t.Errorf("transport = %v, want sse", server["transport"])
			}
			// Should NOT have command
			if _, hasCommand := server["command"]; hasCommand {
				t.Error("Remote MCP should not have command field")
			}
			break
		}
	}
	if !found {
		t.Error("remote-sse not found")
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

[[mcp]]
name = "existing-server"
transport = "stdio"
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

	// Both servers should exist
	mcpList := config["mcp"].([]map[string]any)
	var foundExisting, foundNew bool
	for _, server := range mcpList {
		if server["name"] == "existing-server" {
			foundExisting = true
		}
		if server["name"] == "new-server" {
			foundNew = true
		}
	}
	if !foundExisting {
		t.Error("existing-server should be preserved")
	}
	if !foundNew {
		t.Error("new-server should be added")
	}
}

func TestCodexMCPHandler_UpdateExistingServer(t *testing.T) {
	targetBase := t.TempDir()

	// Create existing config.toml with a server
	existingConfig := `
[[mcp]]
name = "my-server"
transport = "stdio"
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
	mcpList := config["mcp"].([]map[string]any)

	// Should only have one server entry
	if len(mcpList) != 1 {
		t.Errorf("Expected 1 server, got %d", len(mcpList))
	}

	server := mcpList[0]
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
		Name:      "server1",
		Transport: "stdio",
		Command:   "cmd1",
		Args:      []string{"arg1"},
	}
	if err := AddMCPServer(configPath, "server1", entry1); err != nil {
		t.Fatalf("AddMCPServer failed: %v", err)
	}

	// Add second server
	entry2 := MCPServerEntry{
		Name:      "server2",
		Transport: "stdio",
		Command:   "cmd2",
	}
	if err := AddMCPServer(configPath, "server2", entry2); err != nil {
		t.Fatalf("AddMCPServer failed: %v", err)
	}

	// Verify both exist
	config, _, err := ReadCodexConfig(configPath)
	if err != nil {
		t.Fatalf("ReadCodexConfig failed: %v", err)
	}

	if len(config.MCP) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(config.MCP))
	}

	var found1, found2 bool
	for _, s := range config.MCP {
		if s.Name == "server1" {
			found1 = true
		}
		if s.Name == "server2" {
			found2 = true
		}
	}
	if !found1 || !found2 {
		t.Error("Both servers should exist")
	}
}

func TestRemoveMCPServer(t *testing.T) {
	targetBase := t.TempDir()
	configPath := filepath.Join(targetBase, "config.toml")

	// Add two servers
	AddMCPServer(configPath, "keep", MCPServerEntry{Name: "keep", Command: "cmd"})
	AddMCPServer(configPath, "remove", MCPServerEntry{Name: "remove", Command: "cmd"})

	// Remove one
	if err := RemoveMCPServer(configPath, "remove"); err != nil {
		t.Fatalf("RemoveMCPServer failed: %v", err)
	}

	config, _, _ := ReadCodexConfig(configPath)
	if len(config.MCP) != 1 {
		t.Errorf("Expected 1 server, got %d", len(config.MCP))
	}
	if config.MCP[0].Name != "keep" {
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
	AddMCPServer(configPath, "test", MCPServerEntry{Name: "test", Command: "cmd"})

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
	if len(config.MCP) != 0 {
		t.Error("MCP should be empty")
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

[[mcp]]
name = "server1"
transport = "stdio"
command = "/usr/bin/server"
args = ["--port", "8080"]

[[mcp]]
name = "server2"
transport = "sse"
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

	if len(config.MCP) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(config.MCP))
	}

	// Check server1
	var s1 *MCPServerEntry
	for i := range config.MCP {
		if config.MCP[i].Name == "server1" {
			s1 = &config.MCP[i]
			break
		}
	}
	if s1 == nil {
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
		MCP: []MCPServerEntry{
			{Name: "test", Transport: "stdio", Command: "cmd", Args: []string{"a", "b"}},
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
	if len(config2.MCP) != 1 {
		t.Errorf("MCP servers not preserved: %d", len(config2.MCP))
	}
	if config2.MCP[0].Name != "test" {
		t.Errorf("MCP name = %q", config2.MCP[0].Name)
	}
}

func TestMCPConfigPath(t *testing.T) {
	path := mcpConfigPath("/home/user/.codex")
	if !strings.HasSuffix(path, "config.toml") {
		t.Errorf("Expected config.toml, got: %s", path)
	}
}
