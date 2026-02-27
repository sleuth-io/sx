package handlers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

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
