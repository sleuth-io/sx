package handlers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadMCPConfig_ValidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "mcp.json")

	content := `{
		"mcpServers": {
			"my-server": {
				"command": "node",
				"args": ["server.js"]
			}
		}
	}`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	config, err := ReadMCPConfig(configPath)
	if err != nil {
		t.Fatalf("ReadMCPConfig failed: %v", err)
	}

	if _, exists := config.MCPServers["my-server"]; !exists {
		t.Error("Expected my-server to exist")
	}
}

func TestReadMCPConfig_NonExistent(t *testing.T) {
	config, err := ReadMCPConfig("/nonexistent/path/mcp.json")
	if err != nil {
		t.Fatalf("ReadMCPConfig should not error for non-existent file: %v", err)
	}
	if len(config.MCPServers) != 0 {
		t.Errorf("Expected empty MCPServers, got %d entries", len(config.MCPServers))
	}
}

// TestReadMCPConfig_JSONC_TrailingComma reproduces the reported bug:
//
//	"failed to register MCP server: invalid character '}' looking for beginning of object key string"
//
// This happens when users edit mcp.json in Kiro (a VS Code fork) which allows trailing commas.
func TestReadMCPConfig_JSONC_TrailingComma(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "mcp.json")

	// This is the exact pattern that triggers the bug: trailing commas
	// produce "invalid character '}' looking for beginning of object key string"
	// with Go's strict json.Unmarshal.
	content := `{
		"mcpServers": {
			"my-server": {
				"command": "node",
				"args": ["server.js"],
			},
		}
	}`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	config, err := ReadMCPConfig(configPath)
	if err != nil {
		t.Fatalf("ReadMCPConfig should handle trailing commas: %v", err)
	}

	if _, exists := config.MCPServers["my-server"]; !exists {
		t.Error("Expected my-server to exist")
	}
}

// TestReadMCPConfig_JSONC_Comments verifies that comments (another JSONC feature)
// are handled, since Kiro's editor supports them.
func TestReadMCPConfig_JSONC_Comments(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "mcp.json")

	content := `{
		// MCP servers
		"mcpServers": {
			"gitlab": {
				"command": "npx",
				"args": ["-y", "@zereight/mcp-gitlab"],
				"env": {
					"GITLAB_API_URL": "https://gitlab.com" // inline comment
				}
			}
			/* block comment */
		}
	}`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	config, err := ReadMCPConfig(configPath)
	if err != nil {
		t.Fatalf("ReadMCPConfig should handle comments: %v", err)
	}

	if _, exists := config.MCPServers["gitlab"]; !exists {
		t.Error("Expected gitlab to exist")
	}
}

// TestAddMCPServer_PreExistingJSONC verifies that AddMCPServer works when the
// existing mcp.json contains JSONC features — the exact scenario from the bug report
// where a user has no MCP servers but Kiro wrote JSONC to the file.
func TestAddMCPServer_PreExistingJSONC(t *testing.T) {
	targetBase := t.TempDir()
	settingsDir := filepath.Join(targetBase, DirSettings)
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("Failed to create settings dir: %v", err)
	}

	// Pre-populate with JSONC content (trailing comma + comment)
	content := `{
		// User's existing config
		"mcpServers": {
			"existing": {
				"command": "npx",
				"args": ["-y", "@example/server"],
			},
		}
	}`
	if err := os.WriteFile(filepath.Join(settingsDir, "mcp.json"), []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// This is what sx does when registering the skills MCP server
	err := AddMCPServer(targetBase, "skills", map[string]any{
		"command": "/usr/local/bin/sx",
		"args":    []string{"serve"},
	})
	if err != nil {
		t.Fatalf("AddMCPServer should handle pre-existing JSONC: %v", err)
	}

	// Verify both servers exist
	config, err := ReadMCPConfig(filepath.Join(settingsDir, "mcp.json"))
	if err != nil {
		t.Fatalf("ReadMCPConfig failed: %v", err)
	}

	if _, exists := config.MCPServers["existing"]; !exists {
		t.Error("Expected existing server to be preserved")
	}
	if _, exists := config.MCPServers["skills"]; !exists {
		t.Error("Expected skills server to be added")
	}
}
