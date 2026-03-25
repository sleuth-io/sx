package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/metadata"
)

func TestGetMCPConfigPath(t *testing.T) {
	// This test verifies the path structure is correct
	path, err := GetMCPConfigPath()
	if err != nil {
		t.Fatalf("GetMCPConfigPath() failed: %v", err)
	}

	// Path should always end with cline_mcp_settings.json
	if !strings.Contains(path, "cline_mcp_settings.json") {
		t.Errorf("Path should end with 'cline_mcp_settings.json', got: %s", path)
	}

	// Path should contain 'settings'
	if !strings.Contains(path, "settings") {
		t.Errorf("Path should contain 'settings', got: %s", path)
	}

	// Path should be either CLI format or VS Code format
	isCLIPath := strings.Contains(path, ".cline/data/settings")
	isVSCodePath := strings.Contains(path, "globalStorage") && strings.Contains(path, VSCodeExtensionID)

	if !isCLIPath && !isVSCodePath {
		t.Errorf("Path should be either CLI (~/.cline/data/settings/) or VS Code (globalStorage), got: %s", path)
	}
}

func TestReadMCPConfig_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "nonexistent.json")

	config, err := ReadMCPConfig(configPath)
	if err != nil {
		t.Fatalf("ReadMCPConfig should not error for non-existent file: %v", err)
	}

	if config.MCPServers == nil {
		t.Error("MCPServers should be initialized")
	}
	if len(config.MCPServers) != 0 {
		t.Errorf("MCPServers should be empty, got %d entries", len(config.MCPServers))
	}
}

func TestReadMCPConfig_ExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Write a test config
	testConfig := `{
		"mcpServers": {
			"test-server": {
				"command": "node",
				"args": ["server.js"]
			}
		}
	}`
	if err := os.WriteFile(configPath, []byte(testConfig), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := ReadMCPConfig(configPath)
	if err != nil {
		t.Fatalf("ReadMCPConfig failed: %v", err)
	}

	if _, exists := config.MCPServers["test-server"]; !exists {
		t.Error("Expected test-server to exist in config")
	}
}

func TestReadMCPConfig_JSONC_TrailingCommas(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Kiro/Cursor/VS Code editors allow trailing commas in JSON
	testConfig := `{
		"mcpServers": {
			"server-a": {
				"command": "node",
				"args": ["server.js"],
			},
			"server-b": {
				"command": "npx",
				"args": ["-y", "@example/mcp"],
			},
		}
	}`
	if err := os.WriteFile(configPath, []byte(testConfig), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := ReadMCPConfig(configPath)
	if err != nil {
		t.Fatalf("ReadMCPConfig should handle trailing commas: %v", err)
	}

	if len(config.MCPServers) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(config.MCPServers))
	}
	if _, exists := config.MCPServers["server-a"]; !exists {
		t.Error("Expected server-a to exist")
	}
	if _, exists := config.MCPServers["server-b"]; !exists {
		t.Error("Expected server-b to exist")
	}
}

func TestReadMCPConfig_JSONC_Comments(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// VS Code-based editors treat JSON as JSONC, allowing comments
	testConfig := `{
		// MCP server configuration
		"mcpServers": {
			"gitlab": {
				"command": "npx",
				"args": ["-y", "@zereight/mcp-gitlab"],
				"env": {
					"GITLAB_PERSONAL_ACCESS_TOKEN": "token",
					"GITLAB_API_URL": "https://gitlab.com" // API URL
				}
			}
			/* more servers can be added here */
		}
	}`
	if err := os.WriteFile(configPath, []byte(testConfig), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := ReadMCPConfig(configPath)
	if err != nil {
		t.Fatalf("ReadMCPConfig should handle comments: %v", err)
	}

	if _, exists := config.MCPServers["gitlab"]; !exists {
		t.Error("Expected gitlab to exist")
	}
}

func TestReadMCPConfig_JSONC_CommentsAndTrailingCommas(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Realistic file a user might create in Kiro/Cursor
	testConfig := `{
		"mcpServers": {
			"gitlab": {
				"command": "npx",
				"args": ["-y", "@zereight/mcp-gitlab"],
				"env": {
					"GITLAB_PERSONAL_ACCESS_TOKEN": "token",
					"GITLAB_API_URL": "https://gitlab.com", // API URL
					"GITLAB_READ_ONLY_MODE": "true",
				},
				"autoApprove": ["get_merge_request", "list_merge_requests"],
			},
		}
	}`
	if err := os.WriteFile(configPath, []byte(testConfig), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := ReadMCPConfig(configPath)
	if err != nil {
		t.Fatalf("ReadMCPConfig should handle JSONC: %v", err)
	}

	if _, exists := config.MCPServers["gitlab"]; !exists {
		t.Error("Expected gitlab to exist")
	}
}

func TestWriteMCPConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "subdir", "config.json")

	config := &MCPConfig{
		MCPServers: map[string]any{
			"my-server": map[string]any{
				"command": "python",
				"args":    []string{"-m", "server"},
			},
		},
	}

	if err := WriteMCPConfig(configPath, config); err != nil {
		t.Fatalf("WriteMCPConfig failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("Config file should exist")
	}

	// Read and verify content
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	var readConfig MCPConfig
	if err := json.Unmarshal(data, &readConfig); err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	if _, exists := readConfig.MCPServers["my-server"]; !exists {
		t.Error("Expected my-server to exist in written config")
	}
}

func TestAddMCPServer(t *testing.T) {
	// Skip this test if we can't create the config path
	// (would require mocking the entire path resolution)
	t.Skip("Skipping AddMCPServer test - requires mocking globalStorage path")
}

func TestRemoveMCPServer(t *testing.T) {
	// Skip this test if we can't create the config path
	t.Skip("Skipping RemoveMCPServer test - requires mocking globalStorage path")
}

func TestMCPHandler_GeneratePackagedMCPEntry(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name: "test-mcp",
		},
		MCP: &metadata.MCPConfig{
			Command: "node",
			Args:    []string{"server.js"},
			Env: map[string]string{
				"API_KEY": "test-key",
			},
		},
	}

	handler := NewMCPHandler(meta)
	entry := handler.generatePackagedMCPEntry("/path/to/server")

	// Verify command is resolved with full path
	command, ok := entry["command"].(string)
	if !ok {
		t.Fatal("Expected command to be a string")
	}
	if !strings.Contains(command, "/path/to/server") && !strings.Contains(command, "node") {
		t.Errorf("Expected command to reference server path, got: %s", command)
	}

	// Verify env is included
	env, ok := entry["env"].(map[string]string)
	if !ok {
		t.Fatal("Expected env to be present")
	}
	if env["API_KEY"] != "test-key" {
		t.Errorf("Expected API_KEY to be test-key, got: %s", env["API_KEY"])
	}
}

