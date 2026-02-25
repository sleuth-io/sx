package handlers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindJetBrainsConfigDirs(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)

	// Initially no JetBrains IDEs
	products, err := FindJetBrainsConfigDirs()
	if err != nil {
		t.Fatalf("FindJetBrainsConfigDirs() error: %v", err)
	}
	if len(products) != 0 {
		t.Errorf("FindJetBrainsConfigDirs() returned %d products, want 0", len(products))
	}

	// Create JetBrains config directory
	jetbrainsBase := filepath.Join(tempDir, ".config/JetBrains")
	if err := os.MkdirAll(jetbrainsBase, 0755); err != nil {
		t.Fatalf("Failed to create JetBrains dir: %v", err)
	}

	// Still empty (no product directories)
	products, err = FindJetBrainsConfigDirs()
	if err != nil {
		t.Fatalf("FindJetBrainsConfigDirs() error: %v", err)
	}
	if len(products) != 0 {
		t.Errorf("FindJetBrainsConfigDirs() returned %d products, want 0", len(products))
	}

	// Create IntelliJ directory
	intellijDir := filepath.Join(jetbrainsBase, "IntelliJIdea2025.1")
	if err := os.MkdirAll(intellijDir, 0755); err != nil {
		t.Fatalf("Failed to create IntelliJ dir: %v", err)
	}

	products, err = FindJetBrainsConfigDirs()
	if err != nil {
		t.Fatalf("FindJetBrainsConfigDirs() error: %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("FindJetBrainsConfigDirs() returned %d products, want 1", len(products))
	}
	if products[0].Name != "IntelliJIdea" {
		t.Errorf("Product name = %q, want %q", products[0].Name, "IntelliJIdea")
	}
	if products[0].Version != "2025.1" {
		t.Errorf("Product version = %q, want %q", products[0].Version, "2025.1")
	}
}

func TestFindJetBrainsConfigDirs_MultipleProducts(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)

	jetbrainsBase := filepath.Join(tempDir, ".config/JetBrains")

	// Create multiple products
	products := []string{
		"IntelliJIdea2025.1",
		"IntelliJIdea2024.3",
		"PyCharm2025.1",
		"GoLand2024.2",
		"AndroidStudio2024.2",
	}

	for _, p := range products {
		dir := filepath.Join(jetbrainsBase, p)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create %s dir: %v", p, err)
		}
	}

	found, err := FindJetBrainsConfigDirs()
	if err != nil {
		t.Fatalf("FindJetBrainsConfigDirs() error: %v", err)
	}
	if len(found) != 5 {
		t.Errorf("FindJetBrainsConfigDirs() returned %d products, want 5", len(found))
	}

	// Check sorted by version descending
	if found[0].Version < found[len(found)-1].Version {
		t.Error("Products should be sorted by version descending")
	}
}

func TestParseProductVersion(t *testing.T) {
	tests := []struct {
		input       string
		wantProduct string
		wantVersion string
	}{
		{"IntelliJIdea2025.1", "IntelliJIdea", "2025.1"},
		{"PyCharm2024.3", "PyCharm", "2024.3"},
		{"GoLand2024.2", "GoLand", "2024.2"},
		{"AndroidStudio2024.2", "AndroidStudio", "2024.2"},
		{"WebStorm2025.1.2", "WebStorm", "2025.1.2"},
		{"NoVersion", "NoVersion", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			product, version := parseProductVersion(tt.input)
			if product != tt.wantProduct {
				t.Errorf("parseProductVersion(%q) product = %q, want %q", tt.input, product, tt.wantProduct)
			}
			if version != tt.wantVersion {
				t.Errorf("parseProductVersion(%q) version = %q, want %q", tt.input, version, tt.wantVersion)
			}
		})
	}
}

func TestIsJetBrainsInstalled(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)

	// Initially not installed
	if IsJetBrainsInstalled() {
		t.Error("IsJetBrainsInstalled() = true when no JetBrains IDEs exist")
	}

	// Create JetBrains directory without mcp.json
	jetbrainsDir := filepath.Join(tempDir, ".config/JetBrains/IntelliJIdea2025.1")
	if err := os.MkdirAll(jetbrainsDir, 0755); err != nil {
		t.Fatalf("Failed to create JetBrains dir: %v", err)
	}

	// Still not installed (no mcp.json or Gemini plugin)
	if IsJetBrainsInstalled() {
		t.Error("IsJetBrainsInstalled() = true when no mcp.json exists")
	}

	// Create mcp.json
	mcpPath := filepath.Join(jetbrainsDir, "mcp.json")
	if err := os.WriteFile(mcpPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("Failed to create mcp.json: %v", err)
	}

	// Now should be installed
	if !IsJetBrainsInstalled() {
		t.Error("IsJetBrainsInstalled() = false when mcp.json exists")
	}
}

func TestIsJetBrainsInstalled_GeminiPlugin(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)

	// Create JetBrains directory with Gemini plugin folder
	jetbrainsDir := filepath.Join(tempDir, ".config/JetBrains/IntelliJIdea2025.1")
	pluginsDir := filepath.Join(jetbrainsDir, "plugins/gemini-code-assist")
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		t.Fatalf("Failed to create plugins dir: %v", err)
	}

	// Should be installed (detected via plugins directory)
	if !IsJetBrainsInstalled() {
		t.Error("IsJetBrainsInstalled() = false when Gemini plugin directory exists")
	}
}

func TestAddRemoveJetBrainsMCPServer(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)

	// Create JetBrains directory
	jetbrainsDir := filepath.Join(tempDir, ".config/JetBrains/IntelliJIdea2025.1")
	if err := os.MkdirAll(jetbrainsDir, 0755); err != nil {
		t.Fatalf("Failed to create JetBrains dir: %v", err)
	}

	// Add MCP server
	server := JetBrainsMCPServer{
		Command: "npx",
		Args:    []string{"-y", "test-server"},
	}
	if err := AddJetBrainsMCPServer("test-server", server); err != nil {
		t.Fatalf("AddJetBrainsMCPServer() error: %v", err)
	}

	// Verify it exists
	if !HasJetBrainsMCPServer("test-server") {
		t.Error("HasJetBrainsMCPServer() = false after adding server")
	}

	// Remove it
	if err := RemoveJetBrainsMCPServer("test-server"); err != nil {
		t.Fatalf("RemoveJetBrainsMCPServer() error: %v", err)
	}

	// Verify it's gone
	if HasJetBrainsMCPServer("test-server") {
		t.Error("HasJetBrainsMCPServer() = true after removing server")
	}
}
