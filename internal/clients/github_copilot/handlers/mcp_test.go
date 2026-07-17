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

func remoteMCPMeta(name, transport, url string) *metadata.Metadata {
	return &metadata.Metadata{
		Asset: metadata.Asset{Name: name, Version: "1.0.0", Type: asset.TypeMCP},
		MCP: &metadata.MCPConfig{
			Transport: transport,
			URL:       url,
		},
	}
}

func remoteMCPZip(t *testing.T, name, transport, url string) []byte {
	t.Helper()
	return createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "` + name + `"
version = "1.0.0"
type = "mcp"

[mcp]
transport = "` + transport + `"
url = "` + url + `"
`,
	})
}

func readServerEntry(t *testing.T, targetBase, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(targetBase, "mcp.json"))
	if err != nil {
		t.Fatalf("Failed to read mcp.json: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("Failed to parse mcp.json: %v", err)
	}
	servers, ok := config["servers"].(map[string]any)
	if !ok {
		t.Fatalf("servers key not found in mcp.json, got: %s", data)
	}
	entry, ok := servers[name].(map[string]any)
	if !ok {
		t.Fatalf("server %q not found in mcp.json, got: %s", name, data)
	}
	return entry
}

func TestCopilotMCPHandler_Packaged_SubcommandArgs(t *testing.T) {
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
	entry := handler.generateMCPEntry(installPath)

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

func TestCopilotMCPHandler_Packaged_BareCommand(t *testing.T) {
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
	entry := handler.generateMCPEntry(installPath)

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

func TestCopilotMCPHandler_Remote_HTTPInstall(t *testing.T) {
	targetBase := t.TempDir()
	meta := remoteMCPMeta("remote-http", "http", "https://example.com/mcp")
	zipData := remoteMCPZip(t, "remote-http", "http", "https://example.com/mcp")

	handler := NewMCPHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	entry := readServerEntry(t, targetBase, "remote-http")
	if entry["type"] != "http" {
		t.Errorf("type = %v, want \"http\"", entry["type"])
	}
	if entry["url"] != "https://example.com/mcp" {
		t.Errorf("url = %v, want \"https://example.com/mcp\"", entry["url"])
	}
	if _, hasCommand := entry["command"]; hasCommand {
		t.Error("remote MCP entry should not have a command field")
	}

	// Remote servers run nothing locally — no extraction
	serverDir := filepath.Join(targetBase, DirMCPServers, "remote-http")
	if _, err := os.Stat(serverDir); !os.IsNotExist(err) {
		t.Errorf("remote MCP should not extract a server directory, found %s", serverDir)
	}
}

func TestCopilotMCPHandler_Remote_SSEInstall(t *testing.T) {
	targetBase := t.TempDir()
	meta := remoteMCPMeta("remote-sse", "sse", "https://example.com/mcp/sse")
	zipData := remoteMCPZip(t, "remote-sse", "sse", "https://example.com/mcp/sse")

	handler := NewMCPHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	entry := readServerEntry(t, targetBase, "remote-sse")
	if entry["type"] != "sse" {
		t.Errorf("type = %v, want \"sse\"", entry["type"])
	}
	if entry["url"] != "https://example.com/mcp/sse" {
		t.Errorf("url = %v, want \"https://example.com/mcp/sse\"", entry["url"])
	}
}

func TestCopilotMCPHandler_Remote_EnvNotWritten(t *testing.T) {
	// VS Code's mcp.json only supports env on stdio servers; remote servers
	// authenticate via headers, so env must not leak into the remote entry.
	targetBase := t.TempDir()
	meta := remoteMCPMeta("remote-env", "http", "https://example.com/mcp")
	meta.MCP.Env = map[string]string{"API_KEY": "secret"}
	zipData := remoteMCPZip(t, "remote-env", "http", "https://example.com/mcp")

	handler := NewMCPHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	entry := readServerEntry(t, targetBase, "remote-env")
	if _, hasEnv := entry["env"]; hasEnv {
		t.Errorf("remote MCP entry should not have an env field, got: %v", entry)
	}
}

func TestCopilotMCPHandler_Remote_Remove(t *testing.T) {
	targetBase := t.TempDir()
	meta := remoteMCPMeta("remote-rm", "http", "https://example.com/mcp")
	zipData := remoteMCPZip(t, "remote-rm", "http", "https://example.com/mcp")

	handler := NewMCPHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(targetBase, "mcp.json"))
	if err != nil {
		t.Fatalf("Failed to read mcp.json: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("Failed to parse mcp.json: %v", err)
	}
	servers, _ := config["servers"].(map[string]any)
	if _, exists := servers["remote-rm"]; exists {
		t.Errorf("mcp.json should not contain remote-rm after remove, got: %s", data)
	}
}

func TestCopilotMCPHandler_VerifyInstalled_Remote(t *testing.T) {
	targetBase := t.TempDir()
	meta := remoteMCPMeta("remote-verify", "http", "https://example.com/mcp")
	zipData := remoteMCPZip(t, "remote-verify", "http", "https://example.com/mcp")

	handler := NewMCPHandler(meta)

	installed, _ := handler.VerifyInstalled(targetBase)
	if installed {
		t.Error("should not be installed before install")
	}

	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	installed, msg := handler.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("should be installed after install, got: %s", msg)
	}

	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	installed, _ = handler.VerifyInstalled(targetBase)
	if installed {
		t.Error("should not be installed after remove")
	}
}

func TestCopilotMCPHandler_VerifyInstalled_ConfigOnly(t *testing.T) {
	// Config-only stdio servers (external commands like npx) also have no
	// extracted directory; verification falls back to the mcp.json entry.
	targetBase := t.TempDir()
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "config-only", Version: "1.0.0", Type: asset.TypeMCP},
		MCP: &metadata.MCPConfig{
			Command: "npx",
			Args:    []string{"-y", "some-server"},
		},
	}
	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "config-only"
version = "1.0.0"
type = "mcp"

[mcp]
command = "npx"
args = ["-y", "some-server"]
`,
	})

	handler := NewMCPHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	installed, msg := handler.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("config-only MCP should verify as installed, got: %s", msg)
	}
}