func TestMCPHandler_GenerateConfigOnlyMCPEntry(t *testing.T) {
	tests := []struct {
		name          string
		meta          *metadata.Metadata
		expectCommand bool
		expectURL     bool
		expectEnv     bool
	}{
		{
			name: "command-based MCP",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "test"},
				MCP: &metadata.MCPConfig{
					Command: "npx",
					Args:    []string{"-y", "@modelcontextprotocol/server"},
				},
			},
			expectCommand: true,
			expectURL:     false,
			expectEnv:     false,
		},
		{
			name: "command with env",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "test"},
				MCP: &metadata.MCPConfig{
					Command: "docker",
					Args:    []string{"run", "mcp-server"},
					Env: map[string]string{
						"DEBUG": "true",
					},
				},
			},
			expectCommand: true,
			expectURL:     false,
			expectEnv:     true,
		},
		{
			name: "remote URL-based MCP",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "test"},
				MCP: &metadata.MCPConfig{
					Transport: "sse", // IsRemote() checks Transport, not URL
					URL:       "https://api.example.com/mcp",
				},
			},
			expectCommand: false,
			expectURL:     true,
			expectEnv:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewMCPHandler(tt.meta)
			entry := handler.generateConfigOnlyMCPEntry()

			_, hasCommand := entry["command"]
			_, hasURL := entry["url"]
			_, hasEnv := entry["env"]

			if hasCommand != tt.expectCommand {
				t.Errorf("Expected command=%v, got %v", tt.expectCommand, hasCommand)
			}
			if hasURL != tt.expectURL {
				t.Errorf("Expected url=%v, got %v", tt.expectURL, hasURL)
			}
			if hasEnv != tt.expectEnv {
				t.Errorf("Expected env=%v, got %v", tt.expectEnv, hasEnv)
			}
		})
	}
}

func TestMCPHandler_VerifyInstalled_ConfigOnly(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a mock MCP config file
	settingsDir := filepath.Join(tmpDir, "settings")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("Failed to create settings dir: %v", err)
	}

	configPath := filepath.Join(settingsDir, "cline_mcp_settings.json")
	configContent := `{
		"mcpServers": {
			"test-mcp": {
				"command": "node",
				"args": ["server.js"]
			}
		}
	}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Note: VerifyInstalled uses GetMCPConfigPath() which returns the real path,
	// so we can't easily test it without mocking. This test is skipped.
	t.Skip("Skipping VerifyInstalled test - requires mocking globalStorage path")
}

func TestMCPHandler_Install_CreatesConfig(t *testing.T) {
	// This test would require mocking the MCP config path
	// Skip for now as it's covered by integration tests
	t.Skip("Skipping Install test - covered by integration tests")
}

func TestMCPHandler_Remove_DeletesEntry(t *testing.T) {
	// This test would require mocking the MCP config path
	t.Skip("Skipping Remove test - covered by integration tests")
}

func TestMCPConfig_JSONFormat(t *testing.T) {
	config := &MCPConfig{
		MCPServers: map[string]any{
			"server1": map[string]any{
				"command": "node",
				"args":    []any{"server.js"},
			},
			"server2": map[string]any{
				"url": "https://api.example.com",
				"env": map[string]any{
					"API_KEY": "secret",
				},
			},
		},
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	// Verify it can be unmarshaled
	var readConfig MCPConfig
	if err := json.Unmarshal(data, &readConfig); err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}

	if len(readConfig.MCPServers) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(readConfig.MCPServers))
	}
}
