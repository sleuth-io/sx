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

func TestMCPHandler_ConfigOnly_Install(t *testing.T) {
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

	// Config-only zip: only metadata.toml
	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "remote-server"
version = "1.0.0"
type = "mcp"
description = "Remote MCP server"

[mcp]
command = "npx"
args = ["-y", "@example/mcp-server"]
`,
	})

	handler := NewMCPHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify .claude.json was created with the server entry
	config := readJSON(t, filepath.Join(targetBase, ".claude.json"))
	mcpServers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers section not found in .claude.json")
	}

	server, ok := mcpServers["remote-server"].(map[string]any)
	if !ok {
		t.Fatal("remote-server not found in mcpServers")
	}

	if server["command"] != "npx" {
		t.Errorf("command = %v, want \"npx\"", server["command"])
	}
	if server["type"] != "stdio" {
		t.Errorf("type = %v, want \"stdio\"", server["type"])
	}

	// Config-only should NOT extract files
	installDir := filepath.Join(targetBase, "mcp-servers", "remote-server")
	if _, err := os.Stat(installDir); !os.IsNotExist(err) {
		t.Error("Config-only MCP should not create install directory")
	}
}

func TestMCPHandler_Packaged_Install(t *testing.T) {
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

	// Packaged zip: metadata.toml + content files
	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "local-server"
version = "1.0.0"
type = "mcp"
description = "Local MCP server"

[mcp]
command = "node"
args = ["src/index.js"]
`,
		"src/index.js": "console.log('server')",
		"package.json": `{"name": "server"}`,
	})

	handler := NewMCPHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify .claude.json was created
	config := readJSON(t, filepath.Join(targetBase, ".claude.json"))
	mcpServers := config["mcpServers"].(map[string]any)
	server := mcpServers["local-server"].(map[string]any)

	// Command should be made absolute
	command, ok := server["command"].(string)
	if !ok {
		t.Fatal("command should be a string")
	}
	if !filepath.IsAbs(command) {
		t.Errorf("Packaged MCP command should be absolute, got: %s", command)
	}

	// Install directory should exist
	installDir := filepath.Join(targetBase, "mcp-servers", "local-server")
	if _, err := os.Stat(installDir); os.IsNotExist(err) {
		t.Error("Packaged MCP should create install directory")
	}
}

func TestMCPHandler_ConfigOnly_Remove(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "remote-server",
			Version: "1.0.0",
			Type:    asset.TypeMCP,
		},
		MCP: &metadata.MCPConfig{
			Command: "npx",
			Args:    []string{"server"},
		},
	}

	// Pre-populate .claude.json
	config := map[string]any{
		"mcpServers": map[string]any{
			"remote-server": map[string]any{
				"command":   "npx",
				"type":      "stdio",
				"_artifact": "remote-server",
			},
			"other-server": map[string]any{
				"command": "other",
			},
		},
	}
	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(filepath.Join(targetBase, ".claude.json"), data, 0644)

	handler := NewMCPHandler(meta)
	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify remote-server was removed but other-server preserved
	updated := readJSON(t, filepath.Join(targetBase, ".claude.json"))
	servers := updated["mcpServers"].(map[string]any)
	if _, exists := servers["remote-server"]; exists {
		t.Error("remote-server should have been removed")
	}
	if _, exists := servers["other-server"]; !exists {
		t.Error("other-server should be preserved")
	}
}

func TestMCPHandler_Remove_LegacyMCPJson(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "old-remote",
			Version: "1.0.0",
			Type:    asset.TypeMCP,
		},
		MCP: &metadata.MCPConfig{
			Command: "npx",
			Args:    []string{"server"},
		},
	}

	// Pre-populate both .claude.json and .mcp.json (legacy)
	claudeConfig := map[string]any{
		"mcpServers": map[string]any{
			"old-remote": map[string]any{"command": "npx"},
		},
	}
	mcpConfig := map[string]any{
		"mcpServers": map[string]any{
			"old-remote": map[string]any{"command": "npx"},
		},
	}
	claudeData, _ := json.MarshalIndent(claudeConfig, "", "  ")
	mcpData, _ := json.MarshalIndent(mcpConfig, "", "  ")
	os.WriteFile(filepath.Join(targetBase, ".claude.json"), claudeData, 0644)
	os.WriteFile(filepath.Join(targetBase, ".mcp.json"), mcpData, 0644)

	handler := NewMCPHandler(meta)
	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Both files should have the server removed
	updatedClaude := readJSON(t, filepath.Join(targetBase, ".claude.json"))
	if servers, ok := updatedClaude["mcpServers"].(map[string]any); ok {
		if _, exists := servers["old-remote"]; exists {
			t.Error("old-remote should be removed from .claude.json")
		}
	}

	updatedMCP := readJSON(t, filepath.Join(targetBase, ".mcp.json"))
	if servers, ok := updatedMCP["mcpServers"].(map[string]any); ok {
		if _, exists := servers["old-remote"]; exists {
			t.Error("old-remote should be removed from .mcp.json")
		}
	}
}

func TestMCPHandler_ConfigOnly_VerifyInstalled(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "remote-server",
			Version: "1.0.0",
			Type:    asset.TypeMCP,
		},
		MCP: &metadata.MCPConfig{
			Command: "npx",
			Args:    []string{"server"},
		},
	}

	handler := NewMCPHandler(meta)

	// Before install, should not be installed
	installed, _ := handler.VerifyInstalled(targetBase)
	if installed {
		t.Error("Should not be installed before installation")
	}

	// Write .claude.json with server entry
	config := map[string]any{
		"mcpServers": map[string]any{
			"remote-server": map[string]any{
				"command": "npx",
			},
		},
	}
	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(filepath.Join(targetBase, ".claude.json"), data, 0644)

	// After install, should be installed
	installed, msg := handler.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("Should be installed after writing config, got msg: %s", msg)
	}
}

