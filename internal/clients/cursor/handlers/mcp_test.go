package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read %s: %v", path, err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to parse %s: %v", path, err)
	}
	return result
}

func TestCursorMCPHandler_ConfigOnly_Install(t *testing.T) {
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

	// Verify mcp.json was created
	config := readJSON(t, filepath.Join(targetBase, "mcp.json"))
	servers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers not found in mcp.json")
	}

	server, ok := servers["remote-server"].(map[string]any)
	if !ok {
		t.Fatal("remote-server not found")
	}
	if server["command"] != "npx" {
		t.Errorf("command = %v, want npx", server["command"])
	}

	// No install directory for config-only
	installDir := filepath.Join(targetBase, "mcp-servers", "remote-server")
	if _, err := os.Stat(installDir); !os.IsNotExist(err) {
		t.Error("Config-only should not create install directory")
	}
}

func TestCursorMCPHandler_Packaged_Install(t *testing.T) {
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

	// Verify mcp.json
	config := readJSON(t, filepath.Join(targetBase, "mcp.json"))
	servers := config["mcpServers"].(map[string]any)
	server := servers["local-server"].(map[string]any)

	command, ok := server["command"].(string)
	if !ok {
		t.Fatal("command should be string")
	}
	if !filepath.IsAbs(command) {
		t.Errorf("Packaged command should be absolute, got: %s", command)
	}

	// Install directory should exist
	installDir := filepath.Join(targetBase, "mcp-servers", "local-server")
	if _, err := os.Stat(installDir); os.IsNotExist(err) {
		t.Error("Packaged should create install directory")
	}
}

func TestCursorMCPHandler_Remove(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "my-server", Version: "1.0.0", Type: asset.TypeMCP},
		MCP:   &metadata.MCPConfig{Command: "npx", Args: []string{"s"}},
	}

	// Pre-populate mcp.json
	config := &MCPConfig{
		MCPServers: map[string]any{
			"my-server":    map[string]any{"command": "npx"},
			"other-server": map[string]any{"command": "other"},
		},
	}
	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(filepath.Join(targetBase, "mcp.json"), data, 0644)

	handler := NewMCPHandler(meta)
	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	updated := readJSON(t, filepath.Join(targetBase, "mcp.json"))
	servers := updated["mcpServers"].(map[string]any)
	if _, exists := servers["my-server"]; exists {
		t.Error("my-server should be removed")
	}
	if _, exists := servers["other-server"]; !exists {
		t.Error("other-server should be preserved")
	}
}

func TestCursorMCPHandler_VerifyInstalled_ConfigOnly(t *testing.T) {
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

	// Write mcp.json
	config := &MCPConfig{
		MCPServers: map[string]any{
			"remote": map[string]any{"command": "npx"},
		},
	}
	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(filepath.Join(targetBase, "mcp.json"), data, 0644)

	installed, msg := handler.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("Should be installed, got: %s", msg)
	}
}

func TestCursorMCPHandler_ConfigOnly_RemoteMCP_Install(t *testing.T) {
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

	config := readJSON(t, filepath.Join(targetBase, "mcp.json"))
	servers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers not found in mcp.json")
	}

	server, ok := servers["remote-sse"].(map[string]any)
	if !ok {
		t.Fatal("remote-sse not found")
	}

	if server["url"] != "https://example.com/mcp/sse" {
		t.Errorf("url = %v, want \"https://example.com/mcp/sse\"", server["url"])
	}

	// Should NOT have command
	if _, hasCommand := server["command"]; hasCommand {
		t.Error("Remote MCP should not have command field")
	}

	// Config-only should NOT extract files
	installDir := filepath.Join(targetBase, "mcp-servers", "remote-sse")
	if _, err := os.Stat(installDir); !os.IsNotExist(err) {
		t.Error("Config-only remote MCP should not create install directory")
	}
}

func TestCursorMCPHandler_ConfigOnly_WithEnv(t *testing.T) {
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

	if entry["command"] != "docker" {
		t.Errorf("command = %v, want docker", entry["command"])
	}
	env, ok := entry["env"].(map[string]string)
	if !ok || env["TOKEN"] != "abc" {
		t.Errorf("env not correct: %v", entry["env"])
	}
}
