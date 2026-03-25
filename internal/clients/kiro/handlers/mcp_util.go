package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// MCPConfig represents Kiro's mcp.json structure
type MCPConfig struct {
	MCPServers map[string]any `json:"mcpServers"`
}

// ReadMCPConfig reads Kiro's mcp.json file
func ReadMCPConfig(path string) (*MCPConfig, error) {
	config := &MCPConfig{
		MCPServers: make(map[string]any),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil // Return empty config
		}
		return nil, err
	}

	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}

	return config, nil
}

// WriteMCPConfig writes Kiro's mcp.json file
func WriteMCPConfig(path string, config *MCPConfig) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// AddMCPServer adds an MCP server to Kiro's configuration
func AddMCPServer(targetBase, name string, serverConfig map[string]any) error {
	mcpConfigPath := filepath.Join(targetBase, DirSettings, "mcp.json")

	config, err := ReadMCPConfig(mcpConfigPath)
	if err != nil {
		return err
	}

	if config.MCPServers == nil {
		config.MCPServers = make(map[string]any)
	}

	config.MCPServers[name] = serverConfig

	return WriteMCPConfig(mcpConfigPath, config)
}

// RemoveMCPServer removes an MCP server from Kiro's configuration
func RemoveMCPServer(targetBase, name string) error {
	mcpConfigPath := filepath.Join(targetBase, DirSettings, "mcp.json")

	config, err := ReadMCPConfig(mcpConfigPath)
	if err != nil {
		return err
	}

	delete(config.MCPServers, name)

	return WriteMCPConfig(mcpConfigPath, config)
}