func TestMCPHandler_ConfigOnly_BuildConfig(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeMCP},
		MCP: &metadata.MCPConfig{
			Command: "docker",
			Args:    []string{"run", "-i", "mcp/server"},
			Env:     map[string]string{"API_KEY": "xxx"},
			Timeout: 30,
		},
	}

	handler := NewMCPHandler(meta)
	config := handler.buildConfigOnlyMCPServerConfig()

	if config["command"] != "docker" {
		t.Errorf("command = %v, want docker", config["command"])
	}
	if config["type"] != "stdio" {
		t.Errorf("type = %v, want stdio", config["type"])
	}
	if config["_artifact"] != "test" {
		t.Errorf("_artifact = %v, want test", config["_artifact"])
	}
	if config["timeout"] != 30 {
		t.Errorf("timeout = %v, want 30", config["timeout"])
	}
	if env, ok := config["env"].(map[string]string); !ok || env["API_KEY"] != "xxx" {
		t.Errorf("env not set correctly: %v", config["env"])
	}
}

func TestMCPHandler_Packaged_BuildConfig(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeMCP},
		MCP: &metadata.MCPConfig{
			Command: "node",
			Args:    []string{"src/index.js"},
		},
	}

	handler := NewMCPHandler(meta)
	config := handler.buildPackagedMCPServerConfig("/opt/install/mcp-servers/test")

	command, ok := config["command"].(string)
	if !ok {
		t.Fatal("command should be string")
	}
	if command != "/opt/install/mcp-servers/test/node" {
		t.Errorf("command = %q, want absolute path", command)
	}

	args, ok := config["args"].([]any)
	if !ok || len(args) != 1 {
		t.Fatalf("args should have 1 element, got %v", config["args"])
	}
	// src/index.js is a relative path, should be made absolute
	argStr, ok := args[0].(string)
	if !ok || argStr != "/opt/install/mcp-servers/test/src/index.js" {
		t.Errorf("arg = %v, want absolute path", args[0])
	}
}

func TestMCPHandler_ConfigOnly_RemoteMCP_Install(t *testing.T) {
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

	config := readJSON(t, filepath.Join(targetBase, ".claude.json"))
	mcpServers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers section not found in .claude.json")
	}

	server, ok := mcpServers["remote-sse"].(map[string]any)
	if !ok {
		t.Fatal("remote-sse not found in mcpServers")
	}

	if server["type"] != "sse" {
		t.Errorf("type = %v, want \"sse\"", server["type"])
	}
	if server["url"] != "https://example.com/mcp/sse" {
		t.Errorf("url = %v, want \"https://example.com/mcp/sse\"", server["url"])
	}
	if server["_artifact"] != "remote-sse" {
		t.Errorf("_artifact = %v, want \"remote-sse\"", server["_artifact"])
	}

	// Should NOT have command or args
	if _, hasCommand := server["command"]; hasCommand {
		t.Error("Remote MCP should not have command field")
	}

	// Config-only should NOT extract files
	installDir := filepath.Join(targetBase, "mcp-servers", "remote-sse")
	if _, err := os.Stat(installDir); !os.IsNotExist(err) {
		t.Error("Config-only remote MCP should not create install directory")
	}
}

func TestMCPHandler_ConfigOnly_RemoteMCP_BuildConfig(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		url       string
	}{
		{"sse", "sse", "https://example.com/mcp/sse"},
		{"http", "http", "https://example.com/mcp"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			meta := &metadata.Metadata{
				Asset: metadata.Asset{Name: "test-" + tc.name, Version: "1.0.0", Type: asset.TypeMCP},
				MCP: &metadata.MCPConfig{
					Transport: tc.transport,
					URL:       tc.url,
					Env:       map[string]string{"API_KEY": "xxx"},
					Timeout:   30,
				},
			}

			handler := NewMCPHandler(meta)
			config := handler.buildConfigOnlyMCPServerConfig()

			if config["type"] != tc.transport {
				t.Errorf("type = %v, want %v", config["type"], tc.transport)
			}
			if config["url"] != tc.url {
				t.Errorf("url = %v, want %v", config["url"], tc.url)
			}
			if config["_artifact"] != "test-"+tc.name {
				t.Errorf("_artifact = %v, want test-%s", config["_artifact"], tc.name)
			}
			if config["timeout"] != 30 {
				t.Errorf("timeout = %v, want 30", config["timeout"])
			}
			if env, ok := config["env"].(map[string]string); !ok || env["API_KEY"] != "xxx" {
				t.Errorf("env not set correctly: %v", config["env"])
			}
			// Should NOT have command or args
			if _, has := config["command"]; has {
				t.Error("Remote config should not have command")
			}
			if _, has := config["args"]; has {
				t.Error("Remote config should not have args")
			}
		})
	}
}

func TestMCPHandler_Validate_MCPRemoteType(t *testing.T) {
	// A zip with type "mcp-remote" should validate correctly since it maps to TypeMCP
	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "test-remote"
version = "1.0.0"
type = "mcp-remote"
description = "Legacy mcp-remote"

[mcp]
command = "npx"
args = ["server"]
`,
	})

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test-remote", Version: "1.0.0", Type: asset.TypeMCP},
		MCP:   &metadata.MCPConfig{Command: "npx", Args: []string{"server"}},
	}

	handler := NewMCPHandler(meta)
	if err := handler.Validate(zipData); err != nil {
		t.Errorf("Validate should accept mcp-remote type: %v", err)
	}
}
